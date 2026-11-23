#!/bin/bash
set -e


do_remove(){
	systemctl daemon-reload || true
	if systemctl is-active --quiet nginx; then
		nginx -t && systemctl reload nginx || true
	fi
}

do_purge(){
	do_remove
	rm -rf /var/lib/jasta
	rm -f /etc/jasta/master.key
}

do_other(){
	:
}

case "$1" in
  remove)
    do_remove
    ;;
  upgrade)
    systemctl daemon-reload || true
    ;;
  purge)
    do_purge
    ;;
  *)
    do_other
    ;;
esac
