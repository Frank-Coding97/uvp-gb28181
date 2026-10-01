# UVP-GB28181 编码规范全面扫描报告

- **扫描时间**：2026-09-30
- **扫描对象**：整个仓库（根 + `server/` Go 后端 + `web/` Vue3/TS 前端）
- **扫描方式**：规范配置盘点 + 6 类工具实跑 + 项目自定义约定 grep 实扫 + 提交历史量化 + 门禁有效性回溯验证
- **代码基线**：分支 `develop`；统计为扫描时快照，后续改动需重新运行命令

> **修复后复核（2026-09-30）**：快速质量门禁全绿：gofmt、go vet、全量 golangci-lint、ESLint（0 error）、Vue 表单规则、Vitest（257 个文件 / 1919 项）、Prettier 和 Stylelint 均通过。完整 Go 集成测试未运行：生产 bootstrap 会因本机运行中 UVP 进程持有日志独占锁而退出，且全量测试需要实际开发数据库。定向 shutdown 测试以不加载生产 bootstrap 的源文件方式通过。下文 §3-§4 的数量保留为原始扫描基线。

---

## 一、执行摘要

| 判定 | 结论 |
| --- | --- |
| 规范体系 | ✅ **完整**。前端 ESLint/Prettier/Stylelint/EditorConfig/commitlint，后端 golangci-lint，Git 钩子，CI，本地一键门禁均齐备 |
| 门禁有效性 | ✅ 本地快速门禁包含全量检查；CI 已补增量 Go lint 和前端格式、代码、样式及表单规则检查 |
| 存量债 | ✅ 原扫描记录的 638 条 Go lint、2222 条 Stylelint、390 个 Prettier 文件均已清零；ESLint 保留 23 条 warning、0 error |
| 主要缺口 | ⚠️ 完整 Go 集成测试需要空闲的日志锁与可用开发数据库；历史暴露令牌的吊销状态待确认 |
| 附加风险 | ⚠️ 原扫描发现 `gitee` remote 曾含访问令牌；当前本机 URL 已不含内嵌凭据（见 §5.4） |

**一句话**：本次代码规范扫描发现的存量问题已修复并纳入门禁；剩余事项是完整 Go 集成测试的运行环境和远端凭据吊销。

---

## 二、规范体系全景（已建立的"规章制度"）

### 2.1 前端（`web/`）

| 文件 | 作用 | 关键口径 |
| --- | --- | --- |
| `eslint.config.js` | 代码检查（ESLint 9 flat config） | `tseslint.configs.recommended` + `pluginVue flat/essential`；`no-var: error`、`no-multiple-empty-lines`；`no-explicit-any: off`、`prefer-const: off` |
| `.prettierrc.cjs` | 格式化 | `printWidth: 130`、`tabWidth: 2`、`semi: true`、`singleQuote: false`、`trailingComma: none`、`bracketSameLine: false`、`endOfLine: lf`、`arrowParens: avoid` |
| `.stylelintrc.cjs` | 样式检查 | standard + scss + vue + **`stylelint-config-recess-order`（属性顺序）** |
| `.editorconfig` | 编辑器约定 | utf-8、**`indent_size: 2`**、`max_line_length: 130`、**`end_of_line: lf`**、末尾空行 |
| `tsconfig.json` | TS 编译 | — |
| `commitlint.config.cjs` | 提交信息 | conventional commits；`type` 白名单 17 项；`header-max-length: 108`；`subject-case` 关闭（适配中英混排） |
| `lint-staged.config.cjs` | 暂存文件闸门 | `*.vue → eslint --fix + prettier --write + stylelint --fix` |

### 2.2 后端（`server/`）

| 文件 | 作用 | 关键口径 |
| --- | --- | --- |
| `.golangci.yml` | 静态检查（v2 格式） | `default: standard`（= errcheck + govet + ineffassign + staticcheck + unused）；`tests: true`；`third_party` 排除；**`max-issues-per-linter: 0` / `max-same-issues: 0`（显式关闭静默截断）** |
| `README.md` | 后端约定文档 | Swagger 注释规范、插件开发规范（命名/接口/路由/日志/DB）、代码生成最佳实践、任务调度开发建议 |

