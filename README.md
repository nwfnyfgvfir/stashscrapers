# 拾录：独立 Stash 社区刮削服务

[GitHub](https://github.com/nwfnyfgvfir/stashscrapers) · [Actions 镜像构建](https://github.com/nwfnyfgvfir/stashscrapers/actions/workflows/release.yml) · [VPS 部署教程](deploy/README.md)

Go 单进程 HTTP API，内嵌中文管理页面。直接读取原始 CommunityScrapers YAML，复用 Stash 的映射与后处理代码，不需要部署 Stash、填写 Stash 地址或提供 Stash API Key。

**每日自动规则更新已实现，默认开启。** 首次启动自动读取上游实际默认分支，下载固定 commit 的完整资源，离线验证后切换。浏览器、Python 与目标网站凭据按需配置。目录中的脚本是可信可执行代码；当前只为经过测试的 RealJamVR Python 动作开放独立执行。

## 快速启动

开发、测试、构建统一使用项目专属 Conda 环境，禁止直接调用全局 Go、Python 或 npm。非交互 Shell 不使用 `conda activate`。

```powershell
conda env create -f environment.yml
conda run -n stashscrapers python tools/check.py --full
conda run -n stashscrapers python tools/build.py
conda run --no-capture-output -n stashscrapers .\dist\stashscrapers-windows-amd64.exe
```

Linux 开发启动：

```bash
conda run --no-capture-output -n stashscrapers go run ./cmd/stashscrapers
```

打开 [管理页面](http://127.0.0.1:8080)。首次资源安装在后台进行，页面与健康检查会先启动。进度见「版本与更新」。第一次安装失败可点击「立即检查」。安装成功后的启动与加载完全使用本地资源；实际刮削仍然需要访问目标站点。

构建产物独立运行，不要求最终用户安装 Conda 或 Go。开发工作站未将 Conda 加入 PATH 时，用安装路径调用 `conda.exe`，例如 `& 'D:\soft\anaconda3\Scripts\conda.exe' run -n stashscrapers ...`。

可用参数：`--data <持久化目录>`、`--listen <地址:端口>`、`--version`。环境变量为 `STASHSCRAPERS_DATA`、`STASHSCRAPERS_LISTEN`。默认监听 `127.0.0.1:8080`。

## Docker 部署

VPS 推荐直接使用公开预构建镜像 `ghcr.io/nwfnyfgvfir/stashscrapers:latest`，支持 x86-64 / ARM64。已安装 Docker Engine 与 Compose 插件后：

```bash
curl -fsSL https://raw.githubusercontent.com/nwfnyfgvfir/stashscrapers/main/deploy/install.sh | bash
```

VPS 无需 Conda、Go 或 Python。脚本生成访问 Token，默认只绑定回环地址，保留 `.env` 和 `stashscrapers-data` 数据卷。通过 SSH 隧道或 HTTPS 访问；镜像升级失败时尝试恢复原镜像。固定版本、域名、反向代理、更新和备份步骤见 [完整 VPS 部署教程](deploy/README.md)。

从本地源码构建（开发环境）：

```bash
conda run -n stashscrapers docker compose up -d --build
```

默认镜像仅包含 Go 服务、CA 证书、内嵌页面与源码下载包，不含 Python、Chromium、Git 或完整 Stash。构建阶段在 Conda 中编译。Compose 将端口绑定到宿主机回环地址，数据保存在 `scraper-data` volume。

需要 Python 时使用可选构建目标：

```bash
conda run -n stashscrapers docker compose -f compose.yml -f compose.python.yml up -d --build
```

该镜像的 Python 基础解释器路径是 `/opt/conda/envs/scripts/bin/python`；在「运行配置」中填写，再在 RealJamVR 详情中勾选可信脚本授权并「重新加载」。不会启动额外 Python 常驻进程。

外部访问时设置环境变量 `STASHSCRAPERS_TOKEN`，页面中填写同一 Bearer Token。代理反向转发应保留 `Host`，并使用 HTTPS。CDP 浏览器由用户单独部署并连接，默认镜像不捆绑浏览器。

## 完整资源与独立配置

持久化目录默认是 `data/`：

```text
data/
  config.json                  # 用户配置、启用状态、Cookie、目标站点密钥
  state.json                   # 活跃版本、计划、ETag、失败提交、更新与回滚历史
  versions/<commit>/
    repo/                      # 同一 commit 的完整仓库，保持脚本与共享模块关系
    manifest.json              # 规则 / 引擎 / Python 版本关系与依赖版本
    runtime/                   # 如已配置，版本专属 Python venv
  runtimes/<commit>/<generation>/ # 对已安装版本补齐依赖时产生的独立环境
  staging/                     # 暂存目录，验证成功前不会用于生产请求
```

完整备份 `data/`，包括版本与其绑定的依赖环境。不要只备份单个 YAML。Windows 文件权限继承宿主目录 ACL；POSIX 配置文件以 `0600` 写入。Git 不跟踪数据与密钥。

本地安装：在「运行配置」填写可信目录的**绝对路径**，再到「版本与更新」安装本地目录。既支持仓库根目录下的 `scrapers/`，也支持现有平铺刮削器目录。整个目录会复制成快照，拒绝符号链接；本地版本用目录内容 SHA-256 的前 40 位标识，并明确记录为本地来源，不冒充 Git commit。本地安装不改变远程来源策略；仅使用本地资源时暂停远程更新。

来源配置支持 GitHub 及兼容其仓库元数据、commit 与 codeload tar.gz 接口的服务。`source.repository` 为 `owner/name`；`source.branch` 留空自动发现默认分支，也可填写固定分支或标签。它不是任意 Git URL / 任意发布包协议。认证私有上游仓库暂未接入；公共社区仓库不需要权限。

## 能力、依赖与验证范围

页面按动作展示 `available`、`missing_dependency`、`needs_configuration`、`unsupported` 及原因。`available` 只表示本机执行前提满足，**不代表目标网站已在线实测**。原始配置不支持搜索时，API 会拒绝名称搜索，不会假装支持番号检索。

- 首版 API 对象为 `scene`、`performer`。其他对象可以随完整仓库保存，但不对外执行。
- XPath、GJSON、`common`、嵌套映射、数组、正则、日期、JavaScript、拼接/拆分与 subScraper 使用固定版本 Stash 实现。HTTP 支持规则请求头、Cookie、编码、重定向与 HTTP(S) 代理。
- 已建立原始规则 fixture 回归：AdultEmpire（XPath、scene 搜索与候选详情）、1Pondo（JSON、URL 重写、嵌套演员/标签）、Boobpedia（performer JSON 搜索、XPath 详情、日期与 JavaScript）。[详细验证记录](docs/VALIDATION.md)限定具体版本与动作。
- RealJamVR 名称搜索执行原始 YAML、原始脚本与 `py_common`，stdin/stdout 为 JSON，stderr 单独收集并限制大小。Python ≥ 3.12 由用户明确指定，依赖在规则版本的 venv 中安装并校验导入与协议。安装不会升级共享 Conda 环境。
- 其他社区脚本尚未逐一审核其 Stash API、数据库、文件系统、第三方包及配置副作用，明确显示不支持。`Filename`、`CopyFromScene` 等依赖完整 Stash 的动作不会执行。并非「所有脚本只要 Python 存在就可用」。
- RealJamVR 当前锁定 5 个包；上游增加新依赖而当前锁无法满足时，更新验证失败并保留旧版本。更改依赖锁需要走引擎验证流程。依赖版本不会自动猜测或无条件升级。
- Python venv 仍依赖创建它的基础解释器；部署方需要保留该解释器路径。移动或替换基础解释器后应重新验证，不能仅依据旧 manifest 宣称环境兼容。
- CDP 使用可选远程连接，支持页面导航、Cookie、请求头、等待与点击。未配置时为缺少依赖。远程浏览器代理、网络沙箱和运行资源由浏览器部署负责。`useSurf` TLS 指纹模拟不支持。JSON + CDP 暂不开放。
- 原生 HTTP 默认禁止私网、环回、链路本地及非 HTTP(S) 目标，校验 DNS 与重定向。Python 和 CDP 自行联网，需明确开启 `allow_private_targets` 才可执行；该开关也允许原生 HTTP 访问私网。只向配置中可信域名发送单独保存的请求头与凭据。
- 缺失字段保持 `null` 或空数组，不补造标题、代码、演员、时长等数据。`raw` 保存引擎或脚本原始返回，不是目标网页全文。原生映射的 Duration 类型转换沿用上游边界；未输出时保持空值。

## API 与可执行示例

[OpenAPI](http://127.0.0.1:8080/api/openapi.json) 与 [接口说明](docs/API.md)。示例客户端使用 Python 标准库，无额外依赖：

```powershell
conda run -n stashscrapers python tools/api.py health
conda run -n stashscrapers python tools/api.py list
conda run -n stashscrapers python tools/api.py search --scraper AdultEmpire --object scene --query "example title"
conda run -n stashscrapers python tools/api.py detail --scraper 1pondo --object scene --url "https://en.1pondo.tv/movies/010124_001/"
conda run -n stashscrapers python tools/api.py check
```

示例 URL 仅展示调用格式，不保证该目标存在或当前网站可访问。返回多个搜索候选后，页面要求用户选定一项再获取详情；服务不会把第一项作为精确匹配。跨更新提交详情时可传 `rules_version`，版本发生变化会返回 `409 version_conflict`，提示重新搜索。

目标站点 Cookie 与 API Key 通过 `PATCH /api/scrapers/{id}` 或详情页保存。带目录的稳定 ID 如 `RealjamVR/RealjamVR`，客户端需要 URL 编码。读取配置会脱敏，`__REDACTED__` 在保存时保留原值；要清空凭据可在完整配置 JSON 中将其值改为空字符串。

## 每日自动更新与离线运行

默认每隔 24 小时检查一次，时区 `Asia/Shanghai`，不是分钟级 GitHub 轮询。可设置 `update.daily_at` 为 `HH:MM`，此时按指定时区每天执行，优先于间隔设置。计划、上次检查与下次检查持久化，重启不会额外触发每日检查。首次初始化与手动检查不受计划限制。

内部计划器执行如下流程：

1. 发现实际默认分支，条件请求 commit；记录 SHA、ETag 与限流时间。
2. 下载整个提交归档到独立暂存目录，校验路径、文件类型与大小，拒绝链接和路径穿越。
3. 严格加载定义，检查动作及运行时兼容性，执行固定真实规则的离线 fixture 回归；候选中包含受测规则时也使用其新配置执行回归。
4. 需要 Python 时创建新版本隔离环境，安装固定依赖，执行导入检查和原脚本协议回归。其他未审核脚本保持不支持。
5. 完成运行时注册表加载，原子提交活跃版本指针和持久化状态。进行中的请求继续持有旧目录，新请求获取新快照。更新锁排斥手动检查、计划任务、重新加载与回滚。
6. 保留最近可用快照。已删除/改名规则不会在新目录残留；存在用户设置的旧 ID 显示明确的删除/改名状态，设置仍保留。

新增规则自动进入目录；执行由动作、依赖与策略决定。兼容更新保留已启用状态；已启用的动作丢失兼容性会阻止提交应用。新增但无法加载的规则可被发现并显示原因，不会宣称可执行。仓库完全不可加载或包含损坏 YAML 的快照拒绝安装。

下载失败、归档中断、离线验证失败或加载失败不会替换当前版本。验证失败的 commit 被记住，避免重复安装，后续新 commit 仍继续检测。目标网站一次超时只影响当前请求，不触发全局回滚。网站结构发生变化但仍返回 HTTP 200、字段缺失等情况无法被通用离线测试完全发现。

更新暂停、固定版本、立即检查和回滚入口均在页面。回滚默认临时固定 24 小时；手动固定 SHA 默认持续到清除固定。保留数量可配置。离线运行时仍启动并加载已有快照，网络失败记录到更新状态，下次计划再检查。

## 引擎与部署独立更新

社区规则、脚本和资源由服务内部热更新，不重新构建镜像。Go 引擎通过独立每日 CI 跟随上游：

- `tools/sync_engine.py` 按 SHA 提取指定文件与原始选择器实现，检查补丁应用边界，记录原始文件哈希与许可证。
- `tools/follow_engine.py` 检查最新 Stash `develop`，重放提取和补丁，更新被提取层使用的 Go 依赖。
- `.github/workflows/engine-upgrade.yml` 每日 UTC 01:25（北京时间 09:25）执行，跑完整兼容测试、Python HTTPS 集成、race 检查和多平台构建。失败时只保留失败日志，不改变当前部署。
- 默认上传候选构建产物。需要自动发布时，在自己的 GitHub 仓库设置 `AUTO_RELEASE_ENGINE=true`，授权 `contents:write`、`packages:write`，并允许机器人在默认分支提交。流水线将已验证源码提交、打标签、发布二进制与 GHCR 镜像。受分支保护或缺少权限时会失败并保留现有部署；不要绕过保护规则。
- `.github/workflows/deploy.yml` 在成功发布流程后执行可选部署。配置具有 `stashscrapers-deploy` 标签的自托管 runner、项目 Conda 环境、Docker 权限、`DEPLOY_COMPOSE` 的部署文件绝对路径，以及 `ENABLE_ENGINE_DEPLOY=true`。`tools/deploy.py` 拉取镜像、重建服务、检查健康，失败恢复旧镜像；数据 volume 始终保留。
- Windows 用户可用发布的独立二进制在现有进程管理器中替换并重启；不自动安装 Windows 系统服务或修改系统设置。

本项目的公开仓库已设置 `AUTO_RELEASE_ENGINE=true`。[首次实际引擎检查](https://github.com/nwfnyfgvfir/stashscrapers/actions/runs/43384734225)成功读取上游，确认与固定的 Stash commit 一致，因此没有生成新的引擎发布。Fork 的仓库需要单独配置变量。

项目发布到 [nwfnyfgvfir/stashscrapers](https://github.com/nwfnyfgvfir/stashscrapers)。推送 `v*` 或 `engine-*` tag 后，`publish-container` 自动执行兼容测试、容器健康与持久卷 smoke，再发布多架构镜像；常规稳定版本还更新 `latest`。Actions 使用仓库的 `GITHUB_TOKEN`，无需另存个人发布 Token。首次 GHCR 包的 public 可见性需要单独核对，不随仓库自动继承。

维护者发布新版本（将示例版本号换成尚未使用的新版本）：

```bash
conda run -n stashscrapers git tag -a v0.1.2 -m "Release v0.1.2"
conda run -n stashscrapers git push origin v0.1.2
```

不要移动已经发布的 tag；修复后发布新版本。Actions 页面记录了镜像 tag 与 digest。镜像公开不等于已部署到某台 VPS；本任务没有用户 VPS 地址或登录凭据。自托管部署 runner 仍需按前文自行配置。GitHub 的计划触发可能延迟，服务内部的规则更新不依赖 GitHub Actions。

GitHub 处理 push 事件也可能延迟。本次首次提交约 11 分钟后才出现自动 CI 记录；不要因此移动或重复创建 tag。需要补跑时，在 Actions 的 `publish-container` 页面选择「Run workflow」，填写已有 tag。工作流会检出并核对该 tag 的实际 commit，不会误把当前 `main` 当作旧版源码发布。

## 验证与排障

```powershell
conda run -n stashscrapers python tools/check.py
conda run -n stashscrapers python tools/check.py --full
conda run -n stashscrapers python tools/build.py
```

完整测试为 Python 创建独立临时 venv，初次需要访问包索引；原始脚本抓取使用受控本地 HTTPS 与测试 CA，不访问目标成人网站。基础测试无需目标站点在线。可选 `TEST_CDP` 用于单独验证 CDP 执行。

已安装 Chromium 时可运行 `conda run -n stashscrapers python tools/check_browser.py`，它使用临时 headless 配置文件，不操作用户的浏览器配置。Race 检查使用 `conda run -n stashscrapers python tools/race.py`；Windows 需要先在此环境安装 `gcc_win-64`，Linux 使用 `gcc_linux-64`，不使用全局编译器。

故障定位：

- 「尚未安装」：查看 `/api/updates` 的阶段与原因，恢复网络后立即检查。
- 「缺少依赖 / 需要配置」：查看动作具体原因；设置 Python 后授权已审核脚本并重新加载；设置 CDP 后再执行浏览器动作。
- `target_denied`：目标返回 401/403/429，检查本站要求的 Cookie、API Key、代理或浏览器。服务不处理验证码。
- `timeout / network_error`：检查 DNS、代理和网络；不会导致全局规则回滚。
- `parse_error / not_found`：查看原始结果与配置版本，可能是选择器或网站内容变化；不要将缺失结果当作精确匹配。
- 更新验证失败：继续使用旧版本，查看历史；已知坏提交自动跳过。可手动回滚 retained 列表中的版本。

已测结果、资源指标及无法实测项目见 [VALIDATION.md](docs/VALIDATION.md)。保证范围以已验证提交、动作和环境为限；不承诺永久零维护或所有网站持续可用。

## 来源、许可证与修改说明

本项目按 **AGPL-3.0** 分发，根目录 `LICENSE` 保留完整文本，页面提供当前构建对应的源码下载。发布前 `tools/build.py` 自动重新打包源码，排除密钥、数据与二进制。

Stash 固定提交 `47057616fe9da0cf38cfa8c39ab160e5441eeafc`；CommunityScrapers fixture 固定提交 `da55cf6ee5ff12b4b6bc33b05e306d1fbe550d6b`。两仓库许可证分别保存在 `upstream/STASH-LICENSE`、`upstream/COMMUNITY-LICENSE`。日常安装版本不固定在 fixture SHA，实际活跃 commit 以页面和 manifest 为准。

原始文件哈希见 `upstream/engine-lock.json`、`upstream/community-lock.json`；提取边界、最小依赖与差异见 [PATCHES.md](upstream/PATCHES.md)。许可证判断直接依据各仓库在这些 SHA 下的 LICENSE 原文，不构成法律意见。

来源记录：[stashapp/Stash，固定提交，刮削器执行层](https://github.com/stashapp/stash/tree/47057616fe9da0cf38cfa8c39ab160e5441eeafc/pkg/scraper)、[stashapp/Stash，固定提交，格式规范](https://github.com/stashapp/stash/blob/47057616fe9da0cf38cfa8c39ab160e5441eeafc/ui/v2.5/src/docs/en/Manual/ScraperDevelopment.md)、[stashapp/CommunityScrapers，固定提交，真实规则](https://github.com/stashapp/CommunityScrapers/tree/da55cf6ee5ff12b4b6bc33b05e306d1fbe550d6b/scrapers)。这些是代码与规范的一手来源，未用第三方网站可用性陈述替代实测。
