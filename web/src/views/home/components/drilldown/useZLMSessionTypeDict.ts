/**
 * 「运行态·网络会话类型」的字典注入层。
 *
 * 分工（⛔ 别合并）：
 * - `zlmSessionType.ts` —— 纯函数，只认"传进来的一张表"，可脱离 Vue 单测。
 * - 本文件 —— 唯一的 Vue 侧出口，从 `sys_dict` 取 `zlm_session_type` 注入给纯函数。
 */
import { useDictLabelMap } from "@/hooks/useDictOptions";
import { DICT_CODE_ZLM_SESSION_TYPE, ZLM_SESSION_TYPE_LABEL_FALLBACK, zlmSessionTypeLabel } from "./zlmSessionType";

/** 会话类型文案（字典 `zlm_session_type` 驱动；字典缺失时回落纯函数里的兜底表）。 */
export function useZLMSessionTypeLabel(): (value: string | null | undefined) => string {
  const labels = useDictLabelMap(DICT_CODE_ZLM_SESSION_TYPE, ZLM_SESSION_TYPE_LABEL_FALLBACK);
  return (value: string | null | undefined) => zlmSessionTypeLabel(value, labels.value);
}
