package httpproxy

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- HeaderRewriter Tests ---

func TestNewHeaderRewriter_ValidRules(t *testing.T) {
	rules := []RewriteRule{
		{Match: "X-Forwarded-For", Action: "set", Replace: "1.2.3.4"},
		{Match: "^X-Internal-.*", Action: "remove"},
		{Match: "Authorization", Action: "rename", NewName: "X-Auth"},
	}
	hr, err := NewHeaderRewriter(rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hr == nil {
		t.Fatal("expected non-nil rewriter")
	}
	if len(hr.compiled) != 3 {
		t.Fatalf("expected 3 compiled regexes, got %d", len(hr.compiled))
	}
}

func TestNewHeaderRewriter_InvalidRegex(t *testing.T) {
	rules := []RewriteRule{
		{Match: "[invalid", Action: "set", Replace: "val"},
	}
	_, err := NewHeaderRewriter(rules)
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
	if !strings.Contains(err.Error(), "compile regex") {
		t.Fatalf("expected compile regex error, got: %v", err)
	}
}

func TestNewHeaderRewriter_EmptyMatchSkipped(t *testing.T) {
	rules := []RewriteRule{
		{Match: "", Action: "set", Replace: "val"},
	}
	hr, err := NewHeaderRewriter(rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hr.compiled) != 0 {
		t.Fatalf("expected 0 compiled regexes for empty match, got %d", len(hr.compiled))
	}
}

func TestHeaderRewriter_RewriteSet(t *testing.T) {
	rules := []RewriteRule{
		{Match: "X-Request-Id", Action: "set", Replace: "new-id-123"},
	}
	hr, err := NewHeaderRewriter(rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	in := http.Header{"X-Request-Id": {"old-id"}}
	out := hr.Rewrite(in)

	if out.Get("X-Request-Id") != "new-id-123" {
		t.Fatalf("expected 'new-id-123', got '%s'", out.Get("X-Request-Id"))
	}
}

func TestHeaderRewriter_RewriteSetAddsNew(t *testing.T) {
	rules := []RewriteRule{
		{Match: "^X-New-Header$", Action: "set", Replace: "value"},
	}
	hr, err := NewHeaderRewriter(rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	in := http.Header{"X-New-Header": {"old"}, "X-Other": {"keep"}}
	out := hr.Rewrite(in)

	if out.Get("X-New-Header") != "value" {
		t.Fatalf("expected 'value', got '%s'", out.Get("X-New-Header"))
	}
	if out.Get("X-Other") != "keep" {
		t.Fatalf("expected 'keep', got '%s'", out.Get("X-Other"))
	}
}

func TestHeaderRewriter_RewriteSetRegex(t *testing.T) {
	rules := []RewriteRule{
		{Match: "^X-Trace-.*", Action: "set", Replace: "traced"},
	}
	hr, err := NewHeaderRewriter(rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	in := http.Header{"X-Trace-Id": {"old"}, "X-Trace-Span": {"old"}}
	out := hr.Rewrite(in)

	if out.Get("X-Trace-Id") != "traced" {
		t.Fatalf("expected 'traced' for X-Trace-Id, got '%s'", out.Get("X-Trace-Id"))
	}
	if out.Get("X-Trace-Span") != "traced" {
		t.Fatalf("expected 'traced' for X-Trace-Span, got '%s'", out.Get("X-Trace-Span"))
	}
}

func TestHeaderRewriter_RewriteRemove(t *testing.T) {
	rules := []RewriteRule{
		{Match: "X-Internal-Debug", Action: "remove"},
	}
	hr, err := NewHeaderRewriter(rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	in := http.Header{
		"X-Internal-Debug": {"true"},
		"Content-Type":     {"application/json"},
	}
	out := hr.Rewrite(in)

	if _, ok := out["X-Internal-Debug"]; ok {
		t.Fatal("expected X-Internal-Debug to be removed")
	}
	if out.Get("Content-Type") != "application/json" {
		t.Fatal("expected Content-Type to remain")
	}
}

func TestHeaderRewriter_RewriteRemoveRegex(t *testing.T) {
	rules := []RewriteRule{
		{Match: "^X-Internal-.*", Action: "remove"},
	}
	hr, err := NewHeaderRewriter(rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	in := http.Header{
		"X-Internal-A": {"1"},
		"X-Internal-B": {"2"},
		"X-Public":     {"keep"},
	}
	out := hr.Rewrite(in)

	if _, ok := out["X-Internal-A"]; ok {
		t.Fatal("expected X-Internal-A to be removed")
	}
	if _, ok := out["X-Internal-B"]; ok {
		t.Fatal("expected X-Internal-B to be removed")
	}
	if out.Get("X-Public") != "keep" {
		t.Fatal("expected X-Public to remain")
	}
}

func TestHeaderRewriter_RewriteAppend(t *testing.T) {
	rules := []RewriteRule{
		{Match: "X-Custom", Action: "append", Replace: "; appended"},
	}
	hr, err := NewHeaderRewriter(rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	in := http.Header{"X-Custom": {"original"}}
	out := hr.Rewrite(in)

	vals := out["X-Custom"]
	if len(vals) != 2 {
		t.Fatalf("expected 2 values, got %d", len(vals))
	}
	if vals[0] != "original" || vals[1] != "; appended" {
		t.Fatalf("unexpected values: %v", vals)
	}
}

func TestHeaderRewriter_RewriteRename(t *testing.T) {
	rules := []RewriteRule{
		{Match: "X-Old-Name", Action: "rename", NewName: "X-New-Name"},
	}
	hr, err := NewHeaderRewriter(rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	in := http.Header{"X-Old-Name": {"value"}}
	out := hr.Rewrite(in)

	if _, ok := out["X-Old-Name"]; ok {
		t.Fatal("expected X-Old-Name to be removed")
	}
	if out.Get("X-New-Name") != "value" {
		t.Fatalf("expected 'value' in X-New-Name, got '%s'", out.Get("X-New-Name"))
	}
}

func TestHeaderRewriter_RewriteRenameRegex(t *testing.T) {
	rules := []RewriteRule{
		{Match: "^X-Old-(.*)", Action: "rename", NewName: "X-New-${1}"},
	}
	hr, err := NewHeaderRewriter(rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	in := http.Header{"X-Old-Foo": {"bar"}}
	out := hr.Rewrite(in)

	// Rename uses NewName literally (not regex replacement)
	if out.Get("X-New-${1}") != "bar" {
		t.Fatalf("expected 'bar' in X-New-${1}, got '%s'", out.Get("X-New-${1}"))
	}
	if _, ok := out["X-Old-Foo"]; ok {
		t.Fatal("expected X-Old-Foo to be removed")
	}
}

func TestHeaderRewriter_RewriteDoesNotMutateOriginal(t *testing.T) {
	rules := []RewriteRule{
		{Match: "X-Test", Action: "set", Replace: "changed"},
	}
	hr, err := NewHeaderRewriter(rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	in := http.Header{"X-Test": {"original"}}
	_ = hr.Rewrite(in)

	if in.Get("X-Test") != "original" {
		t.Fatal("expected original header to not be mutated")
	}
}

func TestHeaderRewriter_RewriteDefaultAction(t *testing.T) {
	// Empty action should behave like "set"
	rules := []RewriteRule{
		{Match: "X-Header", Replace: "new-value"},
	}
	hr, err := NewHeaderRewriter(rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	in := http.Header{"X-Header": {"old"}}
	out := hr.Rewrite(in)

	if out.Get("X-Header") != "new-value" {
		t.Fatalf("expected 'new-value', got '%s'", out.Get("X-Header"))
	}
}

// --- CircuitBreaker Tests ---

func TestCircuitBreaker_InitialState(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Second)
	if cb.State() != CircuitClosed {
		t.Fatalf("expected closed, got %v", cb.State())
	}
	if !cb.Allow() {
		t.Fatal("expected Allow to return true when closed")
	}
}

func TestCircuitBreaker_TransitionsClosedToOpen(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Hour) // long timeout so it stays open
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}
	if cb.State() != CircuitOpen {
		t.Fatalf("expected open after 3 failures, got %v", cb.State())
	}
	if cb.Allow() {
		t.Fatal("expected Allow to return false when open")
	}
}

func TestCircuitBreaker_TransitionsOpenToHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(2, 50*time.Millisecond)
	cb.RecordFailure()
	cb.RecordFailure() // threshold=2, now open

	if cb.State() != CircuitOpen {
		t.Fatalf("expected open, got %v", cb.State())
	}

	time.Sleep(60 * time.Millisecond) // wait for timeout

	if !cb.Allow() {
		t.Fatal("expected Allow to return true after timeout (half-open)")
	}
	if cb.State() != CircuitHalfOpen {
		t.Fatalf("expected half-open, got %v", cb.State())
	}
}

func TestCircuitBreaker_TransitionsHalfOpenToClosed(t *testing.T) {
	cb := NewCircuitBreaker(2, 50*time.Millisecond)
	cb.RecordFailure()
	cb.RecordFailure() // open

	time.Sleep(60 * time.Millisecond)
	cb.Allow() // half-open

	// Record enough successes to close (halfOpenMax = 3)
	cb.RecordSuccess()
	cb.RecordSuccess()
	cb.RecordSuccess()

	if cb.State() != CircuitClosed {
		t.Fatalf("expected closed after 3 successes, got %v", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenToOpenOnFailure(t *testing.T) {
	cb := NewCircuitBreaker(2, 50*time.Millisecond)
	cb.RecordFailure()
	cb.RecordFailure() // open

	time.Sleep(60 * time.Millisecond)
	cb.Allow() // half-open

	cb.RecordFailure() // back to open

	if cb.State() != CircuitOpen {
		t.Fatalf("expected open after failure in half-open, got %v", cb.State())
	}
}

func TestCircuitBreaker_RecordSuccessResetsCount(t *testing.T) {
	cb := NewCircuitBreaker(5, time.Hour)
	cb.RecordFailure()
	cb.RecordFailure() // failureCount=2

	cb.RecordSuccess() // resets failureCount

	// Need 5 more failures to open again
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitClosed {
		t.Fatalf("expected closed (only 2 failures since reset), got %v", cb.State())
	}
}

func TestCircuitBreaker_AllowHalfOpenAlwaysAllows(t *testing.T) {
	cb := NewCircuitBreaker(2, time.Hour)
	cb.RecordFailure()
	cb.RecordFailure() // open

	// Manually set lastFailureTime to past to force half-open transition
	cb.mu.Lock()
	cb.lastFailureTime = time.Now().Add(-2 * time.Hour)
	cb.mu.Unlock()

	if !cb.Allow() {
		t.Fatal("expected Allow=true in half-open")
	}
	// Allow again should still work (half-open allows all)
	if !cb.Allow() {
		t.Fatal("expected Allow=true in half-open again")
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
			t.Errorf("state(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

// --- CircuitTransport Tests ---

func TestNewCircuitTransport(t *testing.T) {
	ct := NewCircuitTransport(5, 30*time.Second)
	if ct == nil {
		t.Fatal("expected non-nil transport")
	}
	if ct.base == nil {
		t.Fatal("expected non-nil base transport")
	}
	if ct.breaker == nil {
		t.Fatal("expected non-nil breaker")
	}
}

func TestCircuitTransport_RoundTrip_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello"))
	}))
	defer srv.Close()

	ct := NewCircuitTransport(5, 30*time.Second)
	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := ct.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello" {
		t.Fatalf("expected 'hello', got '%s'", string(body))
	}
}

func TestCircuitTransport_RoundTrip_Records5xxFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ct := NewCircuitTransport(3, time.Hour)
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest("GET", srv.URL, nil)
		ct.RoundTrip(req)
	}

	if ct.breaker.State() != CircuitOpen {
		t.Fatalf("expected circuit open after 3 5xx responses, got %v", ct.breaker.State())
	}
}

func TestCircuitTransport_RoundTrip_RecordsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ct := NewCircuitTransport(5, 30*time.Second)
	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := ct.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if ct.breaker.failureCount != 0 {
		t.Fatalf("expected failure count 0, got %d", ct.breaker.failureCount)
	}
}

// --- Proxy Tests ---

func TestCircuitTransport_RoundTrip_CircuitOpenError(t *testing.T) {
	ct := NewCircuitTransport(1, time.Hour)
	ct.breaker.RecordFailure() // open the circuit

	req, _ := http.NewRequest("GET", "http://localhost:1", nil)
	_, err := ct.RoundTrip(req)
	if err == nil {
		t.Fatal("expected error when circuit open")
	}
	var coe *CircuitOpenError
	if !strings.Contains(err.Error(), "circuit breaker is open") {
		t.Fatalf("expected circuit breaker error, got: %v", err)
	}
	_ = coe // type assertion would also work
}

// --- Proxy Tests ---

func TestNewProxy_NilConfig(t *testing.T) {
	logger := slog.Default()
	p := NewProxy(nil, logger)
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if p.cfg.Timeout != 30*time.Second {
		t.Fatalf("expected default timeout 30s, got %v", p.cfg.Timeout)
	}
	if p.cfg.CircuitThreshold != 5 {
		t.Fatalf("expected default threshold 5, got %d", p.cfg.CircuitThreshold)
	}
	if p.cfg.CircuitTimeout != 30*time.Second {
		t.Fatalf("expected default circuit timeout 30s, got %v", p.cfg.CircuitTimeout)
	}
}

func TestNewProxy_WithConfig(t *testing.T) {
	logger := slog.Default()
	cfg := &Config{
		Target:           "http://example.com",
		Timeout:          10 * time.Second,
		CircuitThreshold: 3,
		CircuitTimeout:   15 * time.Second,
	}
	p := NewProxy(cfg, logger)
	if p.cfg.Target != "http://example.com" {
		t.Fatalf("expected target 'http://example.com', got '%s'", p.cfg.Target)
	}
	if p.cfg.Timeout != 10*time.Second {
		t.Fatalf("expected timeout 10s, got %v", p.cfg.Timeout)
	}
}

func TestProxy_Proxy_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upstream", "true")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("proxied response"))
	}))
	defer srv.Close()

	logger := slog.Default()
	cfg := &Config{
		Target: srv.URL,
	}
	p := NewProxy(cfg, logger)

	req := &ProxyRequest{
		Method:  "GET",
		URL:     "/test",
		Headers: map[string]string{"X-Custom": "value"},
	}
	resp, err := p.Proxy(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if string(resp.Body) != "proxied response" {
		t.Fatalf("expected 'proxied response', got '%s'", string(resp.Body))
	}
	if resp.Headers["X-Upstream"] != "true" {
		t.Fatalf("expected X-Upstream header")
	}
	if resp.Duration == 0 {
		t.Fatal("expected non-zero duration")
	}
}

func TestProxy_Proxy_WithBody(t *testing.T) {
	var receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	logger := slog.Default()
	p := NewProxy(&Config{Target: srv.URL}, logger)

	req := &ProxyRequest{
		Method: "POST",
		URL:    "/post",
		Body:   []byte("request body data"),
	}
	resp, err := p.Proxy(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body // ProxyResponse.Body is []byte, already read

	if receivedBody != "request body data" {
		t.Fatalf("expected body 'request body data', got '%s'", receivedBody)
	}
}

func TestProxy_Proxy_StripsHeaders(t *testing.T) {
	var receivedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	logger := slog.Default()
	cfg := &Config{
		Target:       srv.URL,
		StripHeaders: []string{"X-Strip-Me"},
	}
	p := NewProxy(cfg, logger)

	req := &ProxyRequest{
		Method:  "GET",
		URL:     "/test",
		Headers: map[string]string{"X-Strip-Me": "gone", "X-Keep": "yes"},
	}
	resp, err := p.Proxy(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp

	if _, ok := receivedHeaders["X-Strip-Me"]; ok {
		t.Fatal("expected X-Strip-Me to be stripped")
	}
}

func TestProxy_Proxy_AddsHeaders(t *testing.T) {
	var receivedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	logger := slog.Default()
	cfg := &Config{
		Target:     srv.URL,
		AddHeaders: map[string]string{"X-Added": "yes"},
	}
	p := NewProxy(cfg, logger)

	req := &ProxyRequest{
		Method:  "GET",
		URL:     "/test",
		Headers: map[string]string{},
	}
	resp, err := p.Proxy(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp

	if receivedHeaders.Get("X-Added") != "yes" {
		t.Fatalf("expected X-Added header, got '%s'", receivedHeaders.Get("X-Added"))
	}
}

func TestProxy_Proxy_ForwardAuthDisabled(t *testing.T) {
	var receivedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	logger := slog.Default()
	cfg := &Config{
		Target:      srv.URL,
		ForwardAuth: false,
	}
	p := NewProxy(cfg, logger)

	req := &ProxyRequest{
		Method:  "GET",
		URL:     "/test",
		Headers: map[string]string{"Authorization": "Bearer secret"},
	}
	resp, err := p.Proxy(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp

	if _, ok := receivedHeaders["Authorization"]; ok {
		t.Fatal("expected Authorization to be stripped when ForwardAuth=false")
	}
}

func TestProxy_Proxy_ForwardAuthEnabled(t *testing.T) {
	var receivedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	logger := slog.Default()
	cfg := &Config{
		Target:      srv.URL,
		ForwardAuth: true,
	}
	p := NewProxy(cfg, logger)

	req := &ProxyRequest{
		Method:  "GET",
		URL:     "/test",
		Headers: map[string]string{"Authorization": "Bearer secret"},
	}
	resp, err := p.Proxy(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp

	if receivedHeaders.Get("Authorization") != "Bearer secret" {
		t.Fatal("expected Authorization to be forwarded when ForwardAuth=true")
	}
}

func TestProxy_Proxy_CircuitOpen(t *testing.T) {
	logger := slog.Default()
	cfg := &Config{Target: "http://localhost:1", CircuitThreshold: 3}
	p := NewProxy(cfg, logger)

	// Force circuit open
	p.transport.breaker.RecordFailure()
	p.transport.breaker.RecordFailure()
	p.transport.breaker.RecordFailure()

	req := &ProxyRequest{
		Method: "GET",
		URL:    "/test",
	}
	_, err := p.Proxy(context.Background(), req)
	if err == nil {
		t.Fatal("expected error when circuit open")
	}
	var pe *ProxyError
	if ok := errorAs(err, &pe); !ok || pe.Code != ErrCodeCircuitOpen {
		t.Fatalf("expected ErrCodeCircuitOpen, got: %v", err)
	}
}

func TestProxy_Proxy_RewriteRules(t *testing.T) {
	var receivedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	logger := slog.Default()
	cfg := &Config{
		Target: srv.URL,
		RewriteRules: []RewriteRule{
			{Match: "X-Custom", Action: "set", Replace: "rewritten"},
		},
	}
	p := NewProxy(cfg, logger)

	req := &ProxyRequest{
		Method:  "GET",
		URL:     "/test",
		Headers: map[string]string{"X-Custom": "original"},
	}
	resp, err := p.Proxy(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp

	if receivedHeaders.Get("X-Custom") != "rewritten" {
		t.Fatalf("expected 'rewritten', got '%s'", receivedHeaders.Get("X-Custom"))
	}
}

func TestProxy_ServeHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "true")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("backend response"))
	}))
	defer srv.Close()

	logger := slog.Default()
	p := NewProxy(&Config{Target: srv.URL}, logger)

	// Use p as an HTTP handler via httptest
	req := httptest.NewRequest("GET", "/handler-test", nil)
	req.Header.Set("X-From-Client", "yes")
	w := httptest.NewRecorder()

	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "backend response" {
		t.Fatalf("expected 'backend response', got '%s'", w.Body.String())
	}
	if w.Header().Get("X-Backend") != "true" {
		t.Fatal("expected X-Backend header from upstream")
	}
}

func TestProxy_ServeHTTP_CircuitOpen(t *testing.T) {
	logger := slog.Default()
	p := NewProxy(&Config{Target: "http://localhost:1"}, logger)

	// Force circuit open
	p.transport.breaker.RecordFailure()
	p.transport.breaker.RecordFailure()
	p.transport.breaker.RecordFailure()

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	p.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestProxy_Health(t *testing.T) {
	logger := slog.Default()
	p := NewProxy(&Config{Target: "http://localhost", CircuitThreshold: 3}, logger)

	if p.Health() != CircuitClosed {
		t.Fatalf("expected closed, got %v", p.Health())
	}

	p.transport.breaker.RecordFailure()
	p.transport.breaker.RecordFailure()
	p.transport.breaker.RecordFailure()

	if p.Health() != CircuitOpen {
		t.Fatalf("expected open, got %v", p.Health())
	}
}

func TestProxyError_Error(t *testing.T) {
	err := &ProxyError{Code: ErrCodeProxyFailed, Message: "test error"}
	if err.Error() != "PROXY_FAILED: test error" {
		t.Fatalf("unexpected error string: %s", err.Error())
	}
}

func TestProxyError_ErrorWithCause(t *testing.T) {
	cause := io.ErrUnexpectedEOF
	err := &ProxyError{Code: ErrCodeUpstreamError, Message: "upstream failed", Cause: cause}
	s := err.Error()
	if !strings.Contains(s, "UPSTREAM_ERROR") || !strings.Contains(s, "upstream failed") {
		t.Fatalf("unexpected error string: %s", s)
	}
}

func TestProxyError_Unwrap(t *testing.T) {
	cause := io.ErrUnexpectedEOF
	err := &ProxyError{Code: ErrCodeProxyFailed, Message: "fail", Cause: cause}
	if err.Unwrap() != cause {
		t.Fatal("expected Unwrap to return cause")
	}
}

func TestCircuitOpenError_Error(t *testing.T) {
	err := &CircuitOpenError{State: CircuitOpen, Message: "circuit is open"}
	if err.Error() != "circuit is open" {
		t.Fatalf("unexpected error string: %s", err.Error())
	}
}

// errorAs is a simplified helper for testing
func errorAs(err error, target interface{}) bool {
	switch t := target.(type) {
	case **ProxyError:
		if pe, ok := err.(*ProxyError); ok {
			*t = pe
			return true
		}
	}
	return false
}
