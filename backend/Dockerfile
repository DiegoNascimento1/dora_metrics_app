# syntax=docker/dockerfile:1.7

# ---------- Builder ----------
FROM golang:1.26-alpine AS builder
WORKDIR /src

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build \
      -ldflags='-s -w -extldflags "-static"' \
      -o /out/api ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux go build \
      -ldflags='-s -w -extldflags "-static"' \
      -o /out/worker ./cmd/worker && \
    CGO_ENABLED=0 GOOS=linux go build \
      -ldflags='-s -w -extldflags "-static"' \
      -o /out/cli ./cmd/cli

# ---------- API runtime ----------
FROM gcr.io/distroless/static-debian12:nonroot AS api
WORKDIR /app
COPY --from=builder /out/api /app/api
COPY --from=builder /src/migrations /app/migrations
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/api"]

# ---------- Worker runtime ----------
FROM gcr.io/distroless/static-debian12:nonroot AS worker
WORKDIR /app
COPY --from=builder /out/worker /app/worker
COPY --from=builder /src/migrations /app/migrations
USER nonroot:nonroot
ENTRYPOINT ["/app/worker"]

# ---------- CLI runtime ----------
FROM gcr.io/distroless/static-debian12:nonroot AS cli
WORKDIR /app
COPY --from=builder /out/cli /app/cli
USER nonroot:nonroot
ENTRYPOINT ["/app/cli"]
