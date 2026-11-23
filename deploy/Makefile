.DEFAULT_GOAL := help
.ONESHELL:

SHELL := /bin/sh
.SHELLFLAGS := -eu -c

DEPLOY_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
PROJECT_ROOT := $(abspath $(DEPLOY_DIR)/..)
ARTIFACT_DIR ?= $(PROJECT_ROOT)/build/deploy-transfer
PODMAN ?= podman
COMPOSE ?= $(PODMAN) compose
IMAGE := jasta:local

IMAGE_ARCHIVE := $(ARTIFACT_DIR)/jasta-image.tar
DEPLOY_ARCHIVE := $(ARTIFACT_DIR)/jasta-deploy.tar.gz

SVC_USER ?=
SVC_HOST ?=
SVC_PATH ?=
DEPLOY_MODE ?= auto
export SVC_USER SVC_HOST SVC_PATH DEPLOY_MODE

.PHONY: help check-vars image package transfer remote-deploy install update deploy owner-bootstrap

help:
	@printf '%s\n' \
		'make -C deploy image       Build jasta:local with Podman Compose' \
		'make -C deploy package    Save the image and deployment files under build/deploy-transfer' \
		'make -C deploy install SVC_USER=... SVC_HOST=... SVC_PATH=/absolute/path' \
		'make -C deploy update  SVC_USER=... SVC_HOST=... SVC_PATH=/absolute/path' \
		'make -C deploy deploy SVC_USER=... SVC_HOST=... SVC_PATH=/absolute/path (auto-detect mode)' \
		'make -C deploy owner-bootstrap SVC_USER=... SVC_HOST=... SVC_PATH=/absolute/path' \
		'                            Interactively create the initial admin owner'

check-vars:
	: "$${SVC_USER:?SVC_USER is required}"
	: "$${SVC_HOST:?SVC_HOST is required}"
	: "$${SVC_PATH:?SVC_PATH is required}"
	case "$$DEPLOY_MODE" in
		install|update|auto) ;;
		*) printf 'DEPLOY_MODE must be install, update, or auto\n' >&2; exit 2 ;;
	esac
	case "$$SVC_USER" in
		[!A-Za-z0-9_]*|*[!A-Za-z0-9_.-]*) printf 'SVC_USER must not start with a dash or contain unsupported characters\n' >&2; exit 2 ;;
	esac
	case "$$SVC_HOST" in
		[!A-Za-z0-9_]*|*[!A-Za-z0-9_.-]*) printf 'SVC_HOST must not start with a dash and must be an SSH alias, DNS name, or IPv4 address\n' >&2; exit 2 ;;
	esac
	case "$$SVC_PATH" in
		/*) ;;
		*) printf 'SVC_PATH must be an absolute remote path\n' >&2; exit 2 ;;
	esac
	case "$$SVC_PATH" in
		/|*/|*//*|*/../*|*/..|*/./*|*/.) printf 'SVC_PATH must be a normalized non-root path without whitespace\n' >&2; exit 2 ;;
		*[!A-Za-z0-9_./-]*) printf 'SVC_PATH contains unsupported characters\n' >&2; exit 2 ;;
	esac

image:
	cd "$(DEPLOY_DIR)"
	$(COMPOSE) -f compose.yaml build web
	$(PODMAN) image exists "$(IMAGE)"

package: image
	mkdir -p "$(ARTIFACT_DIR)"
	image_tmp="$(IMAGE_ARCHIVE).tmp.$$$$"
	rm -f "$$image_tmp"
	trap 'rm -f "$$image_tmp"' EXIT
	$(PODMAN) save --format docker-archive --output "$$image_tmp" "$(IMAGE)"
	mv -f "$$image_tmp" "$(IMAGE_ARCHIVE)"
	tar -czf "$(DEPLOY_ARCHIVE)" -C "$(DEPLOY_DIR)" compose.yaml config.yaml admin.yaml websites
	cd "$(ARTIFACT_DIR)"
	sha256sum jasta-image.tar > jasta-image.tar.sha256
	sha256sum jasta-deploy.tar.gz > jasta-deploy.tar.gz.sha256

transfer: check-vars package
	ssh "$$SVC_USER@$$SVC_HOST" "umask 077; mkdir -p '$$SVC_PATH/.incoming'; chmod 700 '$$SVC_PATH/.incoming'"
	scp \
		"$(IMAGE_ARCHIVE)" \
	"$(IMAGE_ARCHIVE).sha256" \
	"$(DEPLOY_ARCHIVE)" \
	"$(DEPLOY_ARCHIVE).sha256" \
	"$$SVC_USER@$$SVC_HOST:$$SVC_PATH/.incoming/"

