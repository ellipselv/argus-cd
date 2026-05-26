#!/bin/sh
set -e

mkdir -p /etc/argus
mkdir -p /opt/argus/apps
chmod 700 /etc/argus
chmod 755 /opt/argus/apps

if [ ! -f /etc/argus/config.toml ]; then
    cp /etc/argus/config.toml.template /etc/argus/config.toml
    chmod 600 /etc/argus/config.toml
fi

if [ -d /run/systemd/system ]; then
    systemctl daemon-reload
    systemctl enable argus.service
    systemctl start argus.service
fi
