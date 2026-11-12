import { existsSync, readFileSync, readdirSync } from "node:fs";
import { relative, resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { hasRuleBlock } from "@/test/source-assert";

/**
 * 分页统一防回归（2026-09-29）。
 *
 * 基准 = `src/views/system/account/account.vue`：
 *   · 表格带 `class="uvp-data-table"`
 *   · `{ current, pageSize: 10, total, showPageSize, showTotal, showJumper }` 三开关全开
 *   · `pageSizeOptions: [10, 20, 50, 100]`
 *   · 页面内**不**重复声明 `.arco-pagination-*` 尺寸
 *
 * 样式唯一落点 = `src/style/model/uvp-ui-language.scss` 的
 * `.uvp-data-table / .uvp-pagination-bar` 共享块。
 *
 * 2026-09-29 补：流媒体管理（ZLM）下 6 个子页面原先被当成「独立视觉体系」豁免，
 * 但它们的独立分页器留在带边框卡片内部 ⇒ 分页器四周有边框、圆角仍是 Arco 默认 2px。
 * 现全部改为「分页条移出卡片 + 挂 uvp-pagination-bar」，豁免取消。
 */

const SRC = resolve(process.cwd(), "src");
const OPTIONS_STANDARD = "[10,20,50,100]";

/** 允许保留非标准分页档位的文件（每一项都要有理由）。 */
const PAGE_SIZE_OPTIONS_ALLOWLIST: Record<string, string> = {
  "plugins/example/views/examplelist.vue": "插件示例，明确未纳入 2026-09-29 统一范围",
  "views/gb28181/snapshot-library/snapshotLibraryState.ts": "图库按张浏览，后端把 pageSize 硬截到 200，档位上限需 ≥ 常用浏览密度",
  "views/gb28181/device-mgmt/index.vue": "设备列表卡片视图沿用 12/24/48 例外"
};

/** 允许不挂 `uvp-pagination-bar` 的文件。 */
const PAGINATION_BAR_ALLOWLIST: Record<string, string> = {
  "plugins/example/views/examplelist.vue": "插件示例，明确未纳入统一范围"
};

/** 分页条必须落在带边框卡片**之外**的文件（卡片内会让分页器四周有边框）。 */
const PAGINATION_OUTSIDE_CARD: Array<{ file: string; cardClass: string; barClass: string }> = [
  { file: "views/gb28181/playback-log/index.vue", cardClass: "playback-log-table-panel", barClass: "playback-log-pagination" },
  {
    file: "views/gb28181/zlm/workbench/monitoring/StreamPanel.vue",
    cardClass: "stream-table-panel",
    barClass: "stream-pagination"
  },
  {
    file: "views/gb28181/zlm/workbench/monitoring/NetworkSessionPanel.vue",
    cardClass: "session-table-panel",
    barClass: "session-pagination"
  },
  {
    file: "views/gb28181/zlm/workbench/scheduling/SchedulerLogPanel.vue",
    cardClass: "log-table-panel",
    barClass: "log-pagination"
  },
  {
    file: "views/gb28181/zlm/workbench/ingress/FFmpegPanel.vue",
    cardClass: "data-panel",
    barClass: "ingress-pagination"
  },
  {
    file: "views/gb28181/zlm/workbench/ingress/ProxyPanel.vue",
    cardClass: "data-panel",
    barClass: "ingress-pagination"
  },
  {
    file: "views/gb28181/zlm/workbench/ingress/RTPPanel.vue",
    cardClass: "data-panel",
    barClass: "ingress-pagination"
  }
];

function listVueFiles(): string[] {
  return (readdirSync(SRC, { recursive: true, encoding: "utf8" }) as string[])
    .filter(name => name.endsWith(".vue") && !name.includes(".test."))
    .map(name => name.split("\\").join("/"))
    .sort();
}

function read(relPath: string): string {
  return readFileSync(resolve(SRC, relPath), "utf8");
}

/** 从 `from` 开始找标签收尾的 `>`（跳过引号内的字符）。 */
function endOfTag(source: string, from: number): number {
  let quote: string | null = null;
  for (let i = from; i < source.length; i += 1) {
    const ch = source[i];
    if (quote) {
      if (ch === quote) quote = null;
    } else if (ch === '"' || ch === "'") {
      quote = ch;
    } else if (ch === ">") {
      return i + 1;
    }
  }
  return source.length;
}

/** 抽取 `<tag ...>` 的完整标签文本（属性可折行，闭合 `>` 也可悬挂）。 */
function tagBlocks(source: string, tag: string): string[] {
  const blocks: string[] = [];
  let cursor = source.indexOf(`<${tag}`);
  while (cursor !== -1) {
    const end = endOfTag(source, cursor);
    blocks.push(source.slice(cursor, end));
    cursor = source.indexOf(`<${tag}`, end);
  }
  return blocks;
}

/** 按花括号配平取出变量定义块。 */
function definitionBlock(source: string, name: string): string | null {
  const matcher = new RegExp(`(?:const|let)\\s+${name}\\s*=`, "g");
  let matched: RegExpExecArray | null;
  while ((matched = matcher.exec(source)) !== null) {
    const rest = source.slice(matched.index + matched[0].length);
    const open = rest.indexOf("{");
    if (open === -1 || open > 60) continue;
    let depth = 0;
    for (let i = open; i < rest.length; i += 1) {
      if (rest[i] === "{") depth += 1;
      else if (rest[i] === "}") {
        depth -= 1;
        if (depth === 0) return rest.slice(open, i + 1);
      }
    }
  }
  return null;
}

const vueFiles = listVueFiles();
const globalStyles = readFileSync(resolve(SRC, "style/model/uvp-ui-language.scss"), "utf8");
const squashedGlobal = globalStyles.replace(/\s+/g, "");

describe("分页统一：全局样式落点", () => {
  it("标准块同时覆盖表格内建分页与独立分页条", () => {
    expect(squashedGlobal).toContain(".uvp-data-table,.uvp-pagination-bar{");
    expect(squashedGlobal).toContain("&.arco-pagination{");
  });

  it("32px 行高与 box-sizing 收在全局标准块里", () => {
    // 之前 10 个页面各自复制了这几条声明，是「分页样式没统一」的根因。
    expect(
      hasRuleBlock(
        globalStyles,
        ".arco-pagination-item",
        "box-sizing: border-box",
        "min-width: 32px",
        "height: 32px",
        "min-height: 32px"
      )
    ).toBe(true);
    expect(hasRuleBlock(globalStyles, ".arco-pagination-options .arco-select-view", "height: 32px", "min-height: 32px")).toBe(
      true
    );
    expect(hasRuleBlock(globalStyles, ".arco-pagination-jumper-input", "min-height: 32px")).toBe(true);
  });

  it("保留侧栏 mini 分页器的紧凑尺寸", () => {
    expect(
      hasRuleBlock(
        globalStyles,
        ".arco-pagination-size-mini .arco-pagination-item",
        "min-width: 24px",
        "height: 24px",
        "min-height: 24px"
      )
    ).toBe(true);
  });

  it("不再保留 30px 旧口径的 uvp-density.scss", () => {
    expect(existsSync(resolve(SRC, "style/model/uvp-density.scss"))).toBe(false);
  });
});

describe("分页统一：页面不得重复声明分页尺寸", () => {
  it("没有任何 .vue 在样式里覆盖 .arco-pagination-*", () => {
    const offenders = vueFiles.filter(file => read(file).includes("arco-pagination"));
    expect(offenders).toEqual([]);
  });
});

describe("分页统一：表格内建分页", () => {
  const tables: Array<{ file: string; tag: string; binding: string }> = [];
  for (const file of vueFiles) {
    const source = read(file);
    for (const tag of tagBlocks(source, "a-table")) {
      const matched = /:pagination="([^"]+)"/.exec(tag);
      if (!matched || matched[1] === "false") continue;
      tables.push({ file, tag, binding: matched[1] });
    }
  }

  it("扫描到了预期的表格数量（防止扫描逻辑失效后静默通过）", () => {
    expect(tables.length).toBeGreaterThanOrEqual(30);
  });

  it("每个分页表格都带 uvp-data-table 类", () => {
    const offenders = tables.filter(item => !item.tag.includes("uvp-data-table")).map(item => item.file);
    expect(Array.from(new Set(offenders))).toEqual([]);
  });

  it("每个分页对象三开关齐备且声明了 pageSizeOptions", () => {
    const offenders: string[] = [];
    for (const { file, binding } of tables) {
      if (binding.startsWith("{")) {
        for (const flag of ["showTotal: true", "showPageSize: true", "showJumper: true"]) {
          if (!binding.includes(flag)) offenders.push(`${file} → 内联分页缺 ${flag}`);
        }
        continue;
      }
      const block = definitionBlock(read(file), binding);
      if (!block) {
        offenders.push(`${file} → 定位不到 ${binding} 的定义`);
        continue;
      }
      for (const flag of ["showTotal", "showPageSize", "showJumper"]) {
        if (!new RegExp(`\\b${flag}\\b`).test(block)) offenders.push(`${file} → ${binding} 缺 ${flag}`);
      }
      if (!block.includes("pageSizeOptions")) offenders.push(`${file} → ${binding} 缺 pageSizeOptions`);
    }
    expect(offenders).toEqual([]);
  });
});

