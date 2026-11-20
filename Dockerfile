FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/kiusan .

FROM alpine:3.24
RUN addgroup -S -g 1000 kiusan
RUN adduser -S -u 1000 -G kiusan kiusan
RUN apk add --no-cache ca-certificates gosu
COPY --from=build /out/kiusan /usr/local/bin/kiusan
COPY entrypoint.sh /entrypoint.sh
RUN chmod 0755 /entrypoint.sh
ENV KIUSAN_DATA_DIR=/data
WORKDIR /data
# No USER directive on purpose: the entrypoint runs as root to normalize
# the data dir's ownership, then drops to KIUSAN_UID/KIUSAN_GID (default
# the kiusan user, 1000:1000) via gosu — see entrypoint.sh.
EXPOSE 8080
VOLUME /data
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
    CMD wget -qO- http://127.0.0.1:8080/ >/dev/null || exit 1
ENTRYPOINT ["/entrypoint.sh"]
