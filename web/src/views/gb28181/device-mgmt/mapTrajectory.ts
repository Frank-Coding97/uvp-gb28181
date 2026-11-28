/**
 * 轨迹相关的纯计算：里程、回放插值、GeoJSON 转换、空结果文案。
 *
 * 抽成纯模块的理由和 channelPositionForm 一样：这些分支在界面上肉眼分不清 ——
 * "回放停住了"到底是没插值对、还是点本来就重合、还是播放到末尾，只有用例能钉住。
 *
 * ## 坐标系
 * 全链路用 WGS-84 原始经纬度（GB28181 MobilePosition 上报的就是这个），底图是
 * MapLibre + OpenFreeMap，不做任何 GCJ-02 偏移。⛔ 将来若换成高德/腾讯底图，
 * 必须在**渲染层**统一转换，绝不能在数据层动手 —— 否则同一批坐标会在轨迹和
 * 标记点之间错开几百米。
 */

export interface TrackPoint {
  eventTime: string;
  longitude: number;
  latitude: number;
  speed?: number;
  direction?: number;
  altitude?: number;
}

const EARTH_RADIUS_M = 6371008.8;

/** 两点间大圆距离（米）。 */
export function distanceMeters(a: { longitude: number; latitude: number }, b: { longitude: number; latitude: number }) {
  const toRad = (value: number) => (value * Math.PI) / 180;
  const dLat = toRad(b.latitude - a.latitude);
  const dLng = toRad(b.longitude - a.longitude);
  const lat1 = toRad(a.latitude);
  const lat2 = toRad(b.latitude);
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(lat1) * Math.cos(lat2) * Math.sin(dLng / 2) ** 2;
  return 2 * EARTH_RADIUS_M * Math.asin(Math.min(1, Math.sqrt(h)));
}

/**
 * 轨迹总里程（米）。
 *
 * ⛔ 逐段累加而不是"首尾直线距离"：执法仪的路线往往是往返/绕行，
 * 直线距离会把 5 公里的巡逻压缩成 300 米。
 */
export function trajectoryDistanceMeters(points: TrackPoint[]): number {
  if (points.length < 2) return 0;
  let total = 0;
  for (let i = 1; i < points.length; i += 1) {
    total += distanceMeters(points[i - 1], points[i]);
  }
  return total;
}

/** 沿轨迹的累计长度表（长度 = points.length，首项 0）。 */
export function cumulativeDistances(points: TrackPoint[]): number[] {
  const out = new Array<number>(points.length);
  if (!points.length) return out;
  out[0] = 0;
  for (let i = 1; i < points.length; i += 1) {
    out[i] = out[i - 1] + distanceMeters(points[i - 1], points[i]);
  }
  return out;
}

export interface TrackPosition {
  longitude: number;
  latitude: number;
  /** 已走过的里程（米），用于进度条文案。 */
  traveledMeters: number;
  /** 当前位于第几段之后（0 表示停在第一个点）。 */
  segmentIndex: number;
  /** 该位置的插值时间（ISO），用于界面显示"回放到几点"。 */
  eventTime: string;
}

/**
 * 按**里程比例**在轨迹上插值（不是按点序号）。
 *
 * 为什么必须按里程：设备上报频率并不均匀（停车时可能几十秒一条、行驶时 5 秒一条），
 * 按序号插值会让回放"在堵车路段飞快、在畅通路段猛冲"，看起来像播放器卡了。
 * 按里程插值则是匀速——这正是用户对"回放一段行驶过程"的直觉预期。
 *
 * @param progress 0~1，越界会被夹紧
 */
