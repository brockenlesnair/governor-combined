package webhook

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ErrorCode represents a webhook error code type.
type ErrorCode string

const (
	ErrCodeWebhookFailed ErrorCode = "WEBHOOK_FAILED"
	ErrCodeTargetBlocked ErrorCode = "TARGET_BLOCKED"
	ErrCodeCircuitOpen   ErrorCode = "CIRCUIT_OPEN"
	ErrCodeRateLimited   ErrorCode = "RATE_LIMITED"
)

// WebhookError represents a webhook-specific error.
type WebhookError struct {
	Code    ErrorCode
	Message string
	Cause   error
}

func (e *WebhookError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *WebhookError) Unwrap() error { return e.Cause }

// Config controls webhook behavior.
type Config struct {
	AllowList       []string      `json:"allow_list"`
	Timeout         time.Duration `json:"timeout"`
	MaxRetries      int           `json:"max_retries"`
	InitialBackoff  time.Duration `json:"initial_backoff"`
	MaxBackoff      time.Duration `json:"max_backoff"`
	Multiplier      float64       `json:"multiplier"`
	CircuitThreshold int          `json:"circuit_threshold"`
	CircuitTimeout  time.Duration `json:"circuit_timeout"`
	Secret          string        `json:"secret"`
	SignAlgo        string        `json:"sign_algo"`
	RateLimit       int           `json:"rate_limit"`
	RateBurst       int           `json:"rate_burst"`
}

// DeliveryRequest represents a single webhook delivery attempt.
type DeliveryRequest struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    []byte            `json:"body"`
	Event   string            `json:"event"`
}

// DeliveryResult contains the outcome of a webhook delivery.
type DeliveryResult struct {
	URL         string        `json:"url"`
	StatusCode  int           `json:"status_code"`
	Success     bool          `json:"success"`
	Attempts    int           `json:"attempts"`
	Duration    time.Duration `json:"duration"`
	Error       string        `json:"error,omitempty"`
	CircuitOpen bool          `json:"circuit_open"`
}

// Client sends outbound webhook notifications.
type Client struct {
	cfg     *Config
	client  *http.Client
	breaker *CircuitBreaker
	retry   RetryPolicy
	signer  *Signer
	limiter *TokenBucket
	logger  *slog.Logger
	mu      sync.RWMutex
}

// NewClient creates a webhook client.
func NewClient(cfg *Config, logger *slog.Logger) *Client {
	if cfg == nil {
		cfg = &Config{}
	}
	cfg.applyDefaults()

	var signer *Signer
	if cfg.Secret != "" {
		signer = NewSigner(cfg.Secret, cfg.SignAlgo)
	}

	return &Client{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
		breaker: NewCircuitBreaker(cfg.CircuitThreshold, cfg.CircuitTimeout),
		retry: RetryPolicy{
			MaxRetries:     cfg.MaxRetries,
			InitialBackoff: cfg.InitialBackoff,
			MaxBackoff:     cfg.MaxBackoff,
			Multiplier:     cfg.Multiplier,
		},
		signer:  signer,
		limiter: NewTokenBucket(cfg.RateLimit, cfg.RateBurst),
		logger:  logger.With("component", "webhook"),
	}
}

