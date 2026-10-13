import { describe, expect, it } from "vitest";

import {
  emptySnapshotFilters,
  formatCapturedAt,
  formatSnapshotMd5,
  formatSnapshotSize,
  mayViewSnapshotLibrary,
  normalizeSnapshotQuery,
  resolveSnapshotSource,
  snapshotChannelLabel,
  snapshotChannelReportedName,
  snapshotContentImageUrl,
  snapshotDeviceLabel,
  snapshotDownloadName,
  snapshotFiltersFromRoute,
  snapshotQueryFromFilters,
  snapshotShowsReportedName,
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
      sessionId: "sess-1",
      keyword: ""
    });

    // vue-router 会把重复 query 聚成数组；取第一个而不是渲染成 "a,b"。
    expect(snapshotFiltersFromRoute({ deviceCode: ["37010301021320000511", "other"] }).deviceCode).toBe("37010301021320000511");
    expect(snapshotFiltersFromRoute({}).sessionId).toBe("");
    expect(snapshotFiltersFromRoute({ source: "alien" }).source).toBe("");
    // 关键词同样要吃数组/undefined 这两种噪声（深链是外部进来的，不可控）。
    expect(snapshotFiltersFromRoute({ keyword: ["东门", "西门"] }).keyword).toBe("东门");
    expect(snapshotFiltersFromRoute({ keyword: undefined }).keyword).toBe("");
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

