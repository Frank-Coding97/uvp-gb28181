import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";

const source = readFileSync("src/views/gb28181/realtime-log/index.vue", "utf8");

describe("realtime console log page contract", () => {
  it("uses the system page shell and encapsulated search component", () => {
    for (const token of ["snow-fill", "snow-fill-inner uvp-page-shell-flat", "<s-layout-search", "<a-input", "<a-select", "<a-button"]) {
      expect(source).toContain(token);
    }
  });

  it("renders a bounded black terminal with complete structured fields", () => {
    expect(source).toContain("events.value.length > 2000");
    for (const token of ["实时日志控制台", "terminal-view", "#0d1117", "visibleEvents", "item.fields", "item.stack", "tail -f uvp-console.log"]) {
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
    expect(source).toContain("openRealtimeLogStream({ since: lastSequence || undefined }");
    expect(source).toContain("connection.signal");
  });
});
