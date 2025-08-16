package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lckrugel/rinha-backend-25/internal/handlers"
	"github.com/lckrugel/rinha-backend-25/internal/repositories"
	"github.com/lckrugel/rinha-backend-25/internal/workers"
)

func setupRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	return r
}

func setupLogger() {
	logLevel := os.Getenv("LOG_LEVEL")

	var level slog.Level
	switch logLevel {
	case "DEBUG":
		level = slog.LevelDebug
	case "INFO":
		level = slog.LevelInfo
	case "WARN":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	default:
		level = slog.LevelInfo // Default to INFO
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	handler := slog.NewTextHandler(os.Stdout, opts)

	logger := slog.New(handler)
	slog.SetDefault(logger)
}

func registerRoutes(r *gin.Engine, h *handlers.PaymentHandlers) {
	r.POST("payments", h.HandlePayment)
	r.GET("payments-summary", h.HandlePaymentSummary)
	r.POST("payments-purge", h.PurgePayments)
}

func main() {
	setupLogger()
	time.Sleep(1 * time.Second)

	redisHost := os.Getenv("REDIS_HOST")
	redisPassword := os.Getenv("REDIS_PASSWORD")
	redisRepo := repositories.NewRedisRepository(redisHost+":6379", redisPassword)
	defer redisRepo.Close()

	paymentHandlers := handlers.NewPaymentHandlers(redisRepo)

	r := setupRouter()
	registerRoutes(r, paymentHandlers)

	selector := workers.NewServiceSelector()

	biasStr := os.Getenv("DEFAULT_API_BIAS")
	bias, err := strconv.ParseInt(biasStr, 10, 0)
	if err != nil {
		bias = 3
	}
	healthChecker := workers.NewHealthCheckWorker(selector, int(bias))

	workers := workers.NewWorkers(redisRepo, selector)
	nWorkersStr := os.Getenv("N_WORKERS")
	nWorkers, err := strconv.ParseInt(nWorkersStr, 10, 0)
	if err != nil {
		nWorkers = 1
	}

	go healthChecker.Start(context.Background())
	go workers.StartWorkers(context.Background(), int(nWorkers))

	slog.Info("API is running on port 8080")
	r.Run(":8080")
}
