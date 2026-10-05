.PHONY: run test build eval docker-up

run:
	go run ./cmd/server

test:
	go test -race ./...

build:
	CGO_ENABLED=0 go build -trimpath -o bin/codeatlas ./cmd/server

eval:
	go run ./cmd/eval -dataset evals/video-platform-qa.json -out artifacts/eval-reports/latest.json

docker-up:
	docker compose up --build
