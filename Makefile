.PHONY: run worker test vet build

run:
	go run ./cmd/api

worker:
	go run ./cmd/worker

test:
	go test ./...

vet:
	go vet ./...

build:
	go build ./cmd/api ./cmd/worker
