package dtos

import "time"

type PaymentRequest struct {
	CorrelationId string  `json:"correlationId"`
	Amount        float64 `json:"amount"`
	RequestedAt   time.Time
	RedisStreamId string
}

type PaymentAPIRequest struct {
	CorrelationId string  `json:"correlationId"`
	Amount        float64 `json:"amount"`
	RequestedAt   string  `json:"requestedAt"`
}

type SummaryResponse struct {
	Default  APISummary `json:"default"`
	Fallback APISummary `json:"fallback"`
}

type APISummary struct {
	TotalRequests int     `json:"totalRequests"`
	TotalAmount   float64 `json:"totalAmount"`
}

type HealthCheckResponse struct {
	Failing         bool `json:"failing"`
	MinResponseTime int  `json:"minResponseTime"`
}

type ProcessedPayment struct {
	CorrelationId string     `json:"correlationId"`
	Api           PaymentAPI `json:"paymentAPI"`
	Amount        float64    `json:"amount"`
	ProcessedAt   string     `json:"processedAt"`
}

type PaymentAPI uint8

const (
	DEFAULT_API PaymentAPI = iota
	FALLBACK_API
)

var ApiUrl = map[PaymentAPI]string{
	DEFAULT_API:  "http://payment-processor-default:8080",
	FALLBACK_API: "http://payment-processor-fallback:8080",
}
