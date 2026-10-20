#!/usr/bin/env bash
# Extracted from the release image and invoked by CNB over SSH.
set -euo pipefail
set +x

die() { echo "部署失败：$*" >&2; exit 1; }
[[ $# -ge 1 && $# -le 5 ]] || die "用法：$0 IMAGE [ENV_FILE] [DATA_VOLUME] [NETWORK] [POSTGRES_IMAGE]"
image=$1
env_file=${2:-/run/baoctl/altasci-tunnel.env}
volume=${3:-altasci-server-data}
network=${4:-bridge}
postgres_image=${5:-postgres:18}
container=altasci-tunnel
lock_dir=${ALTASCI_DEPLOY_LOCK_DIR:-/run/lock}
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)

[[ -r "$env_file" ]] || die "无法读取环境文件：$env_file"
command -v docker >/dev/null || die "需要 Docker"
command -v flock >/dev/null || die "需要 flock（util-linux）"
mkdir -p "$lock_dir"
exec 9>"$lock_dir/altasci-tunnel-deploy.lock"
flock -x 9

# The SSH bootstrap already pulled the image to extract these scripts.
# Check image availability before stopping the current release.
docker image inspect "$image" >/dev/null
if ! docker image inspect "$postgres_image" >/dev/null 2>&1; then
    docker pull "$postgres_image"
fi
# This project supports one Control/Edge pair. Stop it before DB writes.
existing=$(docker container ls -a --filter "name=^/${container}$" --format '{{.ID}}')
if [[ -n "$existing" ]]; then
    docker stop --time 30 "$container" >/dev/null
fi

bash "$script_dir/init-docker.sh" "$image" "$env_file" "$volume" "$network" "$postgres_image"

# A flag must not skip migrations introduced by future releases.
docker run --rm --network "$network" \
    --env-file "$env_file" --env ALTASCI_DATA_DIR=/data \
    --mount "type=volume,src=$volume,dst=/data" \
    --read-only --cap-drop ALL --security-opt no-new-privileges \
    "$image" migrate

# Keep the stopped previous container until initialization/migration succeeds.
if [[ -n "$existing" ]]; then
    docker rm "$container" >/dev/null
fi
docker run -d --name "$container" --network "$network" \
    -p 0.0.0.0:18080:18080 \
    -p 0.0.0.0:18081:18081 \
    --env-file "$env_file" --env ALTASCI_DATA_DIR=/data \
    --mount "type=volume,src=$volume,dst=/data" \
    --tmpfs /run/altasci:uid=65532,gid=65532,mode=0700 \
    --read-only --cap-drop ALL --security-opt no-new-privileges \
    --stop-timeout 30 --restart unless-stopped \
    "$image" >/dev/null

deadline=$((SECONDS + 180))
while (( SECONDS < deadline )); do
    state=$(docker inspect --format '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}' "$container")
    case "$state" in
        'running healthy') echo "部署成功：$image"; exit 0 ;;
        *unhealthy*|exited*|dead*) break ;;
    esac
    sleep 2
done
docker logs --tail 100 "$container" >&2
die "容器未在 180 秒内通过健康检查；请查看日志"
