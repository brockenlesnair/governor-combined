package webhook

import (
	"math"
	"math/rand"
	"time"
)

// RetryPolicy controls retry behavior.
type RetryPolicy struct {
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Multiplier     float64
}

// DefaultRetryPolicy returns sensible defaults.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:     3,
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     10 * time.Second,
		Multiplier:     2.0,
	}
}

// retryableStatus returns true if the HTTP status is worth retrying.
func retryableStatus(code int) bool {
	switch code {
	case 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

// Backoff computes the delay for retry attempt n.
// Uses exponential backoff with full jitter.
func (rp RetryPolicy) Backoff(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	exp := math.Pow(rp.Multiplier, float64(attempt))
	backoff := float64(rp.InitialBackoff) * exp
	if backoff > float64(rp.MaxBackoff) {
		backoff = float64(rp.MaxBackoff)
	}
	jitter := rand.Float64() * backoff
	return time.Duration(jitter)
}

// ShouldRetry returns true if another attempt should be made.
func (rp RetryPolicy) ShouldRetry(attempt int, statusCode int) bool {
	if attempt >= rp.MaxRetries {
		return false
	}
	return retryableStatus(statusCode)
}
