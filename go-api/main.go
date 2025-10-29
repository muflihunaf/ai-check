package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/example/aiverify/go-api/internal/auth"
	"github.com/example/aiverify/go-api/internal/grpcclient"
	"github.com/example/aiverify/go-api/internal/handlers"
	"github.com/example/aiverify/go-api/internal/repository"
	"github.com/example/aiverify/go-api/internal/usecase"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db := initDatabase(ctx)
	repo := repository.NewVerificationRepository(db)
	if err := repo.AutoMigrate(ctx); err != nil {
		log.Fatalf("auto migrate failed: %v", err)
	}

	redisClient := initRedis()

	imageProcessorAddr := getEnv("IMAGE_PROCESSOR_ADDR", "rust-service:50051")
	client, conn, err := grpcclient.DialImageProcessor(ctx, imageProcessorAddr)
	if err != nil {
		log.Fatalf("failed to connect to image processor: %v", err)
	}
	defer conn.Close()

	uc := usecase.NewVerificationUseCase(repo, redisClient, client)

	r := gin.Default()

	jwtSecret := getEnv("JWT_SECRET", "dev-secret")
	jwtAudience := os.Getenv("JWT_AUDIENCE")
	authMiddleware := auth.JWTMiddleware(jwtSecret, jwtAudience)

	handlers.RegisterRoutes(r, uc, authMiddleware)

	server := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	log.Println("Golang API listening on :8080")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server failed: %v", err)
	}
}

func initDatabase(ctx context.Context) *gorm.DB {
	dsn := getEnv("DATABASE_DSN", "host=postgres user=postgres password=postgres dbname=aiverify port=5432 sslmode=disable")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Info)})
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("failed to access db handle: %v", err)
	}
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := sqlDB.PingContext(ctx); err != nil {
		log.Fatalf("database ping failed: %v", err)
	}

	return db
}

func initRedis() *redis.Client {
	addr := getEnv("REDIS_ADDR", "redis:6379")
	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("redis connection failed: %v", err)
	}
	return client
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
