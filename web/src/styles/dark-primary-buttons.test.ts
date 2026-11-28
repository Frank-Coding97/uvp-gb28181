import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { compileString } from "sass-embedded";
import postcss from "postcss";
import { compileStyle, parse } from "vue/compiler-sfc";
import { afterEach, describe, expect, it } from "vitest";

const readSource = (path: string) => readFileSync(resolve(process.cwd(), "src", path), "utf8");
const css = postcss.parse(compileString(readSource("styles/arco-overrides.scss")).css);
const primaryRules: { selector: string; declarations: Record<string, string> }[] = [];
css.walkRules(rule => {
  if (!rule.selector.startsWith("body[arco-theme=dark]") || !rule.selector.includes(".arco-btn-primary")) return;
  const declarations: Record<string, string> = {};
  rule.walkDecls(decl => {
    declarations[decl.prop] = decl.value;
  });
  primaryRules.push({ selector: rule.selector, declarations });
});

function rulesFor(classes: string, disabled = false) {
  const button = document.createElement("button");
  button.className = classes;
  button.disabled = disabled;
  document.body.append(button);
  return primaryRules.filter(
    rule => !/:(hover|active|focus-visible)\b/.test(rule.selector) && button.matches(rule.selector.replace(/\s+/g, " "))
  );
}

afterEach(() => {
  document.body.removeAttribute("arco-theme");
  document.body.replaceChildren();
});

