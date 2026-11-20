#!/bin/bash
set -e


do_remove(){
	stop_services
}

do_upgrade(){
	stop_services
}

stop_services(){
	systemctl disable --now jasta-admin.service jasta-web.service 2>/dev/null || true
	systemctl disable --now jasta.service 2>/dev/null || true
}

do_other(){
	:
}

case "$1" in
  remove)
    do_remove
    ;;
  upgrade)
    do_upgrade
    ;;
  *)
    do_other
    ;;
esac
