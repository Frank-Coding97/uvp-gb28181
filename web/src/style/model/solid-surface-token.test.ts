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

/** 找出「白字压 --uvp-brand 底色」的规则块 —— 暗色下必然糊。 */
function whiteOnBrandOffenders() {
  const block = /([^{}]*)\{([^{}]*)\}/g;
  const offenders: string[] = [];
  for (const file of SOURCE_FILES) {
    const source = stripComments(readFileSync(file, "utf8"));
    for (const match of source.matchAll(block)) {
      const [, selector, body] = match;
      if (!/color:\s*#ffffff/i.test(body)) continue;
      if (!/background:\s*var\(--uvp-brand(-strong)?\)/.test(body)) continue;
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
