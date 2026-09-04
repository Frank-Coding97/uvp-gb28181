import { describe, expect, it } from "vitest";
import { buildDashboardTrendSpec } from "./dashboardTrend";

describe("dashboard drilldown trend spec", () => {
  it("keeps unknown SIP buckets null so VChart breaks the area", () => {
    const spec = buildDashboardTrendSpec("sip-rpm", {
      range: "1h", from: "", to: "", bucketSeconds: 60, timezone: "UTC", status: "partial", coverage: "partial",
      points: [{ bucketStart: "10:00", requests: null, transactions: null, success: null, failure: null, rpm: null }],
      ledger: [], gaps: [], todayRequests: 0, rollingRequests: 0
    });
    expect(spec.type).toBe("area");
    expect((spec.data?.[0] as { values: Array<{ value: number | null }> }).values[0].value).toBeNull();
    expect(spec).toMatchObject({ invalidType: "break" });
  });

  it("renders upstream and downstream as two traffic series", () => {
    const spec = buildDashboardTrendSpec("media-traffic-today", {
      range: "24h", from: "", to: "", bucketSeconds: 3600, timezone: "UTC", status: "ok", coverage: "complete",
      points: [{ bucketStart: "10:00", upstreamBytes: 1, downstreamBytes: 2 }], summary: { upstreamBytes: 1, downstreamBytes: 2 },
      ledger: { rows: [], total: 0, page: 1, pageSize: 20 }, gaps: []
    });
    const values = (spec.data?.[0] as { values: Array<{ series: string }> }).values;
    expect(values.map(item => item.series)).toEqual(["上行", "下行"]);
  });

  it("uses compact x-axis labels and reserves bottom space so time is not clipped", () => {
    const spec = buildDashboardTrendSpec("sip-rpm", {
      range: "24h", from: "", to: "", bucketSeconds: 300, timezone: "Asia/Shanghai", status: "ok", coverage: "complete",
      points: [{ bucketStart: "2026-09-04T10:05:00+08:00", requests: 5, transactions: 5, success: 5, failure: 0, rpm: 1 }],
      ledger: [], gaps: [], todayRequests: 5, rollingRequests: 5
    });
    const values = (spec.data?.[0] as { values: Array<{ bucketLabel: string }> }).values;
    expect(spec.xField).toBe("bucketLabel");
    expect(values[0].bucketLabel).toBe("10:05");
    expect(spec.axes?.[0]).toMatchObject({ orient: "bottom", label: { autoRotate: false, autoHide: true } });
    expect(spec.padding).toMatchObject({ bottom: 24 });
  });
});
