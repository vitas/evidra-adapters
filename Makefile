.PHONY: test build lint fmt tidy

test:
	go test -race -cover ./...

build:
	go build -o bin/evidra-adapter-terraform ./cmd/evidra-adapter-terraform

lint:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy
