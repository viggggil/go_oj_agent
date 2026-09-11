# syntax=docker/dockerfile:1.7

FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY api ./api
COPY errors ./errors
COPY pkg ./pkg
COPY services/gateway ./services/gateway

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/gateway-service \
    ./services/gateway/cmd/server

FROM alpine:3.23

RUN addgroup -S -g 10001 app \
    && adduser -S -D -H -u 10001 -G app app

WORKDIR /app

COPY --from=builder /out/gateway-service /app/gateway-service
COPY services/gateway/configs/config.yaml /app/config.yaml

USER app

EXPOSE 8080

ENTRYPOINT ["/app/gateway-service"]
CMD ["-conf", "/app/config.yaml"]
