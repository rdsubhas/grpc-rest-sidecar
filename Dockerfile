FROM golang:1.10-stretch

WORKDIR /tmp/src
ADD src .
RUN ./prepare.sh
