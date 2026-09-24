# syntax=docker/dockerfile:1.7

FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY pkg ./pkg
COPY services/judge-worker ./services/judge-worker
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/judge-worker ./services/judge-worker/cmd/judge-worker

FROM alpine:3.23
RUN addgroup -S -g 10001 app && adduser -S -D -H -u 10001 -G app app
WORKDIR /app
COPY --from=builder /out/judge-worker /app/judge-worker
COPY services/judge-worker/configs/config.yaml /app/config.yaml
USER app
ENTRYPOINT ["/app/judge-worker"]
CMD ["-conf", "/app/config.yaml"]

