import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { parse } from "vue/compiler-sfc";

import { SNAPSHOT_LIBRARY_PAGE_SIZE_OPTIONS, SNAPSHOT_LIBRARY_PATH } from "./snapshotLibraryState";

const pageSource = readFileSync(resolve(process.cwd(), "src/views/gb28181/snapshot-library/index.vue"), "utf8");
const stateSource = readFileSync(resolve(process.cwd(), "src/views/gb28181/snapshot-library/snapshotLibraryState.ts"), "utf8");

/**
 * 取出某个 CSS 选择器的声明块，并**先剥掉注释**。
 *
 * ⛔ 为什么必须剥注释：本仓的样式注释里会写"原来用的是 aspect-ratio"这类
 * 说明，而 `not.toContain("aspect-ratio")` 这类反向断言会把注释一起命中
 * ⇒ 门禁永远红 ⇒ 久了就被人注释掉或加豁免。**反向断言的第一前提是
 * 判据只扫声明，不扫"提到这个词的说明"。**
 */
function cssBlock(source: string, selector: string): string {
  const withoutComments = source.replace(/\/\*[\s\S]*?\*\//g, "");
  const start = withoutComments.indexOf(`${selector} {`);
  if (start < 0) return "";
  const end = withoutComments.indexOf("}", start);
  return withoutComments.slice(start, end + 1);
}
const apiSource = readFileSync(resolve(process.cwd(), "src/api/gb28181.ts"), "utf8");
const tokensSource = readFileSync(resolve(process.cwd(), "src/style/var/uvp-ui-tokens.scss"), "utf8");

describe("image library page shell", () => {
  it("gates the whole page behind the snapshot permission", () => {
    // 菜单可见性与接口授权都绑在 device:snapshot，页面本身也必须门禁：
    // 少这一层，越权用户手输 URL 就能看到别人的图（后端也会拦，但页面会先报一堆 403）。
    expect(pageSource).toContain("mayViewSnapshotLibrary(userStore.account.permissions)");
    expect(pageSource).toContain('v-if="!canView"');
  });

  it("loads images through the token-aware helper only", () => {
    // ⛔ 取图接口在鉴权组里，`<img>` 带不了 Authorization 头。
    // 直接绑 item.url 的表现是"整页缩略图全 401 破图"，而且不报错、只在网络面板里看得见。
    expect(pageSource).toContain(':src="imageUrlFor(item)"');
    expect(pageSource).not.toContain(':src="item.url"');
    expect(pageSource).toContain("snapshotContentImageUrl(item.url, accessToken.value, getBaseUrl())");
    // 每次查询重读 token：access token 刷新后旧值会让缩略图全 401。
    expect(pageSource).toContain('accessToken.value = getAccessToken()?.accessToken ?? ""');
  });

  it("only sends filters the backend actually accepts", () => {
    expect(pageSource).toContain("normalizeSnapshotQuery(filters, pagination.page, pagination.pageSize)");
    // 时间格式必须能被后端 time.Parse(time.RFC3339) 解析，否则整页报"参数不合法"。
    expect(pageSource).toContain('value-format="YYYY-MM-DDTHH:mm:ssZ"');
  });

  it("keeps page size inside the backend cap of 200", () => {
    // 后端把 pageSize 硬截到 200；选项超过它就是"显示 500/页、实际只回 200 条"，页数也跟着错。
    expect(Math.max(...SNAPSHOT_LIBRARY_PAGE_SIZE_OPTIONS)).toBeLessThanOrEqual(200);
    expect(pageSource).toContain("SNAPSHOT_LIBRARY_PAGE_SIZE_OPTIONS");
  });

  it("discards late responses so fast paging cannot show stale rows", () => {
    expect(pageSource).toContain("requestToken");
    // ⛔ 必须带**唯一上下文**再断言：`if (token !== requestToken) return;` 在 try 与 catch 里
    // 各出现一次，只盯这一行的话，把主路径那处删掉照样绿（本批变异抓到的第 4 个假锚点）。
    // 判据：断言"某语句在不在"时，匹配串要包含只有该处才有的邻句。
    expect(pageSource).toContain("if (token !== requestToken) return;\n    rows.value = response.data?.list ?? [];");
  });

  it("reuses the shared page shell instead of inventing a new one", () => {
    expect(pageSource).toContain('class="snow-fill-inner uvp-page-shell-flat snapshot-library-shell"');
    expect(pageSource).toContain("<s-layout-search>");
  });

  it("keeps a single template root so the layout transition can animate the page", () => {
    // 布局用 `<Transition :name="transitionPage">` 包住路由页
    // (layout/components/Main/index.vue → components/s-main-transition/index.vue)，
    // 而 Vue 只给「单元素根」挂过渡钩子：根是 Fragment（多根）/文本/注释时，
    // 每趟渲染都打 `Component inside <Transition> renders non-element root node ...`，
    // 页面的淡入淡出静默失效。本页曾把 `<a-image-preview>` 与根 div 并排写 ⇒ 双根。
    // ⛔ 文本断言（toContain）钉不住这个，只有模板 AST 的顶层子节点个数能钉。
    const { descriptor } = parse(pageSource, { filename: "index.vue" });
    const roots = (descriptor.template?.ast?.children ?? []).filter(node => {
      if (node.type === 3) return false; // 注释不算根
      return !(node.type === 2 && !node.content.trim()); // 纯空白文本不算根
    });
    expect(roots.map(node => ("tag" in node ? node.tag : `type:${node.type}`))).toHaveLength(1);
  });
});

describe("image library styles", () => {
  it("only references --uvp-* tokens that actually exist", () => {
    // ⛔ 引用未定义的 --uvp-* 不会报错：整块样式静默失效（背景透明 / 边框不可见），
    // 表现是"这个区块像没渲染出来"。本仓已踩过（--zlm-* 有 14 个未定义）。
    const defined = new Set(Array.from(tokensSource.matchAll(/(--uvp-[a-z0-9-]+)\s*:/g), match => match[1]));
    const used = Array.from(pageSource.matchAll(/var\((--uvp-[a-z0-9-]+)/g), match => match[1]);
    expect(used.length).toBeGreaterThan(0);
    expect(used.filter(token => !defined.has(token))).toEqual([]);
  });
});

describe("image library download and batch actions", () => {
  it("downloads through a native anchor carrying the token, never a bare url", () => {
    // ⛔ 下载必须和 <img> 一样经过 imageUrlFor：`<a>` 同样带不了 Authorization 头，
    // 直接把 item.url 绑到 href 上的表现是「点了没反应 / 下载下来一个 401 页面」，
    // 而且**不报错**，只有看下载目录里那个几百字节的文件才发现。
    expect(pageSource).toContain("triggerNativeDownload(imageUrlFor(item), snapshotDownloadName(item))");
    expect(pageSource).not.toContain("anchor.href = item.url");
    expect(pageSource).toContain("snapshotDownloadName(item)");
  });

  it("never downloads through fetch/blob so bulk runs cannot exhaust memory", () => {
    // ⛔ fetch + createObjectURL 会把整张图攒在内存里：批量十几张时可能把标签页打崩，
    // 而"下载按钮点下去页面白了"很难联想到内存问题。
    expect(pageSource).not.toContain("createObjectURL");
    expect(pageSource).not.toContain("fetch(");
  });

  it("spaces bulk downloads out so the browser does not block them", () => {
    // ⛔ 连续同步点击 N 次会被浏览器判成非用户触发，只有第一张真下载；
    // 剩下的被静默拦掉，表现为"选了 8 张只下来 1 张"。
    expect(pageSource).toContain("window.setTimeout(resolve, 250)");
    expect(pageSource).toContain("if (index < targets.length - 1)");
  });

  it("scopes bulk selection to the current page instead of the whole result set", () => {
    // ⛔ 前端只拿到当前页，勾一个自己都拿不到的集合等于骗人：用户看到"已选 300 张"，
    // 而批量下载只会下本页那几张。
    expect(pageSource).toContain("rows.value.filter(item => selectedIds.value.has(item.id))");
    expect(pageSource).toContain("allSelected");
    expect(pageSource).not.toContain("selectAllMatching");
  });

  it("keeps the selection across paging so batch runs can span pages", () => {
    // ⛔ 翻页就清空的话，"第1页勾几张、切第3页再勾"这个最常见的批量用法直接不可用。
    const changePage = /function changePage\(nextPage: number\) \{[\s\S]*?\n\}/.exec(pageSource)?.[0] ?? "";
    expect(changePage).toContain("pagination.page = nextPage");
    expect(changePage).not.toContain("clearSelection()");
    // 反证：换页**规模**是要清空的（那一页的图已经不在这儿了，勾着等于悬空引用）。
    const changePageSize = /function changePageSize\([\s\S]*?\n\}/.exec(pageSource)?.[0] ?? "";
    expect(changePageSize).toContain("clearSelection()");
  });

  it("shows the cropped-free image in both the card and the detail drawer", () => {
    // ⛔ 抓拍图是取证材料：硬裁（cover）会切掉画面上下缘，判断现场是否拍全
    // 就必须点开大图，等于卡片没提供任何信息。
    expect(pageSource).toContain("object-fit: contain");
    expect(pageSource).not.toContain("object-fit: cover");
  });

  it("sends the keyword to the backend instead of filtering the current page", () => {
    // ⛔ 页内过滤会让「目标在第 7 页」和「目标不存在」表现完全一致（都是"没搜到"），
    // 用户会去反复改条件，而问题在分页不在筛选。
    expect(pageSource).toContain("normalizeSnapshotQuery(filters, pagination.page, pagination.pageSize)");
    expect(stateSource).toContain("query.keyword = keyword");
    // ⛔ 展示用的 rows 必须直接来自接口响应，中间不能插一层本地过滤。
    // 锚点选"赋值即完"而不是"不出现 filter"：批量下载要对 rows 做一次
    // **选中项**筛选（那是正常功能），用"禁止 filter"当判据会误伤正确实现。
    expect(pageSource).toContain("rows.value = response.data?.list ?? [];");
    expect(pageSource).not.toMatch(/rows\.value\s*=\s*response\.data\?\.list\?\.?\?\? \[\]\.filter/);
  });

  it("puts the shareable filters in the URL and skips its own echo", () => {
    // ⛔ 原来只有会话/编码进 URL，"把这批图发给同事"丢掉的正是时间窗/来源/关键词。
    expect(pageSource).toContain("syncRouteFilters()");
    expect(pageSource).toContain("selfSyncedUrl");
    // 自触发的 URL 变化必须被认出来，否则「改条件 → 写 URL → watcher 当深链 → 再查一遍」
    // 会变成每次操作两次请求 + 结果闪一次。
    expect(pageSource).toContain("if (selfSyncedUrl) {");
  });

  it("keeps a single search box instead of two hand-typed 20-digit code fields", () => {
    // ⛔ 老板 2026-10-06 明确去掉「快捷时间」与「按编码精确查询」：
    // keyword 走后端模糊匹配（设备名/通道名/别名/编码片段一框全中），
    // 再摆两个"20 位编码"输入框只会让人以为必须手打才能查。
    expect(pageSource).toContain('data-testid="library-keyword"');
    expect(pageSource).not.toContain('data-testid="library-device-code"');
    expect(pageSource).not.toContain('data-testid="library-channel-code"');
    expect(pageSource).not.toContain("library-advanced");
    // 快捷时间整套（按钮 + 菜单 + 纯函数）都该消失，别留死代码让人以为功能还在。
    expect(pageSource).not.toContain("library-quick-range");
    expect(pageSource).not.toContain("applyQuickRange");
    expect(stateSource).not.toContain("quickRangeBounds");
  });

  it("shows the platform primary keys so a report can name one exact row", () => {
    // ⛔ 光有 20 位编码没法直接定位库行；报障时对方要的是"哪一条记录"。
    expect(pageSource).toContain("设备 ID");
    expect(pageSource).toContain("通道 ID");
    expect(pageSource).toContain("item.deviceId");
    expect(pageSource).toContain("item.channelId");
  });

  it("puts the human-given alias on the card, the reported name only as backup", () => {
    // ⛔ 现场人员认的是 alias（自己起的"东门枪机"），而 channelName 是设备
    // 上报的厂家串（"IPC-HFW2431S"）。只显示上报名 ⇒ 满屏没人认得的字。
    expect(pageSource).toContain("snapshotChannelLabel(item)");
    expect(pageSource).toContain("snapshotDeviceLabel(item)");
    // 上报名只在与别名**确实不同**时并排，避免同一行出现两次同一个名字。
    expect(pageSource).toContain("snapshotShowsReportedName(item)");
    // 回退逻辑必须在纯逻辑层（可单测），不能在模板里现拼三元。
    expect(stateSource).toContain("firstNonBlank(item.channelAlias, item.channelName, item.channelCode");
  });

  it("keeps the card skeleton aligned with the channel cards in the device list", () => {
    // ⛔ 老板 2026-10-06 要求「卡片样式向通道列表的卡片对齐」。
    // 基准 = `device-mgmt/index.vue` 的 `.channel-card-*`（下面是它的实测值）。
    // ⛔ 这些值**必须逐字对齐**，差一档就会看成"这是另一个页面的卡片"。
    // 判据锚点选这些**具体数值**而不是"有没有 class" —— 只查类名的话，
    // 数值被改小（如 148px→120px）照样全绿，而观感已经变了。
    const baseline = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/index.vue"), "utf8");
    // 1) 网格列宽与行高策略一致
    expect(pageSource).toContain("minmax(280px, 1fr)");
    expect(baseline).toContain("minmax(280px, 1fr)");
    expect(pageSource).toContain("grid-auto-rows: max-content");
    expect(pageSource).toContain("align-content: start");
    // 2) 图片区固定高 + 只留下边框（卡片"头"的处理方式）
    expect(pageSource).toContain("height: 148px");
    expect(pageSource).toContain("border-bottom: 1px solid var(--uvp-panel-border)");
    // ⛔ 反证：图片区不能有圆角边框 —— 圆角贴在图片上会显得图片没铺满，
    // 且与 body 之间留出缺口。
    expect(cssBlock(pageSource, ".library-thumb")).not.toContain("border-radius");
    // 3) body 内边距与信息区栅格一致（68px 是设备列表实测值）
    expect(pageSource).toContain("padding: 11px 12px 12px");
    expect(pageSource).toContain("grid-template-columns: 68px minmax(0, 1fr)");
    expect(pageSource).toContain("gap: 5px 8px");
    expect(pageSource).toContain("font-size: 12px");
    expect(pageSource).toContain("line-height: 18px");
    // 4) 标题字号一致
    expect(pageSource).toContain("font-size: 14px");
    expect(pageSource).toContain("line-height: 20px");
    // 5) 等宽字体栈逐字一致（两处不同 ⇒ 同一平台两页卡片粗细差一档）
    expect(pageSource).toContain("ui-monospace, SFMono-Regular, Menlo, monospace");
    expect(baseline).toContain("ui-monospace, SFMono-Regular, Menlo, monospace");
    // 6) 用的是 label:value 竖排结构，不是"标签+值"挤在一行
    expect(pageSource).toContain("library-card-info");
    expect(pageSource).toContain("display: contents");
    // ⛔ 旧结构（标题旁挂一串 ID）必须彻底没了。同样用 cssBlock 口径：
    // 注释里提到 `library-meta` 是正常的，只有**样式与模板**里出现才算。
    const stripped = pageSource.replace(/\/\*[\s\S]*?\*\//g, "");
    expect(stripped).not.toContain("library-meta");
    expect(stripped).not.toContain("library-title__reported");
  });

  it("strips the native button chrome off the image area", () => {
    // ⛔⛔ 老板 2026-10-06 二次反馈：「还是不行」——卡片外围一圈白框。
    // 真因：图片区是 `<button>`，浏览器默认给 `border: 2px outset rgb(255,255,255)`
    // + `appearance: button`。⛔ 只写 `border-bottom` **只覆盖下边**，
    // 上/左/右仍是那圈白色 outset ⇒ 深色下刺眼。
    // ⚠️ 这是**上一次没修好的原因**：当时只改了底色，漏了 border 四边。
    const thumb = cssBlock(pageSource, ".library-thumb");
    // ⛔ 必须先 `border: 0` 再单独给 border-bottom；只有 border-bottom 不够。
    expect(thumb).toContain("border: 0");
    expect(thumb).toContain("appearance: none");
    expect(thumb).toContain("border-bottom: 1px solid var(--uvp-panel-border)");
    // ⛔ 顺序判据：`border: 0` 必须在 `border-bottom` **之前**，
    // 反过来写会被后者覆盖、border: 0 白写。
    expect(thumb.indexOf("border: 0")).toBeLessThan(thumb.indexOf("border-bottom:"));
  });

  it("paints the image area with the card's own background so竖图 leaves no bright bars", () => {
    // ⛔ 老板 2026-10-06 截图反馈：「卡片边框看起来存在一些问题」。
    // 真因不是边框，是**图片区底色与卡片底色不同**：
    // 实测竖图源文件 2252×4000（AR 0.56）在 300×148 横框里只填 **28% 宽度**，
    // 左右各留约 36% 空隙；空隙露出的 `--uvp-shell-muted`（深色下 #101923）
    // 比卡片底色 `--uvp-panel-bg`（#162231）深 ⇒ 两侧各出现一道"竖直亮缝"，
    // 看着像卡片被切成三段。
    // ⭐ 判据：两处底色必须**完全相同**（量 getComputedStyle 比对，不是"接近"）。
    const thumb = cssBlock(pageSource, ".library-thumb");
    expect(thumb).toContain("background: var(--uvp-panel-bg)");
    expect(thumb).not.toContain("--uvp-shell-muted");
    // ⛔ 别给 <img> 加 outline/border：元素本身是 width:100% 的横框，
    // contain 把画面缩在中间 ⇒ 描边画在**留白外沿**，会在图片两侧各画出一条竖线。
    const img = cssBlock(pageSource, ".library-thumb img");
    expect(img).not.toContain("outline");
    expect(img).not.toContain("border");
  });

  it("uses an arco button type that actually exists", () => {
    // ⛔ `type="default"` 在 Arco 里**不存在** ⇒ 渲染成浏览器原生 <button>：
    // 实测拿到 `2px outset` 灰白描边 + `rgb(107,107,107)` 底色，
    // 在深色页面上是一块突兀的白边按钮（老板截图反馈「批量选择按钮样式存在问题」）。
    // ⛔ 判据要钉住**具体值** `secondary` 而不是"不是 default"：
    // 拼错成 `seconday` 同样会静默回退原生样式。
    expect(pageSource).toContain("batchMode ? 'primary' : 'secondary'");
    expect(pageSource).not.toContain('type="default"');
  });

  it("keeps the card image area a fixed height so rows do not stagger", () => {
    // ⛔ 用 `aspect-ratio` 而不是固定高：图幅不同 ⇒ 同屏卡片高低不齐，
    // 而通道列表是定高的，两页并排看时差异明显。
    // ⛔ 判据必须**先剥掉注释**再扫：解释这次改动的注释里就写着 `aspect-ratio`
    // 这个词，直接 `not.toMatch` 会命中注释 ⇒ 门禁永远红，久了就被人注释掉。
    const thumbBlock = cssBlock(pageSource, ".library-thumb");
    expect(thumbBlock).toContain("height: 148px");
    expect(thumbBlock).not.toContain("aspect-ratio");
    // ⛔ 但取证图**必须** contain（这是与通道卡片的**故意**差异，不是漏对齐）：
    // 通道卡片的快照是引流图（cover 无所谓），抓拍图裁掉画面上下缘就看不出现场拍全没有。
    expect(pageSource).toContain("object-fit: contain");
  });

  it("defines the ellipsis and mono helpers locally because they are scoped, not global", () => {
    // ⛔ `text-ellipsis` / `mono` 在本仓是**各文件本地 scoped 类**（scoped 不跨组件生效），
    // 模板里写上去而本文件没定义 ⇒ 类什么都不做：不报错，值直接铺出去撑破卡片布局。
    expect(pageSource).toContain(".library-card-info .mono");
    expect(pageSource).toContain(".library-title.text-ellipsis");
    expect(pageSource).toContain("text-overflow: ellipsis");
  });

  it("keeps batch selection behind an explicit switch and the checkbox off the image", () => {
    // ⛔ 老板 2026-10-06：勾选框**不要默认存在**（浏览是主路径），
    // 开关放**筛选区**（放工具条会鸡生蛋：工具条只有勾了才出现，
    // 而勾选框又只在批量模式显示 ⇒ 找不到入口），勾选框在**卡片左下角**。
    expect(pageSource).toContain('data-testid="library-batch-toggle"');
    expect(pageSource).toContain('batchMode ? "退出批量选择" : "批量选择"');
    // ⛔ 工具条判据是「进入批量模式」不是「有选中」——
    // 否则"没勾选 → 工具条不显示 → 看不到已选 0 张"，用户以为自己没选上。
    expect(pageSource).toContain('v-if="batchMode" class="library-batch"');
    // ⛔ 勾选框在 body 底部行（`library-card-actions`）里，不在图片区（`library-thumb`）里。
    expect(pageSource).toContain('v-if="batchMode" class="library-pick"');
    // ⛔ 浮层时代的样式（半透明黑底/白描边/z-index）必须删干净：
    // 勾选框已改成行内元素，留着就是"行内元素带 z-index"的死样式。
    const pick = cssBlock(pageSource, ".library-pick");
    expect(pick).not.toContain("position: absolute");
    expect(pick).not.toContain("z-index");
    expect(pick).not.toContain("background: rgb(0 0 0");
    // 两端分置靠 auto 外边距，不是 justify-content: flex-end
    expect(cssBlock(pageSource, ".library-actions-spacer")).toContain("margin-right: auto");
  });

  it("clears the selection on every path that leaves the selection invalid", () => {
    // ⛔ 退出批量不清已选 ⇒ 下次进入「已选 N 张」凭空出现而用户一张没勾
    // ⇒ 批量下载**直接下错文件**。重置同理（且还要退出批量模式）。
    const exit = /function exitBatchMode\(\) \{[\s\S]*?\n\}/.exec(pageSource)?.[0] ?? "";
    expect(exit).toContain("clearSelection()");
    const reset = /function reset\(\) \{[\s\S]*?\n\}/.exec(pageSource)?.[0] ?? "";
    expect(reset).toContain("exitBatchMode()");
    // ⛔ 但改筛选/翻页**不能**退批量：常见动作是"换一批范围继续勾"。
    const query = /function query\(\) \{[\s\S]*?\n\}/.exec(pageSource)?.[0] ?? "";
    expect(query).toContain("clearSelection()");
    expect(query).not.toContain("batchMode.value = false");
  });

  it("distinguishes an over-narrow filter from an empty library", () => {
    // ⛔ 不区分的话"没搜到"和"库里本来就没图"是同一句话，
    // 用户会去反复改筛选条件，而真正的问题在部署侧（根本没抓拍过）。
    expect(pageSource).toContain("emptyDescription");
    expect(pageSource).toContain(':description="emptyDescription"');
    expect(pageSource).toContain("图像库还是空的");
  });
});

describe("cross-boundary contracts", () => {
  // 这两条断言跨到 Go 侧读源码：前端菜单行/接口路径与后端常量是**同一份契约的两端**，
  // 分叉的表现都非常隐蔽（菜单点开空白 / 接口通但恒 403），且都不报错。
  const modelsPath = resolve(process.cwd(), "../server/app/gb28181/models/gb_channel_snapshot.go");
  const migrationPath = resolve(
    process.cwd(),
    "../server/resource/database/gb28181/migrations/2026-09-20-channel-snapshot-library.sql"
  );

  it("requests the path the backend actually registers", () => {
    if (!existsSync(modelsPath)) return; // 只有前端目录时跳过（独立打包场景）
    const models = readFileSync(modelsPath, "utf8");
    expect(models).toContain('const SnapshotListRoutePath = "/snapshots"');
    expect(apiSource).toContain('baseUrlApi("gb28181/device-mgmt/snapshots")');
  });

  it("ships the component path the menu row points at", () => {
    if (!existsSync(migrationPath)) return;
    const migration = readFileSync(migrationPath, "utf8");
    const component = /'gb28181\/snapshot-library\/index'/.exec(migration);
    expect(component, "菜单行里的 component 不见了").not.toBeNull();
    // component 是前端组件路径契约（route-output.ts 用 import.meta.glob 逐字比对）；
    // 页面文件不在这个位置时，菜单在库里、点开是空白。
    const expected = resolve(process.cwd(), "src/views", `${component![0].replace(/'/g, "")}.vue`);
    expect(existsSync(expected), `菜单指向的组件文件不存在: ${expected}`).toBe(true);
  });

  it("deep-links to the same route the menu row registers", () => {
    if (!existsSync(migrationPath)) return;
    // 抓拍面板跳图像库用的是这个常量。它和菜单 path 分叉的表现是"点了按钮跳到 404"，
    // 而前端探测路由是否存在也看这个常量 —— 值写歪了门禁就形同虚设（恒不 ready 或恒 ready）。
    expect(SNAPSHOT_LIBRARY_PATH).toBe("/gb28181/snapshot-library");
    expect(readFileSync(migrationPath, "utf8")).toContain(`'${SNAPSHOT_LIBRARY_PATH}'`);
  });
});
