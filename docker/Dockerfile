FROM golang:1.11 AS build
WORKDIR /meshnet
COPY plugin/ ./plugin
COPY daemon/ ./daemon
COPY go.* ./
RUN CGO_ENABLED=0 GOOS=linux go build -o meshnet plugin/meshnet.go
RUN CGO_ENABLED=0 GOOS=linux go build -o meshnetd daemon/main.go daemon/handler.go daemon/daemon.go


FROM alpine:latest

RUN apk add --no-cache jq

COPY --from=build /meshnet/meshnet /
COPY --from=build /meshnet/meshnetd /

COPY etc/cni/net.d/meshnet.conf /
COPY docker/entrypoint.sh /

RUN chmod +x ./entrypoint.sh
RUN chmod +x /meshnetd

RUN mkdir /lib64 && ln -s /lib/libc.musl-x86_64.so.1 /lib64/ld-linux-x86-64.so.2

ENTRYPOINT ./entrypoint.sh
