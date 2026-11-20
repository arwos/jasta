#!/bin/bash
set -euo pipefail

arch=${1:?usage: scripts/build-deb.sh amd64|arm64}
host_arch=$(dpkg --print-architecture)

case "$arch" in
	amd64)
		compiler=${JASTA_CC_AMD64:-gcc}
		if [ "$host_arch" != amd64 ]; then
			compiler=${JASTA_CC_AMD64:-x86_64-linux-gnu-gcc}
		fi
		;;
	arm64)
		compiler=${JASTA_CC_ARM64:-gcc}
		if [ "$host_arch" != arm64 ]; then
			compiler=${JASTA_CC_ARM64:-aarch64-linux-gnu-gcc}
		fi
		;;
	*)
		echo "unsupported Debian architecture: $arch" >&2
		exit 2
		;;
esac

if ! command -v "$compiler" >/dev/null 2>&1; then
	echo "missing C compiler '$compiler' for $arch CGO build" >&2
	if [ "$arch" = arm64 ]; then
		echo "install gcc-aarch64-linux-gnu or build natively on arm64" >&2
	fi
	exit 1
fi

make build-admin
CC="$compiler" goppy build --arch="$arch" --cgo
chmod 0755 "build/jasta_$arch"
