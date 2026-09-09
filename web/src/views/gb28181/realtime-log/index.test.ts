import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";

const source = readFileSync("src/views/gb28181/realtime-log/index.vue", "utf8");

describe("realtime business log console contract", () => {
  it("keeps the client bounded and exposes incident filters", () => {
    expect(source).toContain("events.value.length > 2000");
    for (const key of ["deviceId", "channelId", "module", "nodeId", "streamId", "callId", "requestId", "operationId", "correlationId", "event", "level"]) {
      expect(source).toContain(`filter.${key}`);
    }
  });

  it("shows reconnect, gap, copy and SIP trace actions", () => {
    for (const token of ["reconnecting", "limited", "gapMessage", "copyEvent", "openTrace", "/gb28181/sip-traces"]) {
      expect(source).toContain(token);
    }
  });
});
