package handlers

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/example/ai-check/internal/auth"
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