### 2.3 工程门禁

| 位置 | 覆盖 |
| --- | --- |
| `.husky/pre-commit` | `cd web && lint-staged`（仅暂存文件） |
| `.husky/commit-msg` | `commitlint` 校验提交信息 |
| `.github/workflows/ci-deploy-test.yml` | Go `test ./...` + Linux 构建 + 版本一致性 + 前端 `build:prod`（含 vue-tsc）+ Gitee 镜像；当前无 Windows 构建 |
| `scripts/quality-gate.sh` | 快速档：gofmt / go vet / 全量 golangci-lint / ESLint / Vue 表单规则 / Vitest / Prettier / Stylelint；完整档追加：vue-tsc / 全量 go test |
| `CLAUDE.md` | UI 风格一致性 + **表单输入体验 7 条硬规则** + Git 双推 + 技术栈 + 本地端口 8280 |

---

## 三、实扫结果

> 本节数值记录修复前的扫描快照；修复后状态见文首复核摘要。

### 3.1 后端（Go）

| 检查项 | 命令 | 结果 | 判定 |
| --- | --- | --- | --- |
| 格式 | `gofmt -l .`（排除 third_party） | **5 个文件**不合规 | ⚠️ 轻微 |
| 静态 | `go vet ./...` | **0 问题** | ✅ |
| 全量 lint | `golangci-lint run` | **638 条**：errcheck 376 / staticcheck 197 / unused 43 / ineffassign 16 / govet 6 | ⚠️ 存量 |
| `//nolint` 抑制 | grep | **0 处** | ✅ 无暴力压制 |
| TODO/FIXME | grep | 9 处 | 可接受 |

**gofmt 不合规文件（全部为测试文件）**：
```
app/gb28181/controllers/device_ptz_resources_test.go
app/gb28181/migration/play_lifecycle_contract_test.go
app/gb28181/ptz/storage_card_format_test.go
app/models/sysapigroup_test.go
app/openapi/integration/must_auth_database_test.go
```
> 修复：`cd server && gofmt -w <上述文件>`

### 3.2 前端（Vue3 / TS / SCSS）

| 检查项 | 命令 | 结果 | 判定 |
| --- | --- | --- | --- |
| 类型 | `vue-tsc --noEmit` | **0 错误**（耗时 4m6s） | ✅ |
| 代码 | `eslint src` | **28 条**：5 error + 23 warning | ⚠️ 存量 |
| 格式 | `prettier --check` | **390 个文件** 不合规 | ⚠️ 存量 |
| 样式 | `stylelint "src/**/*.{vue,scss,css}"` | **2222 条 error**（仅 269 可自动修复） | ⚠️ 存量 |

**ESLint 的 5 个 error**：
| 文件 | 位置 | 规则 | 问题 |
| --- | --- | --- | --- |
| `web/src/typings/vue-cropper.d.ts` | 42:56 / 42:60 / 42:64 | `@typescript-eslint/no-empty-object-type` | 使用了 `{}` 空对象类型（应改 `object` 或 `unknown`） |
| `web/src/views/gb28181/sip-log-v2/components/SessionDetail.vue` | 247:34 | `vue/custom-event-name-casing` | 事件名 `select-message` 应为 camelCase |
| `web/src/views/gb28181/sip-log-v2/components/TableView.vue` | 78:56 | `vue/custom-event-name-casing` | 事件名 `select-session` 应为 camelCase |

**Prettier 不合规的一个重要原因是缩进口径分裂**：部分源文件使用 4 空格缩进，而 `.prettierrc.cjs` 规定 2 空格。390 个文件的全部成因仍需逐文件分析。

