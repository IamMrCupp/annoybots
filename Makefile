BINARY := annoybot
IMAGE  := ghcr.io/iammrcupp/annoybots
TAG    ?= latest

.PHONY: build test lint run run-echo run-mimic docker k8s-echo k8s-mimic tidy

build:
	go build -trimpath -o bin/$(BINARY) ./cmd/annoybot

test:
	go test ./...

lint:
	golangci-lint run

tidy:
	go mod tidy

# Run any bot locally, loading quote packs from data/quotes. Export the *_env
# secrets your config references first.
#   make run CONFIG=configs/yourbot.yaml
run:
	ANNOYBOT_QUOTES_DIR=data/quotes go run ./cmd/annoybot -config $(CONFIG)

# Convenience shortcuts for the bundled example bots.
run-echo:
	ANNOYBOT_QUOTES_DIR=data/quotes go run ./cmd/annoybot -config configs/echo.yaml

run-mimic:
	ANNOYBOT_QUOTES_DIR=data/quotes go run ./cmd/annoybot -config configs/mimic.yaml

docker:
	docker build -t $(IMAGE):$(TAG) .

# Render manifests. The bot config lives outside the overlay dir, so disable the
# load restrictor. Pipe to `| kubectl apply -f -` to deploy.
k8s-echo:
	kubectl kustomize --load-restrictor LoadRestrictionsNone deploy/k8s/overlays/echo

k8s-mimic:
	kubectl kustomize --load-restrictor LoadRestrictionsNone deploy/k8s/overlays/mimic
