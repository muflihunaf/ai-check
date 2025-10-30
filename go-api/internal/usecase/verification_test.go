package usecase

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
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
	savedLogs  []*repository.VerificationLog
	saveErr    error
	findLog    *repository.VerificationLog
	findErr    error
	findCalls  int
	duplicates []*repository.VerificationLog
	dupErr     error
	metrics    *repository.MetricsAggregation
	metricsErr error
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

func (s *stubRepository) FindDuplicatesByHash(ctx context.Context, userID, hash, excludeRequestID string) ([]*repository.VerificationLog, error) {
	if s.dupErr != nil {
		return nil, s.dupErr
	}
	return s.duplicates, nil
}

func (s *stubRepository) AggregateMetrics(ctx context.Context) (*repository.MetricsAggregation, error) {
	if s.metricsErr != nil {
		return nil, s.metricsErr
	}
	if s.metrics == nil {
		return &repository.MetricsAggregation{}, nil
	}
	return s.metrics, nil
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
	expectedHash := sha1.Sum([]byte("image"))
	if repo.savedLogs[0].SHA1Hash != hex.EncodeToString(expectedHash[:]) {
		t.Fatalf("expected hash %x, got %s", expectedHash, repo.savedLogs[0].SHA1Hash)
	}
	if repo.savedLogs[0].ProcessingLatencyMs <= 0 {
		t.Fatalf("expected processing latency to be recorded, got %f", repo.savedLogs[0].ProcessingLatencyMs)
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
	expected := &repository.VerificationLog{RequestID: "req", UserID: "user", Details: "from-db", SHA1Hash: "abc"}
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

func TestGetResultReturnsCachedPayload(t *testing.T) {
	createdAt := time.Date(2024, time.January, 15, 10, 30, 0, 0, time.UTC)
	payload := cachedVerification{
		RequestID: "req-123",
		UserID:    "user-42",
		Score:     0.88,
		Success:   true,
		Details:   "cached-details",
		Hash:      "abc123",
		CreatedAt: createdAt,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	cache := &stubCache{getValues: []string{string(data)}}
	repo := &stubRepository{}
	uc := NewVerificationUseCase(repo, cache, &stubProcessor{result: &imageprocessor.Result{}}, zap.NewNop())

	log, err := uc.GetResult(context.Background(), "user-42", "req-123")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if repo.findCalls != 0 {
		t.Fatalf("expected repository not to be queried, got %d calls", repo.findCalls)
	}
	if log.RequestID != payload.RequestID {
		t.Fatalf("expected request id %s, got %s", payload.RequestID, log.RequestID)
	}
	if log.UserID != payload.UserID {
		t.Fatalf("expected user id %s, got %s", payload.UserID, log.UserID)
	}
	if log.Score != payload.Score {
		t.Fatalf("expected score %f, got %f", payload.Score, log.Score)
	}
	if log.Success != payload.Success {
		t.Fatalf("expected success %t, got %t", payload.Success, log.Success)
	}
	if log.Details != payload.Details {
		t.Fatalf("expected details %s, got %s", payload.Details, log.Details)
	}
	if log.SHA1Hash != payload.Hash {
		t.Fatalf("expected hash %s, got %s", payload.Hash, log.SHA1Hash)
	}
	if !log.CreatedAt.Equal(payload.CreatedAt) {
		t.Fatalf("expected created_at %s, got %s", payload.CreatedAt, log.CreatedAt)
	}
}

func TestGetMetricsSummaryComputesSuccessRate(t *testing.T) {
	repo := &stubRepository{metrics: &repository.MetricsAggregation{
		TotalCount:                 5,
		SuccessCount:               3,
		AverageScore:               0.72,
		AverageProcessingLatencyMs: 125.5,
	}}
	uc := NewVerificationUseCase(repo, &stubCache{}, &stubProcessor{result: &imageprocessor.Result{}}, zap.NewNop())

	summary, err := uc.GetMetricsSummary(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if summary.TotalRequests != 5 {
		t.Fatalf("expected total 5, got %d", summary.TotalRequests)
	}
	if summary.SuccessfulRequests != 3 {
		t.Fatalf("expected successes 3, got %d", summary.SuccessfulRequests)
	}
	expectedRate := 0.6
	if summary.SuccessRate != expectedRate {
		t.Fatalf("expected success rate %.2f, got %.2f", expectedRate, summary.SuccessRate)
	}
	if summary.AverageScore != 0.72 {
		t.Fatalf("expected average score 0.72, got %.2f", summary.AverageScore)
	}
	if summary.AverageProcessingLatencyMs != 125.5 {
		t.Fatalf("expected latency 125.5, got %.2f", summary.AverageProcessingLatencyMs)
	}
}

func TestGetMetricsSummaryPropagatesRepositoryError(t *testing.T) {
	repo := &stubRepository{metricsErr: errors.New("db down")}
	uc := NewVerificationUseCase(repo, &stubCache{}, &stubProcessor{result: &imageprocessor.Result{}}, zap.NewNop())

	_, err := uc.GetMetricsSummary(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
