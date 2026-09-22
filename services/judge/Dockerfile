# syntax=docker/dockerfile:1.7

FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY api ./api
COPY errors ./errors
COPY pkg ./pkg
COPY services/judge ./services/judge
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/judge-service ./services/judge/cmd/judge-service

FROM alpine:3.23
RUN apk add --no-cache netcat-openbsd
RUN addgroup -S -g 10001 app && adduser -S -D -H -u 10001 -G app app
WORKDIR /app
COPY --from=builder /out/judge-service /app/judge-service
COPY services/judge/configs/config.yaml /app/config.yaml
USER app
EXPOSE 9003
ENTRYPOINT ["/app/judge-service"]
CMD ["-conf", "/app/config.yaml"]
