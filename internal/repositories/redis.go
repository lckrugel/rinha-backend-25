package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lckrugel/rinha-backend-25/internal/dtos"
	"github.com/redis/go-redis/v9"
)

type RedisRepository struct {
	client *redis.Client
}

func NewRedisRepository(addr, password string) *RedisRepository {
	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           0,
		PoolSize:     8,
		MinIdleConns: 1,
		MaxRetries:   3,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	return &RedisRepository{
		client: rdb,
	}
}

func (r *RedisRepository) Close() error {
	return r.client.Close()
}

func (r *RedisRepository) Enqueue(ctx context.Context, payment dtos.PaymentRequest) error {
	data, err := json.Marshal(payment)
	if err != nil {
		return fmt.Errorf("Erro ao serializar pagamento: %w", err)
	}

	err = r.client.LPush(ctx, "payments:queue", data).Err()
	if err != nil {
		return fmt.Errorf("Erro ao adicionar pagamento na fila: %w", err)
	}

	return nil
}

func (r *RedisRepository) DequeueForProcessing(ctx context.Context) (*dtos.PaymentRequest, error) {
	data, err := r.client.BLPop(ctx, 5*time.Second, "payments:queue").Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // Fila vazia e timeout atingido
		}
		return nil, fmt.Errorf("Erro ao remover pagamento da fila: %w", err)
	}

	paymentJSON := data[1]

	var payment dtos.PaymentRequest
	err = json.Unmarshal([]byte(paymentJSON), &payment)
	if err != nil {
		return nil, fmt.Errorf("Erro ao desserializar pagamento: %w", err)
	}

	err = r.client.SAdd(ctx, "payments:processing", payment.CorrelationId).Err()
	if err != nil {
		r.client.LPush(ctx, "payments:queue", paymentJSON)
		return nil, fmt.Errorf("Erro ao adicionar ao processamento: %w", err)
	}

	return &payment, nil
}

func (r *RedisRepository) RemoveFromProcessing(ctx context.Context, correlationId string) error {
	return r.client.SRem(ctx, "payments:processing", correlationId).Err()
}

func (r *RedisRepository) StoreProcessed(ctx context.Context, payment *dtos.ProcessedPayment) error {
	processedAt, err := time.Parse(time.RFC3339, payment.ProcessedAt)
	if err != nil {
		return fmt.Errorf("Erro ao parsear data: %w", err)
	}

	paymentData, err := json.Marshal(payment)
	if err != nil {
		return fmt.Errorf("Erro ao serializar pagamento: %w", err)
	}

	// Use timestamp as score for efficient range queries
	score := float64(processedAt.Unix())

	err = r.client.ZAdd(ctx, "payments:processed", redis.Z{
		Score:  score,
		Member: paymentData,
	}).Err()
	if err != nil {
		return fmt.Errorf("Erro ao armazenar pagamento processado: %w", err)
	}

	err = r.client.SAdd(ctx, "payments:processed:ids", payment.CorrelationId).Err()
	if err != nil {
		return fmt.Errorf("Erro ao marcar pagamento como processado: %w", err)
	}
	return nil
}

func (r *RedisRepository) IsProcessed(ctx context.Context, correlationId string) (bool, error) {
	exists, err := r.client.SIsMember(ctx, "payments:processed:ids", correlationId).Result()
	if err != nil {
		return false, fmt.Errorf("Erro ao verificar se pagamento foi processado: %w", err)
	}
	return exists, nil
}

func (r *RedisRepository) IsInQueue(ctx context.Context, correlationId string) (bool, error) {
	items, err := r.client.LRange(ctx, "payments:queue", 0, -1).Result()
	if err != nil {
		return false, fmt.Errorf("Erro ao verificar fila: %w", err)
	}

	for _, item := range items {
		var payment dtos.PaymentRequest
		if err := json.Unmarshal([]byte(item), &payment); err != nil {
			continue
		}
		if payment.CorrelationId == correlationId {
			return true, nil
		}
	}

	return false, nil
}

func (r *RedisRepository) IsInProcessing(ctx context.Context, correlationId string) (bool, error) {
	exists, err := r.client.SIsMember(ctx, "payments:processing", correlationId).Result()
	if err != nil {
		return false, fmt.Errorf("Erro ao verificar processamento: %w", err)
	}
	return exists, nil
}

func (r *RedisRepository) IsProcessedOrInQueue(ctx context.Context, correlationId string) (bool, error) {
	isProcessed, err := r.IsProcessed(ctx, correlationId)
	if err != nil {
		return false, err
	}
	if isProcessed {
		return true, nil
	}

	inProcessing, err := r.IsInProcessing(ctx, correlationId)
	if err != nil {
		return false, err
	}
	if inProcessing {
		return true, nil
	}

	return r.IsInQueue(ctx, correlationId)
}

func (r *RedisRepository) DumpQueue(ctx context.Context) ([]dtos.PaymentRequest, error) {
	items, err := r.client.LRange(ctx, "payments:queue", 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("Erro ao obter fila de pagamentos: %w", err)
	}

	var payments []dtos.PaymentRequest
	for _, item := range items {
		var payment dtos.PaymentRequest
		if err := json.Unmarshal([]byte(item), &payment); err != nil {
			continue // Ignora itens inválidos
		}
		payments = append(payments, payment)
	}

	return payments, nil
}

func (r *RedisRepository) ClearAll(ctx context.Context) error {
	err := r.client.FlushAll(ctx).Err()
	if err != nil {
		return fmt.Errorf("Erro ao limpar todos os dados do Redis: %w", err)
	}
	return nil
}

func (r *RedisRepository) GetSummaryByDateRange(ctx context.Context, from, to time.Time) (*dtos.APISummary, error) {
	fromScore := float64(from.Unix())
	toScore := float64(to.Unix())

	// Get all payments in date range
	results, err := r.client.ZRangeByScore(ctx, "payments:processed", &redis.ZRangeBy{
		Min: fmt.Sprintf("%.0f", fromScore),
		Max: fmt.Sprintf("%.0f", toScore),
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("Erro ao buscar pagamentos por data: %w", err)
	}

	var totalAmount float32
	totalRequests := len(results)

	for _, result := range results {
		var payment dtos.ProcessedPayment
		if err := json.Unmarshal([]byte(result), &payment); err != nil {
			continue // Skip invalid entries
		}
		totalAmount += payment.Amount
	}

	return &dtos.APISummary{
		TotalRequests: totalRequests,
		TotalAmount:   totalAmount,
	}, nil
}
