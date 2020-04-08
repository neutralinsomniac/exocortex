#!/usr/bin/env bash
mkdir -p build
CC=x86_64-w64-mingw32-gcc CGO_ENABLED=1 GOOS=windows GOARCH=amd64 go build -o build/exogio-windows-amd64.exe -ldflags="-H windowsgui" ./cmd/exogio
CC=x86_64-w64-mingw32-gcc CGO_ENABLED=1 GOOS=windows GOARCH=amd64 go build -o build/exotui-windows-amd64.exe ./cmd/exotui
GOOS=linux GOARCH=amd64 go build -ldflags '-linkmode external -extldflags -static -w' -o build/exotui-linux-amd64 ./cmd/exotui
GOOS=linux GOARCH=amd64 go build -o build/exogio-linux-amd64 ./cmd/exogio
