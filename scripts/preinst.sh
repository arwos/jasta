#!/bin/bash

if [ -f "/var/log/jasta.log" ]; then
    touch /var/log/jasta.log
    chown www-data:www-data /var/log/jasta.log
fi

if [ -f "/etc/systemd/system/jasta.service" ]; then
    systemctl stop jasta
    systemctl disable jasta
    systemctl daemon-reload
fi
