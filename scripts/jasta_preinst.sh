#!/bin/bash
set -e


do_install(){
	if [ -f "/etc/systemd/system/jasta.service" ]; then
		systemctl stop jasta
		systemctl disable jasta
		systemctl daemon-reload
	fi
}

do_upgrade(){
	if [ -f "/etc/systemd/system/jasta.service" ]; then
		systemctl stop jasta
		systemctl disable jasta
		systemctl daemon-reload
	fi
}

do_other(){

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
