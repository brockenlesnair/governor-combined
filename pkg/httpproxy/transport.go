package httpproxy

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// CircuitTransport wraps http.Transport with circuit breaker logic.
type CircuitTransport struct {
	base    *http.Transport
	breaker *CircuitBreaker
	mu      sync.RWMutex
}

// NewCircuitTransport creates a transport with circuit breaker.
func NewCircuitTransport(threshold int, timeout time.Duration) *CircuitTransport {
	return &CircuitTransport{
		base: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
		},
		breaker: NewCircuitBreaker(threshold, timeout),
	}
}

// RoundTrip implements http.RoundTripper with circuit breaker.
func (ct *CircuitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !ct.breaker.Allow() {
		return nil, &CircuitOpenError{
			State:   ct.breaker.State(),
			Message: "circuit breaker is open",
		}
	}

	resp, err := ct.base.RoundTrip(req)
	if err != nil {
		ct.breaker.RecordFailure()
		return nil, err
	}

	if resp.StatusCode >= 500 {
		ct.breaker.RecordFailure()
	} else {
		ct.breaker.RecordSuccess()
	}

	return resp, nil
}
