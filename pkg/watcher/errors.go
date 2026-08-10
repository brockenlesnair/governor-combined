package watcher

import "fmt"

// ErrorCode categorises watcher failures.
type ErrorCode string

const (
	ErrCodeWatchFailed    ErrorCode = "WATCH_FAILED"
	ErrCodePathNotFound   ErrorCode = "PATH_NOT_FOUND"
	ErrCodeAlreadyRunning ErrorCode = "ALREADY_RUNNING"
	ErrCodeNotRunning     ErrorCode = "NOT_RUNNING"
	ErrCodeConfigInvalid  ErrorCode = "CONFIG_INVALID"
)

// WatcherError is a structured error returned by the watcher.
type WatcherError struct {
	Code    ErrorCode
	Message string
	Path    string
	Err     error
}

func (e *WatcherError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("[%s] %s (path=%s): %v", e.Code, e.Message, e.Path, e.Err)
	}
	return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
}

func (e *WatcherError) Unwrap() error {
	return e.Err
}

// newError is a convenience constructor for WatcherError.
func newError(code ErrorCode, msg, path string, err error) *WatcherError {
	return &WatcherError{
		Code:    code,
		Message: msg,
		Path:    path,
		Err:     err,
	}
}
