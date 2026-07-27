.PHONY: test build worker-test

test:
	go test ./...

build:
	go build -o bin/utsusemi ./cmd/utsusemi

worker-test:
	cd worker && npm install && npm test
