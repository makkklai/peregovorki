.PHONY: up down seed test

up:
	docker compose up --build -d

down:
	docker compose down

seed:
	docker compose exec -T db psql -U booking -d booking < scripts/seed.sql

test:
	go test ./...
