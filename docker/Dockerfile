FROM golang:1.12.7 AS proto_base
ENV GO111MODULE=off
RUN apt-get update && apt-get -y install curl unzip
RUN curl -LO https://github.com/protocolbuffers/protobuf/releases/download/v3.9.1/protoc-3.9.1-linux-x86_64.zip && \
    unzip protoc-3.9.1-linux-x86_64.zip
RUN go get -u github.com/golang/protobuf/protoc-gen-go
COPY daemon/ daemon/
COPY Makefile .
COPY .mk/ .mk/
RUN make proto

FROM golang:1.12.7 AS build_base
WORKDIR /go/src/github.com/networkop/meshnet-cni
ENV GO111MODULE=on
COPY go.mod .
COPY go.sum .
RUN go mod download


FROM build_base AS build
COPY . .
COPY --from=proto_base /go/daemon/generated daemon/generated
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
