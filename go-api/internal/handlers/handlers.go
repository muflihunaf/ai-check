package handlers

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/example/ai-check/internal/auth"
	"github.com/example/ai-check/internal/usecase"
)

// MaxUploadSize defines the maximum supported upload size in bytes.
const MaxUploadSize = 8 << 20 // 8 MiB

var allowedContentTypes = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/gif":  {},
	"image/webp": {},
}

// RegisterRoutes wires the HTTP handlers to the Gin router.
func RegisterRoutes(router *gin.Engine, uc *usecase.VerificationUseCase, authMiddleware gin.HandlerFunc) {
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	protected := router.Group("")
	protected.Use(authMiddleware)

	protected.GET("/metrics/summary", func(c *gin.Context) {
		if _, ok := auth.GetUserID(c.Request.Context()); !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		summary, err := uc.GetMetricsSummary(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load metrics"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"total_requests":                summary.TotalRequests,
			"successful_requests":           summary.SuccessfulRequests,
			"success_rate":                  summary.SuccessRate,
			"average_score":                 summary.AverageScore,
			"average_processing_latency_ms": summary.AverageProcessingLatencyMs,
		})
	})

	protected.POST("/verify", func(c *gin.Context) {
		userID, ok := auth.GetUserID(c.Request.Context())
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		file, err := c.FormFile("image")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "image file is required"})
			return
		}

		if file.Size <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "image file is empty"})
			return
		}

		if file.Size > MaxUploadSize {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "image file is too large"})
			return
		}

		if !isAllowedContentType(file.Header.Get("Content-Type")) {
			c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "unsupported content type"})
			return
		}

		src, err := file.Open()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unable to open image"})
			return
		}
		defer src.Close()

		limited := io.LimitReader(src, MaxUploadSize+1)
		data, err := io.ReadAll(limited)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read image"})
			return
		}

		if len(data) > MaxUploadSize {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "image file is too large"})
			return
		}

		requestID, result, metadata, err := uc.VerifyImage(c.Request.Context(), userID, data)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		response := gin.H{
			"request_id": requestID,
			"verified":   result.Success,
			"score":      result.Score,
			"message":    result.Message,
		}

		if metadata != nil {
			response["metadata"] = gin.H{
				"timestamp": metadata.Timestamp,
				"success":   metadata.Success,
				"score":     metadata.Score,
			}
			response["created_at"] = metadata.Timestamp
		}

		c.JSON(http.StatusOK, response)
	})

	protected.GET("/result/:id", func(c *gin.Context) {
		userID, ok := auth.GetUserID(c.Request.Context())
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		requestID := c.Param("id")
		if requestID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
			return
		}

		log, err := uc.GetResult(c.Request.Context(), userID, requestID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "result not found"})
			return
		}

		if log.UserID == "" {
			log.UserID = userID
		}
		if log.RequestID == "" {
			log.RequestID = requestID
		}

		c.JSON(http.StatusOK, gin.H{
			"request_id": log.RequestID,
			"user_id":    log.UserID,
			"score":      log.Score,
			"success":    log.Success,
			"details":    log.Details,
			"sha1_hash":  log.SHA1Hash,
			"created_at": log.CreatedAt,
		})
	})

	protected.GET("/duplicates/:id", func(c *gin.Context) {
		userID, ok := auth.GetUserID(c.Request.Context())
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		requestID := c.Param("id")
		if requestID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
			return
		}

		report, err := uc.GetDuplicateReport(c.Request.Context(), userID, requestID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "result not found"})
			return
		}

		duplicates := make([]gin.H, 0, len(report.Duplicates))
		for _, duplicate := range report.Duplicates {
			duplicates = append(duplicates, gin.H{
				"request_id": duplicate.RequestID,
				"score":      duplicate.Score,
				"success":    duplicate.Success,
				"details":    duplicate.Details,
				"created_at": duplicate.CreatedAt,
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"request_id":      report.Request.RequestID,
			"user_id":         report.Request.UserID,
			"sha1_hash":       report.Request.SHA1Hash,
			"duplicate_count": len(report.Duplicates),
			"duplicates":      duplicates,
		})
	})
}

func isAllowedContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if idx := strings.Index(contentType, ";"); idx != -1 {
		contentType = strings.TrimSpace(contentType[:idx])
	}
	if contentType == "" {
		return false
	}
	_, ok := allowedContentTypes[contentType]
	return ok
}
