#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

# Run released containers on a VPS; no compiler, Python or Conda is needed here.
repository="${STASHSCRAPERS_REPOSITORY:-nwfnyfgvfir/stashscrapers}"
version="${VERSION:-latest}"
install_dir="${INSTALL_DIR:-${HOME}/stashscrapers}"
bind_address="${BIND_ADDRESS:-127.0.0.1}"
port="${PORT:-8080}"

fail() { printf '%s\n' "$*" >&2; exit 1; }
[[ "$repository" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || fail "仓库名称无效。"
[[ "$version" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]] || fail "镜像版本无效。"
[[ "$install_dir" == /* ]] || fail "INSTALL_DIR 必须是绝对路径。"
[[ "$bind_address" == 127.0.0.1 || "$bind_address" == 0.0.0.0 ]] || fail "BIND_ADDRESS 只接受 127.0.0.1 或 0.0.0.0。"
[[ "$port" =~ ^[0-9]{1,5}$ ]] && (( 10#$port >= 1 && 10#$port <= 65535 )) || fail "PORT 必须是 1 到 65535。"
command -v curl >/dev/null || fail "请先安装 curl。"
command -v docker >/dev/null || fail "请先按 https://docs.docker.com/engine/install/ 安装 Docker Engine 和 Compose 插件。"
docker compose version >/dev/null || fail "需要 Docker Compose 插件（docker compose）。"
docker info >/dev/null || fail "无法连接 Docker；请启动 Docker 或使用具有 Docker 权限的账号。"

mkdir -p "$install_dir"
cd "$install_dir"
temporary="$(mktemp "$install_dir/.compose.XXXXXX")"
trap 'rm -f -- "$temporary"' EXIT
ref="${STASHSCRAPERS_REF:-$version}"
[[ "$ref" != latest ]] || ref=main
[[ "$ref" =~ ^[A-Za-z0-9_.-]+$ ]] || fail "下载来源 ref 无效。"
source_base="${STASHSCRAPERS_SOURCE_BASE:-https://raw.githubusercontent.com/$repository/$ref/deploy}"
curl --fail --silent --show-error --location "$source_base/compose.yml" --output "$temporary"
if [[ -f compose.yml ]]; then cp -- compose.yml compose.yml.previous; fi
mv -- "$temporary" compose.yml

created=false
if [[ ! -f .env ]]; then
  token="$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"
  [[ ${#token} == 64 ]] || fail "无法生成访问 Token。"
  image="ghcr.io/${repository,,}:$version"
  printf 'STASHSCRAPERS_IMAGE=%s\nBIND_ADDRESS=%s\nPORT=%s\nSTASHSCRAPERS_TOKEN=%s\n' "$image" "$bind_address" "$port" "$token" > .env
  chmod 600 .env
  created=true
fi

docker compose -f compose.yml config --quiet
previous_container="$(docker compose -f compose.yml ps -q scrapers)"
previous_image=""
if [[ -n "$previous_container" ]]; then previous_image="$(docker inspect --format '{{.Image}}' "$previous_container")"; fi
docker compose -f compose.yml pull
if ! docker compose -f compose.yml up -d --no-build --wait --wait-timeout 180; then
  if [[ -n "$previous_image" ]]; then
    printf '%s\n' "新镜像未通过健康检查，正在恢复原镜像。" >&2
    STASHSCRAPERS_IMAGE="$previous_image" docker compose -f compose.yml up -d --no-build --pull never --wait --wait-timeout 180 || fail "恢复失败；请保留数据卷并按部署教程手动回滚。"
    printf '%s\n' "已恢复原镜像；.env 保持原样，请先核对版本再重试。" >&2
  fi
  fail "部署失败，未删除任何数据卷。"
fi
printf '\n%s\n' "部署完成，目录：$install_dir" "数据持久化卷：stashscrapers-data（重新部署不会覆盖配置和规则）。"
if [[ "$created" == true ]]; then
  printf '%s\n' "已生成访问 Token，保存在 $install_dir/.env。请在管理页面的「运行配置」中填写。" "查看 Token：grep '^STASHSCRAPERS_TOKEN=' '$install_dir/.env'"
else
  printf '%s\n' "已保留原有 .env，包括镜像版本、端口和访问 Token。"
fi
printf '%s\n' "默认仅监听 VPS 的 127.0.0.1:8080；通过 SSH 隧道或 HTTPS 反向代理访问。" "部署教程：https://github.com/$repository/blob/$ref/deploy/README.md"
