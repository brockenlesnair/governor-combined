package httpproxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ErrorCode represents a proxy error code type.
type ErrorCode string

const (
	ErrCodeProxyFailed   ErrorCode = "PROXY_FAILED"
	ErrCodeCircuitOpen   ErrorCode = "CIRCUIT_OPEN"
	ErrCodeUpstreamError ErrorCode = "UPSTREAM_ERROR"
	ErrCodeTargetInvalid ErrorCode = "TARGET_INVALID"
)

// ProxyError represents a proxy-specific error.
type ProxyError struct {
	Code    ErrorCode
	Message string
	Cause   error
}

func (e *ProxyError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *ProxyError) Unwrap() error { return e.Cause }

// CircuitState represents the circuit breaker state.
type CircuitState int

const (
	CircuitClosed   CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker tracks failures and controls request flow.
type CircuitBreaker struct {
	mu              sync.Mutex
	state           CircuitState
	failureCount    int
	successCount    int
	lastFailureTime time.Time
	threshold       int
	timeout         time.Duration
	halfOpenMax     int
}

// NewCircuitBreaker creates a breaker with the given thresholds.
func NewCircuitBreaker(threshold int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:       CircuitClosed,
		threshold:   threshold,
		timeout:     timeout,
		halfOpenMax: 3,
	}
}

// Allow returns true if the request should proceed.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		if time.Since(cb.lastFailureTime) > cb.timeout {
			cb.state = CircuitHalfOpen
			cb.successCount = 0
			return true
		}
		return false
	case CircuitHalfOpen:
		return true
	default:
		return false
	}
}

// RecordSuccess records a successful request.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitHalfOpen {
		cb.successCount++
		if cb.successCount >= cb.halfOpenMax {
			cb.state = CircuitClosed
			cb.failureCount = 0
		}
	} else {
		cb.failureCount = 0
	}
}

// RecordFailure records a failed request.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailureTime = time.Now()

	if cb.state == CircuitClosed && cb.failureCount >= cb.threshold {
		cb.state = CircuitOpen
	} else if cb.state == CircuitHalfOpen {
		cb.state = CircuitOpen
	}
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// CircuitOpenError is returned when the circuit breaker is open.
type CircuitOpenError struct {
	State   CircuitState
	Message string
}

func (e *CircuitOpenError) Error() string { return e.Message }

// Config controls proxy behavior.
type Config struct {
	Target           string            `json:"target"`
	Timeout          time.Duration     `json:"timeout"`
	CircuitThreshold int               `json:"circuit_threshold"`
	CircuitTimeout   time.Duration     `json:"circuit_timeout"`
	RewriteRules     []RewriteRule     `json:"rewrite_rules"`
	ForwardAuth      bool              `json:"forward_auth"`
	StripHeaders     []string          `json:"strip_headers"`
	AddHeaders       map[string]string `json:"add_headers"`
}

// ProxyRequest represents an incoming request to be proxied.
type ProxyRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    []byte            `json:"body"`
}

// ProxyResponse is the upstream response.
type ProxyResponse struct {
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers"`
	Body       []byte            `json:"body"`
	Duration   time.Duration     `json:"duration"`
}

// Proxy is a reverse proxy for outbound HTTP requests.
type Proxy struct {
	cfg       *Config
	target    *url.URL
	transport *CircuitTransport
	rewriter  *HeaderRewriter
	logger    *slog.Logger
}

// NewProxy creates a proxy with the given configuration.
func NewProxy(cfg *Config, logger *slog.Logger) *Proxy {
	if cfg == nil {
		cfg = &Config{}
	}
	cfg.applyDefaults()

	target, _ := url.Parse(cfg.Target)

	transport := NewCircuitTransport(cfg.CircuitThreshold, cfg.CircuitTimeout)

	rewriter, _ := NewHeaderRewriter(cfg.RewriteRules)

	return &Proxy{
		cfg:       cfg,
		target:    target,
		transport: transport,
		rewriter:  rewriter,
		logger:    logger.With("component", "httpproxy"),
	}
}

// Proxy executes the request against the upstream target.
func (p *Proxy) Proxy(ctx context.Context, req *ProxyRequest) (*ProxyResponse, error) {
	if !p.transport.breaker.Allow() {
		return nil, &ProxyError{
			Code:    ErrCodeCircuitOpen,
			Message: "circuit breaker is open",
		}
	}

	upstreamURL := p.target.ResolveReference(&url.URL{Path: req.URL})
	var bodyReader io.Reader
	if req.Body != nil {
		bodyReader = bytes.NewReader(req.Body)
	}

	upstreamReq, err := http.NewRequestWithContext(ctx, req.Method, upstreamURL.String(), bodyReader)
	if err != nil {
		return nil, &ProxyError{
			Code:    ErrCodeTargetInvalid,
			Message: fmt.Sprintf("create upstream request: %v", err),
			Cause:   err,
		}
	}

	for k, v := range req.Headers {
		upstreamReq.Header.Set(k, v)
	}

	if p.rewriter != nil {
		upstreamReq.Header = p.rewriter.Rewrite(upstreamReq.Header)
	}

	for _, h := range p.cfg.StripHeaders {
		upstreamReq.Header.Del(h)
	}

	for k, v := range p.cfg.AddHeaders {
		upstreamReq.Header.Set(k, v)
	}

	if !p.cfg.ForwardAuth {
		upstreamReq.Header.Del("Authorization")
	}

	start := time.Now()
	resp, err := p.transport.RoundTrip(upstreamReq)
	if err != nil {
		return nil, &ProxyError{
			Code:    ErrCodeUpstreamError,
			Message: fmt.Sprintf("upstream request failed: %v", err),
			Cause:   err,
		}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, &ProxyError{
			Code:    ErrCodeProxyFailed,
			Message: fmt.Sprintf("read upstream response: %v", err),
			Cause:   err,
		}
	}

	headers := make(map[string]string)
	for k, v := range resp.Header {
		headers[k] = strings.Join(v, ", ")
	}

	return &ProxyResponse{
		StatusCode: resp.StatusCode,
		Headers:    headers,
		Body:       body,
		Duration:   time.Since(start),
	}, nil
}

// ServeHTTP implements http.Handler for use as middleware.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 10<<20))

	headers := make(map[string]string)
	for k, v := range r.Header {
		headers[k] = strings.Join(v, ", ")
	}

	proxyReq := &ProxyRequest{
		Method:  r.Method,
		URL:     r.URL.Path,
		Headers: headers,
		Body:    body,
	}

	resp, err := p.Proxy(r.Context(), proxyReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	for k, v := range resp.Headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(resp.Body)
}

// Health returns the circuit breaker status.
func (p *Proxy) Health() CircuitState {
	return p.transport.breaker.State()
}

func (cfg *Config) applyDefaults() {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.CircuitThreshold == 0 {
		cfg.CircuitThreshold = 5
	}
	if cfg.CircuitTimeout == 0 {
		cfg.CircuitTimeout = 30 * time.Second
	}
}
