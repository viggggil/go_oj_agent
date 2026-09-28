FROM alpine:3.23

ARG TARGETARCH
ARG MC_VERSION=RELEASE.2025-08-13T08-35-41Z

RUN apk add --no-cache ca-certificates curl \
    && curl -fsSL -o /usr/local/bin/mc "https://github.com/minio/mc/releases/download/${MC_VERSION}/mc.linux-${TARGETARCH}.${MC_VERSION}" \
    && expected="$(curl -fsSL "https://github.com/minio/mc/releases/download/${MC_VERSION}/mc.linux-${TARGETARCH}.${MC_VERSION}.sha256sum" | awk '{print $1}')" \
    && actual="$(sha256sum /usr/local/bin/mc | awk '{print $1}')" \
    && test "${expected}" = "${actual}" \
    && chmod +x /usr/local/bin/mc \
    && apk del curl

ENTRYPOINT ["/usr/local/bin/mc"]
