
.PHONY: install
install:
	go install go.osspkg.com/goppy/v3/cmd/goppy@latest
	goppy setup-lib

.PHONY: lint
lint:
	goppy lint

.PHONY: license
license:
	goppy license

.PHONY: build-admin
build-admin:
	cd ui/admin && pnpm build

.PHONY: build
build: build-admin
	goppy build --arch=amd64 --cgo

.PHONY: tests
tests:
	goppy test

.PHONY: pre-commit
pre-commit: lint build tests

.PHONY: ci
ci: install pre-commit

run_local:
	go run cmd/jasta/main.go --config=config/config.dev.yaml --config-recovery

prerender_local:
	go run cmd/jasta/main.go prerender

deb:
	deb-builder build

local: build
	cp ./build/jasta_amd64 $(GOPATH)/bin/jasta
