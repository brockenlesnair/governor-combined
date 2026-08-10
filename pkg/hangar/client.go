// Package hangar provides a REST client for the Hangar governance API.
//
// It implements retry with exponential backoff + jitter, circuit breaker,
// idempotency key injection, and request/response logging with secret redaction.
package hangar

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Client is the Hangar REST API client.
type Client struct {
	config     Config
	httpClient *http.Client
	logger     *slog.Logger
	breaker    *circuitBreaker

	// secretPatterns is a compiled list of regexes for redacting secrets in logs.
	secretPatterns []*regexp.Regexp
}

// NewClient creates a new Hangar client. Call config.Validate() first.
func NewClient(config Config, logger *slog.Logger) (*Client, error) {
	if err := config.validateInternal(); err != nil {
		return nil, err
	}

	if logger == nil {
		logger = slog.Default()
	}

	c := &Client{
		config: config,
		httpClient: &http.Client{
			Timeout: config.RequestTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		logger:  logger.With("component", "hangar-client"),
		breaker: newCircuitBreaker(config.CircuitBreaker),
		secretPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)(bearer\s+)(\S{8})\S*`),
			regexp.MustCompile(`(?i)(api[_-]?key[=:]\s*["']?)(\S{8})\S*`),
			regexp.MustCompile(`(?i)(authorization["']?\s*[:=]\s*["']?)(\S{8})\S*`),
		},
	}

	return c, nil
}

// SetHTTPClient overrides the default http.Client (useful for testing).
func (c *Client) SetHTTPClient(hc *http.Client) {
	c.httpClient = hc
}

// Close shuts down the client, draining any idle connections.
func (c *Client) Close() {
	c.httpClient.CloseIdleConnections()
}

// ─── Idempotency Keys ────────────────────────────────────────────────

// idempotencyKey generates a cryptographically random idempotency key.
func idempotencyKey() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp — should never happen in practice
		return fmt.Sprintf("idem-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// ─── Circuit Breaker ─────────────────────────────────────────────────

type breakerState int

const (
	breakerClosed   breakerState = iota // normal operation
	breakerOpen                         // failing fast
	breakerHalfOpen                     // allowing probe requests
)

type circuitBreaker struct {
	config      CircuitBreakerConfig
	state       breakerState
	failures    int
	successes   int
	lastFailure time.Time
	mu          sync.Mutex
}

func newCircuitBreaker(config CircuitBreakerConfig) *circuitBreaker {
	return &circuitBreaker{
		config: config,
		state:  breakerClosed,
	}
}

// allow returns true if the request should proceed.
func (cb *circuitBreaker) allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case breakerClosed:
		return true
	case breakerOpen:
		if time.Since(cb.lastFailure) >= cb.config.RecoveryTime {
			cb.state = breakerHalfOpen
			cb.successes = 0
			return true
		}
		return false
	case breakerHalfOpen:
		return cb.successes < cb.config.HalfOpenMax
	}
	return false
}

// recordSuccess records a successful request.
func (cb *circuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case breakerClosed:
		cb.failures = 0
	case breakerHalfOpen:
		cb.successes++
		if cb.successes >= cb.config.HalfOpenMax {
			cb.state = breakerClosed
			cb.failures = 0
		}
	}
}

// recordFailure records a failed request.
func (cb *circuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case breakerClosed:
		cb.failures++
		if cb.failures >= cb.config.Threshold {
			cb.state = breakerOpen
			cb.lastFailure = time.Now()
		}
	case breakerHalfOpen:
		cb.state = breakerOpen
		cb.lastFailure = time.Now()
	}
}

// stateName returns a human-readable name for the breaker state.
func (cb *circuitBreaker) stateName() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case breakerClosed:
		return "closed"
	case breakerOpen:
		return "open"
	case breakerHalfOpen:
		return "half-open"
	}
	return "unknown"
}

// ─── Request / Response Logging ──────────────────────────────────────

// redactSecrets masks sensitive values in strings.
func (c *Client) redactSecrets(s string) string {
	for _, re := range c.secretPatterns {
		s = re.ReplaceAllString(s, "${1}[REDACTED]")
	}
	return s
}

// ─── Core HTTP Execution ─────────────────────────────────────────────

// doRequest executes an HTTP request with retry, circuit breaker, and logging.
// Mutating operations should pass an idempotencyKey; pass "" for GET/reads.
func (c *Client) doRequest(ctx context.Context, method, path string, body any, idemKey string) (*http.Response, []byte, error) {
	return c.doRequestWithETag(ctx, method, path, body, idemKey, "")
}

