package webhook

import (
	"sync"
	"time"
)

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

	switch cb.state {
	case CircuitClosed:
		cb.failureCount = 0
	case CircuitHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.halfOpenMax {
			cb.state = CircuitClosed
			cb.failureCount = 0
		}
	}
}

// RecordFailure records a failed request.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case CircuitClosed:
		if cb.failureCount >= cb.threshold {
			cb.state = CircuitOpen
		}
	case CircuitHalfOpen:
		cb.state = CircuitOpen
	}
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// FailureCount returns the current failure count.
func (cb *CircuitBreaker) FailureCount() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.failureCount
}
