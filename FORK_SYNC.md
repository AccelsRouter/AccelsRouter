# Fork 同步与定制开发指南（w-newapi）

本仓库是 [QuantumNous/new-api](https://github.com/QuantumNous/new-api) 的 fork，承载 WSPN 的定制化改动（部署配置、支付渠道定制、品牌相关 UI 等）。

本文档定义如何**长期既同步上游更新，又保持定制功能不被冲掉**。所有提交（人工或 AI）必须遵守。

---

## 1. 分支模型

```
upstream/main        QuantumNous/new-api 官方主分支（只读）
        │
        ▼
origin/main          本 fork 的 main 分支（=upstream/main 的镜像，定期 fast-forward）
        │
        ▼
origin/w             长期开发主线，所有定制改动的归集分支（生产环境部署源）
        │
        ▼
feature/<name>       单次功能分支，从 w 切出，开发完 squash merge 回 w
```

**关键纪律**：

- `main` 分支**永远不接受直接提交**，只能从 `upstream/main` fast-forward 合并；
- 所有定制改动只进 `w` 分支或其衍生 feature 分支；
- 生产部署始终从 `w` 分支构建。

---

## 2. 一次性初始化

新克隆仓库或新成员加入时执行：

```bash
git remote add upstream https://github.com/QuantumNous/new-api.git
git fetch upstream
git fetch origin

# 确认 main 跟踪 upstream
git checkout main
git reset --hard upstream/main   # 仅首次校准
git push origin main
```

---

## 3. 例行同步上游（建议每周一次或上游发版后）

```bash
# 1) 拉取上游最新
git fetch upstream
git checkout main
git merge upstream/main --ff-only
git push origin main

# 2) 把 main 合到 w
git checkout w
git pull
git merge main
#    ↑ 如果有冲突，按第 5 节"冲突热点"处理；解完冲突 commit

# 3) 跑一次本地构建+冒烟
go build ./...
cd web/default && bun install && bun run build && cd ../..
go test ./...

# 4) 推送
git push origin w
```

**禁止使用 `git rebase upstream/main`**——会重写 `w` 分支的提交历史，让团队成员的 PR / CI 部署链路全部断裂。

---

## 4. 代码组织铁律（**最关键的一章**）

定制改动必须遵守以下规则。新写代码或合并 PR 时违反任一条 = 拒绝合入 `w`。

### 4.1 加法不减法

| 允许 | 禁止 |
|---|---|
| 新建文件 | 改名上游既有文件 |
| 在既有文件**末尾**追加 if/case/路由 | 修改上游既有 if/case 的内部逻辑 |
| 在 init/register 函数末尾追加注册 | 重新排序上游既有注册顺序 |
| 加新字段到 struct 末尾 | 修改既有字段名/类型/标签 |
| 加新方法到既有类型 | 修改既有方法签名 |

**示例（合规）：**

```go
// model/option.go 末尾追加
optionMap["WCheckoutEnabled"] = "false"
optionMap["WCheckoutSandbox"] = "true"
// ...
```

**示例（违规）：**

```go
// 改了上游 if 分支顺序，会跟上游后续修改产生冲突
if isStripeEnabled() { ... }
- if isWaffoEnabled() { ... }
+ if isWCheckoutEnabled() { ... }   // ❌ 不要插队
+ if isWaffoEnabled() { ... }
```

### 4.2 新功能必须用独立文件承载

WCheckout 这种规模的接入，**99% 的代码应该在新建文件里**。具体结构：

```
service/wcheckout.go                       新增：HTTP 客户端、签名、token 管理
controller/topup_wcheckout.go              新增：HTTP handler
setting/operation_setting/payment_wcheckout.go   新增：配置结构（如果上游没有合适的位置）
web/default/src/features/system-settings/payment/wcheckout-settings-card.tsx   新增
web/default/src/features/topup/components/wcheckout-button.tsx   新增
```

对既有文件的"插入式"改动仅限：

- `model/option.go`：追加 option key（末尾）
- `controller/topup.go::GetTopUpInfo`：追加 if 分支（末尾）
- `router/api-router.go`：追加路由行（路由组末尾）
- `web/default/src/features/system-settings/payment/section-registry.tsx`：注册新卡片
- `web/default/src/features/topup/`：注册新按钮
- i18n 文件：追加翻译 key

### 4.3 命名前缀隔离

- Go 标识符以 `WCheckout` 开头：`WCheckoutEnabled`、`RequestWCheckoutPay`、`WCheckoutWebhook`
- React 组件以 `WCheckout` 开头
- i18n key 用独立命名空间或前缀：`wcheckout.settings.title`
- option key 严格全大写驼峰 `WCheckout*`，跟上游 `Stripe*` / `Creem*` / `Waffo*` 并列

### 4.4 不重构上游既有代码

哪怕看到上游代码有以下情况都**忍住**：

- 重复代码（DRY 违反）
- 命名风格不一致（驼峰/下划线混用）
- 缺少错误处理
- 注释不全
- 文件超过 1000 行

**理由**：每一次重构都是一次未来的合并冲突。重构上游代码 = 把维护责任永久转移到自己头上。

如果某段上游代码确实有 bug，**优先给上游提 PR**；如果上游不接受，再考虑在 fork 里 patch。

### 4.5 上游 PR 优先

如果定制功能可能对上游也有价值（如 WCheckout 接入、bugfix），**先尝试给上游提 PR**。上游接受了，下次同步自动消化，零维护成本。

判断标准：

- ✅ 通用功能、bug 修复、文档改进 → 提上游 PR
- ❌ WSPN 品牌相关、内部部署配置、SaaS 客户专属逻辑 → 仅本 fork

---

## 5. 冲突热点与解决策略

按方案 C 的纪律，长期冲突点收敛到以下几个文件。每次同步如出现冲突，按下表处理：

| 文件 | 冲突原因 | 解决策略 |
|---|---|---|
| `model/option.go` | 上游也加了 option key，跟你的追加位置碰头 | 保留双方所有 key，本地的放最后 |
| `controller/topup.go::GetTopUpInfo` | 上游加了新支付方式 if 分支 | 保留双方的 if 分支，本地的放最后 |
| `router/api-router.go` | 上游加了新路由 | 双方路由都保留，本地的放路由组末尾 |
| `web/default/src/features/system-settings/payment/section-registry.tsx` | 上游加了新支付卡片 | 双方卡片都保留 |
| `web/default/src/features/topup/*.tsx` | 上游改了充值 UI | **优先采用上游版本**，重新打补丁，因为上游 UI 演进快 |
| `go.mod` / `go.sum` | 依赖版本冲突 | 接受上游版本，跑 `go mod tidy` |
| `package.json` / `bun.lock` | 前端依赖冲突 | 接受上游版本，跑 `bun install` |

**通用解决流程**：

```bash
# 冲突发生时
git status                       # 看哪些文件冲突
# 对每个冲突文件，手工编辑保留双方内容
git add <file>
git merge --continue

# 如果某个冲突过于复杂，先放弃合并回到分叉点重新评估
git merge --abort
```

---

## 6. 定制改动清单（保持更新）

下表记录所有当前在 `w` 分支上、相对 `upstream/main` 的定制改动。每次新增/移除定制功能时更新这张表。

| 类别 | 改动 | 关键文件 | 备注 |
|---|---|---|---|
| 部署 | GitHub Actions CICD 工作流 | `.github/workflows/deploy.yml` | ECR 推送 + ArgoCD 同步 |
| 部署 | 禁用上游所有 workflow 自动触发 | `.github/workflows/*.yml` | 改为 `workflow_dispatch` only |
| 部署 | Helm chart 配置 | （在 wspn-helm-deploy-eks-apps） | 外部仓库管理 |
| 定价同步 | BytePlus（AK/SK 签名）与阿里百炼海外版的渠道价格同步，生成计费表达式 | `controller/custom_pricing_sync*.go`；`controller/ratio_sync.go`（仅在请求前插入 9 行钩子，另在结果组装处加 1 个 `expr_errors` 字段 + 1 行调用 `custom_pricing_sync_validate.go`：上游表达式在本站存不下来时（如用量表达式而本地没有任务插件提供该模型）预览里标记并跳过，而不是整批保存失败；只用 `u("characters")` 的线性按字符任务表达式（TTS）自动换算成本站语音转发的普通表达式 `tier("base", p * <每百万字符美元>)`，预览里注明来源表达式）；`aurora/.../models/custom-byteplus-pricing-card.tsx`，由计费设置页的「上游价格配置」页签（`custom-upstream-pricing-config.tsx`，在 `ratio-settings-card.tsx` 与 `billing/section-registry.tsx` 注册）承载，新增上游凭证在该文件加卡片；i18n 三个 locale 顶部 fork 区追加 7 个 key | 选项 `BytePlusPricingAccessKeyID` / `BytePlusPricingSecretKey`（未改 `model/option.go`） |
| 价格同步表格 | 当前价格列的「Includes request rules」长表达式不再把表格撑宽（表格单元格默认 nowrap 压过了 break-all） | `aurora/.../models/upstream-price-cells.tsx` 仅为该元素 class 追加 `whitespace-normal`；`web/` 未改 | 从上游移植该文件时若整份采用上游版本，需重新加上这一处 |
| 价格同步亏本提示 | 同步表格里每个上游价格标注渠道名；上游成本高于当前售价标「亏本风险」，同模型多上游中最贵的标「来源中最高」；页面顶部汇总亏本风险清单 | `aurora/.../models/custom-price-risk.tsx`（新文件）；`upstream-price-cells.tsx` 加 1 行 import + 1 行组件；`upstream-ratio-sync.tsx` 加 1 行 import + 1 行组件；三个 locale 顶部 fork 区追加 4 个 key | 仅比较输入/输出单价（美元/百万 token，未计分组倍率）；阶梯数不同时只比最贵档 |
| 定价预览页签 | 计费设置页新增「定价预览」页签：列出已启用渠道里无价格的模型；点「检查上游价格」后补充价格差异与亏本风险，可按问题类型筛选，并显示厂商定价（优先读 z.ai/DeepSeek(峰时价)/Kimi/Anthropic/xAI 官方价格页，阿里自家 qwen 模型读百炼国际站完整分档价（需启用的国际站阿里渠道，`controller/custom_vendor_alibaba.go`），不再使用 models.dev（OpenAI、Google 的来源见下方「定价以厂商为准」一行）；官网页面改版时需修 `controller/custom_vendor_pages.go`），亏本风险可多选并一键从高价渠道移除模型（需确认）；多档计费按输入长度拆分后逐段对比（`custom-price-risk.tsx` 的 `alignSegments`） | `aurora/.../models/custom-pricing-preview.tsx`（新文件）；`ratio-settings-card.tsx` 加页签类型/标签/渲染共 4 行；`billing/section-registry.tsx` 的 `visibleTabs` 追加 `pricing-preview`；`controller/custom_channel_model_remove.go`（新）+ `router/channel-router.go` 路由表加 1 行 `/remove_models`；`controller/custom_vendor_prices.go` + `custom_vendor_pages.go`（新，夹具在 `controller/testdata/vendor_pages/`）+ `router/api-router.go` 的 ratio_sync 组加 1 行 `/vendor_prices`；`upstream-ratio-sync.tsx` 仅给 `getDefaultEndpointForChannel` 加 `export`；三个 locale 顶部 fork 区追加 10 个 key | 无价格判定：有效配置里没有 ModelPrice、ModelRatio 或有效计费表达式 |
| 厂商价格目录 | 厂商参考标价入库：每天抓取（官方价格页、阿里百炼）并保留变更历史；单次变动超过 3 倍或来源条目锐减时不覆盖旧数据，转待审核；变更发飞书告警；管理员可手填价格（优先级最高）。入口：计费设置「上游价格配置」页签 | `model/custom_vendor_price.go`、`controller/custom_vendor_catalog.go`（新）；`model/main.go` 的 AutoMigrate 列表加 3 个模型；`main.go` 加 1 行 `go controller.StartCustomVendorPriceRefresh()`；`router/api-router.go` 加 1 行 `RegisterCustomVendorPriceRoutes`；`aurora/.../models/custom-vendor-price-catalog.tsx`（新） | 新表 `custom_vendor_prices`、`custom_vendor_price_changes`、`custom_vendor_price_sources`；告警走现有 Lark 告警 webhook |
| 支付 | WCheckout 稳定币支付集成 | 见第 4.2 节文件列表 | _待集成_ |
| 定价以厂商为准 | 厂商价格目录作为价格同步的一个来源「厂商标价」（预设 ID -102）：手填价优先，其次厂商自己的来源；分档变成 `len <= N ? …` 分支，名字含 claude 的模型带 Anthropic 的缓存写入倍数（cc 1.25x、cc1h 2x）；没有输出价的不提供。来源（2026-11-10 起）：z.ai / Kimi / Anthropic / xAI / DeepSeek / Google 的官方价格页，阿里百炼接口，OpenAI 取自 OpenRouter（OpenAI 官方价格页拒绝程序访问）：先读公开模型列表里的 `openai/*`，再逐个读该模型的 endpoints，取 OpenAI 自己、不带 service tier 的那个端点的价（模型列表每个模型只给一个价，可能是别家区域端点、贵 10% 的价，2026-11-12 的 `gpt-5.6-sol` 即如此；flex / priority 不取；OpenAI 自己不提供的模型保留列表价；endpoints 读不到的模型本次不更新）。字节两个来源（`controller/custom_vendor_bytedance.go`，夹具 `byteplus.md`、`volcengine.md`）：`byteplus` 读 BytePlus ModelArk 价格页的 Standard 表（美元、带日期的模型 ID，同时登记去掉日期的名字）和旁边的音频输入表；`volcengine` 读火山方舟价格页的第一张表「在线推理（常规）」（人民币，按单独的设置项 `VendorPriceCNYPerUSD` 换算，在「上游价格配置」的 `custom-vendor-currency-card.tsx` 里填写，汇率写在来源名里；未填写时该来源报错、不读取，不使用全局的 `USDExchangeRate`，因为收款货币是美元时它是 1；只收 doubao 模型；版本号的点和横线两种写法都登记）。两页还读视频（seedance，按「分辨率 × 是否含输入视频」的每百万 token 价）、图片（seedream，按张，1.5K 上下两档、图层拆分、输入图）和向量模型（文本/图片输入价，表达式里用 `img`）。任务类模型的价格以「用量项单价 + 条件」存在 `tiers` JSON 的 `usage`/`when` 里（没有新增数据库列），同步时 `customVendorUsageExpr` 按该模型任务插件声明的维度生成规范表达式（如 `u("resolution") == "480p" && u("video_input") == "none" ? tier(…) : …`），插件没声明的模型名不提供；定价预览里这类模型的厂商价显示为「每张」或「每百万 token」，是否与厂商一致按表达式文本比较。两页都只收字节自家模型，按时段计价的行、离线推理、3D 模型不收；本站在售的带日期快照名（如 `doubao-seed-2-1-pro-260715`）在 `resolveCustomVendorPrices` 里取其系列价。DeepSeek 存峰/谷两档（tier 的 `band`），生成与内置表达式相同的峰谷表达式，页面上的峰时窗口说明变了就只存峰时价且不提供；Google 读标准档的文本输入/输出/缓存、≤200k 分档、音频输入价（`ai`）、按日期生效的价格，按分钟/按张/按输出模态计价的模型不收；OpenAI 的长提示 override 变分档，缓存写入价为 `cc`。额外维度（`cache_write`、`audio_input`、`band`）只存在 `tiers` JSON 里，没有新增数据库列。在「上游价格同步」选该来源、勾选整列、预览确认后保存。定价预览页签新增问题类型「与厂商标价不一致」（比较输入/输出单价，按输入长度分段对齐；峰谷计价的比较峰、谷两档），峰谷表达式的当前价显示峰时价并标「峰时」；「无价格」的模型可选一个上游或「厂商标价」（排第一）、勾选后直接取价保存（`custom-unpriced-fill.tsx`，复用同步页的预览确认与保存，不会动已有价格的模型）；任意行点渠道名可标记把该模型从该渠道移除（复用 `/api/channel/remove_models`）；「上游成本」列对非亏本行显示服务该模型的各渠道的上游价（选了取价来源则显示该来源），取不到价时显示「—」并在悬停提示里给出原因（表达式存不了、上游价目读不懂、该渠道取价失败、来源没列这个模型）；勾选亏本风险后可点「改用该渠道的价格」把售价改成该渠道的上游价（`custom-unpriced-fill.tsx` 的 `CustomSavePricesButton`，与无价格取价共用预览确认和保存；同一模型勾了多个渠道取最贵的）。从渠道移除模型的同时把它们写进该渠道的上游模型忽略列表（`settings.upstream_model_update_ignored_models`，`custom_channel_model_remove.go`），否则上游模型同步会再加回来。可视化编辑器读不了的表达式（多档同名、条件多套括号、项的顺序不同、只有输入价）由 `custom-price-risk.tsx` 的 `readLooseTiers` 读出单价，用于显示和亏本比较。「价格差异」来自后端对整条表达式的文本比较；`comparePriceParts` 逐项（输入、输出、缓存读写、图片、音频、分档界限、请求规则）比较，只是写法不同的不再算差异，输入输出相同但其他项不同的在上游成本下方列出具体项。`comparePriceParts` 对没单独定价的项按计费引擎的回退价比较（缓存和媒体输入按输入价，媒体输出按输出价，`cc1h`/`cc12h` 按短写入价的 1.6/2 倍），多档时标出档位。任务类表达式用前端的表达式求值器处理：`getUsagePriceRange` 给出每单位（每百万 token/每秒/每张）的价格区间用于显示，`chargesSameUsage` 按厂商各档的用量逐一试算来判断是否与厂商价一致（不看写法）。阿里厂商价（`custom_vendor_alibaba.go`）带上显式缓存创建价 `input_token_cache_creation_5m` 作为 `cc`。「无价格」徽标悬停显示原因。「无价格」还包括：表达式或旧价格收费为 0 的（`chargesNothing`），以及 `/api/option/model_pricing/issues` 报告插件无法计费的任务模型（未配置、用量键不兼容）。阿里百炼国际站渠道的取价（`custom_pricing_sync_alibaba.go`）：按张计价且由任务插件服务的模型生成 `tier("image", u("image_count") * 单价)`；DeepSeek 的忙/闲时两档价生成峰谷表达式（峰时固定为北京时间每天 08:00-22:00，接口只给档名不给时段，时段取自百炼的 DeepSeek 模型文档，其他厂商的分时价不转换）；每日刷新后的亏本检查（`controller/custom_vendor_underpriced.go`）：把在售模型的现价（倍率价先转成表达式）与厂商标价表达式逐项求值比较——token 模型比输入/输出/缓存读写/音频/图片在每个输入长度分档两侧的单价，任务模型比插件声明的每个条件组合下每个用量项的单价——低于标价 0.1% 以上的列进 Lark 卡片「售价低于厂商标价」，名单不变不重复发（进程内记忆，重启后会再发一次）；同时读一次市场汇率（Frankfurter 的欧洲央行参考价），市场汇率比 `VendorPriceCNYPerUSD` 低 1% 以上时在卡片里提示，并把市场汇率写进 volcengine 来源的状态说明；汇率改动导致的价格变化（来源名里的汇率变了）不再被 3 倍跳变保护拦成待审核；按秒计价的视频（`video_ratio` 一口价，或 `video_ratio_480p/720p/1080p` 按分辨率）生成 `u("resolution") == "480P" ? tier("480P", u("seconds") * 单价) : …`，分辨率取该模型任务插件声明的那几档，哪一档百炼没给价、插件还按别的维度（如 `audio`）计价、或本网关没有插件计量该模型的秒数，就不转换并给出原因；读不懂的模型连同原因放在同步结果的 `custom_skipped`，由 `ratio_sync.go` 以 `skipped` 返回（3 处 `// Fork`） | `controller/custom_vendor_sync.go`、`controller/custom_vendor_sources.go`（新，Google 与 OpenAI 解析，夹具 `testdata/vendor_pages/google.md`、`openrouter.json`）；`custom_pricing_sync.go` 的 `fetchCustomPricingSync` 加 1 个分支；`ratio_sync.go` 的 `GetSyncableChannels` 追加 1 个预设；`custom_vendor_catalog.go` 的 `/vendor_prices` 返回加 `syncable`；aurora：`models/constants.ts`、`upstream-ratio-sync-helpers.ts`（显示名）、`channel-selector-dialog.tsx`（预设判断加 1 个 ID）、`custom-price-risk.tsx` 的 `matchesVendorPrice`、`custom-pricing-preview.tsx` | 不自动改价：厂商价变动只进目录并发飞书卡片（`pkg/larknotify/vendorprice.go` 的 `VendorPriceCard`，文案在 `i18n/locales/*.yaml` 的 `lark.vendor_price_*`，发到告警机器人）：价格变动、超 3 倍待审核、新上架模型、来源从可读变为不可读（只报一次）；本站在售的模型带标记。改价要人工在同步页确认 |
| 首页 | 「支援你常用的 Agent 工具」加 WorkBuddy、Coze；宽屏 5 列 | `aurora/src/features/home/components/sections/agent-tools.tsx`；WorkBuddy 图标在 @lobehub/icons 6.x 才有，本仓库锁 5.x，故取其彩色图标存为 `aurora/src/assets/brand-icons/workbuddy.svg` 并在 `lib/lobe-icon.tsx` 的 `CUSTOM_ICONS` 注册；Coze 用 coze.cn 当前品牌标（`brand-icons/coze.png`，键名 `CozeMark`，库里的旧 `Coze` 图标仍用于渠道类型） | 图标库升到 6.x 后可删 WorkBuddy 的本地 SVG |
| 任务插件 | Kling 插件增加 `kling-video-v3` / `kling-video-v4` 两个按秒计费名：上游发送官方的 `kling-v3` / `kling-4.0`，用量事实为 seconds + resolution + audio（v3，按 mode std→720p / pro→1080p / 4k，sound 开关）或 seconds + resolution + reference（v4，任何图片/视频/元素输入即 reference；未给时长先按 30 秒预扣、按输出时长结算），与上游网关 aigc 的按秒表达式一致；官方名仍按 units 计费 | `plugins/tasks/kling/plugin.js`（meta 前常量块 + usageProfiles + `perSecondUsage`/`outputSeconds`，extractUsage / parseSubmitResponse / extractUsageOnComplete 各加几行）；测试 `plugins/kling_responses_test.go` 的 `TestKlingPerSecondAliases` | 上游每次改 Kling 插件都要重新合这几处；豆包插件同类改法见 OVERSEAS_MODELS |
| 流式用量 | Claude 格式（`/v1/messages`）的流式请求转成 chat completions 发给上游时的用量：火山方舟 / BytePlus 渠道保留 `stream_options.include_usage`（OpenAI 适配器对非 OpenAI/Azure 渠道类型会删掉它，方舟不带这个参数就不返回流式用量）；上游不返回用量时的本地估算也统计 Claude / Gemini 格式请求的输出（原来只按 chat relay mode 统计，这两种格式的输出被记成 0，输出不计费） | `relay/channel/volcengine/adaptor.go` 的 `ConvertClaudeRequest`、`relay/channel/openai/helper.go` 的 `processTokenData`（各 1 处 `// Fork`）；测试 `relay/channel/volcengine/adaptor_test.go`（新）、`relay/channel/openai/stream_tool_billing_test.go` | 同步上游时若上游已修复这两处，删除 fork 改动 |
| 渠道进价记录 | 每天随厂商标价刷新，按价格同步的方式读一遍每个启用渠道的价目表，只记该渠道在售模型（按上游的模型名）的价格，价格变了才新增一行，价目表里消失的模型记一行空价（表 `custom_channel_prices`：渠道、上游模型名、生效时间、价格 JSON；已在 PostgreSQL 16.15、MySQL 8.0.46、SQLite 上实测建表与读写）。指向字节官方接口的渠道（`*.bytepluses.com` / `*.volces.com`）价目表里没有的模型取价格目录里对应来源的标价。上游账单对账用这份记录给我方每小时的用量定价，得出「应付成本」：按当时生效的价格，早于首次记录的用量按首次记录的价格并标注；价格取决于小时汇总里没有的信息（每次请求的输入长度、视频分辨率）时给出最低到最高的区间。价格同步读取上游 `/api/pricing` 时，若该模型的任务插件有自己的价格（`billing_plugin_variants`）且各插件一致，以插件价为准（原来只读模型级价格，会取到上游没在用的价，例如 0） | `model/custom_channel_price.go`（新）、`controller/custom_channel_prices.go`（新）、`model/reconciliation_bill.go`（`UpstreamBillExpected`）、`model/main.go`（AutoMigrate 加 1 行）、`controller/ratio_sync.go`（2 处 `// Fork`：读 `billing_plugin_variants`；把各上游价目表交给记录器）、`controller/custom_vendor_catalog.go`（每日循环里调用）；前端 `aurora/src/features/reconciliation/upstream-bill-dialog.tsx`、`api.ts`；测试 `controller/custom_channel_prices_test.go`（新）。进价历史查看：`GET /api/ratio_sync/channel_prices`（可按 `channel_id` 过滤，最新在前，最多 2000 条；`middleware/access_token_routes.go` 加 1 条 `option:read` 规则），「上游价格配置」页签底部的「渠道进价历史」（`aurora/src/features/system-settings/models/custom-channel-price-history.tsx`，新；按渠道筛选、按模型名搜索，每行显示生效时间、价格和变动前价格） | 折扣系数未做：转售渠道按其报价计，线下折扣不体现 |
| 分销商呼叫记录与稽核 | 组织钱包计费的消费日志记下计费当时的比例：客户/自用的折扣在 `other.org_discount_ratio`（任务日志即使为 1 也记，用来和此前没记的旧任务行区分），分销商为客户调用付的批发折在 `other.admin_info.org_wholesale_ratio`（客户拿不到 admin_info）；呼叫记录按日志里记的比例显示实付，不再按当前配置现算（旧任务行和没记批发折的旧行才回退到当前配置），分销商和平台管理员的视图多一行批发成本（`cost_quota` / `cost_ratio`，CSV 多两列），客户自己的视图和导出不带。稽核：`org.update` 只记实际改动的设置（JSON，模型按新增/移除），稽核列表给 `org:` / `user:` / `workspace:` 目标补名称（`target_name`），前端把各动作的详情转成一句话（旧行的 Go map 格式也能读）。钱包页充值金额带 `$`。已在 PostgreSQL 16.15、MySQL 8.0.46、SQLite 3.50.4 上实测稽核名称查询与呼叫记录读取，无表结构变更 | `service/log_info_generate.go`（`attachOrgCharge`）；`service/task_billing.go`（3 处 `// Fork` 调用 + `attachTaskOrgCharge`）；`model/org_usage.go`、`model/org_audit.go`、`model/log.go`（两个计算字段）；`controller/organization_admin.go`（`orgUpdateAuditDetail`）、`controller/organization_sso_usage.go`；`aurora/.../organization-console/audit-detail.ts`（新）、`audit-panel.tsx`、`call-records.tsx`；`aurora/.../wallet/components/recharge-form-card.tsx` | `ListOrgLogs` 多一个 `resellerView` 参数：只有分销商和平台管理员的入口传 true；同步上游 `task_billing.go` 时保留三处 `attachOrgCharge` / `attachTaskOrgCharge` 调用，否则新的任务行又会回退到当前配置 |
| 安全加固（2026-11-13 审查） | fork 自有代码的一批修复。**组织密钥**：只有密钥主人能把个人密钥绑进组织；成员被移除/退出/停用、工作区删除、分销商管理员被停用或撤销时，其组织密钥一并停用，主人在身份失效期间不能重新启用。**邀请**：开户邀请的激活码只在邮件发不出去时才返回给邀请人，列表不再带激活码；邀请管理员需 owner。**BYOK**：私有分组只给主人自己的渠道计价（后续任务沿用平台渠道、分销商路由时按用户自己的分组计费）；BYOK 地址保存时和每次请求前都校验内网；巡检、身份探测、上游监控、阿里厂商价来源、每日渠道进价记录、账单对账一律只处理平台渠道。**支付**：WonderGate 入账改用真正的行锁并只从 pending 入账一次，金额走统一上限校验，token 模式不足 1 美元的金额拒绝；验签失败不再把期望签名写进日志；订阅的第三方支付要求二次验证。**其他**：客户自己的用量报表不带分销商成本；分销商聚合呼叫记录不带客户终端 IP；客户划拨有钱包上限；飞书密钥不返回给访问令牌；飞书卡片的值不能 @所有人 或伪装链接；MCP `get-generation` 按用户可见范围裁剪；CloudFront 来访地址只在直连对端是受信代理时采用；厂商价格跳变保护覆盖各档各项价格并拒绝 NaN；后台任务协程带 recover；内存版每日 token 计数饱和不回绕 | `model/organization.go`（`disableOrgTokens`、`HasActiveOrgTie`、`GetTokenOrgId`）、`model/reseller_admin.go`、`model/channel.go`（`GetPlatformChannels`）、`model/topup.go`（`RechargeWonderGate`）、`service/byok.go`（`ValidateByokBaseURL`）、`controller/organization_*.go`、`controller/reseller.go`、`controller/topup_wondergate.go`、`controller/upstream_*.go`、`controller/reconciliation_*.go`、`controller/custom_vendor_*.go`、`middleware/real_client_ip.go`、`pkg/larknotify/card.go`、`mcpserver/account_tools.go`、`common/limiter/token_window.go`；上游文件里只动 fork 自己的块：`relay/helper/price.go`（`HandleGroupRatio` 的 `// Fork` 段）、`middleware/distributor.go`（`SetupContextForSelectedChannel` 开头的 `// Fork` 段）、`controller/token.go`、`controller/option.go`、`router/api-router.go`（订阅路由加 `Require2FAEnabled`） | 同步上游时保留上述 `// Fork` 段。**未修、属上游代码**：`/api/pricing` 的 `enable_groups` 会带出 `user-<id>` / `reseller-<id>` 私有分组名（`model/pricing.go`）；转发用的 HTTP 客户端不做连接时内网校验（`service/http_client.go` 及各渠道适配器），所以 BYOK 地址仍有极窄的 DNS 重绑定窗口；Midjourney / remix / 任务插件的后续请求按原任务渠道走而不看分组（`relay/mjproxy_handler.go`、`relay/relay_task.go`），其计费漏洞已在 fork 的计价段堵住 |
| 安全加固补充（2026-11-13） | 上游监控与探测路由（`/api/admin/upstream/*`）在 `AdminAuth` 之外按渠道权限放行：查看要 `channel.read`，探测与改探测配置要 `channel.operate`，设置/测试价格地址要 `channel.sensitive_write`；BytePlus / 阿里账单拉取的访问令牌权限改为 `billing:write`；`bd:write` 令牌不再继承主人的 `bd.manage`；MCP 排行榜跟随 rankings 模块开关并排除 BYOK 流量；BYOK 渠道的故障不再发飞书告警；创建邀请加限流；计费路径上查分销商归属出错时拒绝请求而不是当作非客户；WonderGate 被拒付的订单保持 pending（只有订单关闭才标失败），重试成功仍能入账；有进行中的退款申请时不能把个人余额转成分销商额度；退款模块（当前无路由）的状态流转改为带条件更新并校验影响行数；稽核详情按字符边界截断 | `router/api-router.go`、`middleware/access_token_routes.go`、`middleware/header_nav_enabled.go`（新）、`controller/bd.go`、`controller/reseller.go`、`controller/topup_wondergate.go`、`mcpserver/account_tools.go`、`service/lark_notify.go`、`service/refund.go`、`model/refund_request.go`、`model/reseller.go`（`resellerOrgIdOfCustomer`）、`model/organization.go`、`model/org_audit.go` | 管理员默认没有 `channel.sensitive_write`，需要设置上游价格地址的管理员要单独授权 |
| UI 主题 | Aurora 第三主题（Crypto Indigo 配色）| `aurora/`；集成点见第 6.1 节 | `aurora/` 位于仓库根，是 `web/default` 的 fork，上游变更不自动同步 |

### 6.1 aurora — forked theme

`aurora/`（仓库根目录）is a fork of `web/default` (a third selectable website
theme, "aurora", Crypto Indigo palette). 放在仓库根而不是 `web/` 下，是为了与
upstream 的 `web/` workspace 完全解耦——upstream 同步永远不会触碰 aurora 子树。
Upstream changes to `web/default` do NOT flow into aurora automatically — port
them manually when desired.

Aurora's own files never conflict with upstream (separate top-level directory).
Only these additive integration touchpoints need re-merge attention on upstream
sync:

| 文件 | 集成内容 |
|---|---|
| `main.go` | aurora embed 指令 + `ThemeAssets` 字段 |
| `router/web-router.go` | `ThemeAssets` aurora 字段 + `NoRoute` 分支 |
| `common/embed-file-system.go` | `themeAwareFileSystem` 中的第三个 FS |
| `common/constants.go` | `SetTheme` 接受 `"aurora"` |
| `controller/option.go` | `theme.frontend` 保存校验放行 `"aurora"` |
| `Dockerfile` | `builder-aurora` 阶段 + `COPY dist` |
| `.github/workflows/release.yml` | aurora 构建步骤 |
| `web/default/.../system-settings/general/system-info-section.tsx` | 主题选择器中的 aurora 选项 |

**同步 web/default 后的操作**：只需检查上表中的 7 个文件是否因上游改动产生冲突或需要 port，`aurora/` 目录本身不受影响。

---

## 7. Release 与版本号

- `w` 分支的每次合入触发一次 ECR 镜像构建；
- 镜像 tag 用 `w-<short-sha>`，便于回滚；
- 重大改动在 commit message 中明确标注 `[wspn-custom]` 前缀，方便上游同步时甄别。

例：

```
[wspn-custom] feat: integrate WCheckout sandbox payment gateway
[wspn-custom] chore: disable upstream workflow auto-triggers
fix: relay timeout when streaming Claude responses    # 上游 cherry-pick，无前缀
```

---

## 8. 回滚

如果某次上游同步引发线上事故：

```bash
# 立刻把 w 回滚到上一个已知良好的 commit
git checkout w
git reset --hard <last-good-sha>
git push --force-with-lease origin w
```

注意：`force-with-lease` 比 `--force` 安全，会拒绝覆盖他人已推送的 commit。

**事故后必做**：在 `w` 分支顶部用 revert commit 而非 reset，保留事故记录可追溯。

---

## 9. AI 协作约定

当使用 Claude Code、Cursor 等 AI 工具开发本仓库时：

1. **首次会话务必让 AI 读本文件**（`@FORK_SYNC.md`），并明确告知它是 fork 项目；
2. AI 写代码时应优先选择"新建文件"而非"修改既有文件"；
3. 改动到本文件第 5 节列出的"冲突热点文件"时，AI 必须明确说明是"插入式追加"还是"修改既有逻辑"，前者通过、后者需要人工二次确认；
4. AI 不得调用 `git rebase upstream/main`、`git push --force`、`git reset --hard`（除非用户明确要求且二次确认）；
5. AI 提交 commit 时，定制改动使用 `[wspn-custom]` 前缀。

---

## 10. 检查清单

每次 PR / 提交前自查：

- [ ] 仅在文件**末尾**追加内容，未修改上游既有行
- [ ] 新功能代码放在以 `wcheckout`/`wspn`/`custom` 命名的新文件
- [ ] 标识符使用了项目前缀（`WCheckout*` 等）
- [ ] commit message 加了 `[wspn-custom]` 标记
- [ ] 本地 `go build ./...` 和前端 `bun run build` 通过
- [ ] 若改动到第 5 节冲突热点文件，已记录到第 6 节定制改动清单

---

**维护历史**

| 日期 | 修订内容 | 维护者 |
|---|---|---|
| 2026-05-26 | 初稿，定义方案 C 的同步纪律 | Jayke |
| 2026-05-27 | 新增 aurora 主题 fork 说明（第 6 节表格 + 第 6.1 节） | Jayke |
