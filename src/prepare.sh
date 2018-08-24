#!/usr/bin/env sh
set -ex

apt-get -qq update
apt-get -qq -y install unzip

curl -fsSL "https://github.com/protocolbuffers/protobuf/releases/download/v3.6.1/protoc-3.6.1-linux-x86_64.zip" > protoc.zip
unzip -j protoc.zip bin/protoc -d /usr/bin/
protoc --version
