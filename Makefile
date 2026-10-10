.PHONY: run build test race vet browser clean

run:
	go run ./cmd/server

build:
	go build -trimpath -ldflags="-s -w" -o bin/family-party ./cmd/server

test:
	go test ./...
	node --test tests/client.test.mjs

race:
	go test -race -cover ./...

vet:
	go vet ./...

browser:
	python tests/browser_smoke.py --bridge

clean:
	rm -rf bin test-results
