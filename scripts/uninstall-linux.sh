#!/bin/sh
set -eu
if [ "$(id -u)" -ne 0 ]; then echo 'Run as root.' >&2; exit 1; fi
case "${1:-}" in
  --purge) ;;
  '') ;;
  *) echo 'Usage: uninstall-linux.sh [--purge]' >&2; exit 1 ;;
esac
systemctl disable --now altascitunnel
rm -f /etc/systemd/system/altascitunnel.service /usr/local/bin/altascitunnel
systemctl daemon-reload
if [ "${1:-}" = '--purge' ]; then
    # This is the fixed application state directory; refuse symlink traversal.
    if [ -L /var/lib/altascitunnel ]; then echo 'State directory is a symlink; refusing purge.' >&2; exit 1; fi
    rm -rf -- /var/lib/altascitunnel
    echo 'Uninstalled and removed the local identity.'
else
    echo 'Uninstalled. Local identity retained; use --purge for complete removal.'
fi
