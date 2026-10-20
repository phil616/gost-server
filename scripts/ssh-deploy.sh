#!/usr/bin/env bash
# Sent from the checked-out commit before the private release image is pulled.
set -euo pipefail
set +x

[[ $# -eq 4 ]] || { echo "用法：$0 REGISTRY USERNAME IMAGE INIT_CLIENT_IMAGE" >&2; exit 1; }
registry=$1
username=$2
image=$3
client_image=$4
env_file=${ALTASCI_DEPLOY_ENV_FILE:-/run/baoctl/altasci-tunnel.env}

baoctl sync altasci-tunnel

# Read Docker env-file values literally. Do not source the secrets file.
DEPLOY_TOKEN=''
line=''
cr=$(printf '\r')
while IFS= read -r line || [[ -n "$line" ]]; do
    line=${line%"$cr"}
    case "$line" in
        DEPLOY_TOKEN=*) DEPLOY_TOKEN=${line#DEPLOY_TOKEN=} ;;
    esac
done < "$env_file"
: "${DEPLOY_TOKEN:?环境文件缺少非空 DEPLOY_TOKEN}"
printf '%s\n' "$DEPLOY_TOKEN" |
    docker login --username "$username" --password-stdin "$registry"
unset DEPLOY_TOKEN line cr

docker pull "$image"
workdir=$(mktemp -d)
source_container=''
cleanup() {
    if [[ -n "$source_container" ]]; then
        docker rm --volumes "$source_container" >/dev/null
    fi
    rm -rf -- "$workdir"
}
trap cleanup EXIT
source_container=$(docker create "$image")
docker cp "$source_container:/usr/share/altasci/deploy/." "$workdir/"
docker rm --volumes "$source_container" >/dev/null
source_container=''
bash "$workdir/deploy-docker.sh" "$image" "$env_file" altasci-server-data bridge "$client_image"
