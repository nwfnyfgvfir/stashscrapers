# 提取来源与补丁记录

Stash 来源 SHA：`47057616fe9da0cf38cfa8c39ab160e5441eeafc`，AGPL-3.0。CommunityScrapers 来源 SHA：`da55cf6ee5ff12b4b6bc33b05e306d1fbe550d6b`，AGPL-3.0。保留两份完整 LICENSE，以及所有原文件已存在的注释/版权声明。未将 AGPL 复用代码改标宽松许可证。

## 为什么提取，而非直接 import

`pkg/scraper/scraper.go` 的 Input 与接口依赖 Stash models；`cache.go`、`defined_scraper.go` 和后续处理连接配置、Repository、数据库匹配与 Stash 业务对象。`script.go` 还构造场景文件与指纹、依赖 Stash Python/exec/model JSON 类型。它不是独立、稳定、无依赖的 SDK。直接导入会扩大依赖到整个应用层，并不能自动摆脱 Stash 服务语义。

提取以下可复用边界，避免重新解释 YAML：

- 完整 `definition.go`、`mapped.go`、`mapped_config.go`、`mapped_result.go`、`mapped_postprocessing.go`。
- `xpath.go` 自 `multiSpaceRE` 起的原始 XPath Query 实现。
- `json.go` 自 `jsonQuery` 起的原始 GJSON Query 实现。

最小第三方依赖为 yaml.v2、htmlquery/xpath、gjson、goja、x/net 的 HTML/编码处理。可选 chromedp/cdproto 用于浏览器，不包含 Chromium。

## 可重复生成

```powershell
conda run -n stashscrapers python tools/sync_engine.py --source .upstream/stash-47057616fe9da0cf38cfa8c39ab160e5441eeafc --sha 47057616fe9da0cf38cfa8c39ab160e5441eeafc
conda run -n stashscrapers python tools/check.py --full
```

脚本为每个原始文件记录 SHA-256，检查选择器提取边界与 JavaScript 补丁唯一匹配；变化时失败，不能静默跳过补丁。生成文件经 `gofmt` 标准化。`follow_engine.py` 可从新提交下载源码并重复同一流程。新提取版本必须通过兼容测试后再构建发布。

## 对提取代码的修改

1. 包名改为 `engine`，Stash logger/models/sliceutil/javascript import 改为本地适配包。
2. JavaScript 每个 VM 增加 `context.AfterFunc` 中断与 context 绑定，限制无限循环及 `util.Sleep` 的执行时间。
3. logger 适配器丢弃上游逐字段值日志，避免 Cookie、密钥或签名 URL 随调试输出泄漏。服务另外记录不含输入值的事件。
4. models 只包含结果传输所需结构，无数据库、GraphQL resolver 或媒体库。映射规则、清理顺序、列表对齐与去重使用上游实现。
5. JavaScript 适配只开放本地值变换、兼容日志空接口和可取消 `util.Sleep`；不开放完整 Stash GraphQL、文件系统或网络 API。

## 新增的独立适配层

`adapter.go` 负责场景/演员路由、动作选择、查询构造与 JSON 输出。名称查询使用上游的 `url.QueryEscape`；URL 动作的 queryURL 重写保持仅在 queryURLReplace 存在时应用的原始条件。Fragment 接受调用方提供的标量字段（包括 `filename`），不查询数据库或自动推导文件。URL 匹配保留原始包含匹配，并增加域名边界校验。

请求层、脚本调用、CDP 接入、原子快照与日程管理是本项目新增代码，未复制完整 Stash 缓存、GraphQL、数据库匹配、媒体扫描或图片自动下载处理。脚本调用保留参数数组、cwd 与根共享模块 PYTHONPATH；仅批准经过审查的 RealJamVR 名称动作。Python 原始脚本和共享模块不修改，测试网络替身只注入离线回归子进程，不用于生产执行。

## 已知语义边界

- 首版暴露 scene / performer；其他类型只保留完整配置解析结构。
- 不提供 Stash `action: stash`、Stash API、数据库、指纹查库及文件库语义。
- 原生 mapped performer fragment 与上游一样不执行；名称结果可由用户选择 URL 后获取详情。
- `useSurf` 尚未提取。CDP 是本服务的有限浏览器适配，未宣称与 Stash 全部浏览器行为等价；JSON + CDP 不开放。
- HTTP 响应及脚本输出有大小上限，私网与重定向校验比上游严格。跨域重定向不转发用户配置的敏感头。
- 未验证的脚本默认不执行；不自动重写原脚本中的配置文件或 Stash 连接。
- 源码提取可重复不等于无人工适配。上游结构变化、JavaScript 新 API、全新 runtime 或 Python 依赖变化会被标为阻塞，仍需适配后发布。
