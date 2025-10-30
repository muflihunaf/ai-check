package usecase

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/example/ai-check/internal/imageprocessor"
	"github.com/example/ai-check/internal/logging"
	"github.com/example/ai-check/internal/repository"
)

// VerificationRepository defines the persistence operations needed by the use case.
type VerificationRepository interface {
	SaveLog(ctx context.Context, log *repository.VerificationLog) error
	FindByRequestIDAndUser(ctx context.Context, requestID, userID string) (*repository.VerificationLog, error)
	FindDuplicatesByHash(ctx context.Context, userID, hash, excludeRequestID string) ([]*repository.VerificationLog, error)
}

// VerificationUseCase encapsulates business logic for the verification flow.
type VerificationUseCase struct {
	repo           VerificationRepository
	cache          Cache
	processor      imageprocessor.Client
	logger         *zap.Logger
	retryAttempts  int
	initialBackoff time.Duration
	maxBackoff     time.Duration
}

type cachedVerification struct {
	RequestID string    `json:"request_id"`
	UserID    string    `json:"user_id"`
	Score     float32   `json:"score"`
	Success   bool      `json:"success"`
	Details   string    `json:"details"`
	Hash      string    `json:"sha1_hash"`
	CreatedAt time.Time `json:"created_at"`
}

// DuplicateReport represents duplicate verification entries for a request.
type DuplicateReport struct {
	Request    *repository.VerificationLog
	Duplicates []*repository.VerificationLog
}

// NewVerificationUseCase constructs a new use case instance.
func NewVerificationUseCase(repo VerificationRepository, cache Cache, processor imageprocessor.Client, logger *zap.Logger) *VerificationUseCase {
	return &VerificationUseCase{
		repo:           repo,
		cache:          cache,
		processor:      processor,
		logger:         logger.Named("verification_usecase"),
		retryAttempts:  3,
		initialBackoff: 50 * time.Millisecond,
		maxBackoff:     time.Second,
	}
}

// VerifyImage orchestrates persistence, caching, and inference calls.
func (uc *VerificationUseCase) VerifyImage(ctx context.Context, userID string, imageBytes []byte) (string, *imageprocessor.Result, error) {
	requestID := uuid.NewString()
	opLogger := logging.WithOperation(uc.logger, "usecase.verify_image", requestID)

	cacheKey := fmt.Sprintf("verification:%s", requestID)
	if err := uc.withRedisRetry(ctx, requestID, "cache.set.processing", func() error {
		return uc.cache.Set(ctx, cacheKey, "processing", time.Minute)
	}); err != nil {
		opLogger.Error("failed to set processing flag", zap.Error(err))
		return "", nil, err
	}

	result, err := uc.processor.Process(ctx, userID, imageBytes)
	if err != nil {
		wrapped := logging.NewOperationError("usecase.grpc_process_image", requestID, err)
		opLogger.Error("grpc processing failed", zap.Error(wrapped))
		return "", nil, wrapped
	}

	hash := sha1.Sum(imageBytes)
	hashHex := hex.EncodeToString(hash[:])
	log := &repository.VerificationLog{
		RequestID: requestID,
		UserID:    userID,
		Score:     result.Score,
		Success:   result.Success,
		CreatedAt: time.Now().UTC(),
		SHA1Hash:  hashHex,
	}
	details := fmt.Sprintf("status:%t score:%f hash:%s", result.Success, result.Score, hashHex)
	log.Details = details
	if err := uc.repo.SaveLog(ctx, log); err != nil {
		wrapped := logging.NewOperationError("usecase.save_log", requestID, err)
		opLogger.Error("failed to persist verification log", zap.Error(wrapped))
		return "", nil, wrapped
	}

	cached := cachedVerification{
		RequestID: requestID,
		UserID:    userID,
		Score:     log.Score,
		Success:   log.Success,
		Details:   log.Details,
		Hash:      log.SHA1Hash,
		CreatedAt: log.CreatedAt,
	}

	serialized, err := json.Marshal(cached)
	if err != nil {
		opLogger.Error("failed to serialize verification result", zap.Error(err))
		return "", nil, err
	}

	if err := uc.withRedisRetry(ctx, requestID, "cache.set.result", func() error {
		return uc.cache.Set(ctx, cacheKey, string(serialized), 5*time.Minute)
	}); err != nil {
		opLogger.Error("failed to cache verification result", zap.Error(err))
		return "", nil, err
	}

	return requestID, result, nil
}

