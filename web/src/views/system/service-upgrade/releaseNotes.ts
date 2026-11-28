// 升级说明的轻量解析：把发布清单里的 Markdown 片段（`- **标题**：正文`）拆成可渲染的结构。
//
// ⛔ 只用结构化渲染，不用 v-html：说明来自远端清单，任何一方被替换都不该获得执行能力。
export interface ReleaseNoteRun {
  text: string;
  bold?: boolean;
  code?: boolean;
}

export type ReleaseNoteBlock =
  | { kind: "heading"; level: number; runs: ReleaseNoteRun[] }
  | { kind: "paragraph"; runs: ReleaseNoteRun[] }
  | { kind: "list"; items: ReleaseNoteRun[][] };

const INLINE_PATTERN = /\*\*([^*\n]+)\*\*|`([^`\n]+)`/g;
const BULLET_PATTERN = /^\s{0,6}(?:[-*•]|\d{1,2}[.)])\s+(.*)$/;
const HEADING_PATTERN = /^\s{0,3}(#{1,6})\s+(.+?)\s*#*\s*$/;

/** 把一行文本拆成普通文本 / 加粗 / 行内代码三种片段。 */
export function parseReleaseNoteRuns(source: string): ReleaseNoteRun[] {
  const runs: ReleaseNoteRun[] = [];
  const text = String(source ?? "");
  if (!text) return runs;
  let cursor = 0;
  INLINE_PATTERN.lastIndex = 0;
  let match = INLINE_PATTERN.exec(text);
  while (match) {
    if (match.index > cursor) runs.push({ text: text.slice(cursor, match.index) });
    if (match[1] !== undefined) runs.push({ text: match[1], bold: true });
    else runs.push({ text: match[2], code: true });
    cursor = match.index + match[0].length;
    match = INLINE_PATTERN.exec(text);
  }
  if (cursor < text.length) runs.push({ text: text.slice(cursor) });
  return runs;
}

function needsSpace(before: string, after: string): boolean {
  return /[A-Za-z0-9]$/.test(before) && /^[A-Za-z0-9]/.test(after);
}

// 软换行的续行并回上一段：英文补空格，中文直接接上（与 Markdown 的观感一致）。
function appendRuns(target: ReleaseNoteRun[], extra: ReleaseNoteRun[]): void {
  if (!extra.length) return;
  if (!target.length) {
    target.push(...extra);
    return;
  }
  const last = target[target.length - 1];
  if (needsSpace(last.text, extra[0].text)) target.push({ text: " " });
  target.push(...extra);
}

// 相邻同格式片段并成一段，避免模板里出现大量只有空白的分片。
function mergeRuns(runs: ReleaseNoteRun[]): ReleaseNoteRun[] {
  const merged: ReleaseNoteRun[] = [];
  for (const run of runs) {
    const last = merged[merged.length - 1];
    if (last && Boolean(last.bold) === Boolean(run.bold) && Boolean(last.code) === Boolean(run.code)) {
      last.text += run.text;
      continue;
    }
    merged.push({ ...run });
  }
  return merged;
}

/** 解析整段升级说明。空输入返回空数组，调用方据此显示占位文案。 */
export function parseReleaseNotes(source?: string | null): ReleaseNoteBlock[] {
  const blocks: ReleaseNoteBlock[] = [];
  const lines = String(source ?? "")
    .replace(/\r\n?/g, "\n")
    .split("\n");
  let list: ReleaseNoteRun[][] | null = null;

  for (const rawLine of lines) {
    const line = rawLine.trimEnd();
    if (!line.trim()) {
      list = null;
      continue;
    }
    // 只有缩进行才算「续行」；顶格行一律另起，避免把两行独立说明粘成一句。
    const indented = /^\s/.test(rawLine);
    const heading = HEADING_PATTERN.exec(line);
    if (heading) {
      list = null;
      blocks.push({ kind: "heading", level: heading[1].length, runs: parseReleaseNoteRuns(heading[2]) });
      continue;
    }
    const bullet = BULLET_PATTERN.exec(line);
    if (bullet) {
      if (!list) {
        list = [];
        blocks.push({ kind: "list", items: list });
      }
      list.push(parseReleaseNoteRuns(bullet[1]));
      continue;
    }
    const runs = parseReleaseNoteRuns(line.trim());
    if (!runs.length) continue;
    if (indented && list?.length) {
      appendRuns(list[list.length - 1], runs);
      continue;
    }
    if (!indented) list = null;
    const previous = blocks[blocks.length - 1];
    if (indented && previous && previous.kind === "paragraph") {
      appendRuns(previous.runs, runs);
      continue;
    }
    blocks.push({ kind: "paragraph", runs });
  }

  return blocks.map(block =>
    block.kind === "list" ? { ...block, items: block.items.map(mergeRuns) } : { ...block, runs: mergeRuns(block.runs) }
  );
}
