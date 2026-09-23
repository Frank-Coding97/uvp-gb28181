import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * 全仓 `--uvp-*` design token 契约。
 *
 * 起因（2026-09-20）：存储卡进度条的**底轨**写成 `background: var(--uvp-border)` ——
 * 而 `--uvp-border` 这个 token **全仓从未定义过**。`background` 引用一个未定义的变量，
 * 会在**计算值阶段**整条声明失效并回退成 `initial = transparent` ⇒ 底轨跟卡片底色一样白
 * ⇒ 整条进度条只剩一段青色，"还剩多少空间"完全看不出来。
 *
 * 为什么烂了这么久没人发现：
 *   1. **没有类型**。CSS 自定义属性没有类型检查，`var(--uvp-typo)` 和 `var(--uvp-border)`
 *      在编译期长得一模一样；`vue-tsc`、`eslint`、任何单测都抓不到。
 *   2. **写不写兜底值决定生死**。`var(--x, #fff)` 正常，`var(--x)` 才是坏的
 *      —— 于是**同一个 token 有的地方正常、有的地方隐形**，看起来像"某个组件样式被覆盖"。
 *   3. **不同属性失效后果不同**，肉眼归因极难：
 *        · `background` 失效 ⇒ 回退 `transparent` ⇒ 整块背景隐形（症状最重）
 *        · `border` 失效     ⇒ 回退 `currentColor` ⇒ 线还在但跟文字同色，看起来"差不多" ⇒ 没人报
 *        · `font-family` 失效 ⇒ 该属性被丢弃 ⇒ 等宽字体静默退化成继承字体
 *      同一处 token 在 `background` 上炸得惊天动地、在 `border` 上安安静静。
 *
 * 所以只能靠这个契约测试兜底。当前口径（2026-09-20 收敛后）应为 0 违规；
 * 这组用例的作用是**让它不能再涨回去**。
 */

const ROOT = process.cwd(); // vitest 的工作目录 = web/
const SRC_DIR = resolve(ROOT, "src");
const TOKENS_FILE = resolve(SRC_DIR, "style/var/uvp-ui-tokens.scss");

const SCAN_EXTENSIONS = [".vue", ".scss", ".css", ".ts", ".js", ".html"];
const PRUNE_DIRS = new Set(["node_modules", "dist", "coverage", ".vite"]);

/**
 * 「基础层」通用语义 token：组件里大量引用、但真源里原本一个都没定义的那批。
 * 钉住它们，防止有人把真源里"看起来没人用"的定义当冗余删掉
 * （`--uvp-success-soft` / `--uvp-font-mono` 的引用**全带兜底值**，
 *  删掉定义不会有任何症状，普通检查抓不到）。
 */
const BASE_LAYER_TOKENS = [
  "--uvp-primary",
  "--uvp-success",
  "--uvp-success-soft",
  "--uvp-border",
  "--uvp-border-subtle",
  "--uvp-bg",
  "--uvp-bg-secondary",
  "--uvp-bg-tertiary",
  "--uvp-page-bg",
  "--uvp-surface-muted",
  "--uvp-text-disabled",
  "--uvp-font-mono"
];

/**
 * 剥掉注释：说明文字里会提到 token 名，不该被算成引用或定义。
 * ⛔ 用**等长空白**替换而不是删掉 —— 否则后面的行号全部前移，报错指到错误的行。
 */
function stripComments(source: string) {
  const blank = (block: string) => block.replace(/[^\n]/g, " ");
  return source.replace(/\/\*[\s\S]*?\*\//g, blank).replace(/<!--[\s\S]*?-->/g, blank);
}

function walk(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    if (PRUNE_DIRS.has(entry)) continue;
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) walk(full, out);
    else if (SCAN_EXTENSIONS.some(ext => entry.endsWith(ext))) out.push(full);
  }
  return out;
}

const SOURCE_FILES = walk(SRC_DIR);

function read(file: string) {
  return stripComments(readFileSync(file, "utf8"));
}

