# 前端按钮规范（全项目统一）

> 建立于 2026-11-04。**唯一真源**：`web/src/styles/uvp-buttons.scss`。
> 门禁：`web/src/styles/uvp-buttons.test.ts`（`cd web && npx vitest run src/styles/uvp-buttons.test.ts`）。
> 本文只讲"怎么写"，不讲"为什么这么写"——原因写在源文件注释里。

## 0. 一句话

**按钮只有三档大小、六种语义；样式只从 `uvp-buttons.scss` 出，页面里不许再写 `.arco-btn` 的尺寸和配色。**

## 1. 起因：三个具体问题

| 问题 | 实测现状（2026-11-04） | 处理 |
|---|---|---|
| 样式不统一 | 圆角 2 / 6 / 8 / 10px 混用；同一种"次级按钮"有 4 份互相覆盖的配色副本 | 全部收敛到一个真源，只保留 6 级语义 |
| 大小不一 | 6 种高度并存：Arco 32、项目 34、页头 40、small 28、mini 24、popconfirm 32 | 只留 3 档：40 / 34 / 28；mini 归并到 28 |
| 只突出重点按钮 | 549 个 `a-button` 里 125 个 primary 有蓝底，其余约 370 个默认按钮是"白底灰字" | 次级按钮改浅蓝底蓝字，整屏有层次但不高饱和 |

## 2. 三档尺寸（唯一）

| 档位 | 高度 | 用途 | 写法 |
|---|---|---|---|
| lg | 40px | 页面头部主 CTA（新增 / 刷新 / 状态动作） | `class="uvp-page-action-btn"` |
| md | 34px | **默认**：页面内、搜索面板、弹窗 footer、表单 | 不写 `size` |
| sm | 28px | 行内紧凑：表格工具条、卡片内、代码块头 | `size="small"` |

```diff
- <a-button size="mini">…</a-button>      // ⛔ 已归并，门禁会红
+ <a-button size="small">…</a-button>
- <a-button style="height: 36px">…</a-button>   // ⛔ 禁止裸 px
+ <a-button>…</a-button>
```

⛔ **不要再引入第 4 档**。确实需要更小（如输入框里的图标按钮）时，只能靠**容器专属**规则局部覆盖（参考 `uvp-ui-language.scss` 里 `.arco-input-suffix` 的 28×28），且要在门禁白名单里说明理由。

## 3. 六级语义（唯一）

| 语义 | 什么时候用 | 写法 | 观感 |
|---|---|---|---|
| **主操作** | 提交 / 保存 / 确认。**一个操作区最多 1 个** | `type="primary"` | 蓝渐变实心 |
| **次级操作** | 其余普通动作：返回、关闭、预览、复制、下载、导出、批量操作 | 不写 `type`（或 `type="secondary"`） | **浅蓝底 + 蓝字** |
| **中性** | 弹窗 / 抽屉底部的"取消" | 自动（footer 里的 secondary） | 灰底深灰字 |
| **危险** | 删除 / 清空 / 卸载 | `status="danger"` | 软红底红字 |
| **危险·实心** | 确认弹窗里的"确认删除" | `type="primary" status="danger"` | 实心红 |
| **停止** | 中断进行中的动作（停止录像、取消任务） | `class="uvp-stop-btn"` | 琥珀软底 |
| **新增** | 列表页的"新增 / 创建" | `class="uvp-create-btn"`（配 `uvp-page-action-btn`） | 绿实心 |
| **工具** | 刷新 / 探测 / 同步 | `class="uvp-refresh-btn"`（配 `uvp-page-action-btn`） | 青色软底 |
| **文字** | 表格行内、轻量动作 | `type="text"` | 无底，hover 出浅底 |

### 判断"该不该 primary"

按**操作区**（一个搜索面板 / 一个弹窗 / 一行工具条）算，不是按页面算：

- 搜索面板：`查询` = primary，`重置` = 次级；
- 列表工具条：**最多一个** primary（通常是"新增"或页面主入口）；
- 弹窗：`确定/保存` = primary，`取消` = 中性；
- 表格操作列：**一律 `a-link` + `uvp-table-action--*`**，不用按钮（见 `frontend-ui` 专题）。

反例（改前）：代码生成预览弹窗里 10 个"复制代码"全是 primary ⇒ 满屏蓝，反而没有重点。现已全部降为次级。

## 4. 场景速查

```vue
<!-- 页面头部 -->
<a-button class="uvp-page-action-btn uvp-refresh-btn" @click="load">刷新</a-button>
<a-button class="uvp-page-action-btn uvp-create-btn" type="primary" @click="create">新增</a-button>

<!-- 搜索面板 -->
<a-button type="primary" @click="query">查询</a-button>
<a-button @click="reset">重置</a-button>

<!-- 弹窗 footer（Arco 自动排右，统一 88px 宽） -->
<template #footer>
  <a-button @click="close">取消</a-button>
  <a-button type="primary" :loading="saving" @click="save">保存</a-button>
</template>

<!-- 危险确认 -->
<a-button type="primary" status="danger" :loading="removing" @click="remove">确认删除</a-button>

<!-- 行内/轻量 -->
<a-button type="text" size="small">复制</a-button>
```

