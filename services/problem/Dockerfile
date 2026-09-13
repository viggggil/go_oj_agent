# syntax=docker/dockerfile:1.7

FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY api ./api
COPY errors ./errors
COPY services/problem ./services/problem
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/problem-service ./services/problem/cmd/problem-service

FROM alpine:3.23
RUN addgroup -S -g 10001 app && adduser -S -D -H -u 10001 -G app app
WORKDIR /app
COPY --from=builder /out/problem-service /app/problem-service
COPY services/problem/configs/config.yaml /app/config.yaml
USER app
EXPOSE 9002
ENTRYPOINT ["/app/problem-service"]
CMD ["-conf", "/app/config.yaml"]
