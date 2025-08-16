package handlers

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lckrugel/rinha-backend-25/internal/dtos"
	"github.com/lckrugel/rinha-backend-25/internal/repositories"
)

type PaymentHandlers struct {
	redisRepo *repositories.RedisRepository
}

func NewPaymentHandlers(redisRepo *repositories.RedisRepository) *PaymentHandlers {
	return &PaymentHandlers{
		redisRepo: redisRepo,
	}
}

func (h *PaymentHandlers) HandlePayment(c *gin.Context) {
	var paymentData dtos.PaymentRequest
	error := c.ShouldBindJSON(&paymentData)
	if error != nil {
		slog.Error("Erro ao vincular dados de pagamento:", "err", error)
		c.Status(http.StatusBadRequest)
		return
	}

	h.redisRepo.AddToStream(c, &paymentData)

	c.Status(http.StatusOK)
}

func (h *PaymentHandlers) HandlePaymentSummary(c *gin.Context) {
	fromQS := c.Query("from")
	toQS := c.Query("to")

	var from time.Time
	if fromQS != "" {
		var err error
		from, err = time.Parse("2006-01-02T15:04:05.000Z", fromQS)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"message": "Formato inválido para o parâmetro 'from'",
				"error":   err.Error(),
			})
			return
		}
	}

	var to time.Time
	if toQS != "" {
		var err error
		to, err = time.Parse("2006-01-02T15:04:05.000Z", toQS)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"message": "Formato inválido para o parâmetro 'to'",
				"error":   err.Error(),
			})
			return
		}
	}

	slog.Info("Summary", "from", from, "to", to)

	defaultSummary, err := h.redisRepo.GetSummaryByDateRange(c, dtos.DEFAULT_API, from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "Erro ao buscar resumo de pagamentos processados pela API default",
			"error":   err.Error(),
		})
		return
	}

	fallbackSummary, err := h.redisRepo.GetSummaryByDateRange(c, dtos.FALLBACK_API, from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "Erro ao buscar resumo de pagamentos processados pela API default",
			"error":   err.Error(),
		})
		return
	}

	response := &dtos.SummaryResponse{
		Default:  *defaultSummary,
		Fallback: *fallbackSummary,
	}

	c.JSON(http.StatusOK, response)
}

func (h *PaymentHandlers) PurgePayments(c *gin.Context) {
	err := h.redisRepo.FlushDB(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Erro ao limpar pagamentos"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Todos os pagamentos foram deletados"})
}
