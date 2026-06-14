BINARY := annoybot
IMAGE  := ghcr.io/iammrcupp/annoybots
TAG    ?= latest

.PHONY: build test lint run-echo run-mimic docker k8s-arywen k8s-kurkutu tidy

build:
	go build -trimpath -o bin/$(BINARY) ./cmd/annoybot

test:
	go test ./...

lint:
	golangci-lint run

tidy:
	go mod tidy

# Run locally against an example config, loading quote packs from data/quotes.
# Export the *_env secrets in your shell first.
run-echo:
	ANNOYBOT_QUOTES_DIR=data/quotes go run ./cmd/annoybot -config configs/echo.yaml

run-mimic:
	ANNOYBOT_QUOTES_DIR=data/quotes go run ./cmd/annoybot -config configs/mimic.yaml

docker:
	docker build -t $(IMAGE):$(TAG) .

# Render manifests. The bot config lives outside the overlay dir, so disable the
# load restrictor. Pipe to `| kubectl apply -f -` to deploy.
k8s-arywen:
	kubectl kustomize --load-restrictor LoadRestrictionsNone deploy/k8s/overlays/arywen

k8s-kurkutu:
	kubectl kustomize --load-restrictor LoadRestrictionsNone deploy/k8s/overlays/kurkutu
