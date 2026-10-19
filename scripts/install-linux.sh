#!/bin/sh
set -eu
if [ "$(id -u)" -ne 0 ]; then echo 'Run as root.' >&2; exit 1; fi
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
project_dir=$(dirname "$script_dir")
binary=${1:-"$project_dir/bin/altascitunnel"}
if [ ! -f "$binary" ]; then echo "Binary not found: $binary" >&2; exit 1; fi
if ! id altascitunnel >/dev/null 2>&1; then useradd --system --home-dir /var/lib/altascitunnel --shell /usr/sbin/nologin altascitunnel; fi
install -m 0755 "$binary" /usr/local/bin/altascitunnel
install -d -m 0700 -o altascitunnel -g altascitunnel /var/lib/altascitunnel
install -m 0644 "$project_dir/deploy/systemd/altascitunnel.service" /etc/systemd/system/altascitunnel.service
systemctl daemon-reload
systemctl enable altascitunnel
# enable --now leaves an already running process on the old executable during
# an upgrade. Restart after installing so both new and existing installs use it.
systemctl restart altascitunnel
echo 'Installed. Run the register command from the console as root.'
