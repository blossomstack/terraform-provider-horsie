.PHONY: build test vet fmt-check generate docs docs-check all

all: fmt-check vet test build docs-check

build:
	go build ./...

test:
	go test ./...

vet:
	go vet ./...

fmt-check:
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then echo "not gofmt-clean:"; echo "$$out"; exit 1; fi

# Regenerate the wire types from horsie's schemas. HORSIE_FLUORITE points at a
# horsie checkout's crates/models/fluorite. `clean` first: generation overwrites
# but never deletes, so a renamed type would leave a stale file behind.
HORSIE_FLUORITE ?= ../horsie/crates/models/fluorite
generate:
	fluorite clean --output internal/horsieapi
	fluorite go --inputs $(HORSIE_FLUORITE) --output internal/horsieapi --package-name horsieapi
	@out="$$(gofmt -l internal/horsieapi)"; \
	if [ -n "$$out" ]; then echo "generated Go is not gofmt-clean: $$out"; exit 1; fi

# Pinned, not `@latest`: an unpinned tool changed its own minimum Go version
# under CI and broke a build that had not changed.
TFPLUGINDOCS_VERSION ?= v0.25.0

# Registry documentation, generated from the schemas plus examples/.
docs:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@$(TFPLUGINDOCS_VERSION) generate --provider-name horsie

# Fail if docs/ is stale. The Registry serves whatever is committed, so a schema
# change with no regenerate ships documentation that describes the old provider.
docs-check: docs
	@out="$$(git status --porcelain docs/)"; \
	if [ -n "$$out" ]; then echo "docs/ is stale — run 'make docs' and commit:"; echo "$$out"; exit 1; fi
