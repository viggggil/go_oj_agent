FROM criyle/go-judge:v1.12.3 AS gojudge

FROM golang:1.26-bookworm
WORKDIR /opt
COPY --from=gojudge /opt/go-judge /opt/go-judge
COPY --from=gojudge /opt/mount.yaml /opt/mount.yaml
ENTRYPOINT ["/opt/go-judge"]

