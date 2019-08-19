FROM golang:1.12.7 AS build_base

WORKDIR /go/src/github.com/networkop/meshnet-cni
ENV GO111MODULE=on

COPY go.mod .
COPY go.sum .

RUN go mod download

FROM build_base AS build

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o meshnet plugin/meshnet.go 
RUN CGO_ENABLED=0 GOOS=linux go build -o meshnetd daemon/*.go


FROM alpine:latest

RUN apk add --no-cache jq

COPY --from=build /go/src/github.com/networkop/meshnet-cni/meshnet /
COPY --from=build /go/src/github.com/networkop/meshnet-cni/meshnetd /

COPY etc/cni/net.d/meshnet.conf /
COPY docker/entrypoint.sh /

RUN chmod +x ./entrypoint.sh
RUN chmod +x /meshnetd

RUN mkdir /lib64 && ln -s /lib/libc.musl-x86_64.so.1 /lib64/ld-linux-x86-64.so.2

ENTRYPOINT ./entrypoint.sh
