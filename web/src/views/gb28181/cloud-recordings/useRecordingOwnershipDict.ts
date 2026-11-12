/**
 * 「录像运行态·持有态」的字典注入层。
 *
 * 分工（⛔ 别合并）：
 * - `recordingRuntimeState.ts` —— 纯函数，只认"传进来的一张表"，可脱离 Vue 单测。
 * - 本文件 —— 唯一的 Vue 侧出口，从 `sys_dict` 取 `recording_holder_state` 注入。
 *
 * ⛔ 只翻译**持有态本身**（谁在占用这路流）。同文件里 `recordingStatusPresentation`
 *   那类"复合结论"（如「ZLM 正在录制，手工归属未知」）是**派生状态**，由码值组合而来，
 *   必须留在代码里 —— 那不是字典能表达的"一码一名"。
 */
import { useDictLabelMap } from "@/hooks/useDictOptions";
import { DICT_CODE_RECORDING_HOLDER_STATE, OWNERSHIP_TYPE_LABEL_FALLBACK } from "./recordingRuntimeState";
import type { ComputedRef } from "vue";

/** 持有态查表（`{type: 名字}`），字典缺失时回落纯函数里的兜底表。 */
export function useRecordingOwnershipLabels(): ComputedRef<Record<string, string>> {
  return useDictLabelMap(DICT_CODE_RECORDING_HOLDER_STATE, OWNERSHIP_TYPE_LABEL_FALLBACK);
}
