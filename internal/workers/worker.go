package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/lckrugel/rinha-backend-25/internal/dtos"
	"github.com/lckrugel/rinha-backend-25/internal/repositories"
)

type Workers struct {
	redisRepo  *repositories.RedisRepository
	httpClient *http.Client
	maxRetries int
	selector   *ServiceSelector
}

func NewWorkers(redisRepo *repositories.RedisRepository, selector *ServiceSelector) *Workers {
	return &Workers{
		redisRepo:  redisRepo,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		maxRetries: 5,
		selector:   selector,
	}
}

func (w *Workers) StartWorkers(ctx context.Context, numWorkers int) {
	slog.Info("Iniciando workers de processamento de pagamentos...", "nworkers", numWorkers)

	var wg sync.WaitGroup
	for i := range numWorkers {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			w.start(ctx, workerID)
		}(i)
	}
	wg.Wait()
}

func (w *Workers) start(ctx context.Context, workerId int) {
	slog.Info("Worker iniciado", "workerId", workerId)

	for {
		select {
		case <-ctx.Done():
			slog.Info("Worker recebeu sinal de parada", "workerId", workerId)
			return
		default:
			processCtx, cancel := context.WithTimeout(ctx, 90*time.Second)

			err := w.processPayment(processCtx, workerId)
			if err != nil {
				slog.Warn("Worker encountered an error", "workerId", workerId, "err", err)
			}

			cancel()
		}
	}
}

func (w *Workers) processPayment(ctx context.Context, workerId int) error {
	consumerId := fmt.Sprintf("%s-%d", w.redisRepo.ConsumerId, workerId)
	paymentRequest, err := w.redisRepo.ReadFromStream(ctx, consumerId)
	if err != nil {
		return fmt.Errorf("Erro ao remover pagamento da fila: %w", err)
	}

	if paymentRequest == nil {
		return nil // Fila vazia, nada a processar
	}

	slog.Debug("Processando pagamento", "code", "PROCESSING_START", "payment-request", paymentRequest)

	paymentResponse, apiUsed, err := w.callPaymentAPIWithRetry(ctx, paymentRequest)
	if err != nil {
		return fmt.Errorf("Erro ao chamar API de pagamento: %w", err)
	}

	processedPayment := dtos.ProcessedPayment{
		CorrelationId: paymentResponse.CorrelationId,
		Amount:        paymentResponse.Amount,
		Api:           *apiUsed,
		ProcessedAt:   paymentResponse.RequestedAt,
	}

	err = w.redisRepo.StoreProcessed(ctx, &processedPayment, paymentRequest.RedisStreamId)
	if err != nil {
		return fmt.Errorf("Erro ao marcar pagamento como processado: %w", err)
	}

	slog.Debug("Pagamento processado com sucesso", "code", "PROCESSING_END", "payment-processed", processedPayment)

	return nil
}

func (w *Workers) callPaymentAPIWithRetry(ctx context.Context, payment *dtos.PaymentRequest) (*dtos.PaymentAPIRequest, *dtos.PaymentAPI, error) {
	var lastErr error

	paymentAPIRequest := dtos.PaymentAPIRequest{
		CorrelationId: payment.CorrelationId,
		Amount:        payment.Amount,
		RequestedAt:   payment.RequestedAt.Format("2006-01-02T15:04:05.000Z"),
	}

	for attempt := 0; attempt <= w.maxRetries; attempt++ {
		api := w.selector.GetActive()
		url := dtos.ApiUrl[api]
		err := w.callPaymentAPI(ctx, url+"/payments", &paymentAPIRequest)
		if err == nil {
			return &paymentAPIRequest, &api, nil
		}

		if attempt == w.maxRetries {
			break
		}

		lastErr = err
		if !w.isRetryableError(err) {
			w.redisRepo.AckMessage(ctx, payment.RedisStreamId, nil)
			return nil, nil, fmt.Errorf("Erro não recuperável: %w", err)
		}

		slog.Debug("Tentativa falhou", "tentativa", attempt, "maxTentativas", w.maxRetries+1, "correlationId", payment.CorrelationId)
	}

	return nil, nil, fmt.Errorf("Todas as tentativas falharam: %w", lastErr)
}

func (w *Workers) callPaymentAPI(ctx context.Context, url string, payment *dtos.PaymentAPIRequest) error {
	payload, err := json.Marshal(payment)
	if err != nil {
		return fmt.Errorf("Erro ao serializar pagamento: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("Erro ao criar requisição HTTP: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Erro ao enviar requisição HTTP: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode != 500 {
			slog.Error("Processamento de pagamento falhou", "url", url, "codigo", resp.StatusCode)
		}
		return &HTTPError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
		}
	}

	return nil
}

type HTTPError struct {
	StatusCode int
	Status     string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP error: %s", e.Status)
}

func (w *Workers) isRetryableError(err error) bool {
	if httpErr, ok := err.(*HTTPError); ok {
		switch httpErr.StatusCode {
		case http.StatusInternalServerError, // 500
			http.StatusBadGateway,         // 502
			http.StatusServiceUnavailable, // 503
			http.StatusGatewayTimeout,     // 504
			http.StatusTooManyRequests:    // 429
			return true
		default:
			return false
		}
	}
	return true
}
