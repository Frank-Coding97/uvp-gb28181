import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * 删除类确认气泡的「确定」必须实心红（`status: 'danger'`）。
 *
 * ⛔ 为什么需要门禁：2026-11-04 之前全仓 15 处删除确认的确定按钮都是**蓝色 primary**，
 *   和「保存/提交」长得一模一样 —— 一眼看不出点下去会毁数据。
 *   按钮真源（`src/styles/uvp-buttons.scss`）早就给了危险语义：
 *   `status="danger"` + `type="primary"` = 实心红，Popconfirm 通过 `:ok-button-props` 透传。
 *   但这条约定纯靠人记，新加一个删除按钮就会漏 ⇒ 这里按**文案**兜底扫描。
 *
 * ⛔ 判据只认「破坏性动作」的动词（删除/移除/卸载/清空），**不能按 `type="warning"` 判**：
 *   `codegen.vue` 里「确定生成代码吗?」「确定同步字段信息吗?」同样是 `type="warning"`，
 *   但它们只读不毁数据，按钮必须保持蓝色。这个反向用例在下面单独钉住。
 */

const SRC = resolve(process.cwd(), "src");
const DESTRUCTIVE = /删除|移除|卸载|清空/;
const DANGER_ATTR = ":ok-button-props=\"{ status: 'danger' }\"";
/** 明确不能变红的确认（只读不改数的操作）。 */
const NOT_DESTRUCTIVE = ["确定生成代码吗?", "确定同步字段信息吗?"];

function walk(dir: string, out: string[] = []) {
  for (const name of readdirSync(dir)) {
    const full = join(dir, name);
    if (statSync(full).isDirectory()) walk(full, out);
    else if (name.endsWith(".vue")) out.push(full);
  }
  return out;
}

/** 从 `<a-popconfirm` 起找到真正的标签结束 `>`，跳过引号内的内容。 */
function tagAt(source: string, start: number) {
  let quote: string | null = null;
  for (let i = start; i < source.length; i += 1) {
    const ch = source[i];
    if (quote) {
      if (ch === quote) quote = null;
    } else if (ch === '"' || ch === "'" || ch === "`") {
      quote = ch;
    } else if (ch === ">") {
      return source.slice(start, i + 1);
    }
  }
  return "";
}

function popconfirmTags() {
  const tags: { file: string; line: number; tag: string }[] = [];
  for (const file of walk(SRC)) {
    const source = readFileSync(file, "utf8");
    const re = /<a-popconfirm\b/g;
    let m: RegExpExecArray | null;
    while ((m = re.exec(source))) {
      tags.push({
        file: relative(process.cwd(), file),
        line: source.slice(0, m.index).split("\n").length,
        tag: tagAt(source, m.index)
      });
    }
  }
  return tags;
}

describe("删除确认气泡的危险语义", () => {
  it("所有破坏性确认（删除/移除/卸载/清空）的确定按钮都是实心红", () => {
    const offenders = popconfirmTags()
      .filter(t => DESTRUCTIVE.test(t.tag) && !t.tag.includes(DANGER_ATTR))
      .map(t => `${t.file}:${t.line}`);

    expect(offenders, `这些删除确认没加 :ok-button-props="{ status: 'danger' }"`).toEqual([]);
  });

  it("只读操作（生成代码 / 同步字段）保持蓝色，不许被无脑改成红", () => {
    const wrong = popconfirmTags()
      .filter(t => NOT_DESTRUCTIVE.some(text => t.tag.includes(text)) && t.tag.includes(DANGER_ATTR))
      .map(t => `${t.file}:${t.line}`);

    expect(wrong, "这些确认不毁数据，确定按钮应保持蓝色 primary").toEqual([]);
  });

  it("扫描本身不是空跑（能找到确认气泡，否则上面两条是假绿）", () => {
    expect(popconfirmTags().length).toBeGreaterThan(10);
  });
});
