# syntax=docker/dockerfile:1

# ---- build stage ----
FROM golang:1.23-bookworm AS build
WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

# Build a static binary so it runs on the distroless/static base.
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/annoybot ./cmd/annoybot

# ---- runtime stage ----
FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/annoybot /annoybot
# Bake the default quote packs; a ConfigMap mount at /quotes can override them.
COPY data/quotes /quotes
ENV ANNOYBOT_QUOTES_DIR=/quotes
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/annoybot"]
CMD ["-config", "/config/bot.yaml"]
