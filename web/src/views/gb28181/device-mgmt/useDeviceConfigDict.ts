/**
 * 设备配置族码值字典的 Vue 注入层 —— 把两个字典读出来喂给 `deviceConfigDict.ts` 的纯函数。
 * 纯模块保持不认识 Vue/pinia，单测可直接跑（同 `useChannelAttributeDict` 的分层）。
 *
 * ⛔ 本层只负责"取字典 + 白名单式改名"，**不碰 value** —— 这两个值是下发报文码值，
 *    动 value 等于改协议（见 `deviceConfigDict.ts` 文件头）。
 */
import { computed, type ComputedRef } from "vue";
import { useDictLabelMap } from "@/hooks/useDictOptions";
import {
  DICT_CODE_FRAME_MIRROR,
  DICT_CODE_STREAM_NUMBER,
  FRAME_MIRROR_LABEL_FALLBACK,
  STREAM_NUMBER_LABEL_FALLBACK,
  applyOptionLabels,
  streamNumberLabelFrom
} from "./deviceConfigDict";
import { MIRROR_OPTIONS, STREAM_NUMBER_OPTIONS, type ConfigSelectOption } from "./deviceConfigGroups";

/** 两个码值字典的已合并查表（`code` → `{value: label}`）。 */
export function useDeviceConfigDictLabels(): ComputedRef<Record<string, Record<string, string>>> {
  const mirror = useDictLabelMap(DICT_CODE_FRAME_MIRROR, FRAME_MIRROR_LABEL_FALLBACK);
  const stream = useDictLabelMap(DICT_CODE_STREAM_NUMBER, STREAM_NUMBER_LABEL_FALLBACK);
  return computed(() => ({
    [DICT_CODE_FRAME_MIRROR]: mirror.value,
    [DICT_CODE_STREAM_NUMBER]: stream.value
  }));
}

/** 画面镜像选项（顺序 / `shortLabel` / `value` 恒由 `MIRROR_OPTIONS` 锁死，只有名字可换）。 */
export function useFrameMirrorOptions(): ComputedRef<ConfigSelectOption[]> {
  const labels = useDictLabelMap(DICT_CODE_FRAME_MIRROR, FRAME_MIRROR_LABEL_FALLBACK);
  return computed(() => applyOptionLabels(MIRROR_OPTIONS, labels.value));
}

/** 录像码流选项（顺序 / `value` 恒由 `STREAM_NUMBER_OPTIONS` 锁死，只有名字可换）。 */
export function useStreamNumberOptions(): ComputedRef<ConfigSelectOption[]> {
  const labels = useDictLabelMap(DICT_CODE_STREAM_NUMBER, STREAM_NUMBER_LABEL_FALLBACK);
  return computed(() => applyOptionLabels(STREAM_NUMBER_OPTIONS, labels.value));
}

/**
 * 码流编号 → 名字（任意编号：0–3 走字典、超出走过程式兜底）。
 * 播放控制台「视频参数」卡按设备实际上报的码流数逐行取名，编号可能 > 3。
 */
export function useStreamNumberLabel(): (value: unknown) => string {
  const labels = useDictLabelMap(DICT_CODE_STREAM_NUMBER, STREAM_NUMBER_LABEL_FALLBACK);
  return (value: unknown): string => streamNumberLabelFrom(labels.value, value);
}