// Send delivers a webhook with retry and circuit breaker.
func (c *Client) Send(ctx context.Context, req *DeliveryRequest) (*DeliveryResult, error) {
	if !c.isAllowed(req.URL) {
		return nil, &WebhookError{
			Code:    ErrCodeTargetBlocked,
			Message: fmt.Sprintf("target URL not in allow-list: %s", req.URL),
		}
	}

	if !c.breaker.Allow() {
		return &DeliveryResult{
			URL:         req.URL,
			Success:     false,
			CircuitOpen: true,
			Error:       "circuit breaker is open",
		}, nil
	}

	c.limiter.Wait(ctx)

	method := req.Method
	if method == "" {
		method = http.MethodPost
	}

	result := &DeliveryResult{
		URL: req.URL,
	}

	var lastErr error
	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := c.retry.Backoff(attempt)
			c.logger.Info("retrying webhook",
				"url", req.URL,
				"attempt", attempt,
				"delay", delay)

			select {
			case <-ctx.Done():
				result.Error = ctx.Err().Error()
				return result, ctx.Err()
			case <-time.After(delay):
			}
		}

		headers := make(map[string]string)
		for k, v := range req.Headers {
			headers[k] = v
		}
		if c.signer != nil && req.Body != nil {
			sig := c.signer.Sign(req.Body)
			headers[c.signer.HeaderName()] = sig
		}

		resp, err := c.executeRequest(ctx, method, req.URL, headers, req.Body)
		if err != nil {
			lastErr = err
			c.breaker.RecordFailure()
			result.Attempts++
			continue
		}

		result.StatusCode = resp.StatusCode
		result.Attempts++

		if retryableStatus(resp.StatusCode) && c.retry.ShouldRetry(attempt, resp.StatusCode) {
			c.breaker.RecordFailure()
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			c.breaker.RecordSuccess()
			result.Success = true
		} else {
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			result.Error = lastErr.Error()
		}

		break
	}

	if !result.Success && lastErr != nil {
		result.Error = lastErr.Error()
	}

	return result, nil
}

// SendAsync delivers a webhook in the background.
func (c *Client) SendAsync(ctx context.Context, req *DeliveryRequest) <-chan *DeliveryResult {
	ch := make(chan *DeliveryResult, 1)
	go func() {
		defer close(ch)
		result, err := c.Send(ctx, req)
		if err != nil && result == nil {
			result = &DeliveryResult{
				URL:   req.URL,
				Error: err.Error(),
			}
		}
		ch <- result
	}()
	return ch
}

func (c *Client) executeRequest(ctx context.Context, method, url string, headers map[string]string, body []byte) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}

	return resp, nil
}

func (c *Client) isAllowed(url string) bool {
	if len(c.cfg.AllowList) == 0 {
		return true
	}
	for _, allowed := range c.cfg.AllowList {
		if strings.HasPrefix(url, allowed) {
			return true
		}
	}
	return false
}

// Health returns the circuit breaker state.
func (c *Client) Health() CircuitState {
	return c.breaker.State()
}

func (cfg *Config) applyDefaults() {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.InitialBackoff == 0 {
		cfg.InitialBackoff = 100 * time.Millisecond
	}
	if cfg.MaxBackoff == 0 {
		cfg.MaxBackoff = 10 * time.Second
	}
	if cfg.Multiplier == 0 {
		cfg.Multiplier = 2.0
	}
	if cfg.CircuitThreshold == 0 {
		cfg.CircuitThreshold = 5
	}
	if cfg.CircuitTimeout == 0 {
		cfg.CircuitTimeout = 30 * time.Second
	}
	if cfg.RateLimit == 0 {
		cfg.RateLimit = 10
	}
	if cfg.RateBurst == 0 {
		cfg.RateBurst = 20
	}
}

// TokenBucket implements a simple rate limiter.
type TokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	max      float64
	rate     float64
	lastTime time.Time
}

// NewTokenBucket creates a rate limiter.
func NewTokenBucket(rate, burst int) *TokenBucket {
	return &TokenBucket{
		tokens:   float64(burst),
		max:      float64(burst),
		rate:     float64(rate),
		lastTime: time.Now(),
	}
}

// Wait blocks until a token is available or context is cancelled.
func (tb *TokenBucket) Wait(ctx context.Context) {
	for {
		if tb.tryAcquire() {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (tb *TokenBucket) tryAcquire() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastTime).Seconds()
	tb.lastTime = now

	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.max {
		tb.tokens = tb.max
	}

	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		return true
	}
	return false
}
