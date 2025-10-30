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

type metricsStubCache struct{}

func (metricsStubCache) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return nil
}
func (metricsStubCache) Get(ctx context.Context, key string) (string, error) { return "", redis.Nil }

type metricsStubProcessor struct{}

func (metricsStubProcessor) Process(ctx context.Context, userID string, imageBytes []byte) (*imageprocessor.Result, error) {
	return &imageprocessor.Result{}, nil
}
