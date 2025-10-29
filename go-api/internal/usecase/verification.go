package usecase

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"

    "github.com/example/ai-check/go-api/internal/repository"
    proto "github.com/example/ai-check/go-api/proto"
)

// VerificationUseCase encapsulates business logic for the verification flow.
type VerificationUseCase struct {
	repo       *repository.VerificationRepository
	cache      *redis.Client
	grpcClient proto.ImageProcessorClient
}

// NewVerificationUseCase constructs a new use case instance.
func NewVerificationUseCase(repo *repository.VerificationRepository, cache *redis.Client, grpcClient proto.ImageProcessorClient) *VerificationUseCase {
	return &VerificationUseCase{repo: repo, cache: cache, grpcClient: grpcClient}
}

// VerifyImage orchestrates persistence, caching, and inference calls.
func (uc *VerificationUseCase) VerifyImage(ctx context.Context, userID string, imageBytes []byte) (string, *proto.VerifyResponse, error) {
	requestID := uuid.NewString()

	cacheKey := fmt.Sprintf("verification:%s", requestID)
	if err := uc.cache.Set(ctx, cacheKey, "processing", time.Minute).Err(); err != nil {
		return "", nil, fmt.Errorf("cache set: %w", err)
	}

	grpcResp, err := uc.grpcClient.ProcessImage(ctx, &proto.VerifyRequest{UserId: userID, ImageData: imageBytes})
	if err != nil {
		return "", nil, fmt.Errorf("grpc call: %w", err)
	}

	hash := sha1.Sum(imageBytes)
	log := &repository.VerificationLog{
		RequestID: requestID,
		UserID:    userID,
		Score:     grpcResp.GetScore(),
		Success:   grpcResp.GetSuccess(),
		CreatedAt: time.Now().UTC(),
	}
	details := fmt.Sprintf("status:%t score:%f hash:%s", grpcResp.GetSuccess(), grpcResp.GetScore(), hex.EncodeToString(hash[:]))
	log.Details = details
	if err := uc.repo.SaveLog(ctx, log); err != nil {
		return "", nil, fmt.Errorf("save log: %w", err)
	}

	if err := uc.cache.Set(ctx, cacheKey, details, 5*time.Minute).Err(); err != nil {
		return "", nil, fmt.Errorf("cache update: %w", err)
	}

	return requestID, grpcResp, nil
}

// GetResult retrieves a cached verification outcome or loads from persistence.
func (uc *VerificationUseCase) GetResult(ctx context.Context, userID, requestID string) (*repository.VerificationLog, error) {
	cacheKey := fmt.Sprintf("verification:%s", requestID)
	if cached, err := uc.cache.Get(ctx, cacheKey).Result(); err == nil {
		return &repository.VerificationLog{RequestID: requestID, UserID: userID, Details: cached}, nil
	}

	log, err := uc.repo.FindByRequestIDAndUser(ctx, requestID, userID)
	if err != nil {
		return nil, err
	}
	return log, nil
}
