.PHONY: dev test runtime-test runtime-turn-test feat126-eval feat126-fake-readiness lint generate sync-contracts contract-check sync-feat136-contracts feat136-contract-check test-feat136 test-feat136-safe sync-feat137-contracts feat137-contract-check test-feat137 test-feat137-safe test-feat137-owner-authorized-fault skills-conformance

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
	# The current FEAT-137 pin is a strict superset of the FEAT-136 surface.
	# Its check also proves byte equality for Runtime v1 and session events v1-v5,
	# so the historical FEAT-136 regression gate must not downgrade the consumer.
	./scripts/check-feat137-contracts.sh

# The former prefix-wide recipe made future TestFEAT136 additions implicit.
# Keep the public target safe by routing it through the reviewed exact-name
# allowlist. No process-fault, permission, archive, symlink, or attack fixture
# test is selected by this target.
test-feat136: test-feat136-safe

test-feat136-safe: feat136-contract-check
	bash scripts/test-feat136-safe.sh

# FEAT-137 safe scoped contract workflow. It consumes only ordinary OpenAPI,
# schemas, compatibility manifests and JSON fixtures. Excluded fixture families
# are represented solely by immutable Git tree object IDs.
sync-feat137-contracts:
	./scripts/sync-feat137-contracts.sh
	$(MAKE) generate

feat137-contract-check:
	./scripts/check-feat137-contracts.sh

# The former ^TestFEAT137 prefix recipe mixed ordinary conformance with
# deterministic fault/drift hooks. That broad recipe was NOT RUN for the
# post-repair safe gate. The compatibility target now aliases the reviewed
# safe allowlist so newly added tests cannot enter evidence implicitly.
test-feat137: test-feat137-safe

test-feat137-safe: feat137-contract-check
	bash scripts/test-feat137-safe.sh

# Owner-only, test-process-local fault/drift evidence. This target is separate
# from every default gate and requires an exact per-invocation authorization.
# It may only use Go test hooks and temporary directories; it must never use a
# real process kill, permission sabotage, binary replacement, or attack fixture.
test-feat137-owner-authorized-fault:
	@test "$(YIJIE_FEAT137_OWNER_AUTHORIZED_TEST_INJECTION)" = "true" || \
		(echo "FEAT-137 fault/drift tests require YIJIE_FEAT137_OWNER_AUTHORIZED_TEST_INJECTION=true" >&2; exit 2)
	bash scripts/test-feat137-owner-authorized-fault.sh

skills-conformance:
	YIJIE_SKILLS_REPO="$(YIJIE_SKILLS_REPO_ABS)" ./scripts/check-skills-producer.sh --provenance-only
	$(MAKE) -C "$(YIJIE_SKILLS_REPO_ABS)" package
	$(MAKE) -C "$(YIJIE_SKILLS_REPO_ABS)" package-desktop-release
	YIJIE_SKILLS_REPO="$(YIJIE_SKILLS_REPO_ABS)" ./scripts/check-skills-producer.sh
	YIJIE_SKILLS_CONFORMANCE=1 \
		YIJIE_SKILLS_LOCAL_BUNDLE_ROOT="$(YIJIE_SKILLS_REPO_ABS)/dist/skill-packages" \
		YIJIE_SKILLS_DESKTOP_RELEASE_BUNDLE_ROOT="$(YIJIE_SKILLS_REPO_ABS)/dist/skill-packages-desktop-release" \
		go test -race ./internal/integration -run '^TestYijieSkillsV030DualChannelConformance$$' -count=1
