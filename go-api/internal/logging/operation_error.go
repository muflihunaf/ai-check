package logging

import "fmt"

// OperationError annotates an error with operation metadata.
type OperationError struct {
	Operation string
	RequestID string
	Err       error
}

// Error implements the error interface.
func (e *OperationError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	if e.RequestID != "" {
		return fmt.Sprintf("%s (request_id=%s): %v", e.Operation, e.RequestID, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Operation, e.Err)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *OperationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// NewOperationError wraps an error with structured context about where it occurred.
func NewOperationError(operation, requestID string, err error) error {
	if err == nil {
		return nil
	}
	return &OperationError{Operation: operation, RequestID: requestID, Err: err}
}
