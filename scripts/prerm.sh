#!/bin/bash


if [ -f "/etc/systemd/system/jasta.service" ]; then
    systemctl stop jasta
    systemctl disable jasta
    systemctl daemon-reload
fi
