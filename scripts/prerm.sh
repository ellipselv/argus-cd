#!/bin/sh
set -e

if [ -d /run/systemd/system ] && systemctl is-active --quiet arguscd.service; then
    systemctl stop arguscd.service
    systemctl disable arguscd.service
fi
