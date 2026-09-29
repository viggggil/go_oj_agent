# syntax=docker/dockerfile:1.7

FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY api ./api
COPY errors ./errors
COPY pkg ./pkg
COPY services/contest ./services/contest
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/contest-service ./services/contest/cmd/contest-service

FROM alpine:3.23
RUN addgroup -S -g 10001 app && adduser -S -D -H -u 10001 -G app app \
    && apk add --no-cache netcat-openbsd
WORKDIR /app
COPY --from=builder /out/contest-service /app/contest-service
COPY services/contest/configs/config.yaml /app/config.yaml
USER app
EXPOSE 9005
ENTRYPOINT ["/app/contest-service"]
CMD ["-conf", "/app/config.yaml"]