const DEFINITION_PATTERN = /(?:^|[\s;{>,(["'])(--uvp-[a-z0-9-]+)\s*:/gm;
const REFERENCE_PATTERN = /var\(\s*(--uvp-[a-z0-9-]+)\s*(,)?/g;

/** 全仓定义过的 --uvp-* 名字（含 JS 对象字面量写法；排除 *.test.ts 以免测试字符串把真名污染成"已定义"）。 */
function definedTokens() {
  const names = new Set<string>();
  for (const file of SOURCE_FILES) {
    if (file.endsWith(".test.ts") || file.endsWith(".test.js")) continue;
    for (const match of read(file).matchAll(DEFINITION_PATTERN)) names.add(match[1]);
  }
  return names;
}

/** 找出「没有兜底值、且真源里也没定义」的引用 —— 即真正会失效的那些。 */
function unresolvedReferences(files: string[], defined: Set<string>) {
  const offenders: { token: string; where: string }[] = [];
  for (const file of files) {
    const lines = read(file).split("\n");
    lines.forEach((line, index) => {
      for (const match of line.matchAll(REFERENCE_PATTERN)) {
        if (!match[2] && !defined.has(match[1])) {
          offenders.push({ token: match[1], where: `${relative(ROOT, file)}:${index + 1}` });
        }
      }
    });
  }
  return offenders;
}

/** 把 token 真源切成「亮色段(:root)」与「暗色段(body[arco-theme=dark])」。 */
function tokenScopes() {
  const source = read(TOKENS_FILE);
  const darkAt = source.indexOf('body[arco-theme="dark"]');
  expect(darkAt, "找不到暗色块：真源结构变了，请同步更新本测试").toBeGreaterThan(-1);
  const declarations = (block: string) =>
    new Map([...block.matchAll(/(--uvp-[a-z0-9-]+)\s*:\s*([^;]+);/g)].map(m => [m[1], m[2].trim()]));
  return { light: declarations(source.slice(0, darkAt)), dark: declarations(source.slice(darkAt)) };
}

function formatOffenders(offenders: { token: string; where: string }[]) {
  return offenders.length ? offenders.map(o => `${o.token} @ ${o.where}`).join("\n") : "（无）";
}

describe("design token 契约：不引用未定义的 --uvp-*", () => {
  const defined = definedTokens();

  it("真源确实读到了（防止路径或正则写错让整组用例空转）", () => {
    expect(defined.has("--uvp-panel-border")).toBe(true);
    expect(defined.has("--uvp-brand")).toBe(true);
    expect(defined.size).toBeGreaterThan(80);
  });

  it("全仓没有「无兜底 + 未定义」的 var(--uvp-*) 引用", () => {
    const offenders = unresolvedReferences(SOURCE_FILES, defined);
    expect(formatOffenders(offenders)).toBe("（无）");
  });

  it("基础层通用 token 全部有定义（它们被大量引用，缺了就成片隐形）", () => {
    const missing = BASE_LAYER_TOKENS.filter(token => !defined.has(token));
    expect(missing.join(", ")).toBe("");
  });
});

describe("design token 契约：别名必须跟随暗色主题", () => {
  /**
   * ⛔ 这是本仓最阴的一个坑，已实测确认：
   *   `:root { --a: #111; --alias: var(--a); }` + `body[arco-theme="dark"] { --a: #999; }`
   *   ⇒ 暗色下 `--alias` 仍然是 **#111**。
   *   原因：自定义属性里含的 var() 在**计算值阶段**就被替换成字面量，替换用的是
   *   **声明所在元素**（:root）上的值，替换结果再往下继承；暗色只覆盖被引用的 `--a`，
   *   **不会回头重算** `--alias`。
   *   ⇒ 凡是值里含 var() 的别名，必须在暗色块里**再写一遍**。
   *   （`--uvp-shell-bg` / `--uvp-sidebar-bg` 之所以在两块里都出现，就是这个原因。）
   */
  it("亮色段里值含 var() 的别名，在暗色段必须重复声明", () => {
    const { light, dark } = tokenScopes();
    const aliases = [...light.entries()].filter(([, value]) => value.includes("var(")).map(([name]) => name);
    expect(aliases.length, "一个别名都没有？检查真源是否被改动").toBeGreaterThan(0);
    const pinnedToLight = aliases.filter(name => !dark.has(name));
    expect(pinnedToLight.join(", ")).toBe("");
  });

  it("别名指向的 token 必须真的存在（别名链不能悬空）", () => {
    const defined = definedTokens();
    const { light, dark } = tokenScopes();
    const dangling: string[] = [];
    for (const [name, value] of [...light, ...dark]) {
      for (const match of value.matchAll(/var\(\s*(--uvp-[a-z0-9-]+)/g)) {
        if (!defined.has(match[1])) dangling.push(`${name} → ${match[1]}`);
      }
    }
    expect(dangling.join(", ")).toBe("");
  });
});
