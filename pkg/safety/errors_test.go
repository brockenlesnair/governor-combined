package safety

import (
	"errors"
	"testing"
)

func TestSafetyError_Error_WithCause(t *testing.T) {
	err := &SafetyError{
		Code:    "TEST_CODE",
		Message: "test message",
		Err:     errors.New("root cause"),
	}
	got := err.Error()
	if got != "TEST_CODE: test message: root cause" {
		t.Errorf("got %q", got)
	}
}

func TestSafetyError_Error_WithoutCause(t *testing.T) {
	err := &SafetyError{
		Code:    "TEST_CODE",
		Message: "test message",
	}
	got := err.Error()
	if got != "TEST_CODE: test message" {
		t.Errorf("got %q", got)
	}
}

func TestSafetyError_Unwrap(t *testing.T) {
	cause := errors.New("underlying")
	err := &SafetyError{Code: "X", Message: "y", Err: cause}
	if !errors.Is(err, cause) {
		t.Error("Unwrap should return inner error")
	}
}

func TestNewPolicyNotFoundError(t *testing.T) {
	err := NewPolicyNotFoundError("strict")
	if err.Code != ErrCodePolicyNotFound {
		t.Errorf("code: got %q", err.Code)
	}
	if err.Message != "policy not found: strict" {
		t.Errorf("message: got %q", err.Message)
	}
}

func TestNewInvalidPatternError(t *testing.T) {
	err := NewInvalidPatternError("[invalid")
	if err.Code != ErrCodeInvalidPattern {
		t.Errorf("code: got %q", err.Code)
	}
}

func TestNewParseFailedError(t *testing.T) {
	inner := errors.New("bad syntax")
	err := NewParseFailedError(inner)
	if err.Code != ErrCodeParseFailed {
		t.Errorf("code: got %q", err.Code)
	}
	if !errors.Is(err, inner) {
		t.Error("should wrap inner error")
	}
}

func TestNewDiffParseFailedError(t *testing.T) {
	inner := errors.New("bad diff")
	err := NewDiffParseFailedError(inner)
	if err.Code != ErrCodeDiffParseFailed {
		t.Errorf("code: got %q", err.Code)
	}
}

func TestNewAuditWriteFailedError(t *testing.T) {
	inner := errors.New("disk full")
	err := NewAuditWriteFailedError(inner)
	if err.Code != ErrCodeAuditWriteFailed {
		t.Errorf("code: got %q", err.Code)
	}
}

func TestNewRuleNotFoundError(t *testing.T) {
	err := NewRuleNotFoundError("GO999")
	if err.Code != ErrCodeRuleNotFound {
		t.Errorf("code: got %q", err.Code)
	}
}

func TestIsPolicyNotFound(t *testing.T) {
	if !IsPolicyNotFound(NewPolicyNotFoundError("x")) {
		t.Error("should be policy not found")
	}
	if IsPolicyNotFound(NewInvalidPatternError("x")) {
		t.Error("should not match different code")
	}
	if IsPolicyNotFound(errors.New("generic")) {
		t.Error("should not match generic error")
	}
}

func TestIsInvalidPattern(t *testing.T) {
	if !IsInvalidPattern(NewInvalidPatternError("x")) {
		t.Error("should be invalid pattern")
	}
	if IsInvalidPattern(NewPolicyNotFoundError("x")) {
		t.Error("should not match different code")
	}
}

func TestErrorCodesAreDefined(t *testing.T) {
	codes := []string{
		ErrCodePolicyNotFound,
		ErrCodeInvalidPattern,
		ErrCodeParseFailed,
		ErrCodeDiffParseFailed,
		ErrCodeAuditWriteFailed,
		ErrCodeRuleNotFound,
		ErrCodeLanguageNotSupported,
		ErrCodeFileTooLarge,
		ErrCodeValidationFailed,
		ErrCodeTimeout,
	}
	for _, c := range codes {
		if c == "" {
			t.Error("empty error code constant")
		}
	}
}
