package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/example/ai-check/internal/auth"
	"github.com/example/ai-check/internal/grpcclient"
	"github.com/example/ai-check/internal/handlers"
	"github.com/example/ai-check/internal/logging"
	"github.com/example/ai-check/internal/repository"
	"github.com/example/ai-check/internal/usecase"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	logger, err := logging.NewLogger()
	if err != nil {
		panic(err)
	}
	defer logger.Sync() //nolint:errcheck

	db := initDatabase(ctx, logger)
	repo := repository.NewVerificationRepository(db, logger)
	if err := repo.AutoMigrate(ctx); err != nil {
		logger.Fatal("auto migrate failed", zap.Error(err))
	}

	redisCtx, redisCancel := context.WithTimeout(ctx, 5*time.Second)
	defer redisCancel()
	redisClient := initRedis(redisCtx, logger)

	imageProcessorAddr := getEnv("IMAGE_PROCESSOR_ADDR", "rust-service:50051")
	client, conn, err := grpcclient.DialImageProcessor(ctx, imageProcessorAddr, logger)
	if err != nil {
		logger.Fatal("failed to connect to image processor", zap.Error(err))
	}
	defer conn.Close()

	cache := usecase.NewRedisCache(redisClient)
	uc := usecase.NewVerificationUseCase(repo, cache, client, logger)

	r := gin.Default()
	r.MaxMultipartMemory = handlers.MaxUploadSize

	jwtSecret := getEnv("JWT_SECRET", "dev-secret")
	jwtAudience := os.Getenv("JWT_AUDIENCE")
	authMiddleware := auth.JWTMiddleware(jwtSecret, jwtAudience)

	handlers.RegisterRoutes(r, uc, authMiddleware)

	server := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	logger.Info("Golang API listening", zap.String("addr", ":8080"))
	if err := serveHTTPServer(server, 15*time.Second, logger); err != nil {
		logger.Fatal("server failed", zap.Error(err))
	}
}

func initDatabase(ctx context.Context, zapLogger *zap.Logger) *gorm.DB {
	dsn := getEnv("DATABASE_DSN", "host=postgres user=postgres password=postgres dbname=aiverify port=5432 sslmode=disable")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Info)})
	if err != nil {
		zapLogger.Fatal("failed to connect to database", zap.Error(err))
	}

	sqlDB, err := db.DB()
	if err != nil {
		zapLogger.Fatal("failed to access db handle", zap.Error(err))
	}
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := sqlDB.PingContext(ctx); err != nil {
		zapLogger.Fatal("database ping failed", zap.Error(err))
	}

	return db
}

func initRedis(ctx context.Context, zapLogger *zap.Logger) *redis.Client {
	addr := getEnv("REDIS_ADDR", "redis:6379")
	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(ctx).Err(); err != nil {
		zapLogger.Fatal("redis connection failed", zap.Error(err))
	}
	return client
}

func serveHTTPServer(server *http.Server, shutdownTimeout time.Duration, logger *zap.Logger) error {
	return serveHTTPServerWithOptions(server, shutdownTimeout, logger, nil, nil)
}

func serveHTTPServerWithListener(server *http.Server, shutdownTimeout time.Duration, logger *zap.Logger, listener net.Listener) error {
	return serveHTTPServerWithOptions(server, shutdownTimeout, logger, listener, nil)
}

func serveHTTPServerWithOptions(server *http.Server, shutdownTimeout time.Duration, logger *zap.Logger, listener net.Listener, signalCh <-chan os.Signal) error {
	errCh := make(chan error, 1)
	go func() {
		var err error
		if listener != nil {
			err = server.Serve(listener)
		} else {
			err = server.ListenAndServe()
		}
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	var (
		sigCh       <-chan os.Signal
		stopSignals func()
	)

	if signalCh != nil {
		sigCh = signalCh
		stopSignals = func() {}
	} else {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
		sigCh = ch
		stopSignals = func() {
			signal.Stop(ch)
		}
	}
	defer stopSignals()

	select {
	case err := <-errCh:
		return err
	case sig, ok := <-sigCh:
		if !ok {
			return <-errCh
		}
		logger.Info("received shutdown signal", zap.String("signal", sig.String()))
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return <-errCh
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
