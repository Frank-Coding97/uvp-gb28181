import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it, vi } from "vitest";
import { handleDashboardCardDrilldownKeydown, handleHistoryDrilldownKeydown, openDashboardCardDrilldown, openHistoryDrilldown } from "./dashboardCardDrilldown";

describe("home dashboard card drilldown", () => {
  it("opens all four history cards in browse mode and never in edit mode", () => {
    const open = vi.fn();
    for (const id of ["sip-rpm", "sip-today", "play-success-24h", "media-traffic-today"] as const) openHistoryDrilldown(id, false, open);
    expect(open.mock.calls.map(call => call[0])).toEqual(["sip-rpm", "sip-today", "play-success-24h", "media-traffic-today"]);
    openHistoryDrilldown("sip-rpm", true, open);
    expect(open).toHaveBeenCalledTimes(4);
  });

  it("treats Enter and Space like click and prevents Space scrolling", () => {
    const open = vi.fn();
    const enter = new KeyboardEvent("keydown", { key: "Enter", cancelable: true });
    const space = new KeyboardEvent("keydown", { key: " ", cancelable: true });
    handleHistoryDrilldownKeydown(enter, "sip-rpm", false, open);
    handleHistoryDrilldownKeydown(space, "sip-today", false, open);
    expect(open).toHaveBeenCalledTimes(2);
    expect(enter.defaultPrevented).toBe(true);
    expect(space.defaultPrevented).toBe(true);
  });

  it("stops traffic direction controls from bubbling into the card", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/home/home.vue"), "utf8");
    expect(source).toContain('@click.stop="trafficDirection = \'upstream\'"');
    expect(source).toContain('@click.stop="trafficDirection = \'downstream\'"');
    expect(source).toContain("handleDashboardCardDrilldownKeydown");
    expect(source).toContain("<DashboardDrilldownDialog");
  });

  it("opens media runtime on streams by default and supports keyboard without editing", () => {
    const openHistory = vi.fn(), openMedia = vi.fn();
    openDashboardCardDrilldown("media-runtime", false, openHistory, openMedia);
    expect(openMedia).toHaveBeenCalledOnce();
    expect(openHistory).not.toHaveBeenCalled();
    const space = new KeyboardEvent("keydown", { key: " ", cancelable: true });
    handleDashboardCardDrilldownKeydown(space, "media-runtime", false, openHistory, openMedia);
    expect(openMedia).toHaveBeenCalledTimes(2);
    expect(space.defaultPrevented).toBe(true);
    openDashboardCardDrilldown("media-runtime", true, openHistory, openMedia);
    expect(openMedia).toHaveBeenCalledTimes(2);
  });

  it("wires four media values as stop-propagation buttons using the shared ledger", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/home/home.vue"), "utf8");
    for (const kind of ["streams", "viewers", "sessions", "recordings"]) {
      expect(source).toContain(`@click.stop="openMediaRuntimeLedger('${kind}')"`);
      expect(source).toContain(`mediaRuntimeLedger.totals.${kind}`);
    }
    const ledgerSource = readFileSync(resolve(process.cwd(), "src/views/home/components/drilldown/MediaRuntimeLedgerDialog.vue"), "utf8");
    expect(ledgerSource).not.toContain("getZLM");
    expect(ledgerSource).not.toContain("@/api/");
  });
});
