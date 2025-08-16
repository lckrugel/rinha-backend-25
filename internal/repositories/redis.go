package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lckrugel/rinha-backend-25/internal/dtos"
	"github.com/redis/go-redis/v9"
)

type processedSet uint8

const (
	PROCESSED_DEFAULT = iota
	PROCESSED_FALLBACK
)

var processedSetKey = map[processedSet]string{
	PROCESSED_DEFAULT:  "payments:processed:default",
	PROCESSED_FALLBACK: "payments:processed:fallback",
}

func createStreamGroup(r *redis.Client, stream, group string) error {
	err := r.XGroupCreateMkStream(context.Background(), stream, group, "$").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return err
	}
	return nil
}

type RedisRepository struct {
	client     *redis.Client
	streamKey  string
	readGroup  string
	ConsumerId string
}

func NewRedisRepository(addr, password string) *RedisRepository {
	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           0,
		PoolSize:     15,
		MinIdleConns: 1,
		MaxRetries:   3,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	stream := "payments:stream"
	group := "read-group"

	err := createStreamGroup(rdb, stream, group)
	if err != nil {
		log.Fatalf("Falha ao criar redis stream com read group: %v", err)
	}

	return &RedisRepository{
		client:     rdb,
		streamKey:  stream,
		readGroup:  group,
		ConsumerId: os.Getenv("CONSUMER_ID"),
	}
}

func (r *RedisRepository) Close() error {
	return r.client.Close()
}

func (r *RedisRepository) AddToStream(ctx context.Context, payment *dtos.PaymentRequest) error {
	payment.RequestedAt = time.Now().UTC()
	values := map[string]any{
		"correlationId": payment.CorrelationId,
		"amount":        payment.Amount,
		"requestedAt":   payment.RequestedAt.Format("2006-01-02T15:04:05.000Z"),
	}
	err := r.client.XAdd(ctx, &redis.XAddArgs{Stream: r.streamKey, Values: values}).Err()
	if err != nil {
		return fmt.Errorf("Falha ao adicionar pagamento à stream: %w", err)
	}
	return nil
}

func (r *RedisRepository) ReadFromStream(ctx context.Context, consumerId string) (*dtos.PaymentRequest, error) {
	data, err := r.client.XReadGroup(ctx, &redis.XReadGroupArgs{Streams: []string{r.streamKey, ">"},
		Group:    r.readGroup,
		Consumer: consumerId,
		Count:    1,
		Block:    5 * time.Second,
		NoAck:    false,
	}).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // Timeout, sem mensagens
		}
		return nil, fmt.Errorf("Erro ao ler stream de pagamentos: %w", err)
	}

	if len(data) == 0 || len(data[0].Messages) == 0 {
		return nil, nil // Não leu nada
	}

	message := data[0].Messages[0]
	messageId := data[0].Messages[0].ID

	correlationId, _ := message.Values["correlationId"].(string)
	amount := 0.0
	switch v := message.Values["amount"].(type) {
	case float64:
		amount = v
	case string:
		amount, err = strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("Erro ao converter string para float: %w", err)
		}
	}
	requestedAtStr, _ := message.Values["requestedAt"].(string)

	requestedAt, err := time.Parse("2006-01-02T15:04:05.000Z", requestedAtStr)
	if err != nil {
		return nil, fmt.Errorf("Erro ao converter data: %w", err)
	}

	return &dtos.PaymentRequest{
		CorrelationId: correlationId,
		Amount:        amount,
		RedisStreamId: messageId,
		RequestedAt:   requestedAt,
	}, nil
}

func (r *RedisRepository) StoreProcessed(ctx context.Context, payment *dtos.ProcessedPayment, messageId string) error {
	processedAt, err := time.Parse("2006-01-02T15:04:05.000Z", payment.ProcessedAt)
	if err != nil {
		return fmt.Errorf("Erro ao converter data: %w", err)
	}

	paymentData, err := json.Marshal(payment)
	if err != nil {
		return fmt.Errorf("Erro ao serializar pagamento: %w", err)
	}

	pipe := r.client.TxPipeline()

	score := float64(processedAt.UnixMilli())
	key := processedSetKey[processedSet(payment.Api)]

	pipe.ZAdd(ctx, key, redis.Z{
		Score:  score,
		Member: paymentData,
	})

	err = r.AckMessage(ctx, messageId, &pipe)
	if err != nil {
		return err
	}

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("Erro armazenar pagamento processado: %w", err)
	}

	return nil
}

func (r *RedisRepository) AckMessage(ctx context.Context, messageId string, pipePtr *redis.Pipeliner) error {
	if pipePtr != nil {
		pipe := *pipePtr
		pipe.XAck(ctx, r.streamKey, r.readGroup, messageId)
		return nil
	} else {
		err := r.client.XAck(ctx, r.streamKey, r.readGroup, messageId).Err()
		if err != nil {
			return fmt.Errorf("Erro ao dar ack no pagamento: %w", err)
		}
	}
	return nil
}

func (r *RedisRepository) FlushDB(ctx context.Context) error {
	err := r.client.FlushDB(ctx).Err()
	if err != nil {
		return fmt.Errorf("Erro ao limpar todos os dados do Redis: %w", err)
	}
	return nil
}

func (r *RedisRepository) GetSummaryByDateRange(ctx context.Context, api dtos.PaymentAPI, from, to time.Time) (*dtos.APISummary, error) {
	fromScore := from.UnixMilli()
	toScore := to.UnixMilli()

	key := processedSetKey[processedSet(api)]

	results, err := r.client.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min: fmt.Sprintf("%d", fromScore),
		Max: fmt.Sprintf("%d", toScore),
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("Erro ao buscar pagamentos por data: %w", err)
	}

	totalAmount := 0.0
	totalRequests := 0
	for _, result := range results {
		var payment dtos.ProcessedPayment
		if err := json.Unmarshal([]byte(result), &payment); err != nil {
			slog.Warn("Failed to unmarshall processed payment. Skipping")
			continue // Pula entrada inválida
		}
		totalAmount += payment.Amount
		totalRequests++
	}

	totalAmount = math.Round(totalAmount*100) / 100

	return &dtos.APISummary{
		TotalRequests: totalRequests,
		TotalAmount:   totalAmount,
	}, nil
}