| 缩进单位 | 文件数（`web/src` 内 .vue/.ts） |
| --- | --- |
| 2 空格（合规） | 432 |
| **4 空格（不合规）** | **145** |
| 其他（1/3，多为多行字符串等检测噪声） | 99 |

**Stylelint 2222 条的主要规则**：
- `declaration-block-single-line-max-declarations`（单行写多个声明）
- `order/properties-order`（recess-order 属性顺序，如 `gap` 应在 `align-items` 前）
- `color-hex-length`（`#fff` 应为 `#ffffff`）
- `media-feature-range-notation`、`at-rule-empty-line-before`

### 3.3 提交信息规范

| 指标 | 数值 |
| --- | --- |
| 全量提交 | 1392 条 |
| 符合 conventional commits | 1312 条 → **94.3%** |
| type 分布 | feat 698 / fix 355 / test 95 / style 51 / refactor 43 / chore 38 / docs 16 / merge 14 / perf 1 / ci 1 |
| 非法 type | **0** |
| header 超长 | **0** |
| 不合规 80 条构成 | Merge 提交（本应豁免）+ 门禁引入前的早期历史（`T0~T13` 任务编号开头、`cross-review round-N:` 等） |
| **门禁（2026-09-18）之后** | 67 条提交，仅 2 条 Merge 不符合结构（commitlint 对此类豁免）→ **实际 100% 合规** |

**结论**：提交规范已被 commitlint 有效接管，历史不规范的 80 条不需要追溯（重写历史风险大于收益）。

### 3.4 项目自定义硬规则（CLAUDE.md 表单 7 条）实扫

| 规则 | 实际违规 | 说明 |
| --- | --- | --- |
| ① 所有输入框加 `allow-clear` | **`a-input` 47/195、`a-textarea` 5/11、`a-input-password` 1/9** | ⛔ 真实违规 |
| ② 不用 `a-input-number` 的 `:min/:max` clamp | **25 处**（共 29 个 `a-input-number`） | ⛔ 真实违规 |
| ⑥ 表单 label 用 `<a-form-item label>`，不手写 `<label>` | 60 处 | ⚠️ **需人工甄别**：部分为云台/自定义控件的标签，非表单字段 label |
| ⑦ 不用 `alert()`/`confirm()`/`prompt()` | **1 处** | ⛔ `web/src/components/s-recorder-pcm/index.vue:42` |

**规则① 缺 `allow-clear` 的完整清单（53 处）**：

`a-input`（47）：
```
components/s-select-icon/index.vue:3
views/gb28181/zlm/components/ZLMDangerActionDialog.vue:163
views/gb28181/zlm/components/NodeConfigPanel.vue:319
views/gb28181/security/preview.vue:1341
views/gb28181/cloud-recordings/components/RecordingRuntimeControl.vue:259,260,261,262,264,294
views/gb28181/components/play-console/PtzScanCard.vue:38,81
views/gb28181/components/play-console/PlayConsolePtzSidebar.vue:296,313,330
views/gb28181/components/play-console/PlayConsoleDialogs.vue:85
views/gb28181/sip/steps/IdentityStep.vue:103
views/gb28181/device-mgmt/index.vue:3879,3882,3941,3944,3947,3955
views/gb28181/device-mgmt/DeviceConfigWeekPlan.vue:122,132
views/gb28181/device-mgmt/DeviceConfigDrawer.vue:2217,2575,2602,2671
views/gb28181/device-mgmt/DeviceConfigOsdBlocks.vue:304,344
views/gb28181/device-mgmt/DeviceConfigSlider.vue:112
views/gb28181/device-mgmt/DeviceConfigTextItems.vue:80,92,103
views/system/role/components/datascope.vue:5
views/system/codegen/components/codegen-config-drawer.vue:19,24,29,114,119
views/system/sysjobs/sysjobslist.vue:154,180
views/system/sysconfig/sysconfig.vue:54,63,71,81
```
`a-textarea`（5）：
```
views/gb28181/zlm/components/ZLMDangerActionDialog.vue:158
views/gb28181/security/preview.vue:1348
views/system/codegen/components/codegen-config-drawer.vue:88
views/system/sysjobs/sysjobslist.vue:157,223
```
`a-input-password`（1）：
```
views/system/sysconfig/sysconfig.vue:90
```

