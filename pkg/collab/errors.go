package collab

import "fmt"

// ErrorCode identifies the type of collaboration error.
type ErrorCode string

const (
	ErrCodeLockDenied    ErrorCode = "LOCK_DENIED"
	ErrCodeLockTimeout   ErrorCode = "LOCK_TIMEOUT"
	ErrCodeLockExpired   ErrorCode = "LOCK_EXPIRED"
	ErrCodeFileNotFound  ErrorCode = "FILE_NOT_FOUND"
	ErrCodeShadowExists  ErrorCode = "SHADOW_EXISTS"
	ErrCodeMergeConflict ErrorCode = "MERGE_CONFLICT"
	ErrCodeNotLocked     ErrorCode = "NOT_LOCKED"
	ErrCodeInvalidAgent  ErrorCode = "INVALID_AGENT"
)

// CollabError is a collaboration-specific error.
type CollabError struct {
	Code    ErrorCode
	Message string
	Path    string
	AgentID AgentID
	Err     error
}

func (e *CollabError) Error() string {
	if e.Path != "" && e.AgentID != "" {
		return fmt.Sprintf("%s: %s [path=%s agent=%s]", e.Code, e.Message, e.Path, e.AgentID)
	}
	if e.Path != "" {
		return fmt.Sprintf("%s: %s [path=%s]", e.Code, e.Message, e.Path)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *CollabError) Unwrap() error {
	return e.Err
}