export function interpolateAlongTrajectory(points: TrackPoint[], progress: number): TrackPosition | null {
  if (!points.length) return null;
  if (points.length === 1) {
    const only = points[0];
    return {
      longitude: only.longitude,
      latitude: only.latitude,
      traveledMeters: 0,
      segmentIndex: 0,
      eventTime: only.eventTime
    };
  }
  const clamped = Number.isFinite(progress) ? Math.min(1, Math.max(0, progress)) : 0;
  const cum = cumulativeDistances(points);
  const total = cum[cum.length - 1];
  // 所有点重合：总长为 0，任何比例都落在原点 —— 直接返回最后一个点，
  // 避免 0/0 得到 NaN 之后 MapLibre 把标记点丢到 (0,0)。
  if (total <= 0) {
    const last = points[points.length - 1];
    return {
      longitude: last.longitude,
      latitude: last.latitude,
      traveledMeters: 0,
      segmentIndex: points.length - 1,
      eventTime: last.eventTime
    };
  }
  const target = clamped * total;
  let segment = 0;
  while (segment < cum.length - 2 && cum[segment + 1] < target) segment += 1;

  const segmentStart = cum[segment];
  const segmentLength = cum[segment + 1] - segmentStart;
  const ratio = segmentLength > 0 ? (target - segmentStart) / segmentLength : 0;
  const from = points[segment];
  const to = points[segment + 1];
  const fromTime = new Date(from.eventTime).getTime();
  const toTime = new Date(to.eventTime).getTime();
  const eventTime =
    Number.isFinite(fromTime) && Number.isFinite(toTime) && toTime >= fromTime
      ? new Date(fromTime + (toTime - fromTime) * ratio).toISOString()
      : from.eventTime;

  return {
    longitude: from.longitude + (to.longitude - from.longitude) * ratio,
    latitude: from.latitude + (to.latitude - from.latitude) * ratio,
    traveledMeters: target,
    segmentIndex: segment,
    eventTime
  };
}

/** 轨迹转 GeoJSON LineString。点数 < 2 时返回 null（一条线至少要两个点）。 */
export function trajectoryLineFeature(points: TrackPoint[]) {
  if (points.length < 2) return null;
  return {
    type: "Feature" as const,
    properties: {},
    geometry: {
      type: "LineString" as const,
      coordinates: points.map(point => [point.longitude, point.latitude])
    }
  };
}

/** 起止点 GeoJSON。⛔ 只在有轨迹时给，否则会在地图左上角画两个 (0,0) 的点。 */
export function trajectoryEndpoints(points: TrackPoint[]) {
  if (!points.length) return null;
  const first = points[0];
  const last = points[points.length - 1];
  return {
    start: { longitude: first.longitude, latitude: first.latitude },
    end: { longitude: last.longitude, latitude: last.latitude }
  };
}

export interface EmptyTrajectoryContext {
  /** 服务端「位置历史记录」总开关。 */
  historyEnabled: boolean;
  /** 通道当前是否有坐标（有坐标说明它至少上报过位置或人工录过）。 */
  hasCoordinate: boolean;
}

/**
 * 空轨迹的说明文案。
 *
 * ⛔ 三种空必须说成三句不同的话，统一成"暂无轨迹"会让用户分别去查三个不相干的地方：
 *   - 开关关着 → 是平台没记录，去服务配置打开；
 *   - 通道连坐标都没有 → 这台设备根本没上报过位置（固定枪机就是这种）；
 *   - 两者都正常 → 只是这个时间段没动/没上报，换个时间段再看。
 */
export function describeEmptyTrajectory(context: EmptyTrajectoryContext): string {
  if (!context.historyEnabled) {
    return "位置历史记录未开启，平台没有保存轨迹。请到「服务配置」打开位置历史后再查看。";
  }
  if (!context.hasCoordinate) {
    return "该通道没有位置上报记录。固定摄像机不会上报经纬度，轨迹功能只适用于执法仪、车载等移动设备。";
  }
  return "该时间段内没有位置记录，设备可能处于静止或未上报状态，换个时间段再试。";
}

/** 把米格式化成界面文案（<1km 用米，否则保留一位小数）。 */
export function formatDistance(meters: number): string {
  if (!Number.isFinite(meters) || meters <= 0) return "0 米";
  if (meters < 1000) return `${Math.round(meters)} 米`;
  return `${(meters / 1000).toFixed(1)} 公里`;
}
