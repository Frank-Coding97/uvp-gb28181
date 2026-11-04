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

/**
 * 结构性例外：**下拉菜单项里的删除**只能留 Modal。
 *
 * `a-doption` 被点后下拉立刻卸载（父层 `contextMenuCode=''` / Arco dropdown 自身关闭），
 * 挂在上面的 a-popconfirm 永远没机会显形 —— 不是"懒得改"，是这条路走不通。
 * 想彻底统一得先把入口从下拉里挪出来（改成行内常显按钮），属于产品改动，不在本次范围。
 *
 * ⛔ 名单必须写 `count`：同文件里再新加第二个 Modal 删除，`count` 对不上就会红，
 *   防止"文件进白名单 ⇒ 整文件豁免"。
 */
const MODAL_DELETE_EXCEPTIONS: Record<string, { count: number; reason: string }> = {
  "src/views/gb28181/device-mgmt/index.vue": {
    count: 1,
    reason: "设备列表「更多」下拉里的删除项：a-doption 点击即卸载下拉，气泡无处可挂（其余 6 个入口已改气泡）。"
  },
  "src/views/gb28181/cascade/BusinessGroupManager.vue": {
    count: 2,
    reason:
      "① 业务目录树的右键菜单删除项：a-doption 卸载后气泡无处可挂（行内常显按钮已改气泡）；" +
      '② 「取消共享」不是删除：它只是把通道加进取消共享草稿，保存前还能撤回，措辞里的"移除归属"指的是草稿内容。'
  }
};

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

/** 从 `(` 起配平取到与之匹配的 `)`，跳过引号内内容（含模板串）。 */
function callArgsAt(source: string, openParen: number) {
  let depth = 0;
  let quote: string | null = null;
  for (let i = openParen; i < source.length; i += 1) {
    const ch = source[i];
    if (quote) {
      if (ch === "\\") i += 1;
      else if (ch === quote) quote = null;
      continue;
    }
    if (ch === '"' || ch === "'" || ch === "`") quote = ch;
    else if (ch === "(") depth += 1;
    else if (ch === ")") {
      depth -= 1;
      if (depth === 0) return source.slice(openParen, i + 1);
    }
  }
  return "";
}

/**
 * 扫出函数式确认弹窗（`Modal.warning(...)` / `Modal.confirm(...)`）。
 *
 * ⛔ 判据是**这份 config 的正文里有没有删除类动词**，而不是"文件里有没有删除功能"：
 *    `device-mgmt` 同一个文件里既有已改气泡的入口、也有下拉里那条保留 Modal，
 *    按文件判会把两者混为一谈。
 */
function modalConfirmCalls() {
  const calls: { file: string; line: number; args: string }[] = [];
  for (const file of walk(SRC)) {
    const source = readFileSync(file, "utf8");
    const re = /\b(?:A?Modal)\.(?:warning|confirm)\s*\(/g;
    let m: RegExpExecArray | null;
    while ((m = re.exec(source))) {
      const openParen = m.index + m[0].length - 1;
      const args = callArgsAt(source, openParen);
      if (!args) continue;
      calls.push({
        file: relative(process.cwd(), file),
        line: source.slice(0, m.index).split("\n").length,
        args
      });
    }
  }
  return calls;
}

/** 只保留"确认文案本身在说删除"的那些（消息串里的"删除成功"不算：那在 onOk 里）。 */
function destructiveModalCalls() {
  return modalConfirmCalls().filter(call => {
    const title = call.args.match(/title:\s*([`"'])([\s\S]*?)\1/)?.[2] ?? "";
    const content = call.args.match(/content:\s*([`"'])([\s\S]*?)\1/)?.[2] ?? "";
    return DESTRUCTIVE.test(title) || DESTRUCTIVE.test(content);
  });
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

  // ⛔ 2026-11-04 老板拍板：全站删除确认统一成气泡，`Modal.warning/confirm` 那条路
  //    只允许留在"下拉菜单项里"这种结构上挂不住气泡的入口（名单见 MODAL_DELETE_EXCEPTIONS）。
  it("删除类确认不许再走 Modal（下拉菜单项除外）", () => {
    const byFile = new Map<string, number>();
    for (const call of destructiveModalCalls()) byFile.set(call.file, (byFile.get(call.file) ?? 0) + 1);

    const stray = [...byFile]
      .filter(([file, count]) => count > (MODAL_DELETE_EXCEPTIONS[file]?.count ?? 0))
      .map(([file, count]) => `${file}（${count} 处，名单允许 ${MODAL_DELETE_EXCEPTIONS[file]?.count ?? 0} 处）`);

    expect(stray, "这些删除确认还在弹 Modal，请改成 a-popconfirm 气泡（确认按钮用红色 danger）").toEqual([]);
  });

  // 反向：名单里的每一条都必须"真的还有那一处" —— 代码改成气泡了却不删名单 = 红，
  // 否则名单会一直膨胀，最后等于没有门禁。
  it("Modal 例外名单不许过期，且每条都写清了理由", () => {
    const byFile = new Map<string, number>();
    for (const call of destructiveModalCalls()) byFile.set(call.file, (byFile.get(call.file) ?? 0) + 1);

    for (const [file, entry] of Object.entries(MODAL_DELETE_EXCEPTIONS)) {
      expect(byFile.get(file) ?? 0, `${file} 已经不用 Modal 了，请从名单里删掉`).toBe(entry.count);
      expect(entry.reason.length).toBeGreaterThan(20);
    }
  });

  it("函数式弹窗扫描本身不是空跑（全仓确实还有别的确认弹窗）", () => {
    expect(modalConfirmCalls().length).toBeGreaterThan(5);
  });
});
