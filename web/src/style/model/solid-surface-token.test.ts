/**
 * 「实心面」色彩契约 —— 白字压在实心底色上的元素（选中态标签页、主按钮、徽标、
 * 播放头气泡……）必须走 `--uvp-solid-*` 这组 token，**不许直接用 `--uvp-brand` 当底色**。
 *
 * 起因（2026-10-06）：仪表盘「流媒体运行态」弹窗的标签页选中态，在暗色下白字压浅蓝
 * （`--uvp-brand` 暗色值 #60a5fa），对比度只有约 2:1，整个选中块"发白发糊"，
 * 在深色底上几乎糊成一片。
 *
 * 为什么必须立 token 而不是逐处打补丁：
 *   `--uvp-brand` 是**文字/图标色**，暗色下刻意调浅（否则文字在深底上看不见）；
 *   而实心面要的是**底色**，必须够深才能压住白字。同一个 token 承担两种语义，
 * 必然有一边不对 —— 所以拆成两个语义，各页面只准选对的那个。
 *
 * 这组用例的作用是让"随手抓 --uvp-brand 当底色"不能再涨回去。
 */
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { describe, expect, it } from "vitest";

const ROOT = process.cwd();
const SRC_DIR = resolve(ROOT, "src");
const TOKENS_FILE = resolve(SRC_DIR, "style/var/uvp-ui-tokens.scss");

const SCAN_EXTENSIONS = [".vue", ".scss", ".css"];
const PRUNE_DIRS = new Set(["node_modules", "dist", "coverage", ".vite"]);

function walk(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    if (PRUNE_DIRS.has(entry)) continue;
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) walk(full, out);
    else if (SCAN_EXTENSIONS.some(ext => entry.endsWith(ext))) out.push(full);
  }
  return out;
}

