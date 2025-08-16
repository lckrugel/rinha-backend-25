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
