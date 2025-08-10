# Estágio 1: Construção o binário
FROM golang:1.24.5-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/server ./cmd/api

# Estágio 2: Imagem mínima para executar o binário
FROM alpine:latest

COPY --from=builder /app/bin/server /usr/local/bin/server

WORKDIR /usr/local/bin

EXPOSE 8080

# Run the binary
CMD ["server"]
