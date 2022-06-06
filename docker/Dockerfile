# GRPC generation
FROM golang:1.18.2 AS proto_base

WORKDIR /src

RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
RUN go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
RUN go install github.com/bufbuild/buf/cmd/buf@latest

COPY daemon/proto .
COPY buf.gen.yaml .
COPY buf.yaml .

RUN buf lint && buf generate -v

# go mod download

FROM golang:1.18.2 AS build_base

WORKDIR /go/src/github.com/networkop/meshnet-cni

COPY go.mod .
COPY go.sum .
RUN go mod download

# Building the binaries

FROM --platform=${BUILDPLATFORM:-linux/amd64} build_base AS build

ENV CGO_ENABLED=0
ARG LDFLAGS
ARG TARGETOS
ARG TARGETARCH

COPY daemon/ daemon/
COPY api/ api/
COPY plugin/ plugin/
COPY --from=proto_base /src/ .

RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags "${LDFLAGS}" -o meshnet plugin/meshnet.go
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags "${LDFLAGS}" -o meshnetd daemon/main.go

FROM alpine:latest
RUN apk add --no-cache jq
ADD https://raw.githubusercontent.com/stedolan/jq/master/COPYING /third_party/licenses/jq/
COPY --from=build /go/src/github.com/networkop/meshnet-cni/meshnet /
COPY --from=build /go/src/github.com/networkop/meshnet-cni/meshnetd /
#COPY etc/cni/net.d/meshnet.conf /
COPY docker/new-entrypoint.sh /entrypoint.sh
COPY LICENSE /
RUN chmod +x ./entrypoint.sh
RUN chmod +x /meshnetd
#RUN mkdir /lib64 && ln -s /lib/libc.musl-x86_64.so.1 /lib64/ld-linux-x86-64.so.2

ENTRYPOINT ./entrypoint.sh
