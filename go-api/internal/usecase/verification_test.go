package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"

	"github.com/example/ai-check/internal/imageprocessor"
	"github.com/example/ai-check/internal/logging"
	"github.com/example/ai-check/internal/repository"
)

type stubRepository struct {
	savedLogs []*repository.VerificationLog
	saveErr   error
	findLog   *repository.VerificationLog
	findErr   error
	findCalls int
}

func (s *stubRepository) SaveLog(ctx context.Context, log *repository.VerificationLog) error {
	s.savedLogs = append(s.savedLogs, log)
	return s.saveErr
}

func (s *stubRepository) FindByRequestIDAndUser(ctx context.Context, requestID, userID string) (*repository.VerificationLog, error) {
	s.findCalls++
	if s.findErr != nil {
		return nil, s.findErr
	}
	if s.findLog != nil {
		return s.findLog, nil
	}
	return nil, errors.New("not found")
}

type stubCache struct {
	setErrs   []error
	getErrs   []error
	getValues []string
	setKeys   []string
	getKeys   []string
}

func (s *stubCache) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	s.setKeys = append(s.setKeys, key)
	if len(s.setErrs) == 0 {
		return nil
	}
	err := s.setErrs[0]
	s.setErrs = s.setErrs[1:]
	return err
}

func (s *stubCache) Get(ctx context.Context, key string) (string, error) {
	s.getKeys = append(s.getKeys, key)
	var value string
	if len(s.getValues) > 0 {
		value = s.getValues[0]
		s.getValues = s.getValues[1:]
	}
	var err error
	if len(s.getErrs) > 0 {
		err = s.getErrs[0]
		s.getErrs = s.getErrs[1:]
	}
	return value, err
}

type stubProcessor struct {
	result *imageprocessor.Result
	err    error
}

func (s *stubProcessor) Process(ctx context.Context, userID string, imageBytes []byte) (*imageprocessor.Result, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

type transientRedisError struct{}

func (transientRedisError) Error() string   { return "redis transient" }
func (transientRedisError) Timeout() bool   { return true }
func (transientRedisError) Temporary() bool { return true }

func TestVerifyImageRetriesRedisSet(t *testing.T) {
	cache := &stubCache{setErrs: []error{transientRedisError{}}}
	repo := &stubRepository{}
	client := &stubProcessor{result: &imageprocessor.Result{Success: true, Score: 0.9}}
	uc := NewVerificationUseCase(repo, cache, client, zap.NewNop())

	_, resp, err := uc.VerifyImage(context.Background(), "user-1", []byte("image"))
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success response, got %v", resp.Success)
	}
	if len(cache.setKeys) < 3 {
		t.Fatalf("expected at least 3 cache set calls (retry + result), got %d", len(cache.setKeys))
	}
	if cache.setKeys[0] != cache.setKeys[1] {
		t.Fatalf("expected retry to target same key, got %s and %s", cache.setKeys[0], cache.setKeys[1])
	}
	if len(repo.savedLogs) != 1 {
		t.Fatalf("expected log to be saved, got %d entries", len(repo.savedLogs))
	}
}

func TestVerifyImageReturnsOperationErrorOnCacheFailure(t *testing.T) {
	cache := &stubCache{setErrs: []error{errors.New("boom")}}
	repo := &stubRepository{}
	client := &stubProcessor{result: &imageprocessor.Result{Success: true}}
	uc := NewVerificationUseCase(repo, cache, client, zap.NewNop())

	_, _, err := uc.VerifyImage(context.Background(), "user-1", []byte("image"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var opErr *logging.OperationError
	if !errors.As(err, &opErr) {
		t.Fatalf("expected OperationError, got %T", err)
	}
	if opErr.Operation != "cache.set.processing" {
		t.Fatalf("unexpected operation: %s", opErr.Operation)
	}
}

func TestGetResultFallsBackToRepositoryWhenCacheMiss(t *testing.T) {
	cache := &stubCache{getErrs: []error{redis.Nil}}
	expected := &repository.VerificationLog{RequestID: "req", UserID: "user", Details: "from-db"}
	repo := &stubRepository{findLog: expected}
	uc := NewVerificationUseCase(repo, cache, &stubProcessor{result: &imageprocessor.Result{}}, zap.NewNop())

	log, err := uc.GetResult(context.Background(), "user", "req")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if log != expected {
		t.Fatalf("expected %+v, got %+v", expected, log)
	}
	if repo.findCalls != 1 {
		t.Fatalf("expected repository to be queried once, got %d", repo.findCalls)
	}
}
