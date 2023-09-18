
.PHONY: install
install:
	go install github.com/osspkg/devtool@latest
	go install github.com/dewep-online/deb-builder/cmd/deb-builder@latest

.PHONY: setup
setup:
	devtool setup-lib

.PHONY: lint
lint:
	devtool lint

.PHONY: license
license:
	devtool license

.PHONY: build
build:
	devtool build --arch=amd64

.PHONY: tests
tests:
	devtool test

.PHONY: pre-commite
pre-commite: setup lint build tests

.PHONY: ci
ci: install setup lint build tests

run_local:
	go run cmd/jasta/main.go --config=config/config.dev.yaml

debpkg: build
	deb-builder build --base-dir=build --tmp-dir=/tmp