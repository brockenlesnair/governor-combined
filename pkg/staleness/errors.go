package staleness

import "fmt"

// ErrorCode represents a category of staleness error.
type ErrorCode string

const (
	ErrCodeScanFailed   ErrorCode = "SCAN_FAILED"
	ErrCodePathNotFound ErrorCode = "PATH_NOT_FOUND"
	ErrCodeConfigInvalid ErrorCode = "CONFIG_INVALID"
)

// StalenessError is the error type returned by staleness operations.
type StalenessError struct {
	Code    ErrorCode
	Message string
	Path    string
	Err     error
}

func (e *StalenessError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("[%s] %s (path: %s)", e.Code, e.Message, e.Path)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *StalenessError) Unwrap() error {
	return e.Err
}

// NewScanError creates a scan failure error.
func NewScanError(msg string, path string, err error) *StalenessError {
	return &StalenessError{
		Code:    ErrCodeScanFailed,
		Message: msg,
		Path:    path,
		Err:     err,
	}
}

// NewPathError creates a path-not-found error.
func NewPathError(path string, err error) *StalenessError {
	return &StalenessError{
		Code:    ErrCodePathNotFound,
		Message: "path not found",
		Path:    path,
		Err:     err,
	}
}

// NewConfigError creates a configuration error.
func NewConfigError(msg string, err error) *StalenessError {
	return &StalenessError{
		Code:    ErrCodeConfigInvalid,
		Message: msg,
		Err:     err,
	}
}
