/**
 * 「多屏回放·槽位可播性」的字典注入层。
 *
 * 分工（⛔ 别合并）：
 * - `playbackAvailability.ts` —— 纯函数，只认"传进来的一张表"，可脱离 Vue 单测。
 * - 本文件 —— 唯一的 Vue 侧出口，从 `sys_dict` 取 `playable_state` 注入给纯函数。
 */
import { useDictLabelMap } from "@/hooks/useDictOptions";
import { DICT_CODE_PLAYABLE_STATE, PLAYABLE_STATE_LABEL_FALLBACK, playableStateLabel } from "./playbackAvailability";

/** 槽位可播性文案（字典 `playable_state` 驱动；字典缺失时回落纯函数里的兜底表）。 */
export function usePlayableStateLabel(): (value: string | null | undefined) => string {
  const labels = useDictLabelMap(DICT_CODE_PLAYABLE_STATE, PLAYABLE_STATE_LABEL_FALLBACK);
  return (value: string | null | undefined) => playableStateLabel(value, labels.value);
}
