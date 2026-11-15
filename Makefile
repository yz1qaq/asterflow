ASTERFLOW_GO := env -u GOROOT GOTOOLCHAIN=go1.26.6 go

.PHONY: docs-check deps-check test vet build run check

docs-check:
	python3 scripts/check_repo.py
	python3 -m unittest discover -s scripts -p 'test_*.py' -v
	git diff --check

deps-check:
	$(ASTERFLOW_GO) mod tidy -diff

test:
	GIN_MODE=test $(ASTERFLOW_GO) test -race -count=1 ./...

vet:
	$(ASTERFLOW_GO) vet ./...

build:
	mkdir -p bin
	$(ASTERFLOW_GO) build -mod=readonly -o bin/asterflow ./cmd/server

run:
	$(ASTERFLOW_GO) run ./cmd/server

check: docs-check deps-check test vet build
