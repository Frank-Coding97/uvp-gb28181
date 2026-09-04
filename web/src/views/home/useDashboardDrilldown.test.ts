import { describe, expect, it, vi } from "vitest";
import { useDashboardDrilldown, type DashboardDrilldownLoader } from "./useDashboardDrilldown";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail; });
  return { promise, resolve, reject };
}

function response(range: "1h" | "24h" | "7d") {
  return {
    status: "ok" as const, asOf: "now", scope: { type: "platform" as const }, coverage: "complete" as const,
    data: { range, from: "", to: "", bucketSeconds: 60, timezone: "UTC", status: "ok" as const, coverage: "complete" as const,
      points: [], ledger: [], gaps: [], todayRequests: 0, rollingRequests: 0 }
  };
}

describe("dashboard drilldown request state", () => {
  it("defaults to 24h and ignores a stale slow response", async () => {
    const first = deferred<ReturnType<typeof response>>();
    const second = deferred<ReturnType<typeof response>>();
    const loader = vi.fn<DashboardDrilldownLoader>()
      .mockImplementationOnce(() => first.promise)
      .mockImplementationOnce(() => second.promise);
    const state = useDashboardDrilldown(loader);
    const opening = state.open("sip-rpm");
    expect(state.range.value).toBe("24h");
    const switching = state.setRange("1h");
    expect(loader.mock.calls[0][2].aborted).toBe(true);
    second.resolve(response("1h"));
    await switching;
    first.resolve(response("24h"));
    await opening;
    expect(state.result.value?.data.range).toBe("1h");
  });

  it("aborts on close and retains last good data as stale after refresh failure", async () => {
    const loader = vi.fn<DashboardDrilldownLoader>()
      .mockResolvedValueOnce(response("24h"))
      .mockRejectedValueOnce(new Error("network"));
    const state = useDashboardDrilldown(loader);
    await state.open("sip-today");
    await state.reload();
    expect(state.stale.value).toBe(true);
    expect(state.result.value?.data.range).toBe("24h");
    state.close();
    expect(loader.mock.calls.at(-1)?.[2].aborted).toBe(true);
    expect(state.visible.value).toBe(false);
  });

  it("never sends an unsupported traffic 1h request", async () => {
    const loader = vi.fn<DashboardDrilldownLoader>().mockResolvedValue(response("24h"));
    const state = useDashboardDrilldown(loader);
    await state.open("media-traffic-today");
    await state.setRange("1h");
    expect(loader).toHaveBeenCalledTimes(1);
    expect(state.ranges.value).toEqual(["24h", "7d"]);
  });
});
