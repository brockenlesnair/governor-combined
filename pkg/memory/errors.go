package memory

import (
	"errors"
	"fmt"
)

// MemoryError is a memory-specific error.
type MemoryError struct {
	Code    string
	Message string
	Err     error
}

func (e *MemoryError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *MemoryError) Unwrap() error {
	return e.Err
}

// Error codes
const (
	ErrCodeNotFound        = "MEMORY_NOT_FOUND"
	ErrCodeAlreadyExists   = "MEMORY_ALREADY_EXISTS"
	ErrCodeInvalidInput    = "MEMORY_INVALID_INPUT"
	ErrCodeStorageError    = "MEMORY_STORAGE_ERROR"
	ErrCodeEmbeddingError  = "MEMORY_EMBEDDING_ERROR"
	ErrCodeSearchError     = "MEMORY_SEARCH_ERROR"
	ErrCodeDecayError      = "MEMORY_DECAY_ERROR"
	ErrCodeClosed          = "MEMORY_CLOSED"
	ErrCodeModelNotFound   = "MEMORY_MODEL_NOT_FOUND"
	ErrCodePruneError      = "MEMORY_PRUNE_ERROR"
)

// NewNotFoundError creates a not found error.
func NewNotFoundError(id string) *MemoryError {
	return &MemoryError{
		Code:    ErrCodeNotFound,
		Message: fmt.Sprintf("entity not found: %s", id),
	}
}

// NewAlreadyExistsError creates an already exists error.
func NewAlreadyExistsError(id string) *MemoryError {
	return &MemoryError{
		Code:    ErrCodeAlreadyExists,
		Message: fmt.Sprintf("entity already exists: %s", id),
	}
}

// NewInvalidInputError creates an invalid input error.
func NewInvalidInputError(msg string) *MemoryError {
	return &MemoryError{
		Code:    ErrCodeInvalidInput,
		Message: msg,
	}
}

// NewStorageError creates a storage error.
func NewStorageError(err error) *MemoryError {
	return &MemoryError{
		Code:    ErrCodeStorageError,
		Message: "storage operation failed",
		Err:     err,
	}
}

// NewEmbeddingError creates an embedding error.
func NewEmbeddingError(err error) *MemoryError {
	return &MemoryError{
		Code:    ErrCodeEmbeddingError,
		Message: "embedding generation failed",
		Err:     err,
	}
}

// NewSearchError creates a search error.
func NewSearchError(err error) *MemoryError {
	return &MemoryError{
		Code:    ErrCodeSearchError,
		Message: "search operation failed",
		Err:     err,
	}
}

// NewClosedError creates a closed error.
func NewClosedError() *MemoryError {
	return &MemoryError{
		Code:    ErrCodeClosed,
		Message: "memory system is closed",
	}
}

// IsNotFound checks if an error is a not found error.
func IsNotFound(err error) bool {
	var memErr *MemoryError
	if errors.As(err, &memErr) {
		return memErr.Code == ErrCodeNotFound
	}
	return false
}

// IsClosed checks if an error is a closed error.
func IsClosed(err error) bool {
	var memErr *MemoryError
	if errors.As(err, &memErr) {
		return memErr.Code == ErrCodeClosed
	}
	return false
}