import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { hasRuleBlock } from "@/test/source-assert";

/**
 * 按钮体系门禁（2026-11-04 建立）。
 *
 * 起因：全仓 549 个 `a-button` 里，尺寸散成 6 种高度（Arco 32 / 项目 34 / 页头 40 /
 * small 28 / mini 24 / popconfirm 32），配色有 4 份互相覆盖的副本（列表工具栏、系统弹窗、
 * 弹窗 body、系统表单），于是同一种「次级按钮」在三个容器里长成三种样子；
 * 而其中约 370 个默认按钮是「白底 + 灰字」，整屏只有一两个主按钮有颜色。
 *
 * 收敛口径：`src/styles/uvp-buttons.scss` 是**唯一真源**，三档尺寸 + 六级语义。
 * 这组用例锁住「真源完整」「加载顺序」「别处不许复活第二套」三件事。
 * 规范正文见 docs/frontend-button-system.md。
 */

const ROOT = process.cwd();
const SRC = resolve(ROOT, "src");
const BUTTONS = readFileSync(resolve(SRC, "styles/uvp-buttons.scss"), "utf8");
const TOKENS = readFileSync(resolve(SRC, "style/var/uvp-ui-tokens.scss"), "utf8");
const OVERRIDES = readFileSync(resolve(SRC, "styles/arco-overrides.scss"), "utf8");
const UI_LANGUAGE = readFileSync(resolve(SRC, "style/model/uvp-ui-language.scss"), "utf8");
const MAIN = readFileSync(resolve(SRC, "main.ts"), "utf8");

const PRUNE = new Set(["node_modules", "dist", "coverage", ".vite"]);
function walk(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    if (PRUNE.has(entry)) continue;
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) walk(full, out);
    else if (entry.endsWith(".vue")) out.push(full);
  }
  return out;
}
const VUE_FILES = walk(SRC);

