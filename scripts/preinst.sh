#!/bin/bash


if ! [ -d /var/lib/jasta/ ]; then
    mkdir /var/lib/jasta
fi

if [ -f "/etc/systemd/system/jasta.service" ]; then
    systemctl stop jasta
    systemctl disable jasta
    systemctl daemon-reload
fi
