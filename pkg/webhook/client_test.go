package webhook

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// --- Retry Tests ---

func TestRetryPolicy_Backoff_NonNegative(t *testing.T) {
	rp := DefaultRetryPolicy()
	for attempt := 0; attempt <= 10; attempt++ {
		d := rp.Backoff(attempt)
		if d < 0 {
			t.Errorf("Backoff(%d) = %v, want >= 0", attempt, d)
		}
	}
}

func TestRetryPolicy_Backoff_ZeroForZeroAttempt(t *testing.T) {
	rp := DefaultRetryPolicy()
	d := rp.Backoff(0)
	if d != 0 {
		t.Errorf("Backoff(0) = %v, want 0", d)
	}
}

func TestRetryPolicy_Backoff_RespectsMax(t *testing.T) {
	rp := RetryPolicy{
		MaxRetries:     5,
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     500 * time.Millisecond,
		Multiplier:     2.0,
	}
	// Try many attempts to ensure jitter doesn't exceed max
	for attempt := 1; attempt <= 20; attempt++ {
		d := rp.Backoff(attempt)
		if d > rp.MaxBackoff {
			t.Errorf("Backoff(%d) = %v, exceeds MaxBackoff %v", attempt, d, rp.MaxBackoff)
		}
	}
}

func TestRetryPolicy_ShouldRetry_MaxRetries(t *testing.T) {
	rp := RetryPolicy{MaxRetries: 3}
	if rp.ShouldRetry(3, 500) {
		t.Error("ShouldRetry(3, 500) = true, want false (exceeds MaxRetries)")
	}
	if !rp.ShouldRetry(2, 500) {
		t.Error("ShouldRetry(2, 500) = false, want true")
	}
}

func TestRetryPolicy_ShouldRetry_NonRetryableStatus(t *testing.T) {
	rp := RetryPolicy{MaxRetries: 5}
	if rp.ShouldRetry(0, 200) {
		t.Error("ShouldRetry(0, 200) = true, want false")
	}
	if rp.ShouldRetry(0, 404) {
		t.Error("ShouldRetry(0, 404) = true, want false")
	}
}

func TestRetryableStatus(t *testing.T) {
	retryable := []int{429, 500, 502, 503, 504}
	for _, code := range retryable {
		if !retryableStatus(code) {
			t.Errorf("retryableStatus(%d) = false, want true", code)
		}
	}
	nonRetryable := []int{200, 201, 400, 401, 403, 404}
	for _, code := range nonRetryable {
		if retryableStatus(code) {
			t.Errorf("retryableStatus(%d) = true, want false", code)
		}
	}
}

// --- Circuit Breaker Tests ---

func TestCircuitBreaker_ClosedToOpen(t *testing.T) {
	cb := NewCircuitBreaker(3, 1*time.Second)
	if cb.State() != CircuitClosed {
		t.Fatalf("initial state = %v, want Closed", cb.State())
	}
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}
	if cb.State() != CircuitOpen {
		t.Errorf("after 3 failures: state = %v, want Open", cb.State())
	}
}

