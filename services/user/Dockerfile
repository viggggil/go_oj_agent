# syntax=docker/dockerfile:1.7

FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY api ./api
COPY errors ./errors
COPY pkg ./pkg
COPY services/user ./services/user

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/user-service \
    ./services/user/cmd/user-service

FROM alpine:3.23

RUN addgroup -S -g 10001 app \
    && adduser -S -D -H -u 10001 -G app app

WORKDIR /app

COPY --from=builder /out/user-service /app/user-service
COPY services/user/configs/config.yaml /app/config.yaml

USER app

EXPOSE 9001

ENTRYPOINT ["/app/user-service"]
CMD ["-conf", "/app/config.yaml"]