## 5. 原生 `<button>`（全项目最容易漏的一半）

全仓还有 **240+ 个原生 `<button>`**，它们**完全不经过 `.arco-btn`** —— 上面所有 `.arco-btn-*` 规则对它们无效。2026-11-04 之前仪表盘页头的"刷新"就是原生 button + 页面本地 `.secondary`（白底黑字），统一按钮体系跑完它还是白的。

**口径：原生按钮挂同样的语义类，就拿到同样的几何（34px）+ 配色。**

| 语义 | 原生 button 写法 |
|---|---|
| 主操作 | `class="uvp-btn-primary"` |
| 次级操作 | `class="uvp-btn-secondary"` |
| 取消 / 关闭 | `class="uvp-btn-neutral"` |
| 刷新 | `class="uvp-refresh-btn"` |

```vue
<!-- 仪表盘页头（2026-11-04 起的基准写法） -->
<button class="uvp-refresh-btn" :disabled="refreshing" @click="refreshData">刷新</button>
<button class="uvp-btn-primary" @click="startEditing">编辑仪表盘</button>

<!-- 编辑态：三档分明 -->
<button class="uvp-btn-secondary" @click="resetLayout">恢复默认</button>
<button class="uvp-btn-neutral" @click="cancelEditing">取消</button>
<button class="uvp-btn-primary" @click="saveLayout">保存布局</button>
```

⛔ **禁止**在页面 scoped 样式里再给这些按钮写 `height / padding / border-radius / background / color` —— 一旦写了就是第二套观感（`home.layout.test.ts` 会红）。
⛔ 40px 档的 `uvp-page-action-btn` 不要和上面几个原生类一起用（会被压回 34px）。

## 6. 禁止事项（门禁会红）

1. ⛔ 页面 / 组件里写 `.arco-btn { height | padding | border-radius | font-size | background | color }`。
2. ⛔ `<a-button size="mini">`。
3. ⛔ 裸 px 覆盖 `uvp-page-action-btn` 的高度或圆角。
4. ⛔ 在 `uvp-ui-language.scss` / `arco-overrides.scss` 里重新定义按钮配色（唯一真源之外的第二套）。
5. ⛔ 用 `:not(.a):not(.b):not(.c)` 串排除条件 —— CSS 选择器 Level 4 里 `:not()` 的特异性会累加，会把规则抬到压过弹窗 footer 的层级。**用 `:where(:not(…))`**。
6. ⛔ 给"取消"用 `type="primary"`；给"删除"用普通 primary（必须带 `status="danger"`）。
7. ⛔ 彩色语义的底色调回"接近白"（`#f2f7ff` 那种）。门禁按 chroma ≥ 20、luma ≤ 0.94 判，肉眼看不出色相就是不合格。

## 7. 主题与对比度

- 按钮配色 token 全部**两套主题各写一遍**（`--uvp-btn-*`），不做 `var()` 别名——别名会在计算值阶段被钉死在亮色。
- 白字压实心底色只走 `--uvp-btn-primary-bg` / `--uvp-btn-danger-solid-bg`，**不要**直接拿 `--uvp-brand` 当底色（暗色下它是浅蓝，白字对比度只有 2:1）。
- 次级/刷新的底色必须"看得出是颜色"：亮色下 `--uvp-btn-secondary-bg = #e3eefe`、`--uvp-refresh-btn-bg = #cfeee7`。

## 8. 改了按钮之后怎么验

```bash
cd web
npx vitest run src/styles/uvp-buttons.test.ts          # 规范本身
npx vitest run src/style/model                        # 弹窗/token/控件高度契约
npx vitest run src/styles                              # 暗色主按钮契约
```

视觉确认（必须）：打开一个列表页 + 一个弹窗 + 深色主题，检查
① 同排按钮等高；② 主按钮一屏只有一个；③ 次级按钮是浅蓝底而不是纯白；④ 弹窗"取消"是灰色。

## 9. 已知遗留

- ⭐ **原生 `<button>` 尚未全量接管**：2026-11-04 只把仪表盘页头（`views/home/home.vue`）接进体系，其余仍是各页 scoped 自造类（白底/灰底）。这是"还有按钮没上色"的**唯一剩余来源**；改法见第 5 节，挂类 + 删掉页面里的几何/配色即可。
- `views/gb28181/device-mgmt/DeviceConfigDrawer.vue` 的 `dcg-btn` 系列有自己一套 scoped 按钮视觉（6 个 apply 键）。它们特异性更高，暂未被统一真源接管；改造时需同步删掉那份 scoped 定义。
- `views/gb28181/components/play-console/*` 播放控制台是深色容器，按钮视觉在 scoped 里自成一套。统一真源的几何（34px）已生效，配色仍是控制台自己的。
- `views/gb28181/device-mgmt/ChannelLocationPicker.vue` 有 2 条弹窗皮肤契约红（未挂 `modal-class`），与按钮体系无关，属既有问题。
