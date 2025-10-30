package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/example/ai-check/internal/logging"
)

type transientTestError struct{}

func (transientTestError) Error() string   { return "transient" }
func (transientTestError) Timeout() bool   { return true }
func (transientTestError) Temporary() bool { return true }

func TestExecuteWithRetryRetriesTransientErrors(t *testing.T) {
	repo := &VerificationRepository{
		logger:         zap.NewNop(),
		retryAttempts:  3,
		initialBackoff: time.Millisecond,
		maxBackoff:     2 * time.Millisecond,
	}

	attempts := 0
	err := repo.executeWithRetry(context.Background(), "test.operation", "req-1", func() error {
		attempts++
		if attempts < 2 {
			return transientTestError{}
		}
		return nil
	})

	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestExecuteWithRetryReturnsOperationError(t *testing.T) {
	repo := &VerificationRepository{
		logger:         zap.NewNop(),
		retryAttempts:  2,
		initialBackoff: time.Millisecond,
		maxBackoff:     2 * time.Millisecond,
	}

	attempts := 0
	err := repo.executeWithRetry(context.Background(), "test.operation", "req-2", func() error {
		attempts++
		return errors.New("boom")
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}

	var opErr *logging.OperationError
	if !errors.As(err, &opErr) {
		t.Fatalf("expected OperationError, got %T", err)
	}
	if opErr.Operation != "test.operation" {
		t.Fatalf("unexpected operation: %s", opErr.Operation)
	}
	if opErr.RequestID != "req-2" {
		t.Fatalf("unexpected request id: %s", opErr.RequestID)
	}
}