remote-deploy: transfer
	ssh "$$SVC_USER@$$SVC_HOST" "SVC_HOST='$$SVC_HOST' SVC_PATH='$$SVC_PATH' DEPLOY_MODE='$(DEPLOY_MODE)' sh -s" <<'REMOTE'
	set -eu

	incoming="$$SVC_PATH/.incoming"
	compose_file="$$SVC_PATH/compose.yaml"
	legacy_dir="$$SVC_PATH/deploy"
	legacy_compose="$$legacy_dir/compose.yaml"
	if [ -f "$$legacy_compose" ]; then
		installed_mode=update
		layout=legacy
		installed_compose="$$legacy_compose"
	elif [ -f "$$compose_file" ]; then
		installed_mode=update
		layout=flat
		installed_compose="$$compose_file"
	else
		installed_mode=install
		layout=new
		installed_compose="$$compose_file"
	fi
	case "$$DEPLOY_MODE" in
		auto) DEPLOY_MODE="$$installed_mode" ;;
		install|update)
			if [ "$$DEPLOY_MODE" != "$$installed_mode" ]; then
				printf 'requested %s but target looks like %s (compose file: %s)\n' \
					"$$DEPLOY_MODE" "$$installed_mode" "$$installed_compose" >&2
				exit 2
			fi
			;;
		*) printf 'unsupported deployment mode: %s\n' "$$DEPLOY_MODE" >&2; exit 2 ;;
	esac
	printf 'Starting %s on %s:%s\n' "$$DEPLOY_MODE" "$$SVC_HOST" "$$SVC_PATH"
	cd "$$incoming"
	sha256sum -c jasta-image.tar.sha256
	sha256sum -c jasta-deploy.tar.gz.sha256

	podman load --input jasta-image.tar
	image_arch=$$(podman image inspect --format '{{.Architecture}}' jasta:local)
	host_arch=$$(podman info --format '{{.Host.Arch}}')
	if [ "$$image_arch" != "$$host_arch" ]; then
		printf 'image architecture %s does not match target Podman architecture %s\n' "$$image_arch" "$$host_arch" >&2
		exit 1
	fi
	mkdir -p "$$SVC_PATH"
	if [ "$$layout" = legacy ]; then
		if [ -d "$$legacy_dir/data" ] && [ -e "$$SVC_PATH/data" ]; then
			printf 'both legacy and flat data directories exist; merge them manually before retrying\n' >&2
			exit 2
		fi
		if [ -d "$$legacy_dir/websites" ] && [ -e "$$SVC_PATH/websites" ]; then
			printf 'both legacy and flat websites directories exist; merge them manually before retrying\n' >&2
			exit 2
		fi
		legacy_containers=$$(podman ps -aq --filter label=io.podman.compose.project=jasta)
		if [ -n "$$legacy_containers" ]; then
			cd "$$legacy_dir"
			podman compose -f compose.yaml down
		fi
		if [ -d "$$legacy_dir/data" ]; then
			mv "$$legacy_dir/data" "$$SVC_PATH/data"
		fi
		if [ -d "$$legacy_dir/websites" ]; then
			mv "$$legacy_dir/websites" "$$SVC_PATH/websites"
		fi
		# Runtime contains only the transient reload socket; recreate it at the flat path.
		rm -rf "$$legacy_dir/runtime"
	fi
	tar -xzf "$$incoming/jasta-deploy.tar.gz" -C "$$SVC_PATH"
	if [ "$$layout" = legacy ]; then
		rm -rf "$$legacy_dir"
	fi

	cd "$$SVC_PATH"
	podman compose -f compose.yaml up -d --no-build
	rm -rf "$$incoming"
	REMOTE

install:
	$(MAKE) --no-print-directory DEPLOY_MODE=install remote-deploy

update:
	$(MAKE) --no-print-directory DEPLOY_MODE=update remote-deploy

deploy: remote-deploy

owner-bootstrap: check-vars
	ssh -tt "$$SVC_USER@$$SVC_HOST" \
		"cd '$$SVC_PATH' && podman compose -f compose.yaml exec --user jasta-admin admin /usr/local/bin/jasta admin --config=/etc/jasta/admin.yaml owner-bootstrap --database=/var/lib/jasta/admin.sqlite --key-file=/var/lib/jasta/master.key"
