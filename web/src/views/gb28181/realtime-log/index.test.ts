import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";

const source = readFileSync("src/views/gb28181/realtime-log/index.vue", "utf8");

describe("realtime console log page contract", () => {
  it("uses the system page shell and encapsulated search component", () => {
    for (const token of [
      "snow-fill",
      "snow-fill-inner uvp-page-shell-flat",
      "<s-layout-search",
      "<a-input",
      "<a-select",
      "<a-button"
    ]) {
      expect(source).toContain(token);
    }
  });

  it("renders a bounded black terminal with complete structured fields", () => {
    expect(source).toContain("events.value.length > 2000");
    for (const token of [
      "运行日志",
      "terminal-view",
      "#0d1117",
      "visibleEvents",
      "item.fields",
      "item.stack",
      "tail -f uvp-console.log"
    ]) {
      expect(source).toContain(token);
    }
  });

  it("shows reconnect, gap, following, copy and SIP trace actions", () => {
    for (const token of ["reconnecting", "limited", "gapMessage", "following", "copyEvent", "openTrace", "/gb28181/sip-traces"]) {
      expect(source).toContain(token);
    }
  });

  it("binds each reconnect loop to its own abort controller", () => {
    expect(source).toContain("const connection = new AbortController()");
    // ⛔ 别钉整行：prettier 会把超长的实参列表拆成多行。
    expect(source).toMatch(/openRealtimeLogStream\(\s*\{\s*since:\s*lastSequence \|\| undefined\s*\}/);
    expect(source).toContain("connection.signal");
  });

  it("drives the follow state from the shared scroll rule", () => {
    // 滚到底部要自动恢复跟随，规则本身由 ./follow-scroll.ts 单测覆盖。
    expect(source).toContain('import { resolveFollowAction } from "./follow-scroll"');
    expect(source).toContain("resolveFollowAction(scrollHeight - scrollTop - clientHeight, following.value)");
    expect(source).toContain("following.value = true");
    expect(source).toContain("滚到底部自动恢复");
  });

  it("forces the search select onto the shared search-control tokens", () => {
    // ⛔ 回归点：arco-overrides.scss 的 .arco-select-view 带 !important，
    // 不提权就会出现「输入框白底无框、下拉框浅蓝底带边框」的同栏不一致。
    const selectBlock = source.slice(source.indexOf(".console-search :deep(.arco-select-view)"));
    expect(selectBlock).toMatch(/background:\s*var\(--uvp-search-control-bg\) !important/);
    expect(selectBlock).toMatch(/box-shadow:\s*var\(--uvp-search-control-shadow\) !important/);
    expect(source).toContain(".console-search :deep(.arco-select-view.arco-select-view-focus)");
    expect(selectBlock).toContain("box-shadow: var(--uvp-search-control-focus-shadow) !important");
  });
});