describe("download naming", () => {
  it("keeps a usable extension so the saved file opens", () => {
    expect(snapshotDownloadName({ id: 1, fileName: "gate-001.jpg" })).toBe("gate-001.jpg");
    expect(snapshotDownloadName({ id: 1, fileName: "gate-001.JPEG" })).toBe("gate-001.JPEG");
    // ⛔ 没有扩展名时兜一个 jpg：看图软件靠扩展名判类型，无后缀文件双击打不开。
    expect(snapshotDownloadName({ id: 7, fileName: "gate-001" })).toBe("gate-001.jpg");
  });

  it("scrubs characters that make the browser silently truncate the name", () => {
    // ⛔ 带 "/" 的 filename 会被浏览器截断成前半段、后半段丢掉，且不报错。
    expect(snapshotDownloadName({ id: 1, fileName: "a/b:c.jpg" })).not.toMatch(/[\\/:*?"<>|]/);
  });

  it("scrubs control characters without embedding them in the source", () => {
    // ⛔ 这里曾经写成裸的 `\u0000-\u001f`：控制字符被直接嵌进 .ts 源文件，
    // 文件变成二进制（编辑器和构建工具都可能读不出来），而正则本身看不出问题。
    // 必须用 \u0000-\u001f 转义写法。
    expect(snapshotDownloadName({ id: 1, fileName: "a\u0000b\u0001c.jpg" })).toBe("a_b_c.jpg");
    expect(snapshotDownloadName({ id: 1, fileName: "a\nb.jpg" })).toBe("a_b.jpg");
  });

  it("falls back to the id when the file name is missing", () => {
    expect(snapshotDownloadName({ id: 42, fileName: "" })).toBe("snapshot-42.jpg");
    expect(snapshotDownloadName({ id: 42, fileName: null })).toBe("snapshot-42.jpg");
    expect(snapshotDownloadName({ id: 42 })).toBe("snapshot-42.jpg");
  });

  it("shows a short md5 but never blanks it out", () => {
    expect(formatSnapshotMd5("d41d8cd98f00b204e9800998ecf8427e")).toBe("d41d8cd9…427e");
    expect(formatSnapshotMd5("")).toBe("-");
    expect(formatSnapshotMd5(undefined)).toBe("-");
  });
});

describe("keyword search is delegated to the backend", () => {
  it("sends a trimmed keyword and drops it when blank", () => {
    const filters = { ...emptySnapshotFilters(), keyword: "  东门 " };
    // ⛔ 必须进请求参数：分页在后端，页内过滤会让「在第 7 页」和「不存在」长得一样。
    expect(normalizeSnapshotQuery(filters, 1, 10).keyword).toBe("东门");
    expect(normalizeSnapshotQuery({ ...emptySnapshotFilters(), keyword: "   " }, 1, 10).keyword).toBeUndefined();
  });

  it("round-trips the keyword through the URL so links can be shared", () => {
    const filters = { ...emptySnapshotFilters(), keyword: "东门" };
    expect(snapshotQueryFromFilters(filters).keyword).toBe("东门");
    expect(snapshotFiltersFromRoute(snapshotQueryFromFilters(filters)).keyword).toBe("东门");
  });
});

describe("filter context survives a refresh and a share", () => {
  it("keeps the time window in the URL", () => {
    // ⛔ 原来只有会话/编码进 URL，于是"我这两天找的那批图"这条最关键的条件在
    // 分享链接时丢掉了 —— 对方看到的是全量列表，还以为你就是这个意思。
    const filters = {
      ...emptySnapshotFilters(),
      range: ["2026-09-19T00:00:00Z", "2026-09-20T23:59:59Z"]
    };
    const query = snapshotQueryFromFilters(filters);
    expect(query.from).toBe("2026-09-19T00:00:00Z");
    expect(query.to).toBe("2026-09-20T23:59:59Z");
    expect(snapshotFiltersFromRoute(query).range).toEqual(filters.range);
  });

  it("keeps source and codes in the URL", () => {
    const filters = {
      ...emptySnapshotFilters(),
      source: "zlm" as const,
      deviceCode: "37010301021320000311"
    };
    const restored = snapshotFiltersFromRoute(snapshotQueryFromFilters(filters));
    expect(restored.source).toBe("zlm");
    expect(restored.deviceCode).toBe("37010301021320000311");
  });

  it("writes nothing for empty filters so shared links stay short", () => {
    expect(snapshotQueryFromFilters(emptySnapshotFilters())).toEqual({});
  });

  it("drops a half-filled time window instead of sending a broken one", () => {
    // 后端 from/to 是 AND 关系且都要求合法时间；只回填一端会让"半个时间窗"看着像用户有意为之。
    expect(snapshotFiltersFromRoute({ from: "2026-09-19T00:00:00Z" }).range).toEqual([]);
  });
});

describe("card labels put the human-given name first", () => {
  // ⛔ 别名必须在最前：现场人员嘴里的"东门那个枪机"只对应 alias，
  // 而 channelName 是设备上报的厂家串（"IPC-HFW2431S"）。
  // 只显示上报名的话，卡片上没有一个字是人能认出来的。
  it("prefers the alias over the reported name and the code", () => {
    expect(snapshotChannelLabel({ channelAlias: "东门枪机", channelName: "IPC-HFW2431S", channelCode: "3701...12" })).toBe(
      "东门枪机"
    );
    expect(snapshotDeviceLabel({ deviceAlias: "一号机房NVR", deviceName: "NVR", deviceCode: "3701...11" })).toBe("一号机房NVR");
  });

  it("falls back name → code → placeholder as the alias goes missing", () => {
    expect(snapshotChannelLabel({ channelAlias: "", channelName: "IPC-HFW2431S" })).toBe("IPC-HFW2431S");
    expect(snapshotChannelLabel({ channelAlias: "", channelName: "", channelCode: "3701...12" })).toBe("3701...12");
    expect(snapshotChannelLabel({})).toBe("未知通道");
    expect(snapshotDeviceLabel({})).toBe("未知设备");
  });

  it("treats a blank alias as absent instead of rendering an empty title", () => {
    // ⛔ 用户清空别名时可能留下一串空格，而空格是 truthy：
    // `channelAlias || channelName` 会被骗过去，标题渲染成一片空白，
    // 看着像"这条数据没名称"。
    expect(snapshotChannelLabel({ channelAlias: "   ", channelName: "IPC-HFW2431S" })).toBe("IPC-HFW2431S");
    expect(snapshotDeviceLabel({ deviceAlias: "  ", deviceName: "NVR" })).toBe("NVR");
  });

  it("keeps the reported name beside the alias only when they really differ", () => {
    // 相同就只显示一个：并排列出会让人以为是两个不同的设备。
    expect(snapshotShowsReportedName({ channelAlias: "东门枪机", channelName: "东门枪机" })).toBe(false);
    expect(snapshotShowsReportedName({ channelAlias: "东门枪机", channelName: "IPC-HFW2431S" })).toBe(true);
    // 没有别名时上报名就是主标题，不必再重复一遍。
    expect(snapshotShowsReportedName({ channelAlias: "", channelName: "IPC-HFW2431S" })).toBe(false);
    expect(snapshotShowsReportedName({ channelName: "" })).toBe(false);
  });

  it("still names the source when the reported name is blank", () => {
    expect(snapshotChannelReportedName({ channelName: "", channelCode: "3701...12" })).toBe("3701...12");
    expect(snapshotChannelReportedName({})).toBe("");
  });
});
