#!/usr/bin/env bash
# Run on the deployment host before starting the long-lived server container.
# Variables inside single-quoted helper scripts expand in the helper container.
# shellcheck disable=SC2016
set -euo pipefail
set +x

die() { echo "初始化失败：$*" >&2; exit 1; }

[[ $# -ge 2 && $# -le 5 ]] || die "用法：$0 IMAGE ENV_FILE [DATA_VOLUME] [NETWORK] [POSTGRES_IMAGE]"
image=$1
env_file=$2
volume=${3:-altasci-server-data}
network=${4:-bridge}
postgres_image=${5:-postgres:18}
lock_dir=${ALTASCI_DEPLOY_LOCK_DIR:-/run/lock}

[[ -r "$env_file" ]] || die "无法读取环境文件：$env_file"
[[ "$volume" =~ ^[a-zA-Z0-9][a-zA-Z0-9_.-]*$ ]] || die "数据卷名称无效"
command -v docker >/dev/null || die "需要 Docker"
command -v flock >/dev/null || die "需要 flock（util-linux）"
mkdir -p "$lock_dir"
exec 8>"$lock_dir/altasci-${volume}-init.lock"
flock -x 8

# Seed a new volume from the server image to preserve nonroot ownership.
seed=$(docker create --mount "type=volume,src=$volume,dst=/data" "$image")
docker rm "$seed" >/dev/null

# This image is a client helper only; no PostgreSQL server is started.
# Docker parses env files. Never source/eval secrets as shell code.
helper() {
    docker run --rm --network "$network" \
        --env-file "$env_file" --entrypoint /bin/sh \
        "$postgres_image" -ec "$1" sh "${@:2}"
}

sql() {
    helper '
        : "${DATABASE_URL:?环境文件缺少 DATABASE_URL}"
        export PGCONNECT_TIMEOUT=10
        exec psql --dbname="$DATABASE_URL" -X -qAt --set=ON_ERROR_STOP=1 --command="$1"
    ' "$1"
}

volume_command() {
    docker run --rm --network none --user 65532:65532 \
        --mount "type=volume,src=$volume,dst=/data" \
        --entrypoint /bin/sh "$postgres_image" -ec "$1"
}

server() {
    docker run --rm -i --network "$network" \
        --env-file "$env_file" --env ALTASCI_DATA_DIR=/data \
        --mount "type=volume,src=$volume,dst=/data" \
        --read-only --cap-drop ALL --security-opt no-new-privileges \
        "$image" "$@"
}

key_state=$(volume_command '
    if [ ! -e /data/secrets.key ]; then
        printf missing
    elif [ -f /data/secrets.key ] && [ "$(wc -c < /data/secrets.key)" -eq 32 ]; then
        printf valid
    else
        printf invalid
    fi
')

if volume_command 'test -f /data/.initialized'; then
    [[ "$key_state" == valid ]] || die "标记存在但密钥缺失或损坏，请恢复原密钥"
    # A stale marker must not silently bootstrap a replaced/empty database.
    [[ "$(sql 'SELECT EXISTS (SELECT 1 FROM admins);')" == t ]] ||
        die "标记与数据库状态不一致，请检查数据库连接和备份"
    echo "项目已初始化，跳过首次初始化。"
    exit 0
fi

[[ "$key_state" != invalid ]] || die "已有密钥损坏，拒绝覆盖"
helper ': "${DATABASE_URL:?缺少 DATABASE_URL}"; : "${ALTASCI_PUBLIC_URL:?缺少 ALTASCI_PUBLIC_URL}"'
has_schema=$(sql "SELECT to_regclass('control_meta') IS NOT NULL OR to_regclass('admins') IS NOT NULL;")
if [[ "$has_schema" == t && "$key_state" == missing ]]; then
    die "数据库已有项目表但缺少原密钥，请先恢复密钥"
fi

server init
has_admin=$(sql 'SELECT EXISTS (SELECT 1 FROM admins);')
[[ "$has_admin" == t || "$has_admin" == f ]] || die "数据库返回了无效的管理员状态"
if [[ "$has_admin" == f ]]; then
    username=$(helper '
        : "${ALTASCI_INIT_ADMIN_USERNAME:?缺少 ALTASCI_INIT_ADMIN_USERNAME}"
        : "${ALTASCI_INIT_ADMIN_PASSWORD:?缺少 ALTASCI_INIT_ADMIN_PASSWORD}"
        case "$ALTASCI_INIT_ADMIN_PASSWORD" in
            *"$(printf "\r")"*|*"
"*) echo "初始化密码不能包含换行符" >&2; exit 1 ;;
        esac
        printf "%s" "$ALTASCI_INIT_ADMIN_USERNAME"
    ')
    helper 'printf "%s\n" "$ALTASCI_INIT_ADMIN_PASSWORD"' |
        server admin-create --username "$username" --password-stdin
else
    echo "数据库已有管理员，保留现有账号和密码。"
fi

# A retry after an interrupted flag write checks the committed administrator.
volume_command '
    umask 077
    marker_tmp=$(mktemp /data/.initialized.tmp.XXXXXX)
    trap '\''rm -f "$marker_tmp"'\'' EXIT
    printf "1\n" > "$marker_tmp"
    mv "$marker_tmp" /data/.initialized
'
echo "项目初始化完成。"
