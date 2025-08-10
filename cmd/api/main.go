package main

import (
	"context"
	"log"
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

func registerRoutes(r *gin.Engine, h *handlers.PaymentHandlers) {
	r.POST("payments", h.HandlePayment)
	r.GET("payments-summary", h.HandlePaymentSummary)
	r.GET("payments-queue", h.HandleQueueDump)
}

func main() {
	time.Sleep(1 * time.Second)

	redisHost := os.Getenv("REDIS_HOST")
	redisPassword := os.Getenv("REDIS_PASSWORD")
	redisRepo := repositories.NewRedisRepository(redisHost+":6379", redisPassword)
	defer redisRepo.Close()

	paymentHandlers := handlers.NewPaymentHandlers(redisRepo)

	r := setupRouter()
	registerRoutes(r, paymentHandlers)

	workers := workers.NewWorkers(redisRepo)
	nWorkersStr := os.Getenv("N_WORKERS")
	nWorkers, err := strconv.ParseInt(nWorkersStr, 10, 0)
	if err != nil {
		nWorkers = 1
	}
	go workers.StartWorkers(context.Background(), int(nWorkers))

	log.Println("API is running on port 8080")
	r.Run(":8080")
}
