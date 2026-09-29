FROM criyle/go-judge:v1.12.3 AS gojudge

FROM golang:1.26-bookworm
RUN apt-get update \
    && apt-get install -y --no-install-recommends openjdk-17-jdk-headless python3 \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /opt
COPY --from=gojudge /opt/go-judge /opt/go-judge
COPY --from=gojudge /opt/mount.yaml /opt/mount.yaml
# The runtime image installs Debian OpenJDK 17; the upstream mount template
# targets OpenJDK 21 and would leave java.security unavailable in the sandbox.
RUN sed -i 's|/etc/java-21-openjdk|/etc/java-17-openjdk|g' /opt/mount.yaml
ENTRYPOINT ["/opt/go-judge"]