describe("分页统一：独立 <a-pagination>", () => {
  const filesWithStandalonePager = vueFiles.filter(file => read(file).includes("<a-pagination"));

  it("扫描到了预期的独立分页器（防止扫描逻辑失效后静默通过）", () => {
    expect(filesWithStandalonePager.length).toBeGreaterThanOrEqual(10);
  });

  it("除 ZLM 控制台与插件示例外，都必须挂 uvp-pagination-bar", () => {
    const offenders = filesWithStandalonePager
      .filter(file => !(file in PAGINATION_BAR_ALLOWLIST))
      .filter(file => !read(file).includes("uvp-pagination-bar"));
    expect(offenders).toEqual([]);
  });
});

describe("分页统一：分页条必须在带边框卡片之外", () => {
  it.each(PAGINATION_OUTSIDE_CARD)("$file 的 $barClass 落在 $cardClass 之后（不在卡片内）", ({ file, cardClass, barClass }) => {
    const source = read(file);
    const cardStart = source.indexOf(`class="${cardClass}"`);
    expect(cardStart, `${file} 定位不到 ${cardClass}`).toBeGreaterThanOrEqual(0);
    const cardEnd = source.indexOf("</section>", cardStart);
    expect(cardEnd, `${file} 定位不到 ${cardClass} 的收尾`).toBeGreaterThan(cardStart);
    const barStart = source.indexOf(`${barClass} uvp-pagination-bar`);
    expect(barStart, `${file} 的 ${barClass} 没挂 uvp-pagination-bar`).toBeGreaterThanOrEqual(0);
    expect(barStart, `${file} 的分页条仍在 ${cardClass} 卡片内`).toBeGreaterThan(cardEnd);
  });

  it("分页条样式不再自带 border-top 分隔线", () => {
    const offenders: string[] = [];
    for (const { file, barClass } of PAGINATION_OUTSIDE_CARD) {
      const source = read(file);
      const rule = new RegExp(`\\.${barClass}\\s*\\{([^}]*)\\}`);
      const matched = rule.exec(source);
      if (!matched) {
        offenders.push(`${file} → 定位不到 .${barClass} 样式`);
        continue;
      }
      if (/border(-top)?:/.test(matched[1])) offenders.push(`${file} → .${barClass} 仍有边框`);
      if (!/margin-top:\s*12px/.test(matched[1])) offenders.push(`${file} → .${barClass} 缺 margin-top: 12px`);
    }
    expect(offenders).toEqual([]);
  });

  it("扫描到的目标文件仍然真实存在且挂了类名（防止清单失效后静默通过）", () => {
    expect(PAGINATION_OUTSIDE_CARD.length).toBeGreaterThanOrEqual(7);
    for (const { file, barClass } of PAGINATION_OUTSIDE_CARD) {
      expect(read(file), file).toContain(`${barClass} uvp-pagination-bar`);
    }
  });
});

