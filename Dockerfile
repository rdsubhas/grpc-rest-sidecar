FROM golang:1.12

WORKDIR /go/github.com/rdsubhas/grpc-rest-sidecar
ADD go.* ./
RUN go get && \
    go get -u github.com/golang/protobuf/protoc-gen-go

ADD . .
