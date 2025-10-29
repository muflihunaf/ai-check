package handlers

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/example/ai-check/internal/auth"
	"github.com/example/ai-check/internal/usecase"
)

// RegisterRoutes wires the HTTP handlers to the Gin router.
func RegisterRoutes(router *gin.Engine, uc *usecase.VerificationUseCase, authMiddleware gin.HandlerFunc) {
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	protected := router.Group("")
	protected.Use(authMiddleware)

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

		src, err := file.Open()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unable to open image"})
			return
		}
		defer src.Close()

		data, err := io.ReadAll(src)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read image"})
			return
		}

		requestID, result, err := uc.VerifyImage(c.Request.Context(), userID, data)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"request_id": requestID,
			"verified":   result.GetSuccess(),
			"score":      result.GetScore(),
			"message":    result.GetMessage(),
		})
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

		c.JSON(http.StatusOK, gin.H{
			"request_id": log.RequestID,
			"user_id":    log.UserID,
			"score":      log.Score,
			"success":    log.Success,
			"details":    log.Details,
			"created_at": log.CreatedAt,
		})
	})
}
