import { readFileSync, readdirSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { hasRuleBlock } from "@/test/source-assert";

/**
 * 全仓守卫：**每一颗 `<a-modal>` 都必须挂房规类 `uvp-system-dialog`**。
 *
 * 为什么值得一条全仓扫描（而不是靠某个组件的用例）：
 *  · 面板底色 / 边框 / 圆角 16px、头部与页脚的内边距和分隔线、页脚 10px 间距、
 *    暗色下的控件底色 —— **全部挂在 `.uvp-system-dialog` 上**（见 `style/model/uvp-ui-language.scss`）；
 *  · 漏挂**不报错、也不崩**，只是那颗弹窗变成一个裸 Arco 弹窗，跟旁边每一颗都"不像一家"。
 *    2026-10-15 老板截图点名级联的「新建业务分组」弹窗，一查全仓还有 8 处同样漏挂；
 *  · 这类"少一个 class"的回归，功能用例一条都拦不住（弹窗照样开、表单照样提交）
 *    ⇒ 只能源码级守，写法参照 `right-drawer-unification.test.ts`。
 *
 * ⛔ 别把判据写成"有 `modal-class` 就行"：`modal-class="xxx"` 同样能构造出来，
 *   必须**值里含 `uvp-system-dialog`**。
 */

const root = resolve(process.cwd(), "src");

function vueFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = resolve(directory, entry.name);
    return entry.isDirectory() ? vueFiles(path) : entry.name.endsWith(".vue") ? [path] : [];
  });
}

/**
 * 取出模板段（并**删掉 HTML 注释**）。
 * ⛔ 两处都踩过：① 注释里提到 `<a-modal` 会被当成真弹窗（`PlayConsoleLinked.vue` 的 JSDoc 就写着）；
 *   ② `<script>` 里的字符串同样会命中。所以先切模板、再去注释。
 */
function templateOf(source: string): string {
  const start = source.indexOf("<template>");
  if (start < 0) return "";
  const end = source.lastIndexOf("</template>");
  return source.slice(start, end).replace(/<!--[\s\S]*?-->/g, "");
}

/**
 * 抠出所有 `<a-modal ...>` 开标签。
 * ⛔ 不能用 `/<a-modal\b[^>]*>/`：属性里的表达式可能自带 `>`（如 `:top="a > b ? 1 : 2"`），
 *   那种正则会把标签**截断**，后面的属性（可能正是 `modal-class`）就"看不见"了 —— 假绿。
 *   这里按 `{}` `()` 配平往后走，只有**同一层**的 `>` 才算标签结束。
 */
function modalTags(template: string): string[] {
  const tags: string[] = [];
  const re = /<a-modal\b/g;
  let match: RegExpExecArray | null;
  while ((match = re.exec(template))) {
    let index = match.index + match[0].length;
    let depth = 0;
    while (index < template.length) {
      const char = template[index];
      if (char === "{" || char === "(") depth++;
      else if (char === "}" || char === ")") depth--;
      else if (char === ">" && depth <= 0) break;
      index++;
    }
    tags.push(template.slice(match.index, index + 1));
    re.lastIndex = index;
  }
  return tags;
}

/**
 * 有意**不带**房规类的弹窗 —— 每条都必须写清理由，且下面有用例反向盯着
 * "这条理由还成立吗"（改了代码却不删名单 = 红）。
 */
const EXCEPTIONS: Record<string, string> = {
  "views/system/codegen/components/codegen-preview-modal.vue":
    "全出血代码预览：它靠 body-style 把内边距压成 0、自己排 600px 高的 flex 列，" +
    "而房规类里的 `.arco-modal-body { padding: 22px 24px !important }` 带 !important、" +
    "会盖掉内联的 0（!important 胜过内联样式）⇒ 挂上反而把预览挤变形。"
};

/**
 * 扫描结果**只读一遍盘**，按文件存好给所有用例共用。
 * ⛔ 别在 `it.each` 里各读一次：这个目录里还有两条全仓扫描的重型门禁，
 *   多出来的几十次读盘会把 CPU 抢到顶穿它们的用例超时（本仓踩过"单跑绿、混跑红"）。
 */
const tagsByFile = new Map<string, string[]>(
  vueFiles(root).map(path => [path.slice(root.length + 1), modalTags(templateOf(readFileSync(path, "utf8")))])
);
const modals = [...tagsByFile].flatMap(([path, tags]) => tags.map(tag => ({ path, tag })));