**规则② 使用 `a-input-number :min/:max` 的完整清单（25 处）**：
```
views/home/components/drilldown/MediaRuntimeLedgerDialog.vue:428
views/gb28181/security/preview.vue:1257,1266
views/gb28181/components/play-console/PlayConsoleDialogs.vue:286,320,357
views/gb28181/sip/ServiceConfig.vue:888,1017,1089,1224,1245
views/gb28181/openapi-client/OpenAPIClientCreateDialog.vue:144,150,156
views/gb28181/device-mgmt/SubscriptionDialog.vue:127,132
views/gb28181/device-mgmt/components/DeviceRecordQueryDrawer.vue:350
views/system/role/role.vue:138
views/system/division/division.vue:158
views/system/menu/menu.vue:348
views/system/sysconfig/sysconfig.vue:119,136,149,157,170
```

> 注：规则②要求这些数值输入改为 `<a-input>` + 手动校验 + `validate-status`，让用户看到自己输错什么，而不是被静默 clamp 到边界值。

### 3.5 其他前端卫生问题（非硬规则，仅供参考）

| 项 | 数量 | 备注 |
| --- | --- | --- |
| `console.log/debug/info` | 26 | 目录页/工具函数残留 |
| `v-html` | 8 | ESLint 已关该规则；集中在 `sip-log-v2` 日志渲染，需确认内容已转义 |
| `@ts-ignore/@ts-nocheck/@ts-expect-error` | 5 | 其中 2 个在自动生成的 `.d.ts`（`auto-import.d.ts`、`components.d.ts`），可忽略 |
| `any` 类型 | 552 | 规则已关闭，不算违规，仅记录 |

---

## 四、门禁有效性回溯（本次扫描的关键结论）

> 本节描述修复前的提交历史回溯样本，不代表修复后的当前门禁状态。

方法：取质量脚本提交 `fe843033`（2026-09-18）→ HEAD 的 `web/src` 变更文件集，与各 linter 报错文件求交。前端 pre-commit 钩子在此前的 `3b569e6f` 已接通。

| 验证 | 结果 |
| --- | --- |
| 门禁后变更过的前端文件 | 235 个 |
| 其中 Prettier 仍不合规 | **1 个**（`src/components.d.ts`，自动生成文件） |
| 3 个 ESLint error 文件是否门禁后改过 | **全部 NO**（均为门禁前存量） |
| Stylelint 重灾区 `SchedulerStrategyPanel.vue` | 最后提交 **2026-09-17**（门禁前一天），属门禁前存量 |

**结论**：当前样本支持“已触达的前端增量文件基本合规”。提交钩子仅覆盖暂存的前端文件；后端 lint 以及绕过钩子的提交仍需独立门禁。不能据此断定 638 / 2222 / 390 全部属于门禁前存量。

---

## 五、缺口与风险（按优先级）

### 5.1 ✅ `quality-gate.sh` 覆盖面已补齐

快速档现在运行：`gofmt / go vet / 全量 golangci-lint / eslint / Vue 表单规则 / vitest / prettier / stylelint`；完整档追加 `vue-tsc / go test`。

修复后的全量 Stylelint、Prettier 和 golangci-lint 均通过。GitHub Actions 也对增量 Go 与前端文件执行相应检查。

### 5.2 ✅ 换行符口径已统一

| 事实 | 详情 |
| --- | --- |
| `.editorconfig` 声明 | `end_of_line = lf` |
| `.prettierrc.cjs` 声明 | `endOfLine: lf` |
| 实际 | 文本文件统一 LF |
| `.gitattributes` | 根级规则已新增；Windows `.bat` / `.ps1` 保留 CRLF |
| `.editorconfig` 覆盖面 | 仅 `web/`，根目录与 `server/` 无 |

