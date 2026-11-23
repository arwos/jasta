#!/bin/sh

set -eu

DATA_DIR=/var/lib/jasta
RUNTIME_DIR=/run/jasta
MASTER_KEY="$DATA_DIR/master.key"
RELOAD_SOCKET="$RUNTIME_DIR/web-reload.sock"

fail() {
	printf 'jasta container: %s\n' "$1" >&2
	exit 1
}

prepare_runtime() {
	mkdir -p "$RUNTIME_DIR"
	chown jasta-web:jasta-content "$RUNTIME_DIR"
	chmod 2770 "$RUNTIME_DIR"
}

prepare_data() {
	mkdir -p \
		"$DATA_DIR/releases" \
		"$DATA_DIR/certificates" \
		"$DATA_DIR/acme-challenges" \
		"$DATA_DIR/nginx"
	chown jasta-admin:jasta-content "$DATA_DIR" "$DATA_DIR/releases"
	chmod 0750 "$DATA_DIR"
	chmod 2750 "$DATA_DIR/releases"
	chown jasta-admin:jasta-admin \
		"$DATA_DIR/certificates" \
		"$DATA_DIR/acme-challenges" \
		"$DATA_DIR/nginx"
	chmod 0700 \
		"$DATA_DIR/certificates" \
		"$DATA_DIR/acme-challenges" \
		"$DATA_DIR/nginx"
}

has_existing_state() {
	for path in \
		"$DATA_DIR/admin.sqlite" \
		"$DATA_DIR/admin.sqlite-wal" \
		"$DATA_DIR/admin.sqlite-shm" \
		"$DATA_DIR/releases"/* \
		"$DATA_DIR/certificates"/*; do
		if [ -e "$path" ] || [ -L "$path" ]; then
			return 0
		fi
	done
	return 1
}

ensure_master_key() {
	if [ -L "$MASTER_KEY" ]; then
		fail "$MASTER_KEY must not be a symlink"
	fi

	if [ ! -e "$MASTER_KEY" ]; then
		if has_existing_state; then
			fail "master key is missing while application data already exists; restore the original key before starting admin"
		fi

		temporary="$MASTER_KEY.tmp.$$"
		trap 'rm -f "$temporary"' EXIT HUP INT TERM
		(umask 077; dd if=/dev/urandom of="$temporary" bs=32 count=1 2>/dev/null)
		chown root:jasta-admin "$temporary"
		chmod 0640 "$temporary"
		mv "$temporary" "$MASTER_KEY"
		trap - EXIT HUP INT TERM
	fi

	if [ ! -f "$MASTER_KEY" ] || [ -L "$MASTER_KEY" ]; then
		fail "$MASTER_KEY must be a regular file"
	fi

	key_size=$(wc -c < "$MASTER_KEY" | tr -d '[:space:]')
	case "$key_size" in
		32|64) ;;
		*) fail "$MASTER_KEY must contain 32 raw bytes or 64 hexadecimal characters" ;;
	esac
	chown root:jasta-admin "$MASTER_KEY"
	chmod 0640 "$MASTER_KEY"
}

wait_for_web_socket() {
	attempt=0
	while [ ! -S "$RELOAD_SOCKET" ]; do
		if [ "$attempt" -ge 60 ]; then
			fail "web reload socket did not become ready within 60 seconds"
		fi
		sleep 1
		attempt=$((attempt + 1))
	done
}

if [ "$(id -u)" -ne 0 ]; then
	fail "entrypoint must start as root so it can initialize bind-mounted permissions"
fi

case "${1:-}" in
	web)
		prepare_runtime
		exec su-exec jasta-web /usr/local/bin/jasta web --config=/etc/jasta/config.yaml
		;;
	admin)
		prepare_data
		ensure_master_key
		wait_for_web_socket
		exec su-exec jasta-admin /usr/local/bin/jasta admin --config=/etc/jasta/admin.yaml
		;;
	*)
		fail "expected service role 'web' or 'admin'"
		;;
esac
