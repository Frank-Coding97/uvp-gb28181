/**
 * P0 防回归锚点：展示层的「启用 / 禁用」必须由 `status` 字典驱动。
 *
 * ⛔ 用 `source-assert` 而不是整行源码比对 —— lint-staged 的 prettier/stylelint 会重排
 *    被改动文件（拆行、悬挂 `>`、统一引号），钉排版等于每次提交红一次。
 * ⛔ 只钉「状态标签」这一处：`a-switch` 的 `#checked` / `#unchecked` 槽位是**控件交互文案**，
 *    不是数据翻译，刻意不动（见文件末尾的边界用例）。
 */
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { hasMarkup, squash } from "@/test/source-assert";
import { DICT_CODE_STATUS, STATUS_LABEL_FALLBACK } from "./useDictOptions";

const readSource = (relative: string) => readFileSync(resolve(process.cwd(), relative), "utf8");

/** 引号归一（prettier 会把模板插值里的单引号改成双引号），便于钉 token 而不是钉引号风格 */
const normalize = (source: string) => squash(source).replace(/['"]/g, '"');

/** 已接入 `status` 字典的展示层落点 */
const STATUS_DRIVEN_FILES = [
  "src/views/system/account/account.vue",
  "src/views/system/role/role.vue",
  "src/views/system/division/division.vue",
  "src/views/system/dictionary/dictionary.vue"
];

/** 接入方式略有差异（`value` 而不是 `record.status`）的落点 */
const STATUS_DRIVEN_INLINE_FILES = [
  "src/views/system/userinfo/userinfo.vue",
  "src/components/select-user/index.vue",
  "src/components/select-department/index.vue"
];

describe("status 字典驱动展示层", () => {
  it.each([...STATUS_DRIVEN_FILES, ...STATUS_DRIVEN_INLINE_FILES])("%s 从统一字典层取翻译函数", file => {
    const source = readSource(file);
    expect(source).toContain('from "@/hooks/useDictOptions"');
    expect(source).toContain("useStatusLabel()");
    expect(source).toContain("statusLabel(");
  });

  it.each([...STATUS_DRIVEN_FILES, ...STATUS_DRIVEN_INLINE_FILES])("%s 的标签里不再写死中文", file => {
    // 旧形态：<a-tag ...>启用</a-tag> / <a-tag ...>禁用</a-tag>
    expect(normalize(readSource(file))).not.toMatch(/<a-tag[^>]*>(启用|禁用)</);
  });

  it.each(STATUS_DRIVEN_FILES)("%s 的表格状态列由 record.status 翻译", file => {
    expect(normalize(readSource(file))).toContain("{{statusLabel(record.status)}}");
  });

  it("个人资料页翻译的是资料值本身", () => {
    const source = normalize(readSource("src/views/system/userinfo/userinfo.vue"));
    expect(source).toContain("{{statusLabel(value)}}");
    // 旧形态：{{ value === 1 ? "启用" : "禁用" }}
    expect(source).not.toContain('?"启用":"禁用"');
  });

  it("字典编码指向种子内置的 status", () => {
    expect(DICT_CODE_STATUS).toBe("status");
  });
});

describe("status 兜底常量与种子不漂移", () => {
  it("STATUS_LABEL_FALLBACK 与 sys_dict_item.jsonl 逐字一致", () => {
    const seedDir = resolve(process.cwd(), "../server/resource/database/baseline/seeds");
    const dicts = readSourceJsonl(resolve(seedDir, "sys_dict.jsonl"));
    const statusDict = dicts.find(entry => entry.code === DICT_CODE_STATUS);
    expect(statusDict, `种子里必须有 code = ${DICT_CODE_STATUS} 的字典`).toBeTruthy();

    const items = readSourceJsonl(resolve(seedDir, "sys_dict_item.jsonl")).filter(entry => entry.dict_id === statusDict!.id);
    const fromSeed = Object.fromEntries(items.map(entry => [String(entry.value), entry.name]));
    expect(fromSeed).toEqual(STATUS_LABEL_FALLBACK);
  });
});

describe("刻意不动的边界", () => {
  it("表单开关的 checked/unchecked 槽位保持原样（控件文案，不是数据翻译）", () => {
    const account = normalize(readSource("src/views/system/account/account.vue"));
    expect(hasMarkup(account, "<template #checked>启用</template>")).toBe(true);
    expect(hasMarkup(account, "<template #unchecked>禁用</template>")).toBe(true);
  });

  it("菜单的「是否禁用」是布尔是/否（语义与 status 相反，不接 status 字典）", () => {
    const menu = readSource("src/views/system/menu/menu.vue");
    expect(menu).toContain('title="是否禁用"');
    expect(hasMarkup(menu, '<a-tag bordered size="small" color="arcoblue" v-if="record.disable">是</a-tag>')).toBe(true);
    expect(hasMarkup(menu, '<a-tag bordered size="small" color="red" v-else>否</a-tag>')).toBe(true);
  });
});

function readSourceJsonl(file: string): Array<Record<string, unknown>> {
  return readFileSync(file, "utf8")
    .split("\n")
    .map(line => line.trim())
    .filter(Boolean)
    .map(line => JSON.parse(line) as Record<string, unknown>);
}
