# HTTP API

默认地址 `http://127.0.0.1:8080`。请求 JSON 最大 1 MiB。设置 `STASHSCRAPERS_TOKEN` 后，除健康、OpenAPI 与源码下载外，请求需 `Authorization: Bearer <token>`。同源管理校验默认开启。

## 接口

- `GET /api/health`：进程状态、资源 ready、引擎与规则版本。
- `GET /api/scrapers`：目录；`operations` 为实际检测的对象、动作、依赖与可用原因。
- `GET /api/scrapers/{id}`：单个规则详情。带目录 ID 需要 URL 编码。
- `PATCH /api/scrapers/{id}`：`enabled`、`script_approved`、`cookie`、`headers`、`api_key`、`api_key_header`。
- `POST /api/search`：名称搜索候选列表。
- `POST /api/detail`：URL、fragment 或搜索候选详情。
- `GET /api/config`、`PUT /api/config`：读取脱敏配置、保存完整配置。
- `POST /api/reload`：重新读取持久化配置、重新加载规则；必要时为当前版本建立隔离 Python 环境。
- `GET /api/dependencies`：当前版本的 Python、包、CDP 配置检测信息。
- `GET /api/updates`：状态、计划、历史与 retained manifest 列表。
- `POST /api/updates/check`：立即检查并自动应用；同步返回，可通过并行 GET 观察阶段。
- `POST /api/install`：`{"source":"upstream"}` 或 `{"source":"local"}`。本地路径只能来自可信配置，不能在该请求中提交任意路径。
- `POST /api/updates/rollback`：`{"version":"<40位SHA>"}`；空字符串选择最近的保留版本，固定 24 小时。
- `GET /api/logs`：最近 200 条脱敏内存事件；安装与回滚历史保存在 state.json。
- `GET /api/openapi.json`：机器可读接口定义。
- `GET /api/source`：当前构建对应的完整可下载源码，包括许可证与原始规则 fixture。

## 请求模式

搜索要求原始规则声明 `sceneByName` / `performerByName`：

```json
{"scraper_id":"AdultEmpire","object":"scene","query":"example title"}
```

搜索结果 `results` 包含所有候选；不自动挑第一项。用户选定结果后，把 `raw` 回传为 fragment。场景搜索详情用 `query_fragment`：

```json
{
  "scraper_id":"AdultEmpire",
  "object":"scene",
  "operation":"query_fragment",
  "fragment":{"urls":["https://www.adultempire.com/10001/example.html"]},
  "rules_version":"da55cf6ee5ff12b4b6bc33b05e306d1fbe550d6b"
}
```

只支持 URL 的规则需要来源 URL：

```json
{"scraper_id":"1pondo","object":"scene","url":"https://en.1pondo.tv/movies/010124_001/"}
```

`fragment` 对应 `sceneByFragment` / `performerByFragment`，与 `query_fragment` 分开校验。输入字段由原规则的 queryURL 占位符或脚本协议决定；服务不连接媒体库来查找缺失 ID、路径、指纹。

`rules_version` 可选；如果提供且已切换版本，返回 409，避免用旧搜索结果请求不兼容的新配置。一次请求从开始到完成固定使用同一快照。

## 返回结构

```json
{
  "results": [{
    "title": null,
    "name": null,
    "code": null,
    "details": null,
    "date": null,
    "duration": null,
    "studio": null,
    "director": null,
    "performers": null,
    "tags": null,
    "images": [],
    "urls": [],
    "scraper_id": "<稳定标识>",
    "rules_version": "<当前版本>",
    "raw": {},
    "warnings": []
  }],
  "rules_version": "<当前版本>",
  "warnings": []
}
```

上例是结构说明，不是演示返回。实际有值字段来自原始刮削结果，缺失字段保持空。演员专有字段保留在 `raw`。不会自动下载图片或返回媒体文件。

## 错误

错误使用 `{"error":{"code":"...","message":"..."}}`。主要状态：

- `400 input_error`：JSON、对象、参数、URL 不合法。
- `401 unauthorized`：缺少或错误的服务 Token。
- `404 not_found`：规则/目标/结果不存在；空字段也可能来自目标结构变化，需进一步诊断。
- `409 disabled / busy / version_conflict`：规则停用、已有版本操作、搜索后的版本冲突。
- `422 unsupported`：原始配置或本服务不支持该动作。
- `424 missing_dependency / needs_configuration`：Python、CDP 或授权未满足。
- `502 target_denied / network_error / parse_error / script_error / browser_error`：目标拒绝、网络、响应格式、脚本或浏览器错误。
- `503 missing_dependency`：尚未安装资源。
- `504 timeout`、`499 cancelled`：超时与请求取消。

脚本 stdout 最大 2 MiB、stderr 最大 256 KiB，超限取消。默认 30 秒超时、并发 4、同站请求间隔 1000 ms。HTTP GET 最多尝试两次，仅对 429/502/503 进行一次延迟重试；不对不兼容或解析错误无限重试。
