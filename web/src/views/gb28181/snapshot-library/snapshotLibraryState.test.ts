import { describe, expect, it } from "vitest";

import {
  emptySnapshotFilters,
  formatCapturedAt,
  formatSnapshotSize,
  mayViewSnapshotLibrary,
  normalizeSnapshotQuery,
  resolveSnapshotSource,
  snapshotContentImageUrl,
  snapshotFiltersFromRoute,
  snapshotSourceLabel,
  SNAPSHOT_LIBRARY_PERMISSION
} from "./snapshotLibraryState";

describe("image library permission", () => {
  it("accepts the snapshot permission code and the全权 wildcard", () => {
    expect(mayViewSnapshotLibrary(["*:*:*"])).toBe(true);
    expect(mayViewSnapshotLibrary([SNAPSHOT_LIBRARY_PERMISSION])).toBe(true);
  });

  it("does not let neighbouring permissions open the page", () => {
    // ⛔ 用别的 gb28181 权限（哪怕是云台控制这种"看起来同级"的）必须为假：
    // 菜单可见性与两个接口的 casbin 授权都绑在 device:snapshot 上，前端放宽就是越权给入口。
    expect(mayViewSnapshotLibrary(["gb28181:ptz:view"])).toBe(false);
    expect(mayViewSnapshotLibrary(["gb28181:device:view"])).toBe(false);
    expect(mayViewSnapshotLibrary([])).toBe(false);
  });
});

describe("snapshot source", () => {
  it("only accepts the three sources the backend whitelists", () => {
    expect(resolveSnapshotSource("device")).toBe("device");
    expect(resolveSnapshotSource("zlm")).toBe("zlm");
    expect(resolveSnapshotSource("browser")).toBe("browser");
    // ⛔ 传错来源时后端直接报错（不是静默空列表），前端也不该把它当成一个有效选项发出去。
    expect(resolveSnapshotSource("alien")).toBe("");
    expect(resolveSnapshotSource(undefined)).toBe("");
    expect(resolveSnapshotSource(3)).toBe("");
  });

  it("renders unknown sources verbatim instead of blank", () => {
    expect(snapshotSourceLabel("device")).toBe("设备抓拍");
    expect(snapshotSourceLabel("zlm")).toBe("平台抓帧");
    // 留白会被读成"页面没渲染出来"，所以历史脏值原样显示。
    expect(snapshotSourceLabel("legacy-import")).toBe("legacy-import");
    expect(snapshotSourceLabel("")).toBe("未知来源");
  });
});

describe("query normalization", () => {
  it("drops every empty filter so the backend sees a bare list request", () => {
    expect(normalizeSnapshotQuery(emptySnapshotFilters(), 1, 40)).toEqual({ page: 1, pageSize: 40 });
  });

  it("trims codes and keeps only what was actually filled", () => {
    const query = normalizeSnapshotQuery(
      { ...emptySnapshotFilters(), deviceCode: "  37010301021320000511 ", channelCode: " 37010301021320000512 " },
      2,
      80
    );
    expect(query).toEqual({
      page: 2,
      pageSize: 80,
      deviceCode: "37010301021320000511",
      channelCode: "37010301021320000512"
    });
  });

  it("sends a half-open time window as-is", () => {
    // 只填起始时间是合法用法（"某时刻之后"），不该被补齐成 to=now。
    const onlyFrom = normalizeSnapshotQuery({ ...emptySnapshotFilters(), range: ["2026-09-20T00:00:00+08:00"] }, 1, 40);
    expect(onlyFrom.from).toBe("2026-09-20T00:00:00+08:00");
    expect(onlyFrom).not.toHaveProperty("to");

    const both = normalizeSnapshotQuery(
      { ...emptySnapshotFilters(), range: ["2026-09-20T00:00:00+08:00", "2026-09-21T00:00:00+08:00"] },
      1,
      40
    );
    expect(both.from).toBe("2026-09-20T00:00:00+08:00");
    expect(both.to).toBe("2026-09-21T00:00:00+08:00");
  });

  it("keeps the session filter, which is the only way to regroup one capture run", () => {
    const query = normalizeSnapshotQuery({ ...emptySnapshotFilters(), sessionId: " sess-1 " }, 1, 40);
    expect(query.sessionId).toBe("sess-1");
  });

  it("never sends an invalid source", () => {
    const query = normalizeSnapshotQuery({ ...emptySnapshotFilters(), source: "" }, 1, 40);
    expect(query).not.toHaveProperty("source");
  });
});