func TestCircuitBreaker_OpenToHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(2, 50*time.Millisecond)
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatalf("state = %v, want Open", cb.State())
	}
	// Wait for timeout
	time.Sleep(100 * time.Millisecond)
	if !cb.Allow() {
		t.Error("Allow() = false after timeout, want true (half-open)")
	}
	if cb.State() != CircuitHalfOpen {
		t.Errorf("state = %v, want HalfOpen", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenToClosed(t *testing.T) {
	cb := NewCircuitBreaker(2, 50*time.Millisecond)
	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(100 * time.Millisecond)
	cb.Allow() // transitions to half-open

	// Record enough successes to close
	for i := 0; i < 3; i++ {
		cb.RecordSuccess()
	}
	if cb.State() != CircuitClosed {
		t.Errorf("state = %v, want Closed", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenToOpen(t *testing.T) {
	cb := NewCircuitBreaker(2, 50*time.Millisecond)
	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(100 * time.Millisecond)
	cb.Allow() // transitions to half-open

	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Errorf("state = %v, want Open after failure in half-open", cb.State())
	}
}

func TestCircuitBreaker_Allow_Closed(t *testing.T) {
	cb := NewCircuitBreaker(5, 1*time.Second)
	if !cb.Allow() {
		t.Error("Allow() = false in closed state, want true")
	}
}

func TestCircuitBreaker_Allow_Open(t *testing.T) {
	cb := NewCircuitBreaker(1, 10*time.Second)
	cb.RecordFailure()
	if cb.Allow() {
		t.Error("Allow() = true in open state, want false")
	}
}

func TestCircuitBreaker_FailureCount(t *testing.T) {
	cb := NewCircuitBreaker(10, 1*time.Second)
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.FailureCount() != 2 {
		t.Errorf("FailureCount() = %d, want 2", cb.FailureCount())
	}
}

func TestCircuitBreaker_StateString(t *testing.T) {
	tests := []struct {
		state CircuitState
		want  string
	}{
		{CircuitClosed, "closed"},
		{CircuitOpen, "open"},
		{CircuitHalfOpen, "half-open"},
		{CircuitState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("CircuitState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

// --- Signer Tests ---

func TestSigner_SignVerify_Roundtrip(t *testing.T) {
	s := NewSigner("my-secret-key", "sha256")
	payload := []byte(`{"event":"test","data":"hello"}`)
	sig := s.Sign(payload)
	if sig == "" {
		t.Fatal("Sign returned empty string")
	}
	if !s.Verify(payload, sig) {
		t.Error("Verify returned false for valid signature")
	}
	if s.Verify([]byte("tampered"), sig) {
		t.Error("Verify returned true for tampered payload")
	}
}

func TestSigner_SHA512(t *testing.T) {
	s := NewSigner("key", "sha512")
	payload := []byte("test data")
	sig := s.Sign(payload)
	if !s.Verify(payload, sig) {
		t.Error("SHA512 Verify failed")
	}
}

func TestSigner_HeaderName(t *testing.T) {
	s256 := NewSigner("k", "sha256")
	if s256.HeaderName() != "X-Webhook-Signature-SHA256" {
		t.Errorf("HeaderName() = %q, want X-Webhook-Signature-SHA256", s256.HeaderName())
	}
	s512 := NewSigner("k", "sha512")
	if s512.HeaderName() != "X-Webhook-Signature-SHA512" {
		t.Errorf("HeaderName() = %q, want X-Webhook-Signature-SHA512", s512.HeaderName())
	}
}

func TestSigner_DefaultAlgo(t *testing.T) {
	s := NewSigner("k", "")
	if s.HeaderName() != "X-Webhook-Signature-SHA256" {
		t.Errorf("default algo HeaderName() = %q, want SHA256", s.HeaderName())
	}
}

// --- Client Tests ---

func TestNewClient_NilConfig(t *testing.T) {
	logger := slog.Default()
	c := NewClient(nil, logger)
	if c.cfg == nil {
		t.Fatal("cfg is nil after NewClient(nil, ...)")
	}
	if c.cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", c.cfg.Timeout)
	}
	if c.cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", c.cfg.MaxRetries)
	}
	if c.cfg.CircuitThreshold != 5 {
		t.Errorf("CircuitThreshold = %d, want 5", c.cfg.CircuitThreshold)
	}
}

func TestClient_IsAllowed_NoAllowList(t *testing.T) {
	c := NewClient(&Config{}, slog.Default())
	if !c.isAllowed("https://example.com/webhook") {
		t.Error("isAllowed returned false with empty allow-list")
	}
}

func TestClient_IsAllowed_MatchingPrefix(t *testing.T) {
	c := NewClient(&Config{
		AllowList: []string{"https://example.com/", "https://hooks.slack.com/"},
	}, slog.Default())
	if !c.isAllowed("https://example.com/webhook") {
		t.Error("isAllowed returned false for matching prefix")
	}
	if !c.isAllowed("https://hooks.slack.com/T00/B00/xxx") {
		t.Error("isAllowed returned false for slack prefix")
	}
}

func TestClient_IsAllowed_Blocked(t *testing.T) {
	c := NewClient(&Config{
		AllowList: []string{"https://example.com/"},
	}, slog.Default())
	if c.isAllowed("https://evil.com/webhook") {
		t.Error("isAllowed returned true for non-matching URL")
	}
}

func TestClient_Send_Success(t *testing.T) {
	var received bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %s, want application/json", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewClient(&Config{MaxRetries: 0}, slog.Default())
	result, err := c.Send(context.Background(), &DeliveryRequest{
		URL:    server.URL,
		Method: http.MethodPost,
		Body:   []byte(`{"event":"test"}`),
	})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if !result.Success {
		t.Errorf("Success = false, want true")
	}
	if result.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", result.StatusCode)
	}
	if result.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", result.Attempts)
	}
	if !received {
		t.Error("server did not receive request")
	}
}

func TestClient_Send_RetriesOnFailure(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewClient(&Config{
		MaxRetries:     3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
		Multiplier:     1.5,
	}, slog.Default())

	result, err := c.Send(context.Background(), &DeliveryRequest{
		URL:  server.URL,
		Body: []byte(`{"event":"test"}`),
	})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if !result.Success {
		t.Errorf("Success = false, want true after retries")
	}
	if result.Attempts < 3 {
		t.Errorf("Attempts = %d, want >= 3", result.Attempts)
	}
}

func TestClient_Send_TargetBlocked(t *testing.T) {
	c := NewClient(&Config{
		AllowList: []string{"https://allowed.com/"},
	}, slog.Default())

	result, err := c.Send(context.Background(), &DeliveryRequest{
		URL: "https://evil.com/webhook",
	})
	if err == nil {
		t.Fatal("expected error for blocked target")
	}
	if result != nil {
		t.Errorf("result should be nil, got %v", result)
	}
	if _, ok := err.(*WebhookError); !ok {
		t.Errorf("error type = %T, want *WebhookError", err)
	}
}

func TestClient_Send_CircuitOpen(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	c := NewClient(&Config{
		MaxRetries:      0,
		CircuitThreshold: 2,
		CircuitTimeout:  10 * time.Second,
	}, slog.Default())

	// Trip the circuit breaker
	c.breaker.RecordFailure()
	c.breaker.RecordFailure()

	result, err := c.Send(context.Background(), &DeliveryRequest{
		URL:  server.URL,
		Body: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if !result.CircuitOpen {
		t.Error("CircuitOpen = false, want true")
	}
}

func TestClient_Send_SignatureHeader(t *testing.T) {
	var sigHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sigHeader = r.Header.Get("X-Webhook-Signature-SHA256")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewClient(&Config{
		MaxRetries: 0,
		Secret:     "test-secret",
		SignAlgo:   "sha256",
	}, slog.Default())

	_, err := c.Send(context.Background(), &DeliveryRequest{
		URL:  server.URL,
		Body: []byte(`{"event":"test"}`),
	})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if sigHeader == "" {
		t.Error("signature header not set")
	}

	// Verify it's a valid HMAC
	signer := NewSigner("test-secret", "sha256")
	if !signer.Verify([]byte(`{"event":"test"}`), sigHeader) {
		t.Error("signature verification failed")
	}
}

func TestClient_SendAsync(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewClient(&Config{MaxRetries: 0}, slog.Default())
	ch := c.SendAsync(context.Background(), &DeliveryRequest{
		URL:  server.URL,
		Body: []byte(`{}`),
	})

	result := <-ch
	if result == nil {
		t.Fatal("result is nil")
	}
	if !result.Success {
		t.Errorf("Success = false, want true")
	}
}

func TestClient_Send_NonRetryableFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := NewClient(&Config{MaxRetries: 3}, slog.Default())
	result, err := c.Send(context.Background(), &DeliveryRequest{
		URL:  server.URL,
		Body: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("Success = true, want false for 404")
	}
	if result.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1 (404 is not retryable)", result.Attempts)
	}
}

func TestClient_Health(t *testing.T) {
	c := NewClient(&Config{}, slog.Default())
	if c.Health() != CircuitClosed {
		t.Errorf("Health() = %v, want Closed", c.Health())
	}
}

// --- TokenBucket Tests ---

func TestTokenBucket_AllowsImmediateBurst(t *testing.T) {
	tb := NewTokenBucket(1, 5)
	for i := 0; i < 5; i++ {
		if !tb.tryAcquire() {
			t.Errorf("tryAcquire() failed on attempt %d, expected burst of 5", i)
		}
	}
}

func TestTokenBucket_ExhaustsTokens(t *testing.T) {
	tb := NewTokenBucket(1, 2)
	tb.tryAcquire()
	tb.tryAcquire()
	if tb.tryAcquire() {
		t.Error("tryAcquire() succeeded after exhausting burst")
	}
}

func TestTokenBucket_RefillsOverTime(t *testing.T) {
	tb := NewTokenBucket(100, 1) // rate=100/s, burst=1
	tb.tryAcquire()              // exhaust
	time.Sleep(20 * time.Millisecond)
	if !tb.tryAcquire() {
		t.Error("tryAcquire() failed after refill period")
	}
}

func TestTokenBucket_Wait_RespectsContext(t *testing.T) {
	tb := NewTokenBucket(1, 0) // no burst, slow rate
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	tb.Wait(ctx) // should return quickly due to context cancellation
}

// --- WebhookError Tests ---

func TestWebhookError_Error(t *testing.T) {
	e := &WebhookError{
		Code:    ErrCodeTargetBlocked,
		Message: "URL blocked",
	}
	if e.Error() != "TARGET_BLOCKED: URL blocked" {
		t.Errorf("Error() = %q", e.Error())
	}
}

func TestWebhookError_ErrorWithCause(t *testing.T) {
	e := &WebhookError{
		Code:    ErrCodeWebhookFailed,
		Message: "send failed",
		Cause:   fmt.Errorf("connection refused"),
	}
	got := e.Error()
	if got != "WEBHOOK_FAILED: send failed: connection refused" {
		t.Errorf("Error() = %q", got)
	}
}

func TestWebhookError_Unwrap(t *testing.T) {
	cause := fmt.Errorf("root cause")
	e := &WebhookError{
		Code:  ErrCodeCircuitOpen,
		Message: "open",
		Cause: cause,
	}
	if e.Unwrap() != cause {
		t.Error("Unwrap() did not return cause")
	}
}