// doRequestWithETag executes an HTTP request with retry, circuit breaker,
// idempotency keys, and optional If-Match ETag for optimistic locking.
func (c *Client) doRequestWithETag(ctx context.Context, method, path string, body any, idemKey, etag string) (*http.Response, []byte, error) {
	var lastErr error

	for attempt := 0; attempt <= c.config.Retry.MaxAttempts; attempt++ {
		if attempt > 0 {
			backoff := c.computeBackoff(attempt)
			c.logger.Warn("retrying request",
				"method", method,
				"path", path,
				"attempt", attempt+1,
				"backoff", backoff,
				"err", lastErr,
			)
			select {
			case <-ctx.Done():
				return nil, nil, fmt.Errorf("context cancelled during retry backoff: %w", ctx.Err())
			case <-time.After(backoff):
			}
		}

		// Circuit breaker check
		if !c.breaker.allow() {
			err := fmt.Errorf("circuit breaker open (state=%s), refusing request", c.breaker.stateName())
			c.logger.Error("circuit breaker rejected request",
				"method", method,
				"path", path,
				"state", c.breaker.stateName(),
			)
			return nil, nil, err
		}

		resp, data, err := c.executeRequest(ctx, method, path, body, idemKey, etag)
		if err != nil {
			c.breaker.recordFailure()
			lastErr = err
			continue
		}

		// Check if status code warrants retry
		if c.isRetryable(resp.StatusCode) {
			c.breaker.recordFailure()
			lastErr = fmt.Errorf("retryable status %d", resp.StatusCode)

			// Handle 429 Rate Limiting with Retry-After
			if resp.StatusCode == 429 {
				retryAfter := c.parseRetryAfter(resp)
				c.logger.Warn("rate limited by Hangar",
					"method", method,
					"path", path,
					"retry_after", retryAfter,
				)
				select {
				case <-ctx.Done():
					resp.Body.Close()
					return nil, nil, ctx.Err()
				case <-time.After(retryAfter):
				}
				resp.Body.Close()
				continue
			}

			resp.Body.Close()
			continue
		}

		// Success or non-retryable error
		c.breaker.recordSuccess()
		return resp, data, nil
	}

	return nil, nil, fmt.Errorf("exhausted %d retries for %s %s: %w",
		c.config.Retry.MaxAttempts, method, path, lastErr)
}

func (c *Client) executeRequest(ctx context.Context, method, path string, body any, idemKey, etag string) (*http.Response, []byte, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	url := strings.TrimRight(c.config.BaseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}

	// Set headers
	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "governor-hangar-client/1.0")

	if idemKey != "" {
		req.Header.Set("X-Idempotency-Key", idemKey)
	}
	if etag != "" {
		req.Header.Set("If-Match", etag)
	}

	// Log request (with redacted secrets)
	c.logger.Debug("request",
		"method", method,
		"path", path,
		"idempotency_key", idemKey,
		"etag", etag,
	)

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("http request %s %s: %w", method, path, err)
	}

	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("read response body: %w", err)
	}

	elapsed := time.Since(start)
	logLevel := slog.LevelDebug
	if resp.StatusCode >= 400 {
		logLevel = slog.LevelWarn
	}

	c.logger.Log(ctx, logLevel, "response",
		"method", method,
		"path", path,
		"status", resp.StatusCode,
		"bytes", len(data),
		"duration", elapsed,
	)

	return resp, data, nil
}

// computeBackoff calculates exponential backoff with jitter.
func (c *Client) computeBackoff(attempt int) time.Duration {
	backoff := float64(c.config.Retry.InitialBackoff) * math.Pow(c.config.Retry.BackoffFactor, float64(attempt-1))
	if backoff > float64(c.config.Retry.MaxBackoff) {
		backoff = float64(c.config.Retry.MaxBackoff)
	}

	// Add jitter: backoff * (1 ± jitterFraction)
	jitter := backoff * c.config.Retry.JitterFraction
	if jitter > 0 {
		jitterBytes := make([]byte, 8)
		if _, err := rand.Read(jitterBytes); err == nil {
			// Map to [-1, 1] range
			frac := float64(uint64(jitterBytes[0])<<8|uint64(jitterBytes[1])) / float64(1<<16) //nolint:gosec
			offset := (frac*2 - 1) * jitter
			backoff += offset
		}
	}

	if backoff < 0 {
		backoff = 0
	}
	return time.Duration(backoff)
}

// isRetryable checks if a status code should trigger a retry.
func (c *Client) isRetryable(status int) bool {
	for _, s := range c.config.Retry.RetryableStatus {
		if s == status {
			return true
		}
	}
	return false
}

// parseRetryAfter extracts the Retry-After duration from response headers.
// Falls back to the configured default if the header is missing or unparseable.
func (c *Client) parseRetryAfter(resp *http.Response) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		// Try parsing as seconds (integer)
		var seconds int
		if _, err := fmt.Sscanf(v, "%d", &seconds); err == nil && seconds > 0 {
			d := time.Duration(seconds) * time.Second
			// Cap at max backoff
			if d > c.config.Retry.MaxBackoff {
				return c.config.Retry.MaxBackoff
			}
			return d
		}
	}
	return c.config.Poll.RetryAfter
}

// ─── Config Validation ───────────────────────────────────────────────

func (c *Config) validateInternal() error {
	if c.BaseURL == "" {
		return fmt.Errorf("hangar: base_url is required")
	}
	if c.APIKey == "" {
		c.APIKey = os.Getenv("HANGAR_API_KEY")
	}
	if c.APIKey == "" {
		return fmt.Errorf("hangar: api_key is required (set in config or HANGAR_API_KEY env)")
	}
	c.applyDefaults()
	return nil
}
