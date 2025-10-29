package grpcclient

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	proto "github.com/example/aiverify/go-api/proto"
)

// DialImageProcessor returns a ready-to-use gRPC client for the Rust service.
func DialImageProcessor(ctx context.Context, addr string) (proto.ImageProcessorClient, *grpc.ClientConn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(
		dialCtx,
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("dial image processor: %w", err)
	}
	return proto.NewImageProcessorClient(conn), conn, nil
}
