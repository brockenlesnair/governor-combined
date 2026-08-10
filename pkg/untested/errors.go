package untested

import "fmt"

// ErrorCode identifies the kind of error that occurred during detection.
type ErrorCode string

const (
	ErrCodeUntestedFailed ErrorCode = "UNTESTED_FAILED"
	ErrCodeGraphEmpty     ErrorCode = "GRAPH_EMPTY"
)

// UntestedError is a typed error returned by the untested detector.
type UntestedError struct {
	Code    ErrorCode
	Message string
	Err     error
}

func (e *UntestedError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *UntestedError) Unwrap() error {
	return e.Err
}

// NewError creates a new UntestedError without a wrapped error.
func NewError(code ErrorCode, msg string) *UntestedError {
	return &UntestedError{
		Code:    code,
		Message: msg,
	}
}

// NewErrorWrap creates a new UntestedError that wraps an existing error.
func NewErrorWrap(code ErrorCode, msg string, err error) *UntestedError {
	return &UntestedError{
		Code:    code,
		Message: msg,
		Err:     err,
	}
}
