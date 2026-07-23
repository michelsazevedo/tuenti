.PHONY: build run test lint docker-up docker-down

build:
	go build -o bin/tuenti ./cmd

run:
	go run ./cmd

test:
	go test -v -race ./...

lint:
	golangci-lint run ./...

docker-up:
	docker compose up -d

docker-down:
	docker compose down
