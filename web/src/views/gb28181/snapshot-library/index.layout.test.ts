import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { parse } from "vue/compiler-sfc";

import { SNAPSHOT_LIBRARY_PAGE_SIZE_OPTIONS, SNAPSHOT_LIBRARY_PATH } from "./snapshotLibraryState";

const pageSource = readFileSync(resolve(process.cwd(), "src/views/gb28181/snapshot-library/index.vue"), "utf8");
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