### 5.3 ✅ CLAUDE.md 表单硬规则已修复并自动化

修复后扫描：缺少 `allow-clear` **0**，`a-input-number` 的 `min/max` clamp **0**，`alert/confirm/prompt` 调用 **0**。数字字段使用 `SNumberField` 保留非法输入并显示校验错误。

Vue 模板规则由 AST 检查脚本覆盖，并有静态/绑定属性测试；脚本已纳入本地质量门禁和 GitHub Actions。ESLint 已启用 `no-alert`。

### 5.4 ⚠️ 安全：历史 Git remote 令牌暴露（吊销状态待确认）

原扫描发现 Gitee remote URL 曾包含明文访问令牌。修复复核时，本机 `.git/config` 的 Gitee URL 已是不带内嵌凭据的 HTTPS 地址。

旧令牌是否仍有效、是否已从其他副本清除，无法由本机 remote 配置证明；为关闭历史暴露风险，应在 Gitee 侧吊销该令牌。

当前无需再切换本机 remote URL。

### 5.5 ⚠️ P2：其他

| 项 | 说明 |
| --- | --- |
| `web/README.md` 仍是模板原文 | 标题为 "GinFast"，核心特性/目录结构与实际项目不符，易误导新成员 |
| `design/` 为空目录 | 无内容，git 不跟踪空目录 |
| golangci-lint 已进 CI | CI 对变更 Go 文件运行增量 lint；本地快速门禁运行全量 lint |
| 根目录无统一规范文档 | 规范散落在 `CLAUDE.md` + `server/README.md` + 各配置文件，缺少一份"规范总览"索引 |

---

## 六、修复与验收状态

> 下表对应本次扫描提出的代码规范修复项。完整 Go 集成测试与凭据吊销单列，不混同源码门禁通过。

| 阶段 | 动作 | 验收标准 |
| --- | --- | --- |
| 0 | 修复 gofmt 和 ESLint error | 已完成：两项均为 0 |
| 1 | 将 Prettier、Stylelint、Go lint 纳入本地门禁 | 已完成：全量检查通过 |
| 2 | 补根级 `.gitattributes` 并统一 LF | 已完成 |
| 3 | 清理 Prettier 390 文件、Stylelint 2222 条、Go lint 638 条 | 已完成：对应全量检查为 0 |
| 4 | 修复表单违规并加自动化防线 | 已完成：AST 扫描和 2 项回归测试通过 |
| 5 | 完整 Go 集成测试 | 待在日志锁空闲且开发数据库可用的环境执行 |
| 6 | 吊销远端旧令牌 | 待 Gitee 侧完成吊销与 remote 凭据迁移 |

---

## 附：本次扫描所用命令

```bash
# 后端
cd server && /opt/homebrew/Cellar/go/1.25.6/bin/gofmt -l .
cd server && go vet ./...
cd server && ~/go/bin/golangci-lint run --timeout 10m

# 前端
cd web && ./node_modules/.bin/vue-tsc --noEmit
cd web && ./node_modules/.bin/eslint src
cd web && ./node_modules/.bin/prettier --check "src/**/*.{js,ts,json,tsx,css,less,scss,vue,html,md}"
cd web && ./node_modules/.bin/stylelint "src/**/*.{vue,scss,css}"
cd web && node scripts/check-form-rules.mjs && node --test scripts/check-form-rules.test.mjs

# 一键门禁（已有）
scripts/quality-gate.sh          # 快速档
scripts/quality-gate.sh --full   # 完整档
```

> 环境提示：本机 `go`/`gofmt` 不在 `PATH`，需显式用 `/opt/homebrew/Cellar/go/1.25.6/bin/`；`golangci-lint` 在 `~/go/bin/`；前端工具在 `web/node_modules/.bin/`。
