
.PHONY: install
install:
	go install go.osspkg.com/goppy/v2/cmd/goppy@latest
	goppy setup-lib

.PHONY: lint
lint:
	goppy lint

.PHONY: license
license:
	goppy license

.PHONY: build
build:
	goppy build --arch=amd64

.PHONY: tests
tests:
	goppy test

.PHONY: pre-commit
pre-commit: lint build tests

.PHONY: ci
ci: install pre-commit

run_local:
	go run cmd/jasta/main.go --config=config/config.dev.yaml

prerender_local:
	go run cmd/jasta/main.go prerender

deb: build
	deb-builder build

local: build
	cp ./build/jasta_amd64 $(GOPATH)/bin/jasta