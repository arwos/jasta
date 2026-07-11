#!/bin/bash
set -e


do_configure(){
	if [ -f "/etc/systemd/system/jasta.service" ]; then
		systemctl start jasta
		systemctl enable jasta
		systemctl daemon-reload
	fi
}

do_other(){

}

case "$1" in
  configure)
    do_configure
    ;;
  *)
    do_other
    ;;
esac
