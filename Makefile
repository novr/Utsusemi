.PHONY: test build worker-test

VERSION ?= dev
LDFLAGS := -s -w -X github.com/novr/utsusemi/internal/version.Version=$(VERSION)

test:
	go test ./...

build:
	go build -ldflags "$(LDFLAGS)" -o bin/utsusemi ./cmd/utsusemi

worker-test:
	cd worker && npm install && npm test
