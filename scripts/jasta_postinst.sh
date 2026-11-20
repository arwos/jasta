#!/bin/bash
set -e


do_configure(){
	if [ -f /etc/sudoers.d/jasta ] && [ ! -L /etc/sudoers.d/jasta ]; then
		chown root:root /etc/sudoers.d/jasta
		chmod 0440 /etc/sudoers.d/jasta
	fi
	systemd-sysusers --create /usr/lib/sysusers.d/jasta.conf
	systemd-tmpfiles --create /usr/lib/tmpfiles.d/jasta.conf
	if [ ! -e /etc/jasta/master.key ]; then
		key_tmp=$(mktemp /etc/jasta/master.key.XXXXXX)
		trap 'rm -f "$key_tmp"' EXIT
		dd if=/dev/urandom of="$key_tmp" bs=32 count=1 status=none
		chown root:jasta-admin "$key_tmp"
		chmod 0640 "$key_tmp"
		if [ ! -e /etc/jasta/master.key ]; then
			mv "$key_tmp" /etc/jasta/master.key
		fi
		rm -f "$key_tmp"
		trap - EXIT
	fi
	systemctl daemon-reload
	systemctl enable --now jasta-web.service
	systemctl enable --now jasta-admin.service
	if systemctl is-active --quiet nginx; then
		nginx -t
		systemctl restart nginx
	fi
}

do_other(){
	:
}

case "$1" in
  configure)
    do_configure
    ;;
  *)
    do_other
    ;;
esac
