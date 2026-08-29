.PHONY: dev test runtime-test runtime-turn-test feat126-eval feat126-fake-readiness lint generate sync-contracts contract-check sync-feat136-contracts feat136-contract-check test-feat136 skills-conformance

YIJIE_SKILLS_REPO ?= ../yijie-skills
YIJIE_SKILLS_REPO_ABS := $(abspath $(YIJIE_SKILLS_REPO))

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

feat126-fake-readiness:
	YIJIE_FEAT126_S10_TEST_PROFILE_ENABLED=true go run ./cmd/feat126-fake-readiness

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

# FEAT-136 safe scoped contract workflow. These targets never inspect archive,
# checksum-corruption, Zip Slip, or archive-error fixture blobs.
sync-feat136-contracts:
	./scripts/sync-feat136-contracts.sh

feat136-contract-check:
	./scripts/check-feat136-contracts.sh

# Safe FEAT-136 evidence only. Full repository suites include pre-existing
# archive, permission, symlink and process-fault scenarios outside this gate.
test-feat136: feat136-contract-check
	go test -race ./internal/session -run '^TestFEAT136' -count=1
	go test -race ./internal/app -run '^TestFEAT136' -count=1
	go test ./internal/app -run '^TestRuntimeCompatibilityProjectionMatchesHostAdapter$$' -count=1

skills-conformance:
	YIJIE_SKILLS_REPO="$(YIJIE_SKILLS_REPO_ABS)" ./scripts/check-skills-producer.sh --provenance-only
	$(MAKE) -C "$(YIJIE_SKILLS_REPO_ABS)" package
	$(MAKE) -C "$(YIJIE_SKILLS_REPO_ABS)" package-desktop-release
	YIJIE_SKILLS_REPO="$(YIJIE_SKILLS_REPO_ABS)" ./scripts/check-skills-producer.sh
	YIJIE_SKILLS_CONFORMANCE=1 \
		YIJIE_SKILLS_LOCAL_BUNDLE_ROOT="$(YIJIE_SKILLS_REPO_ABS)/dist/skill-packages" \
		YIJIE_SKILLS_DESKTOP_RELEASE_BUNDLE_ROOT="$(YIJIE_SKILLS_REPO_ABS)/dist/skill-packages-desktop-release" \
		go test -race ./internal/integration -run '^TestYijieSkillsV030DualChannelConformance$$' -count=1
