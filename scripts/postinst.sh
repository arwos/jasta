#!/bin/bash


if [ -f "/etc/systemd/system/jasta.service" ]; then
    systemctl start jasta
    systemctl enable jasta
    systemctl daemon-reload
fi
