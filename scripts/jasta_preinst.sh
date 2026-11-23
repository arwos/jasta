#!/bin/bash
set -e


do_install(){
	stop_legacy_service
}

do_upgrade(){
	stop_legacy_service
}

stop_legacy_service(){
	if [ -f "/etc/systemd/system/jasta.service" ] || [ -f "/usr/lib/systemd/system/jasta.service" ]; then
		systemctl disable --now jasta.service || true
		systemctl daemon-reload
	fi
}

do_other(){
	:
}

case "$1" in
  install)
    do_install
    ;;
  upgrade)
    do_upgrade
    ;;
  *)
    do_other
    ;;
esac
