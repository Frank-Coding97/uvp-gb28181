import { describe, expect, it } from "vitest";
import {
  cumulativeDistances,
  describeEmptyTrajectory,
  distanceMeters,
  formatDistance,
  interpolateAlongTrajectory,
  trajectoryDistanceMeters,
  trajectoryEndpoints,
  trajectoryLineFeature,
  type TrackPoint
} from "./mapTrajectory";

/** 一个"正东走 1 公里"的近似轨迹：赤道上 0.01 度经度 ≈ 1113 米。 */
function point(longitude: number, latitude: number, offsetSeconds = 0): TrackPoint {
  return {
    eventTime: new Date(Date.UTC(2026, 9, 21, 10, 0, offsetSeconds)).toISOString(),
    longitude,
    latitude
  };
}

describe("distanceMeters", () => {
  it("赤道上 1 度经度约 111.2 公里", () => {
    // 球面大圆距离（R=6371km）下赤道 1 度 ≈ 111195 米；
    // 不是经纬度平面换算的 111.32 公里 —— 那用的是 WGS-84 长半轴。
    const oneDegree = distanceMeters({ longitude: 0, latitude: 0 }, { longitude: 1, latitude: 0 });
    expect(oneDegree).toBeGreaterThan(111000);
    expect(oneDegree).toBeLessThan(111400);
  });

  it("同一点距离为 0", () => {
    expect(distanceMeters({ longitude: 116.4, latitude: 39.9 }, { longitude: 116.4, latitude: 39.9 })).toBe(0);
  });
});

describe("trajectoryDistanceMeters", () => {
  it("不足两个点时为 0", () => {
    expect(trajectoryDistanceMeters([])).toBe(0);
    expect(trajectoryDistanceMeters([point(116.4, 39.9)])).toBe(0);
  });

  it("逐段累加，往返路线不会被压成直线距离", () => {
    // 走出去 0.01 度再走回来：直线距离为 0，但实际走了两段。
    const outbound = trajectoryDistanceMeters([point(116.4, 39.9), point(116.41, 39.9)]);
    const roundTrip = trajectoryDistanceMeters([point(116.4, 39.9), point(116.41, 39.9), point(116.4, 39.9)]);
    expect(outbound).toBeGreaterThan(700);
    expect(roundTrip).toBeCloseTo(outbound * 2, 0);
  });
});

describe("cumulativeDistances", () => {
  it("首项恒为 0 且单调不减", () => {
    const cum = cumulativeDistances([point(116.4, 39.9), point(116.41, 39.9), point(116.42, 39.9)]);
    expect(cum[0]).toBe(0);
    expect(cum[1]).toBeGreaterThan(0);
    expect(cum[2]).toBeGreaterThan(cum[1]);
  });

  it("空轨迹返回空数组", () => {
    expect(cumulativeDistances([])).toEqual([]);
  });
});

