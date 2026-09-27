FROM alpine:3.23

ARG TARGETARCH
ARG MINIO_VERSION=RELEASE.2025-09-07T16-13-09Z

RUN apk add --no-cache ca-certificates curl \
    && curl -fsSL -o /usr/local/bin/minio "https://github.com/minio/minio/releases/download/${MINIO_VERSION}/minio.linux-${TARGETARCH}.${MINIO_VERSION}" \
    && expected="$(curl -fsSL "https://github.com/minio/minio/releases/download/${MINIO_VERSION}/minio.linux-${TARGETARCH}.${MINIO_VERSION}.sha256sum" | awk '{print $1}')" \
    && actual="$(sha256sum /usr/local/bin/minio | awk '{print $1}')" \
    && test "$${expected}" = "$${actual}" \
    && chmod +x /usr/local/bin/minio \
    && apk del curl

ENTRYPOINT ["/usr/local/bin/minio"]
