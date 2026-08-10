package hangar

import (
	"fmt"
	"os"
	"time"
)

// Config holds configuration for the Hangar REST client.
type Config struct {
	// BaseURL is the Hangar API base URL (e.g. "https://hangar.internal:8443").
	BaseURL string `yaml:"base_url"`

	// APIKey is the bearer token for Hangar authentication.
	// Loaded from HANGAR_API_KEY env var if empty.
	APIKey string `yaml:"api_key"`

	// HTTP client timeout for a single request.
	RequestTimeout time.Duration `yaml:"request_timeout"`

	// Retry policy for transient failures.
	Retry RetryConfig `yaml:"retry"`

	// CircuitBreaker thresholds.
	CircuitBreaker CircuitBreakerConfig `yaml:"circuit_breaker"`

	// Polling configuration for async operations (provider sync).
	Poll PollConfig `yaml:"poll"`

	// Fleet pagination and parallel fetching.
	Fleet FleetConfig `yaml:"fleet"`
}

// RetryConfig controls exponential backoff + jitter retry behavior.
type RetryConfig struct {
	MaxAttempts     int           `yaml:"max_attempts"`
	InitialBackoff  time.Duration `yaml:"initial_backoff"`
	MaxBackoff      time.Duration `yaml:"max_backoff"`
	BackoffFactor   float64       `yaml:"backoff_factor"`
	JitterFraction  float64       `yaml:"jitter_fraction"`
	RetryableStatus []int         `yaml:"retryable_status"`
}

// CircuitBreakerConfig controls the circuit breaker for Hangar unavailability.
type CircuitBreakerConfig struct {
	Threshold    int           `yaml:"threshold"`     // failures before opening
	HalfOpenMax  int           `yaml:"half_open_max"` // requests in half-open state
	RecoveryTime time.Duration `yaml:"recovery_time"` // time before half-open
}

// PollConfig controls polling behavior for async operations.
type PollConfig struct {
	Interval   time.Duration `yaml:"interval"`
	MaxWait    time.Duration `yaml:"max_wait"`
	RetryAfter time.Duration `yaml:"retry_after"` // default wait when Retry-After not provided
}

// FleetConfig controls fleet scorecard pagination and parallel fetching.
type FleetConfig struct {
	PageSize       int `yaml:"page_size"`
	MaxConcurrency int `yaml:"max_concurrency"` // parallel connections for large fleets
}

// Validate checks the config for required fields and applies defaults.
func (c *Config) Validate() error {
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

func (c *Config) applyDefaults() {
	if c.RequestTimeout == 0 {
		c.RequestTimeout = 30 * time.Second
	}

	// Retry defaults
	if c.Retry.MaxAttempts == 0 {
		c.Retry.MaxAttempts = 3
	}
	if c.Retry.InitialBackoff == 0 {
		c.Retry.InitialBackoff = 500 * time.Millisecond
	}
	if c.Retry.MaxBackoff == 0 {
		c.Retry.MaxBackoff = 30 * time.Second
	}
	if c.Retry.BackoffFactor == 0 {
		c.Retry.BackoffFactor = 2.0
	}
	if c.Retry.JitterFraction == 0 {
		c.Retry.JitterFraction = 0.3
	}
	if len(c.Retry.RetryableStatus) == 0 {
		c.Retry.RetryableStatus = []int{429, 502, 503, 504}
	}

	// Circuit breaker defaults
	if c.CircuitBreaker.Threshold == 0 {
		c.CircuitBreaker.Threshold = 5
	}
	if c.CircuitBreaker.HalfOpenMax == 0 {
		c.CircuitBreaker.HalfOpenMax = 2
	}
	if c.CircuitBreaker.RecoveryTime == 0 {
		c.CircuitBreaker.RecoveryTime = 30 * time.Second
	}

	// Poll defaults
	if c.Poll.Interval == 0 {
		c.Poll.Interval = 2 * time.Second
	}
	if c.Poll.MaxWait == 0 {
		c.Poll.MaxWait = 5 * time.Minute
	}
	if c.Poll.RetryAfter == 0 {
		c.Poll.RetryAfter = 10 * time.Second
	}

	// Fleet defaults
	if c.Fleet.PageSize == 0 {
		c.Fleet.PageSize = 100
	}
	if c.Fleet.MaxConcurrency == 0 {
		c.Fleet.MaxConcurrency = 10
	}
}

// DefaultConfig returns a Config with production-ready defaults.
// The caller must set BaseURL and APIKey.
func DefaultConfig() *Config {
	cfg := &Config{}
	cfg.applyDefaults()
	return cfg
}
