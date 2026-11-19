FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/kiusan .

FROM alpine:3.24
RUN addgroup -S kiusan && adduser -S kiusan -G kiusan \
 && apk add --no-cache ca-certificates
COPY --from=build /out/kiusan /usr/local/bin/kiusan
ENV KIUSAN_DATA_DIR=/data
WORKDIR /data
USER kiusan
EXPOSE 8080
VOLUME /data
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD wget -qO- http://127.0.0.1:8080/ >/dev/null || exit 1
ENTRYPOINT ["kiusan"]
