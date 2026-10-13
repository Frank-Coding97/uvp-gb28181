/**
 * 「在线状态」字典的 Vue 注入层 —— 把字典读出来喂给 `deviceStatus.ts` 的纯函数。
 * 纯模块保持不认识 Vue/pinia，单测可以直接跑（同 `useVideoParamDict` 的分层）。
 */
import { useDictLabelMap } from "@/hooks/useDictOptions";
import { DEVICE_STATUS_LABEL_FALLBACK, DICT_CODE_DEVICE_STATUS, deviceStatusLabelFrom } from "./deviceStatus";

/**
 * 页面里取一次即可：`const deviceStatusLabel = useDeviceStatusLabel();`
 *
 * ⛔ 传进来的值请按**原站点的极性**转好：
 *   `record.online ? "在线" : "离线"`          ⇒ `deviceStatusLabel(!!record.online)`
 *   `options?.device.online === false ? …`     ⇒ `deviceStatusLabel(options?.device.online !== false)`
 *   `record.status === 1 ? "在线" : "离线"`     ⇒ `deviceStatusLabel(record.status === 1)`
 *   设备自报字符串（`"online" | "offline" | undefined`）⇒ `deviceStatusLabel(state, "未上报")`
 */
export function useDeviceStatusLabel(unknownText = ""): (value: unknown) => string {
  const labels = useDictLabelMap(DICT_CODE_DEVICE_STATUS, DEVICE_STATUS_LABEL_FALLBACK);
  return (value: unknown): string => deviceStatusLabelFrom(labels.value, value, unknownText);
}
