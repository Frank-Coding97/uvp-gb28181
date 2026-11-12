import { readFileSync, readdirSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * 按钮「唯一真源」全仓门禁（2026-11-04）
 *
 * 背景：同一类按钮在不同页面长成三种样子，根因不是没人调色，而是
 * **同一套配色在真源之外还有副本，且副本权重更高**。踩过两次：
 *
 *  1. `device-mgmt/index.vue` 的 `<style scoped>` 里自写 `.btn-ghost { background: 白 }`，
 *     编译后是 0,2,0，压过真源 `.uvp-refresh-btn` 的 0,1,0
 *     ⇒ 刷新键挂了类仍是白的，只有 hover 才变（hover 抬到 0,3,0 才赢）。
 *  2. `s-layout-search/index.vue` 里 `.uvp-search-panel__actions :deep(.arco-btn:not(.arco-btn-primary))`
 *     把「非主按钮」刷成白底（0,3,0），而它被 38 个列表页复用
 *     ⇒ 全站的「重置 / 导出」等次级按钮一律发白。
 *
 * 本文件锁的是**结构性约束**（谁有权决定按钮长什么样），不是具体色值；
 * 色值与档位由 `uvp-buttons.test.ts` 锁。
 * 顺序说明：CSS 同特异性下后加载者胜，而组件 scoped 样式总是晚于全局 CSS 注入，
 * 所以「页面里再写一份」必然赢 —— 只能从源头禁止，靠调特异性是调不完的。
 */

const root = resolve(process.cwd(), "src");

/** 体系语义类：只能在真源 `src/styles/uvp-buttons.scss` 里定义。 */
const SYSTEM_CLASSES = [
  "uvp-btn-primary",
  "uvp-btn-secondary",
  "uvp-btn-neutral",
  "uvp-refresh-btn",
  "uvp-create-btn",
  "uvp-stop-btn",
  "uvp-refresh-text"
];

/** 深色控制台自成一套深色皮肤（有自己的 token 族），允许保留自己的按钮配色。 */
const DARK_CONSOLE = ["components/play-console/", "components/PlayConsoleLinked.vue"];

interface Rule {
  file: string;
  line: number;
  selector: string;
  body: string;
}

function vueFiles(): string[] {
  const out: string[] = [];
  const walk = (dir: string) => {
    for (const entry of readDir(dir)) {
      const full = resolve(dir, entry.name);
      if (entry.isDirectory()) {
        if (entry.name === "node_modules" || entry.name === "__snapshots__") continue;
        walk(full);
        continue;
      }
      if (entry.name.endsWith(".vue")) out.push(full);
    }
  };
  walk(root);
  return out.sort();
}

function readDir(dir: string) {
  return readdirSync(dir, { withFileTypes: true });
}

/** 取出所有 `<style>` 块，并把 CSS 注释清掉（注释里会写类名，留着就是假阳性）。 */
function styleBlocks(source: string): { text: string; offset: number }[] {
  const blocks: { text: string; offset: number }[] = [];
  const styleRe = /<style[^>]*>([\s\S]*?)<\/style>/g;
  let match: RegExpExecArray | null;
  while ((match = styleRe.exec(source))) {
    const raw = match[1];
    // 用等长空白替换注释，保证偏移量还能对回原文件行号。
    const withoutComments = raw.replace(/\/\*[\s\S]*?\*\//g, comment => comment.replace(/[^\n]/g, " "));
    blocks.push({ text: withoutComments, offset: match.index + match[0].indexOf(raw) });
  }
  return blocks;
}

/** 去掉 `:not() / :where() / :is()` 的内容 —— 它们只是排除条件，不是「定义这个类」。 */
function stripConditions(selector: string): string {
  return selector.replace(/:(?:not|where|is)\([^()]*\)/g, "");
}

function scan(predicate: (selector: string, body: string) => boolean): Rule[] {
  const hits: Rule[] = [];
  for (const file of vueFiles()) {
    const source = readFileSync(file, "utf8");
    for (const block of styleBlocks(source)) {
      const ruleRe = /([^{}]+)\{([^{}]*)\}/g;
      let rule: RegExpExecArray | null;
      while ((rule = ruleRe.exec(block.text))) {
        const selector = rule[1].replace(/\s+/g, " ").trim();
        const body = rule[2];
        if (!predicate(selector, body)) continue;
        hits.push({
          file: file.slice(root.length + 1),
          line: source.slice(0, block.offset + rule.index).split("\n").length,
          selector,
          body: body.trim().replace(/\s+/g, " ").slice(0, 120)
        });
      }
    }
  }
  return hits;
}

function format(hits: Rule[]): string {
  return hits.map(h => `  ${h.file}:${h.line}  ${h.selector}\n      ${h.body}`).join("\n");
}

describe("按钮唯一真源（全仓）", () => {
  it("体系语义类只能定义在 src/styles/uvp-buttons.scss", () => {
    const hits = scan(selector => {
      const bare = stripConditions(selector);
      return SYSTEM_CLASSES.some(cls => new RegExp(`\\.${cls}\\b`).test(bare));
    });
    expect(hits, `这些 .vue 里在重新定义按钮语义类，会与真源打架：\n${format(hits)}`).toEqual([]);
  });

  it("页面里不许再写「非主按钮 = 白色底」这种兜底配色", () => {
    // 这条选择器曾经出现在共享搜索面板里，一改就波及 38 个列表页。
    const hits = scan((selector, body) => {
      const compact = selector.replace(/\s+/g, "");
      return compact.includes(".arco-btn") && compact.includes(":not(.arco-btn-primary)") && /(^|[\s;])background/.test(body);
    });
    expect(hits, `兜底配色会覆盖全部次级按钮，请改用真源的语义类：\n${format(hits)}`).toEqual([]);
  });

  it("页面里不许给老类 .btn-ghost / .btn-primary 再配底色", () => {
    // 允许：深色控制台、`：disabled`、`.active` 等状态位（这些不是「另一种默认外观」）。
    const hits = scan((selector, body) => {
      const bare = stripConditions(selector);
      if (!/\.(?:btn-ghost|btn-primary)\b/.test(bare)) return false;
      if (!/(^|[\s;])background/.test(body)) return false;
      if (/:disabled|\.active|is-active|is-on|arco-theme/.test(selector)) return false;
      return true;
    }).filter(hit => !DARK_CONSOLE.some(skip => hit.file.includes(skip)));
    expect(hits, `老类底色必须由 arco-overrides.scss 提供，页面副本会反超语义类：\n${format(hits)}`).toEqual([]);
  });

  it("共享搜索面板不再直接给次级按钮定色（38 个列表页共用）", () => {
    const source = readFileSync(resolve(root, "components/s-layout-search/index.vue"), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
    const actionsRules = [...source.matchAll(/([^{}]*uvp-search-panel__actions[^{}]*)\{([^{}]*)\}/g)].map(m => ({
      selector: m[1].replace(/\s+/g, " ").trim(),
      body: m[2]
    }));
    const offenders = actionsRules.filter(
      rule => /(^|[\s;])background/.test(rule.body) && !rule.selector.includes(".arco-btn-primary")
    );
    expect(offenders, "搜索面板的次级按钮已经在真源里定好了（浅蓝底蓝字），这里只该保留 40px 档位与主按钮的色阶").toEqual([]);
  });

  it("页面不许给搜索面板 extra 槽的按钮再写一套配色", () => {
    // 2026-11-07：在线用户页的「刷新」被 scoped 里带 !important 的 extra 槽配色
    // 压成灰白底（本该是 uvp-refresh-btn 的青绿）。页面 scoped 恒晚于全局真源注入，
    // 靠调特异性追不平 —— 只能禁止页面再定色，几何（圆角/尺寸）可以留。
    // 共享组件自己那条「extra 槽主按钮沿用新增绿」是设计契约，不在此列。
    const hits = scan((selector, body) => {
      const compact = selector.replace(/\s+/g, "");
      if (!compact.includes("uvp-search-panel__extra") || !compact.includes(".arco-btn")) return false;
      if (compact.includes(".arco-btn-primary")) return false;
      return /(^|[\s;])(background|background-color|color|border-color|box-shadow)\s*:/.test(body);
    });
    expect(hits, `extra 槽按钮的配色请交给语义类，页面里只写几何：\n${format(hits)}`).toEqual([]);
  });
});