describe("interpolateAlongTrajectory", () => {
  const points = [point(116.4, 39.9, 0), point(116.41, 39.9, 60), point(116.42, 39.9, 120)];

  it("空轨迹返回 null", () => {
    expect(interpolateAlongTrajectory([], 0.5)).toBeNull();
  });

  it("单点轨迹恒停在该点", () => {
    const only = interpolateAlongTrajectory([point(116.4, 39.9)], 0.7);
    expect(only?.longitude).toBe(116.4);
    expect(only?.traveledMeters).toBe(0);
  });

  it("0 与 1 分别落在首尾点", () => {
    expect(interpolateAlongTrajectory(points, 0)?.longitude).toBeCloseTo(116.4, 6);
    expect(interpolateAlongTrajectory(points, 1)?.longitude).toBeCloseTo(116.42, 6);
  });

  it("中点按里程落在两段之间", () => {
    const mid = interpolateAlongTrajectory(points, 0.5);
    expect(mid?.longitude).toBeCloseTo(116.41, 4);
    expect(mid?.latitude).toBeCloseTo(39.9, 6);
    // ⛔ 不断言 segmentIndex：两段等长时中点正好压在段边界上，
    //    归到第 0 段还是第 1 段取决于浮点舍入，这不是契约。
  });

  it("按里程而不是按序号插值：点距不均时慢速段不会被跳过", () => {
    // 前 2 个点相距极近(约 1 米)，第 3 个点远在 1 公里外。
    const uneven = [point(116.4, 39.9, 0), point(116.40001, 39.9, 10), point(116.41, 39.9, 20)];
    const half = interpolateAlongTrajectory(uneven, 0.5);
    // 按序号插值会停在第二个点(116.40001)；按里程应已走到大约一半路程(≈116.405)。
    expect(half!.longitude).toBeGreaterThanOrEqual(116.405);
  });

  it("进度越界被夹紧而不是产生 NaN", () => {
    expect(interpolateAlongTrajectory(points, -3)?.longitude).toBeCloseTo(116.4, 6);
    expect(interpolateAlongTrajectory(points, 9)?.longitude).toBeCloseTo(116.42, 6);
    const nan = interpolateAlongTrajectory(points, Number.NaN);
    expect(Number.isFinite(nan!.longitude)).toBe(true);
  });

  it("所有点重合时不返回 NaN 坐标", () => {
    const same = [point(116.4, 39.9, 0), point(116.4, 39.9, 30)];
    const pos = interpolateAlongTrajectory(same, 0.5);
    expect(pos?.longitude).toBe(116.4);
    expect(pos?.latitude).toBe(39.9);
  });

  it("插值时间落在相邻两点的时间区间内", () => {
    const mid = interpolateAlongTrajectory(points, 0.5);
    const t = new Date(mid!.eventTime).getTime();
    expect(t).toBeGreaterThanOrEqual(new Date(points[0].eventTime).getTime());
    expect(t).toBeLessThanOrEqual(new Date(points[2].eventTime).getTime());
  });
});

describe("trajectoryLineFeature", () => {
  it("少于两个点不生成线", () => {
    expect(trajectoryLineFeature([])).toBeNull();
    expect(trajectoryLineFeature([point(116.4, 39.9)])).toBeNull();
  });

  it("坐标按 [经度, 纬度] 顺序输出", () => {
    const feature = trajectoryLineFeature([point(116.4, 39.9), point(116.41, 39.91)]);
    expect(feature?.geometry.coordinates).toEqual([
      [116.4, 39.9],
      [116.41, 39.91]
    ]);
  });
});

describe("trajectoryEndpoints", () => {
  it("空轨迹不产生起止点", () => {
    expect(trajectoryEndpoints([])).toBeNull();
  });

  it("首尾点各归各位", () => {
    const ends = trajectoryEndpoints([point(116.4, 39.9), point(116.41, 39.91), point(116.42, 39.92)]);
    expect(ends?.start).toEqual({ longitude: 116.4, latitude: 39.9 });
    expect(ends?.end).toEqual({ longitude: 116.42, latitude: 39.92 });
  });
});

describe("describeEmptyTrajectory", () => {
  it("开关关着时指向服务配置", () => {
    expect(describeEmptyTrajectory({ historyEnabled: false, hasCoordinate: true })).toContain("服务配置");
  });

  it("开关开着但通道没坐标时说明是设备不上报", () => {
    expect(describeEmptyTrajectory({ historyEnabled: true, hasCoordinate: false })).toContain("移动设备");
  });

  it("两者都正常时说时间段内没记录", () => {
    expect(describeEmptyTrajectory({ historyEnabled: true, hasCoordinate: true })).toContain("时间段");
  });

  it("三种原因的文案互不相同", () => {
    const texts = [
      describeEmptyTrajectory({ historyEnabled: false, hasCoordinate: true }),
      describeEmptyTrajectory({ historyEnabled: true, hasCoordinate: false }),
      describeEmptyTrajectory({ historyEnabled: true, hasCoordinate: true })
    ];
    expect(new Set(texts).size).toBe(3);
  });
});

describe("formatDistance", () => {
  it("小于 1 公里用米", () => {
    expect(formatDistance(320.4)).toBe("320 米");
  });

  it("大于 1 公里保留一位小数", () => {
    expect(formatDistance(1543)).toBe("1.5 公里");
  });

  it("非法值归零", () => {
    expect(formatDistance(Number.NaN)).toBe("0 米");
    expect(formatDistance(-5)).toBe("0 米");
  });
});
