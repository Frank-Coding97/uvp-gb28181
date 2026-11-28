import { describe, expect, it } from "vitest";
import { parseReleaseNotes, parseReleaseNoteRuns } from "./releaseNotes";

describe("升级说明解析", () => {
  it("把「- **标题**：正文」拆成带加粗的列表项", () => {
    const blocks = parseReleaseNotes("- **录像中心**：左侧菜单新增「录像中心」。\n- 对讲支持 G.711U。");
    expect(blocks).toHaveLength(1);
    expect(blocks[0].kind).toBe("list");
    if (blocks[0].kind !== "list") throw new Error("应为列表");
    expect(blocks[0].items).toHaveLength(2);
    expect(blocks[0].items[0]).toEqual([{ text: "录像中心", bold: true }, { text: "：左侧菜单新增「录像中心」。" }]);
    expect(blocks[0].items[1]).toEqual([{ text: "对讲支持 G.711U。" }]);
  });

  it("小标题与列表分开，空行结束列表", () => {
    const blocks = parseReleaseNotes("## 修复\n\n- 第一条\n\n- 第二条\n普通段落");
    expect(blocks.map(block => block.kind)).toEqual(["heading", "list", "list", "paragraph"]);
    const heading = blocks[0];
    if (heading.kind !== "heading") throw new Error("应为小标题");
    expect(heading.level).toBe(2);
    expect(heading.runs).toEqual([{ text: "修复" }]);
  });

  it("行内代码单独成片，不与加粗混淆", () => {
    expect(parseReleaseNoteRuns("备份 `config/` 与 `data/`")).toEqual([
      { text: "备份 " },
      { text: "config/", code: true },
      { text: " 与 " },
      { text: "data/", code: true }
    ]);
  });

  it("没有 Markdown 标记时按纯文本段落返回", () => {
    expect(parseReleaseNotes("新增：手动升级")).toEqual([{ kind: "paragraph", runs: [{ text: "新增：手动升级" }] }]);
  });

  it("缩进的续行并回上一段：中文直接接上，英文补空格", () => {
    const chinese = parseReleaseNotes("第一段\n  继续");
    expect(chinese).toEqual([{ kind: "paragraph", runs: [{ text: "第一段继续" }] }]);
    const english = parseReleaseNotes("first line\n  second line");
    expect(english).toEqual([{ kind: "paragraph", runs: [{ text: "first line second line" }] }]);
  });

  it("顶格的多行各自成段，不粘成一句", () => {
    expect(parseReleaseNotes("新增：手动升级\n修复：启动异常")).toEqual([
      { kind: "paragraph", runs: [{ text: "新增：手动升级" }] },
      { kind: "paragraph", runs: [{ text: "修复：启动异常" }] }
    ]);
  });

  it("空说明返回空数组，交给调用方显示占位文案", () => {
    expect(parseReleaseNotes("")).toEqual([]);
    expect(parseReleaseNotes(undefined)).toEqual([]);
    expect(parseReleaseNotes("   \n\n ")).toEqual([]);
  });

  it("HTML 只当文本，不产生标签语义", () => {
    const blocks = parseReleaseNotes("<img src=x onerror=alert(1)>");
    expect(blocks).toEqual([{ kind: "paragraph", runs: [{ text: "<img src=x onerror=alert(1)>" }] }]);
  });

  it("未闭合的加粗标记原样保留", () => {
    expect(parseReleaseNoteRuns("**没闭合")).toEqual([{ text: "**没闭合" }]);
  });
});
