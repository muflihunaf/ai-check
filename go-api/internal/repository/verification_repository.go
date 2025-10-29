package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// VerificationLog represents a persisted verification request.
type VerificationLog struct {
	ID        uint      `gorm:"primaryKey"`
	RequestID string    `gorm:"column:request_id;uniqueIndex;size:64"`
	UserID    string    `gorm:"column:user_id;size:64"`
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
	db *gorm.DB
}

// NewVerificationRepository creates a new repository instance.
func NewVerificationRepository(db *gorm.DB) *VerificationRepository {
	return &VerificationRepository{db: db}
}

// AutoMigrate ensures the schema is available.
func (r *VerificationRepository) AutoMigrate(ctx context.Context) error {
	return r.db.WithContext(ctx).AutoMigrate(&VerificationLog{})
}

// SaveLog persists a verification log entry.
func (r *VerificationRepository) SaveLog(ctx context.Context, log *VerificationLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

// FindByRequestIDAndUser retrieves a verification log matching the request and owner.
func (r *VerificationRepository) FindByRequestIDAndUser(ctx context.Context, requestID, userID string) (*VerificationLog, error) {
	var log VerificationLog
	if err := r.db.WithContext(ctx).First(&log, "request_id = ? AND user_id = ?", requestID, userID).Error; err != nil {
		return nil, err
	}
	return &log, nil
}