// GetResult retrieves a cached verification outcome or loads from persistence.
func (uc *VerificationUseCase) GetResult(ctx context.Context, userID, requestID string) (*repository.VerificationLog, error) {
	cacheKey := fmt.Sprintf("verification:%s", requestID)
	if cached, err := uc.withRedisGet(ctx, requestID, "cache.get.result", cacheKey); err == nil {
		var payload cachedVerification
		if err := json.Unmarshal([]byte(cached), &payload); err != nil {
			logging.WithOperation(uc.logger, "usecase.get_result", requestID).Warn("failed to decode cached result", zap.Error(err))
		} else {
			log := &repository.VerificationLog{
				RequestID: requestID,
				UserID:    userID,
				Score:     payload.Score,
				Success:   payload.Success,
				Details:   payload.Details,
				SHA1Hash:  payload.Hash,
				CreatedAt: payload.CreatedAt,
			}
			if payload.UserID != "" {
				log.UserID = payload.UserID
			}
			if payload.RequestID != "" {
				log.RequestID = payload.RequestID
			}
			return log, nil
		}
	} else if !errors.Is(err, redis.Nil) {
		logging.WithOperation(uc.logger, "usecase.get_result", requestID).Warn("failed to read cache", zap.Error(err))
	}

	log, err := uc.repo.FindByRequestIDAndUser(ctx, requestID, userID)
	if err != nil {
		return nil, err
	}
	return log, nil
}

// GetDuplicateReport builds a duplicate detection report for a verification request.
func (uc *VerificationUseCase) GetDuplicateReport(ctx context.Context, userID, requestID string) (*DuplicateReport, error) {
	log, err := uc.repo.FindByRequestIDAndUser(ctx, requestID, userID)
	if err != nil {
		return nil, err
	}

	duplicates, err := uc.repo.FindDuplicatesByHash(ctx, userID, log.SHA1Hash, log.RequestID)
	if err != nil {
		return nil, err
	}

	return &DuplicateReport{
		Request:    log,
		Duplicates: duplicates,
	}, nil
}

func (uc *VerificationUseCase) withRedisRetry(ctx context.Context, requestID, operation string, fn func() error) error {
	if uc.retryAttempts <= 1 {
		err := fn()
		return logging.NewOperationError(operation, requestID, err)
	}

	backoff := uc.initialBackoff
	opLogger := logging.WithOperation(uc.logger, operation, requestID)
	var err error
	for attempt := 0; attempt < uc.retryAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return logging.NewOperationError(operation, requestID, ctx.Err())
			case <-time.After(backoff):
			}
			if next := backoff * 2; next <= uc.maxBackoff {
				backoff = next
			}
		}

		err = fn()
		if err == nil {
			if attempt > 0 {
				opLogger.Info("redis operation succeeded after retry", zap.Int("attempt", attempt+1))
			}
			return nil
		}

		if !isTransientError(err) || attempt == uc.retryAttempts-1 {
			opLogger.Error("redis operation failed", zap.Error(err), zap.Int("attempt", attempt+1))
			return logging.NewOperationError(operation, requestID, err)
		}

		opLogger.Warn("transient redis error", zap.Error(err), zap.Int("attempt", attempt+1))
	}
	return logging.NewOperationError(operation, requestID, err)
}

func (uc *VerificationUseCase) withRedisGet(ctx context.Context, requestID, operation, cacheKey string) (string, error) {
	var result string
	err := uc.withRedisRetry(ctx, requestID, operation, func() error {
		value, err := uc.cache.Get(ctx, cacheKey)
		if err != nil {
			return err
		}
		result = value
		return nil
	})
	if err != nil {
		return "", err
	}
	return result, nil
}

func isTransientError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	var temporary interface{ Temporary() bool }
	if errors.As(err, &temporary) && temporary.Temporary() {
		return true
	}

	return false
}
