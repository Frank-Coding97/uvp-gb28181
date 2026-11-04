# 前端按钮规范（全项目统一）

> 建立于 2026-11-04。**唯一真源**：`web/src/styles/uvp-buttons.scss`。
> 门禁：`web/src/styles/uvp-buttons.test.ts`（`cd web && npx vitest run src/styles/uvp-buttons.test.ts`）。
> 本文只讲"怎么写"，不讲"为什么这么写"——原因写在源文件注释里。

## 0. 一句话

**按钮只有三档大小、八档语义；样式只从 `uvp-buttons.scss` 出，页面里不许再写 `.arco-btn` 的尺寸和配色。**

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

## 3. 八档语义 + 文字按钮（唯一）

> 2026-11-04 老板拍板：**新增走绿实心**、**次级一律浅蓝底**（不接受白底灰字）。
> 颜色只跟「点下去发生什么」绑定，**不跟页面绑定** —— 同一个词在任何页面必须同一个颜色。

| # | 语义 | 判断标准（点下去发生什么） | 写法 | 观感 |
|---|---|---|---|---|
| 1 | **主操作** | 真正执行 / 提交一次业务操作 | `type="primary"` | 蓝实心 |
| 2 | **创建** | 产生一条新数据 | `class="uvp-create-btn"` | 绿实心 |
| 3 | **刷新** | 不改变任何数据，只重新取一次 | `class="uvp-refresh-btn"` | 青绿软底 |
| 4 | **次级** | 传导数据 / 进入查看 / 轻度变更（筛选、视图、定位） | 不写 `type` | 浅蓝底蓝字 |
| 5 | **否定** | 放弃本次操作或退出当前上下文 | footer 自动 / `uvp-btn-neutral` | 中性灰底 |
| 6 | **停止** | 让正在跑的东西停下 | `class="uvp-stop-btn"` | 琥珀软底 |
| 7 | **危险** | 会删掉数据，但不在确认弹窗里 | `status="danger"` | 软红底红字 |
| 8 | **危险·确认** | 确认弹窗里的那一个「确认删除」 | `type="primary" status="danger"` | 实心红 |
| 9 | **文字** | 表格行内、列表里的轻量动作 | `type="text"` | 无底，hover 出浅蓝 |

### 3.1 文案 → 语义 映射（按全站实测文案归好类，直接查表）

**① 主操作（蓝实心）**
查询 · 查询录像 · 保存 · 保存变更 · 保存规则 · 保存配置 · 保存设置 · 保存授权 · 保存位置 · 保存为中心 · 保存调整 · 保存收藏 · 保存当前画面 · 确认保存 · 确认修改 · 确定 · 提交 · 登录 · 完成 · 我已安全保存 · 确认分配 · 应用并替换当前画面

**② 创建（绿实心）**
新增 · 新增上级 · 新建 · 新建组 · 新建设备 · 创建 · 添加 · 确认创建 · 导入插件

**③ 刷新（青绿软底）**
刷新 · 刷新状态 · 重新加载 · 重新读取最新目录 · 重试 · 只重试失败项 · 重新预检 · 重新查询 · 回读录制状态 · 读取 · 读取当前位置 · 检测端口 · 测试连通性 · 对账 · 自动刷新

**④ 次级（浅蓝底蓝字）**
重置 · 重置筛选 · 清空筛选 · 清空选择 · 清除坐标 · 取消选择 · 取消查询 · 导出 · 导出插件 · 导入 · 导入表 · 下载 · 下载这一张 · 上传文件 · 大文件上传 · 上传固件 · 复制 · 复制代码 · 复制 SIP 接入信息 · 复制 Call-ID · 编辑配置 · 查看 · 查看记录 · 查看操作日志 · 查看缓存任务 · 查看升级记录 · 详情 · 概览 · 更多 · 定位 · 地图选点 · 适应视野 · 设为默认中心 · 加入批量选择 · 应用速度 · 应用定位 · 下发 · 广播 · Talk · 小窗 · 连续控制 · 高级控制 2022 · 格式化 · 续订 · 时段 · 精确数值 · 设右边界 · 生成菜单 · 准备新升级

**⑤ 否定（中性灰底）**
取消 · 关闭 · 返回 · 返回列表 · 返回记录 · 返回客户端列表 · 返回节点管理 · 返回修改 · 退出 · 上一步 · 稍后再配 · 撤销 · 撤销修改 · 放弃草稿 · 立即返回 · 转后台 · 忽略此版本 · 查看全部对象

**⑥ 停止（琥珀软底）**
停止录制 · 停止播放 · 停播 · 停止跟踪 · 取消任务 · 取消确认停止 · 暂停 · 断开 · 强退 · 撤防

**⑦ 危险（软红）**
删除 · 批量删除 · 物理删除 · 卸载插件 · 清空 · 一键清理 · 清理已结束

**⑧ 危险·确认（实心红）**
确认移除 · 确认删除

