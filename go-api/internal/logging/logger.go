package logging

import (
	"go.uber.org/zap"
)

// NewLogger builds a production ready structured logger.
func NewLogger() (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.EncoderConfig.TimeKey = "timestamp"
	return cfg.Build()
}

// WithOperation enriches the logger with operation and request identifiers.
func WithOperation(logger *zap.Logger, operation, requestID string) *zap.Logger {
	fields := []zap.Field{zap.String("operation", operation)}
	if requestID != "" {
		fields = append(fields, zap.String("request_id", requestID))
	}
	return logger.With(fields...)
}
