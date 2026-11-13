# VPS Docker 部署教程

项目：[nwfnyfgvfir/stashscrapers](https://github.com/nwfnyfgvfir/stashscrapers)。镜像：`ghcr.io/nwfnyfgvfir/stashscrapers`，支持 `linux/amd64`、`linux/arm64`。默认镜像只运行 Go 服务，不包含 Python 或 Chromium。

这里运行的是预构建镜像，VPS **无需安装 Conda、Go 或 Python**。项目开发、测试和镜像内的编译仍使用 Conda。

## 1. 准备 Docker

VPS 需要 Linux、可访问 GitHub/GHCR 的网络、Docker Engine 和 Docker Compose 插件。可以使用已经安装好 Docker 的 VPS 镜像。尚未安装时按 [Docker 官方安装文档](https://docs.docker.com/engine/install/)选择自己的发行版。

```bash
docker version
docker compose version
```

以上两条都成功后再安装。若当前账号没有 Docker 权限，使用具备权限的账号执行后续命令。脚本不会自行修改防火墙、安装系统软件或停止其他服务。

## 2. 一键部署

在 VPS 上执行：

```bash
curl -fsSL https://raw.githubusercontent.com/nwfnyfgvfir/stashscrapers/main/deploy/install.sh | bash
```

默认安装到 `$HOME/stashscrapers`，拉取 `latest`，生成随机访问 Token，挂载 `stashscrapers-data` 持久卷，并等待健康检查通过。重复执行保留现有 `.env`、配置、规则、依赖环境与启用状态；不会清空数据。新镜像健康检查失败时尝试恢复此前运行的镜像。

要指定版本、目录和端口：

```bash
curl -fsSL https://raw.githubusercontent.com/nwfnyfgvfir/stashscrapers/main/deploy/install.sh \
  | VERSION=v0.1.0 INSTALL_DIR=/opt/stashscrapers PORT=8080 bash
```

`VERSION` 在首次安装时写入 `.env`。已有安装重新运行脚本时，镜像版本和端口继续使用原 `.env`；升级版本请按第 6 节修改配置。固定版本也可把下载地址中的 `main` 换成对应 tag。

默认绑定 `127.0.0.1:8080`，适合 SSH 隧道或 HTTPS 反向代理。确实需要直接通过 VPS IP 访问时，首次部署增加 `BIND_ADDRESS=0.0.0.0`；已有部署修改 `.env` 后重建容器。直接 HTTP 不加密，填写站点 Cookie/API Key 前应接入 HTTPS。

## 3. 打开管理页面

没有域名时，在**你自己的电脑**运行 SSH 隧道，保持窗口开启：

```bash
ssh -N -L 18088:127.0.0.1:8080 root@你的VPS_IP
```

浏览器打开 `http://127.0.0.1:18088`。在 VPS 查看访问 Token：

```bash
grep '^STASHSCRAPERS_TOKEN=' "$HOME/stashscrapers/.env"
```

将等号后的值填入页面「运行配置 → 服务 Bearer Token → 连接服务」。自定义安装目录时调整上述路径。

首次启动会后台下载完整社区资源。健康检查的 `status=ok` 表示 HTTP 服务正常；`ready=true` 才表示规则已经安装。页面会分别显示下载状态、运行依赖与已验证范围。规则列表中的「依赖齐全」不等于目标网站已在线验证。

## 4. 使用域名与自动 HTTPS

已有 Nginx/Caddy 等反向代理时，转发到 `127.0.0.1:8080` 并保留 `Host` 请求头即可。需要使用本仓库的可选 Caddy 配置时：

1. 将域名 A/AAAA 记录指向 VPS。
2. 确认 80/443 端口可用并能从公网访问。
3. 在安装目录执行：

```bash
cd "$HOME/stashscrapers"
curl -fsSLO https://raw.githubusercontent.com/nwfnyfgvfir/stashscrapers/main/deploy/compose.caddy.yml
curl -fsSLO https://raw.githubusercontent.com/nwfnyfgvfir/stashscrapers/main/deploy/Caddyfile
printf '\nDOMAIN=scrapers.example.com\n' >> .env
docker compose -f compose.yml -f compose.caddy.yml up -d --wait
```

把 `scrapers.example.com` 改为实际域名，随后打开 `https://你的域名`。Caddy 负责申请和续期证书；域名解析、端口开放等前提依据 [Caddy 官方反向代理文档](https://caddyserver.com/docs/quick-starts/reverse-proxy)与 [Docker Compose 示例](https://caddyserver.com/docs/running#docker-compose)。不要在已有 `DOMAIN=` 时反复追加，直接修改原值。

## 5. 不运行安装脚本，手动 Compose 部署

```bash
mkdir -p "$HOME/stashscrapers"
cd "$HOME/stashscrapers"
curl -fsSL https://raw.githubusercontent.com/nwfnyfgvfir/stashscrapers/main/deploy/compose.yml -o compose.yml
curl -fsSL https://raw.githubusercontent.com/nwfnyfgvfir/stashscrapers/main/deploy/.env.example -o .env
chmod 600 .env
```

编辑 `.env`，设置较长的随机 `STASHSCRAPERS_TOKEN`。示例中的占位值必须替换，例如用 `openssl rand -hex 32` 生成。然后：

```bash
docker compose pull
docker compose up -d --no-build --wait --wait-timeout 180
docker compose ps
curl -fsS http://127.0.0.1:8080/api/health
```

Compose 没有 `build:`，VPS 不会编译源码。端口、镜像版本及 Token 都在 `.env` 中配置。镜像公开后拉取不需要 GitHub Token；GitHub 官方说明见 [公开容器匿名拉取](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)。

## 6. 规则更新、镜像升级与回滚

**规则更新**由容器内服务默认每 24 小时检查，时区 `Asia/Shanghai`。不需要 cron、git pull 或重启镜像。检查计划保存在数据卷，重启不会重复检查。

**引擎镜像**随 Git tag 发布。保持 `.env` 使用 `latest` 时，主动升级：

```bash
cd "$HOME/stashscrapers"
docker compose pull
docker compose up -d --no-build --wait --wait-timeout 180
```

修改 `.env` 为 `STASHSCRAPERS_IMAGE=ghcr.io/nwfnyfgvfir/stashscrapers:v0.1.0` 可固定版本；换成其他已发布 tag 后执行同样命令即可升级或回退。也可以使用 Actions 记录的 `@sha256:...` 固定镜像 digest。

有 Caddy 时，后续 Compose 命令继续带 `-f compose.yml -f compose.caddy.yml`。一键脚本具备失败恢复；直接手动执行 Compose 的升级失败时，请将 `.env` 改回上一个可用版本再 `up -d --wait`。

**规则回滚**在管理页面选择保留版本，回滚后默认固定 24 小时。规则回滚和引擎镜像回退是两种独立操作。数据卷始终保留。

## 7. 数据、备份和日志

所有业务数据位于容器 `/data`、Docker named volume `stashscrapers-data`。包含：`config.json`（凭据与启用状态）、`state.json`（日程与历史）、版本快照、隔离的可选 Python 环境。

查看挂载和日志：

```bash
docker volume inspect stashscrapers-data
docker compose logs --tail=100 scrapers
docker compose ps
```

完整备份前停止刮削和更新，停止服务以保持快照一致：

```bash
cd "$HOME/stashscrapers"
mkdir -p backups
docker compose stop scrapers
docker run --rm -v stashscrapers-data:/data:ro -v "$PWD/backups:/backup" \
  alpine:3.22 tar czf /backup/stashscrapers-data.tar.gz -C /data .
docker compose start scrapers
```

同时妥善保存安装目录中的 `.env`。还原到空卷时，将备份解压到该卷后启动；不要将旧备份直接覆盖运行中的数据。Python 环境依赖基础解释器路径，跨镜像迁移时要重新验证。

重建、停止、升级容器均不删除 named volume。不要运行 `docker compose down -v` 或 `docker volume rm stashscrapers-data`，除非确实要永久删除配置、密钥和规则。

## 8. 常见问题

- **GHCR `denied`**：确认镜像已经构建成功且包可见性为 public。仓库公开不会自动让 GHCR 包公开；首次发布后包所有者需要单独设置可见性，此行为依据 [GitHub 官方包权限文档](https://docs.github.com/en/packages/learn-github-packages/configuring-a-packages-access-control-and-visibility)。私有镜像才需要额外登录凭据，公共部署无需它们。
- **网页无法打开**：默认只绑定回环地址，使用 SSH 隧道或反向代理；不要把 VPS 上的 `127.0.0.1` 当成自己电脑的地址。
- **401**：在页面填写 `.env` 中的服务 Token。服务 Token 与目标站点 API Key 不同。
- **规则未下载 / ready=false**：在页面查看更新阶段；检查 VPS 到 GitHub/codeload 的网络，恢复后立即检查。安装成功后的规则加载不依赖 GitHub 在线。
- **目标站点网络错误 / 403**：检查目标站点需要的代理、Cookie 或 API Key。这不触发全局规则回滚。
- **Python/CDP 缺失**：默认镜像不含它们。Python 可使用源码 Dockerfile 的 `python-runtime` 目标自行构建；基础解释器路径见根 README。CDP 配置外部浏览器端点。只有明确审核并授权的脚本才执行。

本教程中的 Docker 和 Caddy 配置须在实际 VPS、DNS 与网络条件下验证。仓库 Actions 会验证容器启动及持久卷写入；没有声称已经登录或部署到你未提供的 VPS。
