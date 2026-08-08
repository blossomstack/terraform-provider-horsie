.PHONY: build test vet fmt-check generate all

all: fmt-check vet test build

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
