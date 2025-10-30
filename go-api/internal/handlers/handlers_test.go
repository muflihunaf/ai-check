package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"github.com/example/ai-check/internal/auth"
	"github.com/example/ai-check/internal/imageprocessor"
	"github.com/example/ai-check/internal/repository"
	"github.com/example/ai-check/internal/usecase"
)

const testJWTSecret = "test-secret"

func TestVerifyRejectsLargeUpload(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.MaxMultipartMemory = MaxUploadSize

	uc := &usecase.VerificationUseCase{}
	RegisterRoutes(router, uc, auth.JWTMiddleware(testJWTSecret, ""))

	token := buildTestToken(t, "user-123")
	body, contentType := buildMultipartBody(t, "image/png", bytes.Repeat([]byte("a"), MaxUploadSize+1))

	req := httptest.NewRequest(http.MethodPost, "/verify", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+token)

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status %d, got %d", http.StatusRequestEntityTooLarge, resp.Code)
	}
}

func TestVerifyRejectsUnsupportedContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.MaxMultipartMemory = MaxUploadSize

	uc := &usecase.VerificationUseCase{}
	RegisterRoutes(router, uc, auth.JWTMiddleware(testJWTSecret, ""))

	token := buildTestToken(t, "user-123")
	body, contentType := buildMultipartBody(t, "text/plain", []byte("hello"))

	req := httptest.NewRequest(http.MethodPost, "/verify", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+token)

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected status %d, got %d", http.StatusUnsupportedMediaType, resp.Code)
	}
}

func TestMetricsSummaryReturnsAggregates(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.MaxMultipartMemory = MaxUploadSize

	uc := usecase.NewVerificationUseCase(&metricsStubRepository{}, &metricsStubCache{}, &metricsStubProcessor{}, zap.NewNop())
	RegisterRoutes(router, uc, auth.JWTMiddleware(testJWTSecret, ""))

	token := buildTestToken(t, "metrics-user")
	req := httptest.NewRequest(http.MethodGet, "/metrics/summary", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.Code)
	}

	var payload struct {
		TotalRequests              int64   `json:"total_requests"`
		SuccessfulRequests         int64   `json:"successful_requests"`
		SuccessRate                float64 `json:"success_rate"`
		AverageScore               float64 `json:"average_score"`
		AverageProcessingLatencyMs float64 `json:"average_processing_latency_ms"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if payload.TotalRequests != 4 {
		t.Fatalf("expected total 4, got %d", payload.TotalRequests)
	}
	if payload.SuccessfulRequests != 3 {
		t.Fatalf("expected successes 3, got %d", payload.SuccessfulRequests)
	}
	if payload.SuccessRate != 0.75 {
		t.Fatalf("expected success rate 0.75, got %.2f", payload.SuccessRate)
	}
	if payload.AverageScore != 0.82 {
		t.Fatalf("expected average score 0.82, got %.2f", payload.AverageScore)
	}
	if payload.AverageProcessingLatencyMs != 87.5 {
		t.Fatalf("expected latency 87.5, got %.2f", payload.AverageProcessingLatencyMs)
	}
}

func TestVerifyEndpointReturnsMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &verifyStubRepository{}
	cache := &verifyStubCache{}
	processor := &verifyStubProcessor{result: &imageprocessor.Result{Success: true, Score: 0.91, Message: "accepted"}}
	uc := usecase.NewVerificationUseCase(repo, cache, processor, zap.NewNop())

	router := gin.New()
	router.MaxMultipartMemory = MaxUploadSize
	RegisterRoutes(router, uc, auth.JWTMiddleware(testJWTSecret, ""))

	token := buildTestToken(t, "metadata-user")
	body, contentType := buildMultipartBody(t, "image/png", []byte("payload"))

	req := httptest.NewRequest(http.MethodPost, "/verify", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+token)

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.Code)
	}

	var payload struct {
		RequestID string    `json:"request_id"`
		Verified  bool      `json:"verified"`
		Score     float32   `json:"score"`
		Message   string    `json:"message"`
		CreatedAt time.Time `json:"created_at"`
		Metadata  struct {
			Timestamp time.Time `json:"timestamp"`
			Success   bool      `json:"success"`
			Score     float32   `json:"score"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if payload.RequestID == "" {
		t.Fatal("expected request id to be returned")
	}
	if payload.Message != "accepted" {
		t.Fatalf("expected message 'accepted', got %q", payload.Message)
	}
	if payload.Metadata.Timestamp.IsZero() {
		t.Fatal("expected metadata timestamp to be set")
	}
	if payload.Metadata.Score != payload.Score {
		t.Fatalf("expected metadata score %.2f to match payload score %.2f", payload.Metadata.Score, payload.Score)
	}
	if payload.Metadata.Success != payload.Verified {
		t.Fatalf("expected metadata success to match verified flag")
	}
	if payload.CreatedAt.IsZero() {
		t.Fatal("expected created_at to be set")
	}
}

