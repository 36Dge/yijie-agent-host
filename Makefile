.PHONY: dev test lint generate

dev:
	go run ./cmd/desktop-host

test:
	go test -race -cover ./...

lint:
	@test -z "$$(gofmt -l $$(find cmd internal -type f -name '*.go'))" || (gofmt -l $$(find cmd internal -type f -name '*.go') && exit 1)
	go vet ./...

generate:
	echo "No generated assets yet"
