package safety

import (
	"errors"
	"fmt"
)

// SafetyError is a safety-specific error.
type SafetyError struct {
	Code    string
	Message string
	Err     error
}

func (e *SafetyError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *SafetyError) Unwrap() error {
	return e.Err
}

// Error codes
const (
	ErrCodePolicyNotFound       = "SAFETY_POLICY_NOT_FOUND"
	ErrCodeInvalidPattern       = "SAFETY_INVALID_PATTERN"
	ErrCodeParseFailed          = "SAFETY_PARSE_FAILED"
	ErrCodeDiffParseFailed      = "SAFETY_DIFF_PARSE_FAILED"
	ErrCodeAuditWriteFailed     = "SAFETY_AUDIT_WRITE_FAILED"
	ErrCodeRuleNotFound         = "SAFETY_RULE_NOT_FOUND"
	ErrCodeLanguageNotSupported = "SAFETY_LANGUAGE_NOT_SUPPORTED"
	ErrCodeFileTooLarge         = "SAFETY_FILE_TOO_LARGE"
	ErrCodeValidationFailed     = "SAFETY_VALIDATION_FAILED"
	ErrCodeTimeout              = "SAFETY_TIMEOUT"
)

// NewPolicyNotFoundError creates a policy not found error.
func NewPolicyNotFoundError(name string) *SafetyError {
	return &SafetyError{
		Code:    ErrCodePolicyNotFound,
		Message: fmt.Sprintf("policy not found: %s", name),
	}
}

// NewInvalidPatternError creates an invalid pattern error.
func NewInvalidPatternError(pattern string) *SafetyError {
	return &SafetyError{
		Code:    ErrCodeInvalidPattern,
		Message: fmt.Sprintf("invalid pattern: %s", pattern),
	}
}

// NewParseFailedError creates a parse failed error.
func NewParseFailedError(err error) *SafetyError {
	return &SafetyError{
		Code:    ErrCodeParseFailed,
		Message: "failed to parse source code",
		Err:     err,
	}
}

// NewDiffParseFailedError creates a diff parse failed error.
func NewDiffParseFailedError(err error) *SafetyError {
	return &SafetyError{
		Code:    ErrCodeDiffParseFailed,
		Message: "failed to parse diff",
		Err:     err,
	}
}

// NewAuditWriteFailedError creates an audit write failed error.
func NewAuditWriteFailedError(err error) *SafetyError {
	return &SafetyError{
		Code:    ErrCodeAuditWriteFailed,
		Message: "failed to write audit log",
		Err:     err,
	}
}

// NewRuleNotFoundError creates a rule not found error.
func NewRuleNotFoundError(id string) *SafetyError {
	return &SafetyError{
		Code:    ErrCodeRuleNotFound,
		Message: fmt.Sprintf("rule not found: %s", id),
	}
}

// IsPolicyNotFound checks if an error is a policy not found error.
func IsPolicyNotFound(err error) bool {
	var safetyErr *SafetyError
	if errors.As(err, &safetyErr) {
		return safetyErr.Code == ErrCodePolicyNotFound
	}
	return false
}

// IsInvalidPattern checks if an error is an invalid pattern error.
func IsInvalidPattern(err error) bool {
	var safetyErr *SafetyError
	if errors.As(err, &safetyErr) {
		return safetyErr.Code == ErrCodeInvalidPattern
	}
	return false
}