> 查不到的词条按上面的「判断标准」推：**改数据 → ①/②；只取数 → ③；只改视图 → ④；放弃 → ⑤；中断 → ⑥；毁数据 → ⑦。**

### 3.2 三个"只上色"的例外类

有些按钮**不该被三档几何接管**（图标按钮、行内文字链、错误态里的「重试」），只想要颜色：

| 类 | 作用 | 用在哪 |
|---|---|---|
| `uvp-refresh-text` | 只把文字/图标染成青绿，不动尺寸与底色 | 行内「重试」、面板角落的图标刷新键 |
| `uvp-refresh-link` | 青绿文字链 + hover 浅青底 | 表格上方的「刷新」 |
| `uvp-btn-neutral` | 中性灰底（a-button 与原生 `<button>` 通用） | 页面内的「取消 / 关闭 / 返回 / 退出」 |

⚠️ `uvp-btn-neutral` 同时写进了主按钮与次级按钮的排除列表（`:where(:not(…))`），
所以 `type="primary"` 的「关闭」加了它也会变灰——不需要再改 `type`。

### 3.3 全站拉平记录（2026-11-04 落地）

| 语义 | 改动量 | 做法 |
|---|---|---|
| 刷新 / 重新加载 / 重试 | 30 处 a-button + 9 处原生 | 统一 `uvp-refresh-btn`（行内的用 `uvp-refresh-text`，避免撑成 34px） |
| 新增 | 8 处 | 统一 `uvp-create-btn` → 绿实心 |
| 取消 / 关闭 / 返回 / 退出 | 32 处 | 统一 `uvp-btn-neutral` → 中性灰 |
| 查询 / 重置 | 56 处 | 本来就一致，未动 |

⛔ 例外：`components/play-console/**` 与 `PlayConsoleLinked.vue` 是深色控制台，按钮自成一套深色观感，本轮**未接管**；要改需连同控制台的深色 token 一起做。

### 3.2 判断"该不该实心"

按**操作区**（一个搜索面板 / 一个弹窗 / 一行工具条）算，不是按页面算：

- 搜索面板：`查询` = 蓝实心，`重置` = 浅蓝次级；
- 列表工具条：最多 **一个蓝实心 + 一个绿实心**（通常是「查询」+「新增」），其余全是浅蓝；
- 弹窗：`确定/保存` = 蓝实心，`取消` = 中性灰；
- 表格操作列：**一律 `a-link` + `uvp-table-action--*`**，不用按钮（见 `frontend-ui` 专题）。

反例（改前）：代码生成预览弹窗里 10 个"复制代码"全是 primary ⇒ 满屏蓝，反而没有重点。现已全部降为次级。

### 3.3 已定的全站执行口径（2026-11-04，尚未动手）

盘点结果：`a-button` 文案 129 种、原生 `<button>` 文案 44 种，归并后只有上面 8 档。
不一致的七处按这张表拉平：

| 按钮 | 数量 | 现状 | 目标 |
|---|---|---|---|
| 查询 | 28 | 全部蓝实心 | 不变 ✅ |
| 重置 | 28 | 全部次级 | 不变 ✅ |
| 刷新 | 6 | 青绿 3 / 蓝实心 1 / 普通 2 | 全部 ③ |
| 重试 | 18 | 普通 15 / 蓝实心 1 / 文字 2 | 全部 ③ |
| 新增 | 8 | 全部蓝实心 | 全部 ② 绿 |
| 关闭 | 7 | 普通 5 / 次级 1 / **蓝实心 1** | 全部 ⑤ 灰 |
| 取消 | 32 | 弹窗里灰、页面里浅蓝 | 全部 ⑤ 灰（页面内也挂 `uvp-btn-neutral`）|

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

- ⭐ **原生 `<button>` 尚未全量接管**：2026-11-04 已接管仪表盘页头 + 行内「重试」文字链，其余约 220 个仍是各页 scoped 自造类（白底/灰底）。这是"还有按钮没上色"的**唯一剩余来源**；最集中的是 `device-mgmt/index.vue`（32 个）、多屏回放各面板（20/16/14/12 个）。改法见第 5 节：挂类 + 删掉页面里的几何/配色。
  ⚠️ 接管前先分清两类：**卡片内可点区块 / PTZ 方向键 / 树节点展开**不是按钮，别按按钮上色。
- `views/gb28181/device-mgmt/DeviceConfigDrawer.vue` 的 `dcg-btn` 系列有自己一套 scoped 按钮视觉（6 个 apply 键）。它们特异性更高，暂未被统一真源接管；改造时需同步删掉那份 scoped 定义。
- `views/gb28181/components/play-console/*` 播放控制台是深色容器，按钮视觉在 scoped 里自成一套。统一真源的几何（34px）已生效，配色仍是控制台自己的。
- `views/gb28181/device-mgmt/ChannelLocationPicker.vue` 有 2 条弹窗皮肤契约红（未挂 `modal-class`），与按钮体系无关，属既有问题。