/**
 * 取这颗弹窗声明的 class 词表。
 * ⛔ 必须同时认三种写法（本仓**三种都有**）：
 *     `modal-class="a b"` / `:modal-class="'a b'"` / `:modal-class="['a', 'b', {…}]"`。
 *   只认第一种会把 `PlayConsoleLinked.vue` 那颗**明明带了类**的弹窗判成违规 —— 假红，
 *   门禁一旦假红就会被当成噪音删掉，等于白写（首版就是这么错的，自己先踩了一次）。
 */
function dialogClassTokens(tag: string): string[] {
  const values = [...tag.matchAll(/(?::|v-bind:)?modal-class\s*=\s*(?:"([^"]*)"|'([^']*)')/g)].map(
    match => match[1] ?? match[2] ?? ""
  );
  return values.flatMap(value => value.split(/[\s,'"[\]]+/)).filter(Boolean);
}
const carriesDialogClass = (tag: string) => dialogClassTokens(tag).includes("uvp-system-dialog");
const withoutClass = modals.filter(({ tag }) => !carriesDialogClass(tag));

describe("system dialog skin coverage", () => {
  it("keeps modal and drawer cancel buttons flat in every interaction state", () => {
    const styles = readFileSync(resolve(root, "styles/arco-overrides.scss"), "utf8");
    for (const selector of [".arco-modal-footer .arco-btn-secondary", ".arco-drawer-footer .arco-btn-secondary"]) {
      expect(hasRuleBlock(styles, selector, "box-shadow: none !important", "transform: none !important")).toBe(true);
      expect(hasRuleBlock(styles, selector, "background: var(--uvp-search-secondary-btn-bg) !important")).toBe(true);
      expect(hasRuleBlock(styles, `${selector}:focus-visible`, "outline: 2px solid var(--uvp-brand) !important")).toBe(true);
    }
    const simpleStyles = readFileSync(resolve(root, "style/model/uvp-modal-simple.scss"), "utf8");
    expect(hasRuleBlock(simpleStyles, ".arco-modal-simple .arco-modal-footer .arco-btn-secondary", "box-shadow: none")).toBe(
      true
    );
  });

  it("finds the dialogs across the whole app (扫描本身不是空转)", () => {
    expect(modals.length).toBeGreaterThan(50);
    // 点名一颗已知成员：扫描器要是哪天切错段落（比如被注释/字符串带偏），这条立刻红
    expect(modals.some(({ path }) => path.endsWith("cascade/BusinessGroupManager.vue"))).toBe(true);
    expect(modals.some(({ path }) => path.endsWith("zlm/workbench/ingress/RTPPanel.vue"))).toBe(true);
  });

  // 逐个文件单独一条用例：标题能直接指出"是谁漏了"，而不是一句"X 处不符"。
  // 名单里的文件不进这张表（它们**本来就该**不带类），由下面"名单不许过期"那条单独盯。
  it.each(
    [...tagsByFile]
      .filter(([, tags]) => tags.length > 0)
      .map(([path]) => path)
      .filter(path => !(path in EXCEPTIONS))
  )("carries the shared dialog class: %s", path => {
    const tags = tagsByFile.get(path) ?? [];
    expect(tags.length).toBeGreaterThan(0);
    tags.forEach(tag => expect(carriesDialogClass(tag)).toBe(true));
  });

  it("只有名单里的弹窗允许不挂，且名单不许过期", () => {
    const offenders = withoutClass.map(({ path }) => path);
    expect(offenders.filter(path => !(path in EXCEPTIONS))).toEqual([]);
    // 反向：名单里每条都还必须"真的没挂" —— 代码补上了却不删名单，说明理由已经过期
    Object.keys(EXCEPTIONS).forEach(path => {
      expect(offenders).toContain(path);
      expect(EXCEPTIONS[path].length).toBeGreaterThan(20);
    });
  });

  it("把裸 Arco 弹窗变回「一家」的正是这个类（样式规则还在）", () => {
    const styles = readFileSync(resolve(root, "style/model/uvp-ui-language.scss"), "utf8");
    // 面板本体：底色 + 边框 + 圆角（漏挂时这三样全缺，就是老板截图里那块"不对"的面板）
    expect(hasRuleBlock(styles, ".uvp-system-dialog", "background: var(--uvp-dialog-bg) !important")).toBe(true);
    expect(hasRuleBlock(styles, ".uvp-system-dialog", "border-radius: 16px !important")).toBe(true);
    // 头 / 体 / 尾的内边距与分隔线
    expect(
      hasRuleBlock(styles, ".uvp-system-dialog .arco-modal-header", "border-bottom: 1px solid var(--uvp-dialog-border)")
    ).toBe(true);
    expect(hasRuleBlock(styles, ".uvp-system-dialog .arco-modal-footer", "gap: 10px")).toBe(true);
  });
});