/** 剥注释：说明性文字里会出现 `size="mini"` 这种举例，不能算违规。 */
function stripComments(source: string) {
  const blank = (block: string) => block.replace(/[^\n]/g, " ");
  return source
    .replace(/<!--[\s\S]*?-->/g, blank)
    .replace(/\/\*[\s\S]*?\*\//g, blank)
    .replace(/(^|\s)\/\/[^\n]*/g, "$1");
}

/**
 * 原生 `<button>` 的语义类几何入口（第 8 节）。
 * 它必须排除两类「不是文字按钮」的东西：
 *   ① `.uvp-page-action-btn` —— 40px 页头档，不排除会被压回 34px；
 *   ② 图标键三族 —— 26/28px 方形定尺，只有图标没有文案。
 *      ⛔ 2026-11-04 踩过：设备卡片那排圆钮里的「刷新通道目录」挂着 uvp-refresh-btn，
 *      被 `min-width: 72px` 撑成 72×28 胶囊（高度被页面 0,3,0 拉回 28，只有横向变形）。
 */
const ICON_KEY_CLASSES = [".icon-btn", ".icon-button", ".tree-icon-button"];
const NATIVE_SEMANTIC_CLASSES = ["uvp-refresh-btn", "uvp-export-btn", "uvp-select-btn", "uvp-clear-btn", "uvp-sky-btn"];

/** 取出 `button.X:where(:not(...))` 括号里的排除列表。 */
function nativeExclusionList(cls: string) {
  return BUTTONS.match(new RegExp(`button\\.${cls}:where\\(:not\\(([^)]*)\\)\\)`))?.[1] ?? "";
}

function expectNativeSemanticEntry(cls: string) {
  const list = nativeExclusionList(cls);
  expect(list, `button.${cls} 缺原生 <button> 的几何入口`).not.toBe("");
  for (const must of [".uvp-page-action-btn", ...ICON_KEY_CLASSES]) {
    expect(list, `button.${cls} 的排除列表漏了 ${must}`).toContain(must);
  }
}

describe("按钮尺寸只有三档", () => {
  it("token 定义三档高度，且与真源的用法一致", () => {
    // ⛔ 改这些数字等于全站按钮尺寸变更，必须同时改文档与所有档位类；
    //    这里钉死是为了防止「顺手调成 36」这种漂移。
    expect(TOKENS).toMatch(/--uvp-btn-h-lg:\s*40px;/);
    expect(TOKENS).toMatch(/--uvp-btn-h-md:\s*34px;/);
    expect(TOKENS).toMatch(/--uvp-btn-h-sm:\s*28px;/);
    expect(TOKENS).not.toMatch(/--uvp-btn-h-xs/);

    expect(hasRuleBlock(BUTTONS, ".arco-btn", "height: var(--uvp-btn-h-md)")).toBe(true);
    expect(hasRuleBlock(BUTTONS, ".arco-btn-size-large", "height: var(--uvp-btn-h-lg)")).toBe(true);
    expect(hasRuleBlock(BUTTONS, ".arco-btn-size-small", "height: var(--uvp-btn-h-sm)")).toBe(true);
  });

  it("Arco 的 mini 归并到 sm 档，不再有自己的高度", () => {
    // mini 与 small 现在同高；两个选择器必须写在同一个规则块里，否则会漂成两档。
    const mini = BUTTONS.match(/\.arco-btn-size-small,\s*\n\.arco-btn-size-mini\s*\{([\s\S]*?)\}/);
    expect(mini, "找不到 small/mini 的合并规则").not.toBeNull();
    expect(mini?.[1]).toContain("height: var(--uvp-btn-h-sm)");
  });

  it('全仓模板里没有 a-button 使用 size="mini"', () => {
    const offenders: string[] = [];
    for (const file of VUE_FILES) {
      const raw = readFileSync(file, "utf8");
      // 廉价预筛：没有这个字面量的文件不必跑剥注释（全仓扫描原本顶到 5s 默认超时）。
      if (!raw.includes('size="mini"')) continue;
      const source = stripComments(raw);
      for (const match of source.matchAll(/<a-button\b[^>]*>/g)) {
        if (match[0].includes('size="mini"')) {
          offenders.push(`${relative(ROOT, file)}:${source.slice(0, match.index).split("\n").length}`);
        }
      }
    }
    expect(offenders.join("\n")).toBe("");
  }, 20000);
});

describe("语义色只有六级，且都从 token 取值", () => {
  it("主按钮 / 次级 / 中性 / 危险 / 停止 / 新增 各有且仅有一个出处", () => {
    expect(hasRuleBlock(BUTTONS, ".arco-btn-primary", "background: var(--uvp-btn-primary-bg)")).toBe(true);
    expect(hasRuleBlock(BUTTONS, ".arco-btn-secondary", "background: var(--uvp-btn-secondary-bg)")).toBe(true);
    expect(hasRuleBlock(BUTTONS, ".arco-modal-footer .arco-btn-secondary", "background: var(--uvp-btn-neutral-bg)")).toBe(true);
    expect(hasRuleBlock(BUTTONS, ".arco-btn-status-danger", "background: var(--uvp-btn-danger-soft-bg)")).toBe(true);
    expect(hasRuleBlock(BUTTONS, ".arco-btn-primary.arco-btn-status-danger", "background: var(--uvp-btn-danger-solid-bg)")).toBe(
      true
    );
    expect(hasRuleBlock(BUTTONS, ".uvp-stop-btn", "background: var(--uvp-btn-stop-bg)")).toBe(true);
    // 新增走绿色，定义在 arco-overrides（含按钮文字与阴影的 !important 收敛）。
    expect(hasRuleBlock(OVERRIDES, ".uvp-create-btn", "background: var(--uvp-btn-success-bg) !important")).toBe(true);
  });

  it("次要按钮不再是纯白底（这是「很多按钮没上色」的直接修法）", () => {
    // 反向断言：真源里不允许再出现 `background: #ffffff`。
    expect(BUTTONS).not.toMatch(/background:\s*#fff(?:fff)?\s*;/i);
  });

  it("彩色语义的底色必须真的看得出颜色（不能又调回接近白）", () => {
    // ⛔⛔ 2026-11-04 二次返工的原因：#f2f7ff 在白卡片上肉眼和白色没差别，
    //    老板的原话是「所有的按钮我都想上色」。只把「白」换成「浅到看不见」等于没改。
    // 判据两条一起用：
    //   ① chroma = max(R,G,B) - min(R,G,B) ≥ 20 —— 肉眼能分辨出色相；
    //   ② luma   = 相对亮度 ≤ 0.94 —— 比纯白明显压暗，白底上能看出一块色。
    const pick = (name: string) => TOKENS.match(new RegExp(`${name}:\\s*#([0-9a-f]{6})`, "i"))?.[1];
    const analyze = (hex: string) => {
      const [r, g, b] = [0, 2, 4].map(i => Number.parseInt(hex.slice(i, i + 2), 16));
      const srgb = [r, g, b].map(v => {
        const c = v / 255;
        return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
      });
      return {
        chroma: Math.max(r, g, b) - Math.min(r, g, b),
        luma: 0.2126 * srgb[0] + 0.7152 * srgb[1] + 0.0722 * srgb[2]
      };
    };

    for (const name of ["--uvp-btn-secondary-bg", "--uvp-refresh-btn-bg"]) {
      const hex = pick(name);
      expect(hex, `${name} 没找到十六进制值`).toBeTruthy();
      const { chroma, luma } = analyze(hex!);
      expect(chroma, `${name} 的色相太弱（chroma=${chroma}），看起来仍是白按钮`).toBeGreaterThanOrEqual(20);
      expect(luma, `${name} 太亮（luma=${luma.toFixed(3)}），白底上分辨不出来`).toBeLessThanOrEqual(0.94);
    }
  });

  it("每次改主题都要给全：亮色定义的按钮 token，暗色必须也有", () => {
    const tokensOf = (block: string) => new Set(block.match(/--uvp-btn-[a-z0-9-]+(?=\s*:)/g) ?? []);
    const lightStart = TOKENS.indexOf(":root {");
    const darkStart = TOKENS.indexOf('body[arco-theme="dark"] {');
    const light = tokensOf(TOKENS.slice(lightStart, darkStart));
    const dark = tokensOf(TOKENS.slice(darkStart));
    expect(light.size).toBeGreaterThan(40);
    expect([...light].filter(name => !dark.has(name)).join(", ")).toBe("");
  });
});

describe("单一真源不许被第二套规则覆盖", () => {
  it("main.ts 把按钮真源排在 Arco 主题之后（同特异性靠顺序取胜）", () => {
    const arco = MAIN.indexOf("@arco-themes/vue-gi-demo/css/arco.css");
    const buttons = MAIN.indexOf("@/styles/uvp-buttons.scss");
    const overrides = MAIN.indexOf("@/styles/arco-overrides.scss");
    expect(arco).toBeGreaterThan(-1);
    expect(buttons).toBeGreaterThan(arco);
    // overrides 是最后一个：页面级视觉补丁要在真源之后
    expect(overrides).toBeGreaterThan(buttons);
  });

  it("uvp-ui-language.scss 里不再定义按钮的几何与配色", () => {
    // 允许的只剩布局类（margin/gap）。⛔ 唯一的例外是输入框尾部那个 28×28 的内嵌图标按钮
    //   —— 它是输入框的一部分，尺寸由输入框决定，不能吃 34px 档。
    const EMBEDDED = ".arco-input-suffix";
    // ⛔ 必须剥注释：块注释里正写着「按钮的尺寸/配色已移出本文件」，不剥会被当成选择器串。
    const buttonSelectors = [...stripComments(UI_LANGUAGE).matchAll(/[^{}]*\.arco-btn[^{}]*\{([^}]*)\}/g)];
    const offenders = buttonSelectors
      .filter(match => {
        if (match[0].includes(EMBEDDED)) return false;
        // height 兜底：值等于 sm 档（28px）视为合规，避免把紧凑按钮误判成自造尺寸。
        const body = match[1].replace(/(min-)?height:\s*28px;/g, "");
        return /(^|;)\s*(height|min-height|padding|padding-inline|background|background-color|border-radius|font-size|color)\s*:/.test(
          body
        );
      })
      .map(match => match[0].slice(0, 60).replace(/\s+/g, " "));
    expect(offenders.join("\n")).toBe("");
  });

  it("arco-overrides.scss 里弹窗取消键不再自带底色（否则双源打架）", () => {
    expect(OVERRIDES).not.toMatch(/\.arco-modal-footer \.arco-btn-secondary[^{]*\{[^}]*background:/s);
    expect(OVERRIDES).not.toMatch(/\.arco-drawer-footer \.arco-btn-secondary[^{]*\{[^}]*background:/s);
  });

  it("uvp-page-action-btn 只覆盖档位，不再写裸 px", () => {
    const rule = OVERRIDES.match(/\.uvp-page-action-btn\s*\{([\s\S]*?)\}/)?.[1] ?? "";
    expect(rule).toContain("height: var(--uvp-btn-h-lg)");
    expect(rule).toContain("border-radius: var(--uvp-btn-radius)");
    expect(rule).not.toMatch(/height:\s*\d+px/);
    expect(rule).not.toMatch(/border-radius:\s*\d+px/);
  });

  it("刷新语义只定义在真源，arco-overrides 不留第二份", () => {
    // 2026-11-04 从 arco-overrides 迁入：那边是 `!important` 硬压，与「语义从一处出」冲突。
    expect(hasRuleBlock(BUTTONS, ".uvp-refresh-btn", "background: var(--uvp-refresh-btn-bg)")).toBe(true);
    expect(OVERRIDES).not.toMatch(/\.uvp-refresh-btn\s*\{/);
  });

  it("取数语义（下载 / 导出）与刷新共用青绿，且不抢主按钮的蓝", () => {
    // 老板 2026-11-04 定：下载/导出 = 「只把数据取走、一个字节都不改」，
    // 与「刷新」同一心智 ⇒ 同一色相（青绿），但类名分开命名，将来要分深浅只改一处。
    expect(hasRuleBlock(BUTTONS, ".uvp-export-btn", "background: var(--uvp-refresh-btn-bg)")).toBe(true);
    // 三条彩色规则都必须排除它，否则会被染成浅蓝/蓝实心。
    for (const block of [".arco-btn-primary", ".arco-btn-secondary", ".arco-btn-outline"]) {
      const rule = BUTTONS.match(new RegExp(`\\${block}:where\\(([\\s\\S]*?)\\)`))?.[1] ?? "";
      expect(rule, `${block} 的排除列表漏了 .uvp-export-btn`).toContain(".uvp-export-btn");
    }
    // 原生 <button> 也要能用同一套语义（几何 + 配色）。
    expectNativeSemanticEntry("uvp-export-btn");
  });

  it("选择 / 清空 / 全选三个新色相各自独立，且都已并进排除列表", () => {
    // 2026-11-04 老板：「我想让每个按钮颜色都不一样」。
    // 口径 = 一个动作族一个色相，族与族之间必须**跨色相**，不许同色深浅凑数。
    const tones: Array<[cls: string, prefix: string]> = [
      [".uvp-select-btn", "select"],
      [".uvp-clear-btn", "clear"],
      [".uvp-sky-btn", "sky"]
    ];
    for (const [cls, prefix] of tones) {
      expect(hasRuleBlock(BUTTONS, cls, `background: var(--uvp-btn-${prefix}-bg)`), `${cls} 没走 --uvp-btn-${prefix}-bg`).toBe(
        true
      );
      expect(hasRuleBlock(BUTTONS, cls, `color: var(--uvp-btn-${prefix}-text)`), `${cls} 没走 --uvp-btn-${prefix}-text`).toBe(
        true
      );
      // 三条彩色规则（primary / secondary / outline）都要排除它，否则
      // `<a-button class="…">` 会先被次级浅蓝染一遍，再靠书写顺序赌胜负。
      for (const block of [".arco-btn-primary", ".arco-btn-secondary", ".arco-btn-outline"]) {
        const rule = BUTTONS.match(new RegExp(`\\${block}:where\\(([\\s\\S]*?)\\)`))?.[1] ?? "";
        expect(rule, `${block} 的排除列表漏了 ${cls}`).toContain(cls);
      }
      // 原生 <button> 也必须能用同一套语义（几何 + 配色），且不压 40px 档、不撑图标键。
      const name = cls.slice(1);
      expectNativeSemanticEntry(name);
      // 暗色段必须跟着定义：只给浅色值 ⇒ 暗色下是一块发光的浅斑。
      const definedTimes = TOKENS.match(new RegExp(`--uvp-btn-${prefix}-bg:`, "g"))?.length ?? 0;
      expect(definedTimes, `--uvp-btn-${prefix}-bg 只在浅色段定义，暗色没跟上`).toBeGreaterThanOrEqual(2);
    }
    // ⛔ 三档底色必须是三个不同的值：抄成同一个就等于「又是同色」，白改一场。
    const values = ["select", "clear", "sky"].map(name =>
      TOKENS.match(new RegExp(`--uvp-btn-${name}-bg:\\s*([^;]+);`))?.[1].trim()
    );
    expect(new Set(values).size).toBe(3);
  });

  it("原生 <button> 也有语义类入口（全仓 240+ 个原生按钮的唯一入口）", () => {
    // ⛔ 仪表盘页头的刷新之前就是原生 button + 本地 `.secondary`（白底黑字），
    //    上一轮统一按钮体系完全没管到它 —— 因为那套规则只作用于 `.arco-btn`。
    for (const cls of ["uvp-btn-primary", "uvp-btn-secondary", "uvp-btn-neutral"]) {
      expect(BUTTONS).toMatch(new RegExp(`^button\\.${cls}\\s*\\{`, "m"));
    }
    // 刷新类要能作用于原生按钮，同时必须排除 40px 档的 page-action（否则被压回 34px）
    // 与图标键三族（否则 28px 圆钮被 min-width 撑成胶囊）。
    expectNativeSemanticEntry("uvp-refresh-btn");
  });

  it("图标键挂语义类时只借配色：排除列表必须覆盖全仓的图标键类名", () => {
    // 反向防回归：全仓 `<button class="…icon… uvp-xxx-btn">` 的图标类名，
    // 必须都在第 8 节的 `:not()` 列表里 —— 否则那个图标键会横向变宽。
    // 新增一种图标键样式时，这条会直接报出「把 .foo 补进排除列表」，不用等肉眼发现。
    // ⚠️ 先做廉价的 raw 预筛再 stripComments：全仓 250+ 个 .vue、index.vue 单文件 330KB，
    //    每个都跑三遍正则剥注释会顶到 5s 默认超时（实测过一次超时）。
    const seen = new Set<string>();
    for (const file of VUE_FILES) {
      const raw = readFileSync(file, "utf8");
      if (!/<button\b[^>]*class="[^"]*icon/i.test(raw)) continue;
      for (const match of stripComments(raw).matchAll(/<button\b[^>]*\bclass="([^"]*)"[^>]*>/g)) {
        const classes = match[1].split(/\s+/).filter(Boolean);
        if (!classes.some(c => NATIVE_SEMANTIC_CLASSES.includes(c))) continue;
        for (const cls of classes.filter(c => /icon/i.test(c))) seen.add(`.${cls}`);
      }
    }
    const missing = [...seen].filter(cls => !ICON_KEY_CLASSES.includes(cls));
    expect(missing.join(", "), "这些图标键类名没进 uvp-buttons.scss 第 8 节的排除列表").toBe("");
    expect(seen.size, "一个图标键都没扫到，扫描规则可能失效了").toBeGreaterThan(0);
  }, 20000);
});

describe("语义变体不被主按钮规则误伤", () => {
  // ⛔ 排除条件必须用 `:where()`：`:not()` 的特异性会累加，
  //    连写几个就把主按钮规则抬到压过 footer 的层次，取消键会被染成蓝色。
  it("主按钮规则用 :where() 排除语义变体", () => {
    const block = BUTTONS.match(/\.arco-btn-primary:where\(([\s\S]*?)\)\s*\{/)?.[1] ?? "";
    expect(block).toContain(".uvp-create-btn");
    expect(block).toContain(".uvp-refresh-btn");
    expect(block).toContain(".uvp-stop-btn");
    expect(block).toContain(".arco-btn-status-danger");
    expect(BUTTONS).not.toMatch(/\.arco-btn-primary:not\([^)]*\):not\([^)]*\):not\(/);
  });
});