describe("route-seeded filters", () => {
  it("reads deep-link params and ignores array/undefined noise", () => {
    expect(snapshotFiltersFromRoute({ sessionId: "sess-1", channelCode: "37010301021320000512", source: "device" })).toEqual({
      deviceCode: "",
      channelCode: "37010301021320000512",
      source: "device",
      range: [],
      sessionId: "sess-1"
    });

    // vue-router 会把重复 query 聚成数组；取第一个而不是渲染成 "a,b"。
    expect(snapshotFiltersFromRoute({ deviceCode: ["37010301021320000511", "other"] }).deviceCode).toBe("37010301021320000511");
    expect(snapshotFiltersFromRoute({}).sessionId).toBe("");
    expect(snapshotFiltersFromRoute({ source: "alien" }).source).toBe("");
  });
});

describe("content image url", () => {
  const base = "/api/gb28181/device-mgmt/snapshots/12/content";

  it("appends the token because <img> cannot send an Authorization header", () => {
    expect(snapshotContentImageUrl(base, "abc123")).toBe(`${base}?token=abc123`);
  });

  it("leaves the url untouched when there is no token to add", () => {
    // 未登录/已登出时也返回可用路径，让请求去 401，而不是拼出 "?token=" 这种空参数。
    expect(snapshotContentImageUrl(base, "")).toBe(base);
    expect(snapshotContentImageUrl(base, undefined)).toBe(base);
  });

  it("uses & when the url already carries a query string", () => {
    expect(snapshotContentImageUrl(`${base}?v=2`, "abc123")).toBe(`${base}?v=2&token=abc123`);
  });

  it("prefixes the deploy base but never doubles it on absolute urls", () => {
    expect(snapshotContentImageUrl(base, "t", "/uvp")).toBe(`/uvp${base}?token=t`);
    expect(snapshotContentImageUrl(base, "t", "/uvp/")).toBe(`/uvp${base}?token=t`);
    expect(snapshotContentImageUrl("https://cdn.example.com/a.jpg", "t", "/uvp")).toBe("https://cdn.example.com/a.jpg?token=t");
  });

  it("percent-encodes the token", () => {
    expect(snapshotContentImageUrl(base, "a+b/c=")).toBe(`${base}?token=a%2Bb%2Fc%3D`);
  });

  it("returns an empty string for a missing url so the caller can branch on it", () => {
    expect(snapshotContentImageUrl(null, "t")).toBe("");
    expect(snapshotContentImageUrl("   ", "t")).toBe("");
  });
});

describe("formatting", () => {
  it("formats byte sizes across the three magnitudes", () => {
    expect(formatSnapshotSize(0)).toBe("0 B");
    expect(formatSnapshotSize(512)).toBe("512 B");
    expect(formatSnapshotSize(136273)).toBe("133.1 KB");
    expect(formatSnapshotSize(2.5 * 1024 * 1024)).toBe("2.50 MB");
    // 脏值不该渲染成 NaN。
    expect(formatSnapshotSize(null)).toBe("0 B");
    expect(formatSnapshotSize(Number.NaN)).toBe("0 B");
  });

  it("renders Go zero-time and garbage as a dash instead of a fake date", () => {
    expect(formatCapturedAt("0001-01-01T00:00:00Z")).toBe("-");
    expect(formatCapturedAt("not-a-time")).toBe("-");
    expect(formatCapturedAt("")).toBe("-");
    expect(formatCapturedAt(undefined)).toBe("-");
    expect(formatCapturedAt("2026-09-20T14:33:30+08:00")).not.toBe("-");
  });
});
