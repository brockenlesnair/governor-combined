package deadcode

import "fmt"

// ErrorCode represents a dead code error code.
type ErrorCode string

const (
	ErrCodeDeadCodeFailed ErrorCode = "DEADCODE_FAILED"
	ErrCodeGraphEmpty     ErrorCode = "GRAPH_EMPTY"
)

// DeadCodeError is a custom error type for dead code detection.
type DeadCodeError struct {
	Code    ErrorCode
	Message string
	Err     error
}

func (e *DeadCodeError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *DeadCodeError) Unwrap() error {
	return e.Err
}

// NewError creates a new DeadCodeError without a wrapped error.
func NewError(code ErrorCode, msg string) *DeadCodeError {
	return &DeadCodeError{
		Code:    code,
		Message: msg,
	}
}

// NewErrorWrap creates a new DeadCodeError wrapping an underlying error.
func NewErrorWrap(code ErrorCode, msg string, err error) *DeadCodeError {
	return &DeadCodeError{
		Code:    code,
		Message: msg,
		Err:     err,
	}
}
