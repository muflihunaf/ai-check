package imageprocessor

import "context"

// Result contains the outcome returned by the image processor service.
type Result struct {
	Success bool
	Score   float32
	Message string
}

// Client exposes the subset of functionality used by the verification flow.
type Client interface {
	Process(ctx context.Context, userID string, imageBytes []byte) (*Result, error)
}