describe("dark primary button contract", () => {
  it("keeps the document background on the UVP shell color", () => {
    const bodyRule = css.nodes.find(node => node.type === "rule" && (node as postcss.Rule).selector === "body") as
      | postcss.Rule
      | undefined;
    expect(bodyRule).toBeDefined();
    expect(
      bodyRule?.nodes.some(
        node => node.type === "decl" && node.prop === "background" && node.value === "var(--uvp-navigation-bg)" && node.important
      )
    ).toBe(true);
  });

  it.each(["arco-btn arco-btn-primary", "btn-primary", "arco-btn arco-btn-primary btn-primary"])(
    "gives %s an opaque blue fill and white text without changing geometry",
    classes => {
      document.body.setAttribute("arco-theme", "dark");
      const rules = rulesFor(classes);
      expect(rules).toHaveLength(1);
      // ⛔ 这里断言的是 **token 名** 而不是字面色值：实心面的色由 `--uvp-solid-*` 统一承载
      //   （见 style/model/solid-surface-token.test.ts），暗色下解析为深蓝 #2563eb。
      expect(rules[0].declarations).toMatchObject({
        color: "var(--uvp-solid-text)",
        background: "var(--uvp-solid-bg)",
        "box-shadow": "none"
      });
      for (const property of ["height", "padding", "border-radius"]) {
        expect(rules[0].declarations).not.toHaveProperty(property);
      }
    }
  );

  it("distinguishes native disabled buttons and Arco disabled links from enabled actions", () => {
    document.body.setAttribute("arco-theme", "dark");
    for (const rules of [rulesFor("btn-primary", true), rulesFor("arco-btn arco-btn-primary arco-btn-disabled")]) {
      expect(rules).toHaveLength(1);
      expect(rules[0].declarations).toMatchObject({
        color: "var(--uvp-text-disabled)",
        background: "var(--uvp-dialog-control-bg)",
        opacity: "1"
      });
    }
  });

  it.each([
    "arco-btn-status-danger",
    "arco-btn-status-warning",
    "arco-btn-status-success",
    "uvp-create-btn",
    "uvp-refresh-btn",
    "btn-danger"
  ])("preserves the independent semantic color of %s", status => {
    document.body.setAttribute("arco-theme", "dark");
    expect(rulesFor(`arco-btn arco-btn-primary btn-primary ${status}`)).toHaveLength(0);
  });

  it("leaves light-mode styles untouched", () => {
    expect(rulesFor("arco-btn arco-btn-primary btn-primary")).toHaveLength(0);
  });

  it("keeps loading buttons out of hover and pressed rules and preserves keyboard focus", () => {
    const hover = primaryRules.find(rule => rule.selector.includes(":hover"));
    const pressed = primaryRules.find(rule => rule.selector.includes(":active"));
    const focus = primaryRules.find(rule => rule.selector.includes(":focus-visible"));
    expect(hover?.selector).toContain(":not(.arco-btn-loading)");
    expect(pressed?.selector).toContain(":not(.arco-btn-loading)");
    expect(focus?.declarations).toMatchObject({ outline: "2px solid var(--uvp-brand)", "outline-offset": "2px" });
  });

  it("compiles OSD dark switch styles onto the control without recoloring the whole page", () => {
    const path = "views/gb28181/device-mgmt/DeviceConfigOsdBlocks.vue";
    const { descriptor } = parse(readSource(path));
    const result = compileStyle({
      source: compileString(descriptor.styles[0].content).css,
      scoped: true,
      id: "data-v-osd",
      filename: path
    });
    expect(result.errors).toEqual([]);
    const rules = postcss
      .parse(result.code)
      .nodes.filter(node => node.type === "rule" && /^body\[arco-theme/.test(node.selector));
    expect(rules).toHaveLength(2);
    for (const rule of rules as postcss.Rule[]) {
      expect(rule.selector).toContain(".osd-switch");
      expect(rule.selector).toContain("[data-v-osd]");
    }
  });

  it.each([
    ["views/gb28181/components/play-console/PictureVideoParamCard.vue", "vpc-segment"],
    ["views/gb28181/device-mgmt/DeviceConfigDrawer.vue", "dcg-segment"]
  ])("compiles %s dark bitrate styles onto the scoped button, never onto the body", (path, segment) => {
    const { descriptor } = parse(readSource(path));
    const style = descriptor.styles[0];
    const result = compileStyle({
      source: style.lang === "scss" ? compileString(style.content).css : style.content,
      scoped: true,
      id: "data-v-bitrate",
      filename: path
    });
    expect(result.errors).toEqual([]);
    const rules = postcss
      .parse(result.code)
      .nodes.filter(node => node.type === "rule" && /^body\[arco-theme="?dark"?\]/.test(node.selector));
    expect(rules.length).toBeGreaterThan(0);
    for (const rule of rules as postcss.Rule[]) {
      expect(rule.selector.replaceAll('"', "")).toContain(`.${segment} button.arco-btn[type=button]`);
      expect(rule.selector).toContain("[data-v-bitrate]");
    }
    const rule = rules.find(node => (node as postcss.Rule).selector.includes("].is-on")) as postcss.Rule;
    expect(rule).toBeDefined();
    expect(rule.selector.replaceAll('"', "")).toContain(`.${segment} button.arco-btn[type=button].is-on`);
    expect(rule.selector).toContain("[data-v-bitrate]");
    expect(rule.selector).toContain(":not(:disabled)");
    expect(rule.nodes).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ prop: "color", value: "#ffffff" }),
        expect.objectContaining({ prop: "background", value: "#2563eb" })
      ])
    );
  });
});

/* ──────────────────────────────────────────────────────────────────────────────
 * 2026-10-15 补：两类「写了但静默失效」的样式门禁
 * ────────────────────────────────────────────────────────────────────────────── */

const SRC_DIR = resolve(process.cwd(), "src");
const PRUNE_DIRS = new Set(["node_modules", "dist", "coverage", ".vite"]);
const SCAN_EXTENSIONS = [".vue", ".scss", ".css"];

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

/** 剥注释：说明性文字里会提到这些写法，不能算违规。 */
function stripComments(source: string) {
  const blank = (block: string) => block.replace(/[^\n]/g, " ");
  return source
    .replace(/\/\*[\s\S]*?\*\//g, blank)
    .replace(/<!--[\s\S]*?-->/g, blank)
    .replace(/(^|\s)\/\/[^\n]*/g, "$1");
}

/** 只看 `<style>` 块内容（.vue）—— 模板/脚本里的 `:global(` 不参与 scoped 编译。 */
function styleBlocks(file: string, source: string) {
  if (!file.endsWith(".vue")) return [stripComments(source)];
  return [...source.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/g)].map(match => stripComments(match[1]));
}

/** `:global(` 的配对右括号下标（括号可嵌套，例如 `:global(.a:not(.b))`）。 */
function closingParen(source: string, openIndex: number) {
  let depth = 0;
  for (let i = openIndex; i < source.length; i += 1) {
    if (source[i] === "(") depth += 1;
    else if (source[i] === ")" && (depth -= 1) === 0) return i;
  }
  return -1;
}

