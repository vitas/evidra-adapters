.PHONY: test build lint fmt tidy clean smoke

BINARY := bin/evidra-adapter-terraform

test:
	go test -race -cover ./...

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY) ./cmd/evidra-adapter-terraform

lint:
	go vet ./...
	@test -z "$$(gofmt -l .)" || { echo "gofmt:"; gofmt -l .; exit 1; }

fmt:
	gofmt -w .

tidy:
	go mod tidy

clean:
	rm -rf bin/ dist/

# Smoke test: build binary and run against fixtures
smoke: build
	@echo '--- simple_create fixture ---'
	./$(BINARY) < terraform/testdata/simple_create.json | jq .
	@echo '--- empty stdin (expect exit 2) ---'
	printf '' | ./$(BINARY) --json-errors 2>&1; test $$? -eq 2
	@echo '--- invalid JSON (expect exit 1) ---'
	echo 'not json' | ./$(BINARY) --json-errors 2>&1; test $$? -eq 1
	@echo 'All smoke tests passed'