describe("分页统一：pageSizeOptions 口径", () => {
  it("统一为 [10, 20, 50, 100]（例外需登记在 allowlist）", () => {
    const offenders: string[] = [];
    const normalize = (value: string) => value.replace(/\s+/g, "");
    for (const file of vueFiles) {
      const source = read(file);
      for (const matched of source.matchAll(/pageSizeOptions:\s*(\[[^\]]*\])/g)) {
        if (normalize(matched[1]) === OPTIONS_STANDARD) continue;
        if (file in PAGE_SIZE_OPTIONS_ALLOWLIST) continue;
        offenders.push(`${file} → ${matched[1]}`);
      }
      for (const matched of source.matchAll(/:page-size-options="(\[[^\]]*\])"/g)) {
        if (normalize(matched[1]) === OPTIONS_STANDARD) continue;
        if (file in PAGE_SIZE_OPTIONS_ALLOWLIST) continue;
        offenders.push(`${file} → :page-size-options ${matched[1]}`);
      }
    }
    expect(offenders).toEqual([]);
  });

  it("标准档位至少覆盖 15 处（防止正则失效后静默通过）", () => {
    let hits = 0;
    for (const file of vueFiles) {
      const source = read(file);
      hits += (source.match(/pageSizeOptions/g) ?? []).length;
    }
    expect(hits).toBeGreaterThanOrEqual(15);
  });

  it("allowlist 里的例外仍然真实存在（避免留下失效白名单）", () => {
    expect(read("views/gb28181/snapshot-library/snapshotLibraryState.ts")).toContain(
      "SNAPSHOT_LIBRARY_PAGE_SIZE_OPTIONS = [10, 20, 40, 80, 120, 200]"
    );
    expect(read("views/gb28181/device-mgmt/index.vue")).toContain('page-size-options="[12, 24, 48]"');
    expect(existsSync(resolve(SRC, "plugins/example/views/examplelist.vue"))).toBe(true);
  });
});

describe("分页统一：允许清单自身可读", () => {
  it("每个允许清单条目都指向真实存在的文件", () => {
    const keys = [...Object.keys(PAGE_SIZE_OPTIONS_ALLOWLIST), ...Object.keys(PAGINATION_BAR_ALLOWLIST)];
    for (const key of keys) {
      expect(existsSync(resolve(SRC, key)), `${key} 不存在`).toBe(true);
      expect(relative(SRC, resolve(SRC, key))).toBe(key);
    }
  });
});
