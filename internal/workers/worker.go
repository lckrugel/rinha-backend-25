package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/lckrugel/rinha-backend-25/internal/dtos"
	"github.com/lckrugel/rinha-backend-25/internal/repositories"
)

type Workers struct {
	redisRepo  *repositories.RedisRepository
	httpClient *http.Client
	maxRetries int
	baseDelay  time.Duration
}

func NewWorkers(redisRepo *repositories.RedisRepository) *Workers {
	return &Workers{
		redisRepo:  redisRepo,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		maxRetries: 5,
		baseDelay:  2 * time.Second,
	}
}

func (w *Workers) StartWorkers(ctx context.Context, numWorkers int) {
	log.Printf("Iniciando %d workers de processamento de pagamentos...", numWorkers)

	var wg sync.WaitGroup
	for i := range numWorkers {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			w.start(ctx, workerID)
		}(i)
	}
	wg.Wait()
	log.Println("Todos os workers de processamento de pagamentos foram finalizados")
}

func (w *Workers) start(ctx context.Context, workerID int) {
	log.Printf("Worker %d iniciado", workerID)

	for {
		select {
		case <-ctx.Done():
			log.Printf("Worker %d recebeu sinal de parada", workerID)
			return
		default:
			processCtx, cancel := context.WithTimeout(ctx, 90*time.Second)

			err := w.processPayment(processCtx)
			if err != nil {
				log.Printf("Worker %d erro: %v", workerID, err)
			}

			cancel()
		}
	}
}

func (w *Workers) processPayment(ctx context.Context) error {
	paymentRequest, err := w.redisRepo.DequeueForProcessing(ctx)
	if err != nil {
		return fmt.Errorf("Erro ao remover pagamento da fila: %w", err)
	}

	if paymentRequest == nil {
		return nil // Fila vazia, nada a processar
	}

	defer func() {
		w.redisRepo.RemoveFromProcessing(ctx, paymentRequest.CorrelationId)
	}()

	isProcessed, err := w.redisRepo.IsProcessed(ctx, paymentRequest.CorrelationId)
	if err != nil {
		return fmt.Errorf("Erro ao verificar pagamento processado: %w", err)
	}
	if isProcessed {
		return nil
	}

	// log.Printf("Processando pagamento: %s\n", paymentRequest.CorrelationId)

	paymentResponse, err := w.callPaymentAPIWithRetry(ctx, paymentRequest)
	if err != nil {
		return fmt.Errorf("Erro ao chamar API de pagamento: %w", err)
	}

	processedPayment := dtos.ProcessedPayment{
		CorrelationId: paymentResponse.CorrelationId,
		Amount:        paymentResponse.Amount,
		ProcessedAt:   paymentResponse.RequestedAt,
	}

	err = w.redisRepo.StoreProcessed(ctx, &processedPayment)
	if err != nil {
		return fmt.Errorf("Erro ao marcar pagamento como processado: %w", err)
	}

	// log.Printf("Pagamento processado com sucesso: %s (Amount: %.2f)\n", processedPayment.CorrelationId, processedPayment.Amount)

	return nil
}

func (w *Workers) callPaymentAPIWithRetry(ctx context.Context, payment *dtos.PaymentRequest) (*dtos.PaymentAPIRequest, error) {
	var lastErr error

	paymentAPIRequest := dtos.PaymentAPIRequest{
		CorrelationId: payment.CorrelationId,
		Amount:        payment.Amount,
		RequestedAt:   time.Now().Format(time.RFC3339),
	}

	defaultUrl := os.Getenv("DEFAULT_API_URL")
	for attempt := 0; attempt <= w.maxRetries; attempt++ {
		err := w.callPaymentAPI(ctx, defaultUrl, &paymentAPIRequest)
		if err == nil {
			return &paymentAPIRequest, nil
		}

		if attempt == w.maxRetries {
			break
		}

		lastErr = err
		if !w.isRetryableError(err) {
			return nil, fmt.Errorf("Erro não recuperável: %w", err)
		}

		fmt.Printf("Tentativa %d/%d falhou para pagamento %s, tentando novamente\n",
			attempt+1, w.maxRetries+1, payment.CorrelationId)
	}

	fmt.Printf("Todas as tentativas falharam para pagamento %s, tentando fallback\n", payment.CorrelationId)
	fallbackUrl := os.Getenv("FALLBACK_API_URL")
	err := w.callPaymentAPI(ctx, fallbackUrl, &paymentAPIRequest)
	if err == nil {
		return &paymentAPIRequest, nil
	}

	return nil, fmt.Errorf("Todas as tentativas falharam: %w", lastErr)
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
		log.Printf("Processamento de pagamento falhou para '%v' com código: %d", url, resp.StatusCode)
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
