.PHONY: dev test runtime-test runtime-turn-test feat126-eval lint generate sync-contracts contract-check

dev:
	go run ./cmd/desktop-host

test:
	$(MAKE) contract-check
	go test -race -cover ./...

runtime-test:
	./scripts/test-runtime-integration.sh

runtime-turn-test:
	./scripts/test-runtime-turn-integration.sh

feat126-eval:
	go test ./internal/session -run '^TestFEAT126FakeProviderEval$$' -count=1 -v

lint:
	@test -z "$$(gofmt -l $$(find cmd internal -type f -name '*.go'))" || (gofmt -l $$(find cmd internal -type f -name '*.go') && exit 1)
	go vet ./...
	bash -n scripts/*.sh

generate:
	go tool oapi-codegen -generate types -package agenthostcontract -o internal/contracts/agenthost.gen.go api/openapi/agent-host.yaml
	gofmt -w internal/contracts/agenthost.gen.go

sync-contracts:
	./scripts/sync-contracts.sh
	$(MAKE) generate

contract-check:
	./scripts/check-contracts.sh
