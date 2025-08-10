.PHONY: build up down restart logs clean test

dev:
		docker compose up --build -d

start:
		docker compose up -d

restart:
		docker compose down && docker compose up -d 

clean:
		docker compose down -v --remove-orphans

logs-api:
		docker compose logs api1 api2 -f

logs-redis:
		docker compose logs redis

logs-nginx:
		docker compose logs nginx -f

inspect-queue:
		@echo "=== Payment Queue (first 10 items) ==="
		@docker compose exec redis redis-cli lrange payments:queue 0 9

inspect-processing:
		@echo "=== Currently Processing ==="
		@docker compose exec redis redis-cli smembers payments:processing

inspect-processed:
		@echo "=== Processed Payments Count ==="
		@docker compose exec redis redis-cli scard payments:processed:ids

summary:
		@echo "=== Payment Summary ==="
		@curl -s http://localhost:9999/payments-summary | jq . || echo "Summary endpoint failed"
