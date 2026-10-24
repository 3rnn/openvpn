FROM golang:1.27.3-trixie@sha256:fbb7ae40b62ff8afe31662df570d39b4f5052ac32b90dae8e60d7657a20a74f4 AS builder
WORKDIR /src
ENV GOTOOLCHAIN=local CGO_ENABLED=0
COPY src/openvpn-web/go.mod src/openvpn-web/go.sum ./
RUN go mod download && go mod verify
COPY src/openvpn-web/ ./
RUN go build -mod=readonly -trimpath -buildvcs=false -ldflags='-s -w' -o /out/openvpn-web .

FROM debian:13-slim@sha256:62bd423aeee912639ca8ee681c2b762cb3e25137e0a92b20be455898a9f034ad
# Signed Debian repositories supply the OpenVPN/EasyRSA system tools, never an
# upstream application executable. Rebuilds may receive security package updates.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates tzdata openssl openvpn easy-rsa iproute2 iptables nftables \
    bash supervisor curl sqlite3 jq media-types coreutils util-linux \
 && rm -rf /var/lib/apt/lists/* \
 && ln -s /usr/share/easy-rsa/easyrsa /usr/local/bin/easyrsa
COPY --from=builder /out/openvpn-web /usr/local/bin/openvpn-web
COPY --chmod=0755 build/docker-entrypoint.sh /usr/bin/docker-entrypoint.sh
COPY --chmod=0755 build/openvpn-auth /usr/lib/openvpn/plugins/openvpn-auth
COPY build/supervisord.conf /etc/supervisor/conf.d/supervisord.conf
ARG VCS_REF=unknown
ARG VERSION=unreleased
ARG SOURCE_URL
LABEL org.opencontainers.image.title="OpenVPN hardened independent fork" \
      org.opencontainers.image.description="Source-built OpenVPN Web on Debian 13" \
      org.opencontainers.image.revision=$VCS_REF \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.source=$SOURCE_URL \
      org.opencontainers.image.licenses="MIT"
ENV GIN_MODE=release OVPN_DATA=/data
VOLUME ["/data"]
EXPOSE 1194/udp 8833
# Root is required by OpenVPN/TUN/nft and PKI management. No privileged mode.
USER root
WORKDIR /data
ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["/usr/bin/supervisord"]
