package search

import "fmt"

const (
	ErrCodeSearchFailed   = "SEARCH_FAILED"
	ErrCodeInvalidInput   = "INVALID_INPUT"
	ErrCodeInvalidRegex   = "INVALID_REGEX"
	ErrCodeGraphNotLoaded = "GRAPH_NOT_LOADED"
)

type SearchError struct {
	Code    string
	Message string
	Err     error
}

func (e *SearchError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *SearchError) Unwrap() error {
	return e.Err
}

func NewSearchError(code, message string) *SearchError {
	return &SearchError{Code: code, Message: message}
}

func NewSearchErrorf(code, format string, args ...interface{}) *SearchError {
	return &SearchError{Code: code, Message: fmt.Sprintf(format, args...)}
}