/** 剥掉注释，说明文字里会提到 token 名/属性名，不该被算成违规。 */
function stripComments(source: string) {
  const blank = (block: string) => block.replace(/[^\n]/g, " ");
  return source.replace(/\/\*[\s\S]*?\*\//g, blank).replace(/<!--[\s\S]*?-->/g, blank);
}

const SOURCE_FILES = walk(SRC_DIR);

const SOLID_TOKENS = [
  "--uvp-solid-bg",
  "--uvp-solid-hover-bg",
  "--uvp-solid-active-bg",
  "--uvp-solid-border",
  "--uvp-solid-text"
];

function tokenScopes() {
  const source = stripComments(readFileSync(TOKENS_FILE, "utf8"));
  const darkAt = source.indexOf('body[arco-theme="dark"]');
  expect(darkAt, "找不到暗色块：真源结构变了，请同步更新本测试").toBeGreaterThan(-1);
  const declarations = (block: string) =>
    new Map([...block.matchAll(/(--uvp-[a-z0-9-]+)\s*:\s*([^;]+);/g)].map(m => [m[1], m[2].trim()]));
  return { light: declarations(source.slice(0, darkAt)), dark: declarations(source.slice(darkAt)) };
}

/** 找出「白字压品牌浅色底」的规则块 —— 暗色下必然糊。 */
function whiteOnBrandOffenders() {
  const block = /([^{}]*)\{([^{}]*)\}/g;
  const offenders: string[] = [];
  for (const file of SOURCE_FILES) {
    const source = stripComments(readFileSync(file, "utf8"));
    for (const match of source.matchAll(block)) {
      const [, selector, body] = match;
      // `(?:^|[;\s{])` 前缀是为了不把 `border-color` / `background-color` 当成 `color`
      if (!/(?:^|[;\s{])color:\s*#fff(?:fff)?\b/i.test(body)) continue;
      // ⛔ 2026-10-18 补：`--zlm-brand-500/600` 是 `--uvp-brand(-strong)` 的别名
      //   （见 `style/model/uvp-ui-language.scss`），暗色下同样是**文字用的浅蓝**
      //   ⇒ 白字压上去一样糊。漏判这两个别名，等于给「白字压浅蓝」留了后门
      //   （节点管理「保存策略」按钮就是这么漏的）。
      if (!/background:\s*var\(--(?:uvp|zlm)-brand(?:-strong|-500|-600)?\)/.test(body)) continue;
      offenders.push(`${relative(ROOT, file)} :: ${selector.trim().split("\n").pop()?.trim()}`);
    }
  }
  return offenders;
}

describe("实心面 token 契约", () => {
  const { light, dark } = tokenScopes();

  it("真源同时在两套主题里定义了整组 solid token", () => {
    for (const token of SOLID_TOKENS) {
      expect(light.has(token), `${token} 亮色段缺失`).toBe(true);
      expect(dark.has(token), `${token} 暗色段缺失`).toBe(true);
    }
  });

  it("实心面在暗色下用深蓝实色，绝不能写成 var() 别名（别名会在计算值阶段钉死在亮色）", () => {
    for (const token of SOLID_TOKENS) {
      expect(dark.get(token), `${token} 暗色值不应含 var()`).not.toMatch(/var\(/);
    }
  });

  it("实心底色与品牌浅色是两套色：暗色下底色必须明显深于 --uvp-brand 才有对比度", () => {
    const brand = dark.get("--uvp-brand");
    const solid = dark.get("--uvp-solid-bg");
    expect(brand && solid && brand !== solid, "暗色下实心底色若等于 --uvp-brand，白字必然糊").toBe(true);
  });

  it("全仓没有「白字压 --uvp-brand 底色」的规则残留", () => {
    expect(whiteOnBrandOffenders().join("\n")).toBe("");
  });
});

describe("仪表盘运行态弹窗的标签页选中态", () => {
  it("走实心面 token，与主按钮同一套色", () => {
    const path = "views/home/components/drilldown/MediaRuntimeLedgerDialog.vue";
    const source = stripComments(readFileSync(resolve(SRC_DIR, path), "utf8"));
    const rule = source.match(/\.media-ledger-tabs button\.active\s*\{([^{}]*)\}/);
    expect(rule, "选中态规则不见了").toBeDefined();
    expect(rule?.[1]).toMatch(/color:\s*var\(--uvp-solid-text\)/);
    expect(rule?.[1]).toMatch(/background:\s*var\(--uvp-solid-bg\)/);
    expect(rule?.[1]).not.toMatch(/var\(--uvp-brand\)/);
  });
});

/*
 * 节点管理 → 调度策略面板的「保存策略」按钮。
 *
 * ⛔ 起因（2026-10-18）：按钮写成 `color: #ffffff; background: var(--zlm-brand-600)`。
 *   `--zlm-brand-600` 只是 `--uvp-brand-strong` 的别名，暗色下 = #93c5fd（文字用浅蓝），
 *   白字压上去对比度 1.6:1，按钮整块糊成一团浅色；而且它绕过了上面的全局扫描
 *   （那条扫描以前只认 `--uvp-brand`，不认 `--zlm-*` 别名）。
 *   ⚠️ hover 也必须盯：同文件里 `.panel-actions button:hover` 会把文字染成 brand-600，
 *   对实心按钮等于把字重新变浅 —— 必须被更高特异性的规则覆盖回白字。
 */
describe("节点管理 · 调度策略面板的保存按钮", () => {
  const source = () =>
    stripComments(readFileSync(resolve(SRC_DIR, "views/gb28181/zlm/workbench/scheduling/SchedulerStrategyPanel.vue"), "utf8"));

  it("静止态走实心面 token，不再把 --zlm-brand-600 当底色、不再硬编码白字", () => {
    const rule = source().match(/\.panel-actions\s+\.primary-button\s*\{([^{}]*)\}/);
    expect(rule, "保存按钮的主规则不见了").toBeDefined();
    expect(rule?.[1]).toMatch(/color:\s*var\(--uvp-solid-text\)/);
    expect(rule?.[1]).toMatch(/background:\s*var\(--uvp-solid-bg\)/);
    expect(rule?.[1]).not.toMatch(/--zlm-brand|#ffffff/);
  });

  it("hover 与按下态覆盖回白字和实心面色，不被通用 :hover 染成浅蓝", () => {
    const hover = source().match(/\.panel-actions\s+\.primary-button:hover[^{]*\{([^{}]*)\}/);
    expect(hover, "hover 规则不见了").toBeDefined();
    expect(hover?.[1]).toMatch(/color:\s*var\(--uvp-solid-text\)/);
    expect(hover?.[1]).toMatch(/background:\s*var\(--uvp-solid-hover-bg\)/);
    const active = source().match(/\.panel-actions\s+\.primary-button:active[^{]*\{([^{}]*)\}/);
    expect(active, "按下态规则不见了").toBeDefined();
    expect(active?.[1]).toMatch(/background:\s*var\(--uvp-solid-active-bg\)/);
  });
});
