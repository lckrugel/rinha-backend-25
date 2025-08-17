package workers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/lckrugel/rinha-backend-25/internal/dtos"
)

type HealthCheckWorker struct {
	bias     int
	selector *ServiceSelector
}

func NewHealthCheckWorker(selector *ServiceSelector, bias int) *HealthCheckWorker {
	return &HealthCheckWorker{
		bias:     bias,
		selector: selector,
	}
}

func (hc *HealthCheckWorker) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():

			return
		default:
			time.Sleep(time.Second * 5) // Espera 5s entre checagens
			healthCheckCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()

			err := hc.chooseService(healthCheckCtx)
			if err != nil {
				slog.Error("Erro ao escolher o serviço", "err", err)
			}
		}
	}
}

func (hc *HealthCheckWorker) chooseService(ctx context.Context) error {
	defaultCh := make(chan *dtos.HealthCheckResponse)
	fallbackCh := make(chan *dtos.HealthCheckResponse)

	defaultURL := dtos.ApiUrl[dtos.DEFAULT_API]
	fallbackURL := dtos.ApiUrl[dtos.FALLBACK_API]
	go check(ctx, defaultURL+"/payments/service-health", defaultCh)
	go check(ctx, fallbackURL+"/payments/service-health", fallbackCh)

	defaultRes := <-defaultCh
	fallbackRes := <-fallbackCh

	choosen := dtos.DEFAULT_API
	if defaultRes == nil || defaultRes.Failing {
		if fallbackRes != nil && !fallbackRes.Failing {
			choosen = dtos.FALLBACK_API
		}
	} else if fallbackRes != nil && !fallbackRes.Failing {
		if defaultRes.MinResponseTime > fallbackRes.MinResponseTime*hc.bias {
			choosen = dtos.FALLBACK_API
		}
	}
	if choosen != hc.selector.GetActive() {
		slog.Info("Trocando API ativa", "url_ativa", choosen)
	}
	hc.selector.SetActive(choosen)
	return nil
}

func check(ctx context.Context, url string, resCh chan<- *dtos.HealthCheckResponse) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic during health check", "url", url, "panic", r)
			resCh <- nil
			return
		}
	}()
	httpClient := http.Client{Timeout: 90 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		slog.Error("Erro criando requisição de health", "err", err)
		resCh <- nil
		return
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		slog.Error("Erro ao enviar requisição de health", "err", err, "url", url)
		resCh <- nil
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Falha ao checar saúde do serviço", "url", url)
		resCh <- nil
		return
	}

	var response dtos.HealthCheckResponse
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		slog.Error("Falha ao decodificar resposta", "err", err)
		resCh <- nil
		return
	}

	resCh <- &response
}
