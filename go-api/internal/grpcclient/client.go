package grpcclient

import (
	"context"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/example/ai-check/internal/imageprocessor"
	"github.com/example/ai-check/internal/logging"
	proto "github.com/example/ai-check/proto"
)

// DialImageProcessor returns a ready-to-use gRPC client for the Rust service.
func DialImageProcessor(ctx context.Context, addr string, logger *zap.Logger) (imageprocessor.Client, *grpc.ClientConn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(
		dialCtx,
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		wrapped := logging.NewOperationError("grpcclient.dial_image_processor", "", err)
		logger.Error("failed to dial image processor", zap.Error(wrapped), zap.String("addr", addr))
		return nil, nil, wrapped
	}
	client := proto.NewImageProcessorClient(conn)
	return &grpcImageProcessor{client: client, logger: logger}, conn, nil
}

type grpcImageProcessor struct {
	client proto.ImageProcessorClient
	logger *zap.Logger
}

func (g *grpcImageProcessor) Process(ctx context.Context, userID string, imageBytes []byte) (*imageprocessor.Result, error) {
	resp, err := g.client.ProcessImage(ctx, &proto.VerifyRequest{UserId: userID, ImageData: imageBytes})
	if err != nil {
		wrapped := logging.NewOperationError("grpcclient.process_image", userID, err)
		g.logger.Error("image processor call failed", zap.Error(wrapped), zap.String("user_id", userID))
		return nil, wrapped
	}
	return &imageprocessor.Result{
		Success: resp.GetSuccess(),
		Score:   resp.GetScore(),
		Message: resp.GetMessage(),
	}, nil
}
