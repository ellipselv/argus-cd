#!/bin/sh
set -e

if [ -d /run/systemd/system ] && systemctl is-active --quiet argus.service; then
    systemctl stop argus.service
    systemctl disable argus.service
fi
