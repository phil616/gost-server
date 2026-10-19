#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
mkdir -p bin
for app in altasci-control altasci-edge altascitunnel; do go build -trimpath -o "bin/$app" "./cmd/$app"; done
(cd web && npm ci --no-audit --no-fund && npm run build)
node scripts/collect-licenses.cjs web/dist/licenses web
(cd bin && sha256sum altasci-control altasci-edge altascitunnel > checksums.txt)