func buildMultipartBody(t *testing.T, contentType string, payload []byte) (*bytes.Buffer, string) {
	t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="image"; filename="upload"`)
	header.Set("Content-Type", contentType)

	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("failed to create multipart part: %v", err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatalf("failed to write payload: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close writer: %v", err)
	}

	return body, writer.FormDataContentType()
}

func buildTestToken(t *testing.T, subject string) string {
	t.Helper()

	claims := jwt.RegisteredClaims{
		Subject:   subject,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return signed
}

type metricsStubRepository struct{}

func (metricsStubRepository) SaveLog(ctx context.Context, log *repository.VerificationLog) error {
	return nil
}
func (metricsStubRepository) FindByRequestIDAndUser(ctx context.Context, requestID, userID string) (*repository.VerificationLog, error) {
	return nil, errors.New("not implemented")
}
func (metricsStubRepository) FindDuplicatesByHash(ctx context.Context, userID, hash, excludeRequestID string) ([]*repository.VerificationLog, error) {
	return nil, errors.New("not implemented")
}
func (metricsStubRepository) AggregateMetrics(ctx context.Context) (*repository.MetricsAggregation, error) {
	return &repository.MetricsAggregation{
		TotalCount:                 4,
		SuccessCount:               3,
		AverageScore:               0.82,
		AverageProcessingLatencyMs: 87.5,
	}, nil
}

type verifyStubRepository struct{}

func (verifyStubRepository) SaveLog(ctx context.Context, log *repository.VerificationLog) error {
	return nil
}

func (verifyStubRepository) FindByRequestIDAndUser(ctx context.Context, requestID, userID string) (*repository.VerificationLog, error) {
	return nil, errors.New("not implemented")
}

func (verifyStubRepository) FindDuplicatesByHash(ctx context.Context, userID, hash, excludeRequestID string) ([]*repository.VerificationLog, error) {
	return nil, errors.New("not implemented")
}

func (verifyStubRepository) AggregateMetrics(ctx context.Context) (*repository.MetricsAggregation, error) {
	return &repository.MetricsAggregation{}, nil
}

type verifyStubCache struct{}

func (verifyStubCache) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return nil
}

func (verifyStubCache) Get(ctx context.Context, key string) (string, error) { return "", redis.Nil }

type verifyStubProcessor struct {
	result *imageprocessor.Result
}

func (v verifyStubProcessor) Process(ctx context.Context, userID string, imageBytes []byte) (*imageprocessor.Result, error) {
	return v.result, nil
}

type metricsStubCache struct{}

func (metricsStubCache) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return nil
}
func (metricsStubCache) Get(ctx context.Context, key string) (string, error) { return "", redis.Nil }

type metricsStubProcessor struct{}

func (metricsStubProcessor) Process(ctx context.Context, userID string, imageBytes []byte) (*imageprocessor.Result, error) {
	return &imageprocessor.Result{}, nil
}
