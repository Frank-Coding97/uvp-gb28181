import { describe, expect, it } from "vitest";
import { MAP_FALLBACK_CENTER, MAP_MAX_ZOOM, MAP_MIN_ZOOM, buildMapCenter, formatMapCenter, normalizeMapCenter } from "./mapView";

describe("normalizeMapCenter", () => {
  it("从未设置(null)返回 null，由调用方回退默认视野", () => {
    expect(normalizeMapCenter(null)).toBeNull();
    expect(normalizeMapCenter(undefined)).toBeNull();
  });

  it("合法值原样返回", () => {
    expect(normalizeMapCenter({ longitude: 116.99505, latitude: 36.66237, zoom: 12.5 })).toEqual({
      longitude: 116.99505,
      latitude: 36.66237,
      zoom: 12.5
    });
  });

  it("缺字段或非数字一律视为未设置", () => {
    expect(normalizeMapCenter({ longitude: 116.4, latitude: 39.9 })).toBeNull();
    expect(normalizeMapCenter({ longitude: "东经", latitude: 39.9, zoom: 10 })).toBeNull();
  });

  it("0/0 视为无坐标而不是真实中心", () => {
    expect(normalizeMapCenter({ longitude: 0, latitude: 0, zoom: 10 })).toBeNull();
  });

  it("经纬度越界或 zoom 越界都回退", () => {
    expect(normalizeMapCenter({ longitude: 181, latitude: 39.9, zoom: 10 })).toBeNull();
    expect(normalizeMapCenter({ longitude: 116.4, latitude: -91, zoom: 10 })).toBeNull();
    expect(normalizeMapCenter({ longitude: 116.4, latitude: 39.9, zoom: 1 })).toBeNull();
    expect(normalizeMapCenter({ longitude: 116.4, latitude: 39.9, zoom: 23 })).toBeNull();
  });

  it("边界值本身是合法的", () => {
    expect(normalizeMapCenter({ longitude: 180, latitude: 90, zoom: MAP_MAX_ZOOM })).not.toBeNull();
    expect(normalizeMapCenter({ longitude: -180, latitude: -90, zoom: MAP_MIN_ZOOM })).not.toBeNull();
  });
});

describe("buildMapCenter", () => {
  it("zoom 被夹在合法区间内", () => {
    expect(buildMapCenter({ longitude: 116.4, latitude: 39.9, zoom: 99 }).zoom).toBe(MAP_MAX_ZOOM);
    expect(buildMapCenter({ longitude: 116.4, latitude: 39.9, zoom: 0 }).zoom).toBe(MAP_MIN_ZOOM);
  });

  it("保留小数缩放，不做取整", () => {
    expect(buildMapCenter({ longitude: 116.4, latitude: 39.9, zoom: 11.46 }).zoom).toBe(11.46);
  });

  it("经纬度截到 6 位小数（约 0.1 米精度）", () => {
    const center = buildMapCenter({ longitude: 116.9950512345, latitude: 36.6623712345, zoom: 12 });
    expect(center.longitude).toBe(116.995051);
    expect(center.latitude).toBe(36.662371);
  });
});

describe("formatMapCenter", () => {
  it("按 5 位小数展示，带缩放级别", () => {
    expect(formatMapCenter({ longitude: 116.995051, latitude: 36.662371, zoom: 12.5 })).toBe("116.99505, 36.66237 · Z12.5");
  });
});

describe("MAP_FALLBACK_CENTER", () => {
  it("兜底视野自身必须能通过校验，否则等于没有兜底", () => {
    expect(normalizeMapCenter(MAP_FALLBACK_CENTER)).toEqual(MAP_FALLBACK_CENTER);
  });
});
