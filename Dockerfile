# syntax=docker/dockerfile:1

# ---- build stage ----
# Pin the build stage to the BUILDPLATFORM (the runner's native arch) and
# cross-compile per TARGETARCH. Pure-Go (CGO_ENABLED=0), so this is a fast
# native cross-compile rather than an emulated build.
FROM --platform=$BUILDPLATFORM golang:1.24-bookworm AS build
WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

# Build a static binary so it runs on the distroless/static base.
ARG TARGETOS TARGETARCH
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/annoybot ./cmd/annoybot
# The IdleRPG web dashboard ships in the same image; its Deployment overrides the
# entrypoint to /dashboard. One image, one CI pipeline.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/dashboard ./cmd/dashboard

# ---- runtime stage ----
FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/annoybot /annoybot
COPY --from=build /out/dashboard /dashboard
# Bake the default quote packs and shared skits; ConfigMap mounts can override.
COPY data/quotes /quotes
COPY data/skits.yaml /skits.yaml
ENV ANNOYBOT_QUOTES_DIR=/quotes
ENV ANNOYBOT_SKITS_FILE=/skits.yaml
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/annoybot"]
CMD ["-config", "/config/bot.yaml"]
