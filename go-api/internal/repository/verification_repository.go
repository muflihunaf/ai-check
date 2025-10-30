package repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/example/ai-check/internal/logging"
)

// VerificationLog represents a persisted verification request.
type VerificationLog struct {
	ID        uint      `gorm:"primaryKey"`
	RequestID string    `gorm:"column:request_id;uniqueIndex;size:64"`
	UserID    string    `gorm:"column:user_id;size:64"`
	SHA1Hash  string    `gorm:"column:sha1_hash;size:40;not null;index;uniqueIndex:idx_verification_logs_user_hash"`
	Score     float32   `gorm:"column:score"`
	Success   bool      `gorm:"column:success"`
	Details   string    `gorm:"column:details;type:text"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName overrides the default table name.
func (VerificationLog) TableName() string {
	return "verification_logs"
}

// VerificationRepository provides persistence APIs for verification logs.
type VerificationRepository struct {
	db             *gorm.DB
	logger         *zap.Logger
	retryAttempts  int
	initialBackoff time.Duration
	maxBackoff     time.Duration
}

// NewVerificationRepository creates a new repository instance.
func NewVerificationRepository(db *gorm.DB, logger *zap.Logger) *VerificationRepository {
	return &VerificationRepository{
		db:             db,
		logger:         logger.Named("verification_repository"),
		retryAttempts:  3,
		initialBackoff: 100 * time.Millisecond,
		maxBackoff:     2 * time.Second,
	}
}

// AutoMigrate ensures the schema is available.
func (r *VerificationRepository) AutoMigrate(ctx context.Context) error {
	return r.executeWithRetry(ctx, "repository.automigrate", "", func() error {
		return r.db.WithContext(ctx).AutoMigrate(&VerificationLog{})
	})
}

// SaveLog persists a verification log entry.
func (r *VerificationRepository) SaveLog(ctx context.Context, log *VerificationLog) error {
	requestID := log.RequestID
	return r.executeWithRetry(ctx, "repository.save_log", requestID, func() error {
		return r.db.WithContext(ctx).Create(log).Error
	})
}

// FindByRequestIDAndUser retrieves a verification log matching the request and owner.
func (r *VerificationRepository) FindByRequestIDAndUser(ctx context.Context, requestID, userID string) (*VerificationLog, error) {
	var log VerificationLog
	err := r.executeWithRetry(ctx, "repository.find_by_request_and_user", requestID, func() error {
		return r.db.WithContext(ctx).First(&log, "request_id = ? AND user_id = ?", requestID, userID).Error
	})
	if err != nil {
		return nil, err
	}
	return &log, nil
}

// FindDuplicatesByHash retrieves verification logs that share the same hash.
func (r *VerificationRepository) FindDuplicatesByHash(ctx context.Context, userID, hash, excludeRequestID string) ([]*VerificationLog, error) {
	var logs []*VerificationLog
	err := r.executeWithRetry(ctx, "repository.find_duplicates_by_hash", excludeRequestID, func() error {
		query := r.db.WithContext(ctx).Where("sha1_hash = ?", hash)
		if userID != "" {
			query = query.Where("user_id = ?", userID)
		}
		if excludeRequestID != "" {
			query = query.Where("request_id <> ?", excludeRequestID)
		}
		return query.Order("created_at DESC").Find(&logs).Error
	})
	if err != nil {
		return nil, err
	}
	return logs, nil
}

func (r *VerificationRepository) executeWithRetry(ctx context.Context, operation, requestID string, fn func() error) error {
	if r.retryAttempts <= 1 {
		return fn()
	}

	backoff := r.initialBackoff
	opLogger := logging.WithOperation(r.logger, operation, requestID)
	var err error
	for attempt := 0; attempt < r.retryAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return logging.NewOperationError(operation, requestID, ctx.Err())
			case <-time.After(backoff):
			}
			if next := backoff * 2; next <= r.maxBackoff {
				backoff = next
			}
		}

		err = fn()
		if err == nil {
			if attempt > 0 {
				opLogger.Info("operation succeeded after retry", zap.Int("attempt", attempt+1))
			}
			return nil
		}

		if !isTransientError(err) || attempt == r.retryAttempts-1 {
			opLogger.Error("operation failed", zap.Error(err), zap.Int("attempt", attempt+1))
			return logging.NewOperationError(operation, requestID, err)
		}

		opLogger.Warn("transient error encountered", zap.Error(err), zap.Int("attempt", attempt+1))
	}
	return logging.NewOperationError(operation, requestID, err)
}

func isTransientError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	if errors.Is(err, driver.ErrBadConn) {
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