/**
 * ⛔⛔ `:global(...)` 必须**独占整条选择器**。
 *   scoped 编译器对含 `:global()` 的选择器**只输出 `:global(...)` 里的内容**，
 *   前后两边的选择器一律静默丢弃（`vue/compiler-sfc` 的 compileStyle 逐形态实测）：
 *     `:global(body[arco-theme="dark"]) .a :deep(.b)` → `body[arco-theme="dark"] { … }`
 *     `:global(.a):hover`                            → `.a`（hover 没了）
 *     `.x:global(.a)`                                → `.a`（前缀没了）
 *   不报错、不警告，而且声明会挂到 `<body>` 上 ⇒ 「看着写了暗色适配、一个像素都不生效」。
 *   判据：`:global(` 前一个非空白字符必须是 `{ } ; ,`（或文件开头），
 *   配对 `)` 后一个非空白字符必须是 `{` 或 `,`。
 */
function truncatedGlobalSelectors(source: string) {
  const out: string[] = [];
  const pattern = /:global\(/g;
  let match: RegExpExecArray | null;
  while ((match = pattern.exec(source))) {
    const close = closingParen(source, match.index + ":global".length);
    if (close < 0) {
      out.push(":global( 括号没闭合");
      continue;
    }
    const before = source.slice(0, match.index).match(/\S(?=\s*$)/)?.[0] ?? "";
    const after = source.slice(close + 1).match(/^\s*(\S)/)?.[1] ?? "";
    if ((before !== "" && !/[{};,]/.test(before)) || (after !== "{" && after !== ",")) {
      out.push(`${source.slice(match.index, close + 1)} → 前[${before}] 后[${after}]`);
    }
    pattern.lastIndex = close + 1;
  }
  return out;
}

/** 编译某个 .vue 的 scoped style（scss 先过 sass）。 */
function compileScopedStyle(path: string, id: string) {
  const style = parse(readSource(path)).descriptor.styles[0];
  return compileStyle({
    source: style.lang === "scss" ? compileString(style.content).css : style.content,
    scoped: true,
    id,
    filename: path
  });
}

/**
 * 全仓扫描**只在模块加载时读一遍盘**：仓库里有 280+ 个样式文件，
 * 两条门禁各读一遍的话，跟几十个测试文件混跑时会被抢到顶到 5s 的用例超时（实测踩过）。
 */
const SCANNED_STYLE_BLOCKS = SOURCE_FILES.map(file => ({
  file: relative(process.cwd(), file),
  blocks: styleBlocks(file, readFileSync(file, "utf8"))
}));

/** 会被 scoped 编译器静默截断的 `:global()` 选择器。 */
const GLOBAL_OFFENDERS = SCANNED_STYLE_BLOCKS.flatMap(({ file, blocks }) =>
  blocks.flatMap(truncatedGlobalSelectors).map(hit => `${file} :: ${hit}`)
);

/** 把 Vue 的 prop 当 CSS 属性选择器写的样式（`[status=...]` 永远匹配不上）。 */
const STATUS_ATTR_OFFENDERS = SCANNED_STYLE_BLOCKS.filter(({ blocks }) => blocks.some(block => /\[status\s*=/.test(block))).map(
  ({ file }) => file
);

describe(":global() 的 scoped 编译契约", () => {
  it("全仓的 :global() 都独占整条选择器（否则会被 scoped 编译器静默截断）", () => {
    expect(GLOBAL_OFFENDERS.join("\n")).toBe("");
  });

  it.each([
    ["views/gb28181/device-mgmt/StorageCardStatusPanel.vue", ".storage-refresh-button"],
    ["views/gb28181/device-mgmt/DeviceStatusFactsPanel.vue", ".fact-refresh-button"]
  ])("把 %s 的暗色规则编译到目标按钮上，而不是把声明丢给 <body>", (path, target) => {
    const result = compileScopedStyle(path, "data-v-darkpanel");
    expect(result.errors).toEqual([]);
    const rules = postcss
      .parse(result.code)
      .nodes.filter((node): node is postcss.Rule => node.type === "rule" && /^body\[arco-theme/.test(node.selector));
    expect(rules.length).toBeGreaterThan(0);
    for (const rule of rules) {
      const selector = rule.selector.replace(/\s+/g, " ");
      // 坏形态的签名：选择器只剩 `body[arco-theme="dark"]`，声明白白挂在 <body> 上
      expect(selector).not.toBe('body[arco-theme="dark"]');
      // `:deep()` 留在 :global() 里会原样残留成非法选择器，永远不命中
      expect(selector).not.toContain(":deep(");
      // 每条暗色规则都必须**真的落到某个具体元素**上
      expect(selector).toMatch(/^body\[arco-theme="dark"\] \./);
    }
    expect(rules.some(rule => rule.selector.includes(target))).toBe(true);
  });
});

/*
 * 危险确认键（`Modal.confirm({ okButtonProps: { status: "danger" } })`）
 *
 * ⛔ 起因（2026-10-15）：全仓 20 多处「确认删除」弹窗的确认键一直是**品牌蓝**而不是红。
 *   `uvp-modal-simple.scss` 写成了属性选择器 `.arco-btn-primary[status="danger"]`，
 *   而 `status` 是 Arco Button 的 **prop**（进了 props、只往 class 上加
 *   `arco-btn-status-danger`），被 Vue 吃掉后元素上**不会留 `status` 属性**
 *   ⇒ 规则永不命中，且不报任何错。
 *   ⚠️ 单测只断言 `config.okButtonProps` 是**恒真**的 —— 「配置传对了、样式没生效」
 *   只有把编译后的选择器拿到真 DOM 上 `button.matches()` 才抓得到。
 */
const modalSimpleCss = postcss.parse(compileString(readSource("style/model/uvp-modal-simple.scss")).css);

/** `.arco-modal-simple` 底部按钮在给定主题下命中的**非伪类**规则（源码顺序）。 */
function footerButtonRules(classes: string, dark = false) {
  if (dark) document.body.setAttribute("arco-theme", "dark");
  const modal = document.createElement("div");
  modal.className = "arco-modal-simple";
  const footer = document.createElement("div");
  footer.className = "arco-modal-footer";
  const button = document.createElement("button");
  button.className = classes;
  footer.append(button);
  modal.append(footer);
  document.body.append(modal);
  return modalSimpleCss.nodes
    .filter(
      (node): node is postcss.Rule =>
        node.type === "rule" &&
        !/:(hover|active|focus-visible)/.test(node.selector) &&
        button.matches(node.selector.replace(/\s+/g, " "))
    )
    .map(rule => {
      const declarations: Record<string, string> = {};
      rule.walkDecls(decl => {
        declarations[decl.prop] = decl.value;
      });
      return { selector: rule.selector.replace(/\s+/g, " "), declarations };
    });
}

describe("危险确认键", () => {
  it("靠 arco-btn-status-danger 类名命中，亮色走 --uvp-danger", () => {
    const danger = footerButtonRules("arco-btn arco-btn-primary arco-btn-status-danger");
    // 最后一条 = 特异性最高的那条 = 级联赢家
    const winner = danger[danger.length - 1];
    expect(winner?.selector).toContain("arco-btn-status-danger");
    expect(winner?.declarations.background).toBe("var(--uvp-danger)");
    // 反面：不带危险语义的普通主按钮不能被染红
    const plain = footerButtonRules("arco-btn arco-btn-primary");
    expect(plain.some(rule => /--uvp-danger/.test(rule.declarations.background ?? ""))).toBe(false);
  });

  it("暗色换一档深红（--uvp-danger 暗色是浅红文字色，白字压上去只有 2.4:1）", () => {
    const danger = footerButtonRules("arco-btn arco-btn-primary arco-btn-status-danger", true);
    const winner = danger[danger.length - 1];
    expect(winner?.selector).toContain("arco-btn-status-danger");
    expect(winner?.declarations.background).toMatch(/^color-mix\(in srgb, var\(--uvp-danger\)/);
  });

  it("全仓样式里没有 [status=...] 属性选择器（status 是 prop，元素上没有该属性）", () => {
    expect(STATUS_ATTR_OFFENDERS.join("\n")).toBe("");
  });
});
