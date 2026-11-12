FROM alpine:3.24

RUN apk add --no-cache \
        ca-certificates \
        bash \
    && rm -f /var/cache/apk/*

ARG VERSION
ENV DAPPERDOX ${VERSION}

COPY bin/dapperdox /usr/local/bin/dapperdox
COPY assets /opt/dapperdox/assets
ENV DEFAULT_ASSETS_DIR=/opt/dapperdox/assets

CMD ["/usr/local/bin/dapperdox"]
