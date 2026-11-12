/**
 * 「视频参数属性」三张码表的**字典注入层**。
 *
 * 分工（⛔ 别把它们合并）：
 * - `videoParamCodec.ts` —— 纯函数，只认一张"传进来的表"，可脱离 Vue 单测。
 * - 本文件 —— 唯一的 Vue 侧出口，从 `sys_dict` 取 `video_format` /
 *   `video_resolution` / `bit_rate_type`，注入给上面的纯函数 / 下拉。
 *
 * ⛔ 字典只改**展示名**，绝不参与值域校验（`isValidVideoFormat` 仍是写死白名单）——
 *   本仓的 `device_record_cache` 与视频参数都吃过"把展示串当逻辑输入"的亏：
 *   现场改个字典名，对账逻辑就静默失效。
 *
 * 兜底：字典没加载（未登录 / 接口失败）时回落 `videoParamCodec` 里与种子逐字对齐的常量。
 */
import { type ComputedRef } from "vue";
import { useDictLabelMap, useDictOptions, type DictOption } from "@/hooks/useDictOptions";
import {
  BIT_RATE_TYPE_LABEL_FALLBACK,
  DICT_CODE_BIT_RATE_TYPE,
  DICT_CODE_VIDEO_FORMAT,
  DICT_CODE_VIDEO_RESOLUTION,
  RESOLUTION_LABEL_FALLBACK,
  VIDEO_FORMAT_LABEL_FALLBACK,
  bitRateTypeText,
  resolutionText,
  videoFormatText,
  type VideoParamLabelTable
} from "./videoParamCodec";

/** `{value: label}` 查表 → 下拉选项。⛔ 数字键在 JS 里本就按数值升序，顺序即协议码序。 */
function optionsFromTable(table: VideoParamLabelTable): DictOption[] {
  return Object.entries(table).map(([value, label]) => ({ value, label }));
}

/** 视频编码格式下拉（字典 `video_format`，兜底 1=MPEG-4 … 5=H.265）。 */
export function useVideoFormatOptions(): ComputedRef<DictOption[]> {
  return useDictOptions(DICT_CODE_VIDEO_FORMAT, optionsFromTable(VIDEO_FORMAT_LABEL_FALLBACK));
}

/** 分辨率下拉（字典 `video_resolution`，兜底 1=QCIF … 6=1080P）。 */
export function useVideoResolutionOptions(): ComputedRef<DictOption[]> {
  return useDictOptions(DICT_CODE_VIDEO_RESOLUTION, optionsFromTable(RESOLUTION_LABEL_FALLBACK));
}

/** 码率类型下拉（字典 `bit_rate_type`，兜底 1=CBR / 2=VBR）。 */
export function useBitRateTypeOptions(): ComputedRef<DictOption[]> {
  return useDictOptions(DICT_CODE_BIT_RATE_TYPE, optionsFromTable(BIT_RATE_TYPE_LABEL_FALLBACK));
}

export interface VideoParamLabelFns {
  videoFormatText: (value: string | null | undefined) => string;
  resolutionText: (value: string | null | undefined) => string;
  bitRateTypeText: (value: string | null | undefined) => string;
}

/**
 * 三个翻译函数（字典驱动）。页面里取一次，往下当"人读串"用。
 * ⛔ 与 `videoFormatCodecToken` / `resolutionPixels` 严格分工：那两个走**码值**，
 *   是对账逻辑用的；本函数只出**展示文案**。
 */
export function useVideoParamLabels(): VideoParamLabelFns {
  const formatLabels = useDictLabelMap(DICT_CODE_VIDEO_FORMAT, VIDEO_FORMAT_LABEL_FALLBACK);
  const resolutionLabels = useDictLabelMap(DICT_CODE_VIDEO_RESOLUTION, RESOLUTION_LABEL_FALLBACK);
  const bitRateLabels = useDictLabelMap(DICT_CODE_BIT_RATE_TYPE, BIT_RATE_TYPE_LABEL_FALLBACK);
  return {
    videoFormatText: (value: string | null | undefined) => videoFormatText(value, formatLabels.value),
    resolutionText: (value: string | null | undefined) => resolutionText(value, resolutionLabels.value),
    bitRateTypeText: (value: string | null | undefined) => bitRateTypeText(value, bitRateLabels.value)
  };
}
