/**
 * 源码断言助手：让「读源文件文本做断言」的单测不受格式化影响。
 *
 * ⛔ 背景（踩过两次）：本仓 lint-staged 的 prettier --write / stylelint --fix 会在提交时
 *    重排被改动的文件：
 *      · 超长标签的属性被拆成多行，闭合 `>` 还会悬挂到下一行（`<a-tab-pane key="x"\n  >`）
 *      · 中文长文本在任意位置折行
 *      · 模板插值里的单引号被统一成双引号
 *      · stylelint-config-recess-order 重排 CSS 声明
 *    所以「把一整行源码钉进 expect(source).toContain(...)」是格式化一次红一次。
 *    这里改成只钉 token 与顺序，不钉排版。
 */

/**
 * 去掉全部空白，用于比对标签/属性/长文本。
 * 两侧的空白一起去掉，所以比对的是「token 与它们的先后顺序」，排版差异不影响结论。
 */
export function squash(source: string): string {
  return source.replace(/\s+/g, "");
}

/** 源码里是否含有这段片段（忽略折行与缩进差异）。 */
export function hasMarkup(source: string, snippet: string): boolean {
  return squash(source).includes(squash(snippet));
}

/**
 * 取出某个选择器的全部规则块内容（去掉换行压成单行）。
 * 用 `[^{]*` 跳过选择器本身，这样并列选择器（`.a .x,\n.b .x {`）也能命中。
 */
export function ruleBlocks(source: string, selector: string): string[] {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const pattern = new RegExp(escaped + "[^{]*\\{([^}]*)\\}", "g");
  const blocks: string[] = [];
  let matched: RegExpExecArray | null;
  while ((matched = pattern.exec(source)) !== null) {
    blocks.push(matched[1].replace(/\s+/g, " ").trim());
  }
  return blocks;
}

/**
 * 某个选择器的**同一个**规则块里是否同时有这几条声明（不要求书写顺序）。
 * ⛔ 不要用 `/\\.x\\s*\\{[^}]*a:[^}]*b:/` 这种串序正则：stylelint 的 recess-order 会
 *    重排声明，顺序一变就红。取「同一块」而不是「全文」是为了避免被另一个块（例如媒体
 *    查询里的同名选择器）的同名声明蒙混过关。
 */
export function hasRuleBlock(source: string, selector: string, ...declarations: string[]): boolean {
  return ruleBlocks(source, selector).some(block => declarations.every(item => block.includes(item)));
}
