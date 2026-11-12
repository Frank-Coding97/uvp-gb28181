/**
 * 地图默认视野（中心点）的归一与校验。
 *
 * 存在 `sys_param` 里、全平台一份；前端拿到的永远是"可能为 null 的一份 JSON"，
 * 所以这里要把三种脏情形都收敛掉：
 *   ① 后端返回 null（从未设置）；
 *   ② 字段缺失/非数字（手改过库、旧版本写入）；
 *   ③ 值域越界（经纬度 0/0 在本仓表示"无坐标"，zoom 超出地图能力）。
 * 任何一条不满足都回退内置默认视野 —— 地图必须能打开，不能因为一条配置坏掉就白屏。
 */

export interface MapCenter {
  longitude: number;
  latitude: number;
  zoom: number;
}

/** 与 index.vue 里 mapMinZoom / mapMaxZoom 保持同一对边界。 */
export const MAP_MIN_ZOOM = 5;
export const MAP_MAX_ZOOM = 22;

/** 未设置时的兜底视野（与历史行为一致：北京、zoom 10）。 */
export const MAP_FALLBACK_CENTER: MapCenter = {
  longitude: 116.3974,
  latitude: 39.9093,
  zoom: 10
};

function isValidCoordinate(longitude: number, latitude: number) {
  if (!Number.isFinite(longitude) || !Number.isFinite(latitude)) return false;
  if (longitude < -180 || longitude > 180) return false;
  if (latitude < -90 || latitude > 90) return false;
  // 0/0 在整条链路上都表示"没有坐标"，不能当成一个真实中心点。
  return !(longitude === 0 && latitude === 0);
}

/** 后端返回值 → 可用视野；任何不可用情形返回 null，由调用方回退。 */
export function normalizeMapCenter(raw: unknown): MapCenter | null {
  if (!raw || typeof raw !== "object") return null;
  const source = raw as Record<string, unknown>;
  const longitude = Number(source.longitude);
  const latitude = Number(source.latitude);
  const zoom = Number(source.zoom);
  if (!isValidCoordinate(longitude, latitude)) return null;
  if (!Number.isFinite(zoom)) return null;
  if (zoom < MAP_MIN_ZOOM || zoom > MAP_MAX_ZOOM) return null;
  return { longitude, latitude, zoom };
}

/**
 * 从地图当前相机取一份可保存的视野。
 *
 * ⛔ zoom 不做四舍五入：MapLibre 的缩放本来就是小数，取整会让用户
 * "明明设的是这个大小，回来却变了"。
 */
export function buildMapCenter(camera: { longitude: number; latitude: number; zoom: number }): MapCenter {
  const zoom = Math.min(MAP_MAX_ZOOM, Math.max(MAP_MIN_ZOOM, camera.zoom));
  return {
    longitude: Number(camera.longitude.toFixed(6)),
    latitude: Number(camera.latitude.toFixed(6)),
    zoom: Number(zoom.toFixed(2))
  };
}

/** 界面上的中心点文案（保留 5 位小数，约 1 米精度）。 */
export function formatMapCenter(center: MapCenter): string {
  return `${center.longitude.toFixed(5)}, ${center.latitude.toFixed(5)} · Z${center.zoom.toFixed(1)}`;
}
