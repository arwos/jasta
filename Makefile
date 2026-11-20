
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

.PHONY: run-web-local run-admin-local run-owner-bootstrap-local local-admin-key run_local
# Run in separate terminals; start web before admin so active snapshots can sync.
run-web-local:
	go run ./cmd/jasta/main.go web --config=config/config.dev.yaml

local-admin-key:
	@if [ ! -s /tmp/jasta-admin-dev-master.key ]; then \
		key_tmp=$$(mktemp /tmp/jasta-admin-dev-master.XXXXXX) && \
		trap 'sudo rm -f "$$key_tmp"' EXIT && \
		sudo chown root:"$$(id -gn)" "$$key_tmp" && \
		sudo chmod 0640 "$$key_tmp" && \
		sudo dd if=/dev/urandom of="$$key_tmp" bs=32 count=1 status=none && \
		sudo ln "$$key_tmp" /tmp/jasta-admin-dev-master.key; \
	fi

# Run once before run-admin-local; this command needs an interactive terminal.
run-owner-bootstrap-local: local-admin-key
	go run ./cmd/jasta/main.go admin owner-bootstrap --config=config/admin.dev.yaml --database=/tmp/jasta-admin-dev.sqlite --key-file=/tmp/jasta-admin-dev-master.key

run-admin-local: local-admin-key
	mkdir -p /tmp/jasta-admin-dev-nginx
	go run ./cmd/jasta/main.go admin --config=config/admin.dev.yaml

run_local: run-web-local

prerender_local:
	go run cmd/jasta/main.go prerender

deb:
	deb-builder build

local: build
	cp ./build/jasta_amd64 $(GOPATH)/bin/jasta
