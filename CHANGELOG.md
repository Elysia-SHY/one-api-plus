# One API Plus 更新日志

> 基于 fork 的 [one-api](https://github.com/songquanpeng/one-api) 改造，定位为「小型 OpenRouter + Agent Gateway」。
> 所有版本日期均为 2026-09-21（同一天内的快速迭代）。

## [v0.3.2] - 2026-09-21

### 修复（启动卡死 + 前端字段错位，来自全仓库审查）
- **启动卡死在「initializing token encoders」**（严重）：上游沿用的 `tiktoken-go` 默认 BPE 加载器用**无超时的 `http.Get`** 直连 `openaipublic.blob.core.windows.net`。国内网络下该域极慢，导致进程永久阻塞、服务起不来（实测卡死）。修复：
  - 新增 `relay/adaptor/openai/bpeloader.go`：带**超时 + 本地缓存 + 可换镜像源**的 BPE 加载器，经 `tiktoken.SetBpeLoader` 注入。支持 `TIKTOKEN_DOWNLOAD_TIMEOUT`（默认 30s）、`TIKTOKEN_BPE_BASE_URL`（镜像）、沿用 `TIKTOKEN_CACHE_DIR` / `DATA_GYM_CACHE_DIR`。
  - `InitTokenEncoders` 增加**总超时预算**（`TIKTOKEN_INIT_TIMEOUT`，默认 45s）与**失败降级**：下载不到就跳过，服务照常启动，不再阻塞；首次实际用到相关模型时再按需重试。
  - `getTokenNum` 增加 `tokenEncoder == nil` 兜底（退化为近似估算），避免无编码器时空指针 panic。
- **实时负载页空白**：`GET /api/plus/load` 返回的 `channels` 是 `map[渠道ID]在途数`，前端 Dashboard 误当数组遍历（`c.name/c.active/c.max`），永远渲染不出数据。已改为按 map 渲染。
- **预算功能读写全错**：`UserBudget` 结构体字段是 `daily_limit` / `monthly_limit`，前端 Config 页读写用的是 `daily_quota` / `monthly_quota`，保存与回显均失效。已修正。
- **能力库搜索框无效**：Capability 页的模型名搜索框未接入过滤，已改为按名称本地过滤。

### 审查结论（其余项）
- `go build ./...`（默认 + `-tags lite`）、`go vet`、CI 测试套件（config/middleware/service/channeltype）全绿。
- 前端 34 个 `/api/plus/*` 调用与后端路由逐一比对，无幽灵端点；`catalog` / `capability` / `ratelimit` / `responses` / `memory` / `mcp` 等字段经交叉核对一致。
- 已发布二进制实测可正常启动（SQLite 建库 + 迁移 + 建 root 账号），版本号正确注入。
- 已知非本项目问题：`common/image` 的 `TestDecode` 依赖外部 Wikimedia 大图（上游遗留、且不在 CI 测试范围内），本机网络下会失败，不影响构建与运行。

---

## [v0.3.1] - 2026-09-21

### 修复（前端字段错位导致功能不可用）
- **模型目录页**：后端 `ModelCatalog` 的 JSON 字段是 `model_name`，前端误用 `it.model`，导致「模型」列全空、启用 / 改状态报「channel_id 与 model 不能为空」。已修正。
- **模型组页**：`AddModelGroup` / `AddGroupMember` 直接绑定结构体，前端却发 `name` / `group_id`+`model`，后端收不到（应为 `group_name` / `group_name`+`model_name`）；表格组名 / 成员名同样取错字段。已修正，「添加成员」的组名改为下拉选择。
- **模型同步 404**：`baseURL()` 不剥离尾部 `/v1`，用户填 `https://host/v1` 这类 Base URL 时会拼出 `/v1/v1/models` 导致拉取静默失败。已修复：自动剥离尾部 `/` 与 `/v1`。
- **同步结果透明化**：Catalog 页同步完成后逐渠道显示错误（此前渠道拉取失败只显示「新增 0 个」，看不出原因）。

### 新增
- **渠道编辑页「从上游拉取」按钮**：在渠道编辑的模型填写区新增按钮，实时调用上游 `/models`（不落库），拉到的模型自动去重合并填入「模型」多选框。新增后端端点 `GET /api/plus/catalog/fetch/:id`（`modelsync.FetchChannelModels`，只读探测）。
- 版本号 `VERSION` 升至 `0.3.1`。

---

## [v0.3.0] - 2026-09-21

### 新增（前端补全：所有 Plus 功能打通管理界面）
此前后端 `service/*`（模型同步 / 能力库 / 模型组 / 限流 / MCP / Memory / Responses / Dashboard / 智能路由 / 别名 / 定价 / 预算）均已就绪，但 web 端**完全没有对应入口**，功能不可见、不可用。本次一次性补齐前端，涵盖 9 个新页面：

- **模型目录 / 自动发现** `pages/Plus/Catalog.js`：查看各渠道上游 `/v1/models` 自动发现的模型，一键「立即同步全部渠道」，启用模型、调整状态（新发现 / 正常 / 降级 / 已暂停 / 无效）。
- **模型能力库** `pages/Plus/Capability.js`：表格展示 `context_length / vision / tool_call / reasoning / embedding`，支持按能力勾选筛选与「重新推断」。
- **模型组** `pages/Plus/Group.js`：创建模型组（first_available / random / round_robin / capability 策略），增删成员并设权重。
- **限流状态** `pages/Plus/RateLimit.js`：展示 QPM / 突发 / 并发 / 实时在途请求。
- **MCP 网关** `pages/Plus/Mcp.js`：注册 / 更新 / 删除 MCP 服务器，一键发现全部工具、调用工具。
- **Agent 记忆** `pages/Plus/Memory.js`：写入 / 搜索 / 删除 / 清空 short / long / profile 三级记忆。
- **Responses API** `pages/Plus/Responses.js`：展示 `/v1/responses` 兼容层状态。
- **仪表盘 / 智能路由** `pages/Plus/Dashboard.js`：用量概览、各渠道实时并发负载、路由评分权重（成本 / 延迟 / 负载 / 稳定性）调节并保存。
- **别名 / 定价 / 预算** `pages/Plus/Config.js`：模型别名 CRUD、定价表（美元 / 1M tokens）CRUD、用户日 / 月预算设置。

### 工程
- 新增统一前端 API 客户端 `helpers/plus.js`，封装全部 `/api/plus/*` 接口（返回 `{success,message,data}` 信封，业务层自行判 `success`）。
- `App.js` 新增 9 条 `/plus/*` 路由（均 `PrivateRoute` 包裹）；`Header.js` 顶部导航新增 9 个菜单项（除「记忆」外均 `admin` 限定），中英双语 `locales/zh` / `en` 同步补充 `header.plus.*` 文案。
- 版本号 `VERSION` 由 `0.2.1` 升至 `0.3.0`。

### 发布
- Windows / Linux(amd64+arm64) / macOS 四个二进制；前端 `web/build/default` 随发行包附带。

---

## [v0.2.1] - 2026-09-21

### 修复（CI / 发布）
- **Windows Release 前端构建失败**：原 `windows-release.yml` 用 `\` 续行设置 `DISABLE_ESLINT_PLUGIN=true` 不稳，改为 `env:` 块设置，并补充：
  - `DISABLE_ESLINT_PLUGIN: "true"`：规避 CRA 的 `eslint-webpack-plugin` 在当前依赖组合下报 `Environment key "jest/globals" is unknown`。
  - `GENERATE_SOURCEMAP: "false"`：缩小产物、加快构建。
  - `NODE_OPTIONS: "--max-old-space-size=4096"`：防止 Windows runner 上 webpack 内存溢出（OOM）。
- 注：GitHub「重新运行（Re-run）」会使用原始 commit 的旧 workflow 文件，修复不会自动生效；本次通过打新 tag `v0.2.1` 触发修复后的 workflow。
- 版本号 `VERSION` 由 `0.2.0` 升至 `0.2.1`。

### 发布产物
- Windows / Linux(amd64+arm64) / macOS 四个二进制均成功发布到 Release 页。

---

## [v0.2.0] - 2026-09-21

### 新增（AI Gateway 第二阶段）
- **能力库（Capability）** `service/capability`：按 `context_length / vision / tool_call / reasoning` 等维度标注模型能力，三层探测（seed → rule → probe）。
- **模型组（Model Group）** `service/modelgroup`：逻辑名 → 多真实模型映射，支持 `first_available / random / round_robin / capability` 调度策略。
- **实时负载计量（Load Meter）** `service/routing/loadmeter.go`：每渠道实时并发计数，参与路由打分。
- **限流（Rate Limit）** `service/ratelimit`：令牌桶 QPM + 并发闸门，Redis / 内存双后端；Redis 故障时放行（不误杀）。
- **提示词缓存（Prompt Cache）** `service/promptcache`：按消息前缀哈希缓存，命中不重复计费。
- **MCP 网关** `service/mcp`：JSON-RPC 2.0 `tools/list`、`tools/call`，支持 SSE 解析。
- **记忆（Memory）** `service/memory`：short / long / profile 三级记忆。
- **Responses API** `service/responses` + `middleware/responses.go`：实现 `/v1/responses` 与 chat 的双向转换，含 SSE/JSON 自适应 writer。
- **Dashboard** `service/dashboard`：QPS / 用量 / 成本 / 趋势统计（Go 侧分桶，避开 SQL 方言差异）。

### 架构
- 接入 `option / router / main`，新增 `/api/plus/*` 第二阶段接口（Dashboard / 能力库 / 模型组 / MCP / Memory）。
- **Lite 编译期裁剪**：`fs_lite.go` / `fs_full.go` 按 build tag 切换嵌入资源；`router/api_lite.go` / `router/api_full.go` 按 tag 拆分管理路由。支持 `go build -tags lite`。

### 发布
- Linux / macOS / Docker Release 均成功；Windows Release 因上述 CI 问题于 v0.2.1 修复。

---

## [v0.1.0] - 2026-09-21

### 改造（fork → One API Plus，第一阶段）
- 全量改名：`module` 由 `github.com/songquanpeng/one-api` 改为 `github.com/Elysia-SHY/one-api-plus`（149 个 Go 文件 + Dockerfile + workflows + README）；系统名改为 **One API Plus**。
- **模型自动同步** `service/modelsync`：拉取 OpenAI / Azure / Gemini / Claude / DeepSeek / OpenRouter 的 `/models` 入库。
- **渠道健康检测** `service/health`：滑动平均延迟 + 错误率 + 连续失败，自动降权 / 暂停。
- **智能路由** `service/routing`：priority / weight / latency / cost / stability 多维打分选路。
- **模型别名** `service/alias`：首次启动播种 `DEFAULT_ALIASES`。
- **定价 / 成本 / 预算** `service/cost`：定价表、成本预测、日 + 月预算。
- **重复请求缓存** `service/responsecache`：Redis 缓存，命中不扣额度。
- **新增数据表**：`ModelCatalog / ChannelHealth / ModelAlias / ModelPrice / UserBudget`（`model/plus.go`，已并入 `migrateDB`）。
- **链路增强**：`distributor` 做别名解析 + 路由选路 + **强制校验 API Key 模型白名单**（原版仅在 `/v1/models` 过滤，属真实安全缺口）；`relay.go` 失败按排除表换渠道、渠道耗尽后按 `FALLBACK_MODELS` 换模型；渠道健康度回写。
- **LITE_MODE**：运行时关闭后台任务。

### 新增接口
- `/api/plus/*` 第一批 18 个接口。

### 发布
- 首次打 tag `v0.1.0`，Linux / macOS Release 成功；Docker / Windows Release 因早期 CI 脚本问题于后续提交修复。

---

## 已知未实现（后续路线）
- Agent / MCP 网关的**前端管理界面**（目前仅有后端 API）。
- 手机管理端。
- Lite 编译期的适配器深度裁剪（当前为运行时 `LITE_MODE` 关闭后台任务）。
- 本机（无 gcc，`CGO_ENABLED=0`）无法起 SQLite 做启动冒烟，数据库逻辑仅编译期 + 单元测试验证。
