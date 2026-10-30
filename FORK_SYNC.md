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
| 定价同步 | BytePlus（AK/SK 签名）与阿里百炼海外版的渠道价格同步，生成计费表达式 | `controller/custom_pricing_sync*.go`；`controller/ratio_sync.go`（仅在请求前插入 9 行钩子）；`aurora/.../models/custom-byteplus-pricing-card.tsx` + `section-registry.tsx` 末尾追加一项；i18n 三个 locale 顶部 fork 区追加 6 个 key | 选项 `BytePlusPricingAccessKeyID` / `BytePlusPricingSecretKey`（未改 `model/option.go`） |
| 支付 | WCheckout 稳定币支付集成 | 见第 4.2 节文件列表 | _待集成_ |
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
