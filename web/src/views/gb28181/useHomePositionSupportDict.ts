/**
 * 「看守位能力三态」的字典注入层。
 *
 * 分工（⛔ 别合并）：
 * - `homePositionSupport.ts` —— 纯函数，只认"传进来的一张表"，可脱离 Vue 单测。
 * - 本文件 —— 唯一的 Vue 侧出口，从 `sys_dict` 取 `home_position_support` 注入。
 *
 * ⛔ 只翻译**能力态本身**（支持 / 不支持 / 尚未确认）。同组件里 `homePresentation`
 *   那套复合结论（如「设备不支持看守位」「支持看守位，尚未配置」）是**派生状态**，
 *   由能力 × 查询阶段 × 已确认配置组合而来，必须留在代码里。
 */
import { useDictLabelMap } from "@/hooks/useDictOptions";
import {
  DICT_CODE_HOME_POSITION_SUPPORT,
  HOME_POSITION_SUPPORT_LABEL_FALLBACK,
  homePositionSupportLabel
} from "./homePositionSupport";

/** 能力态文案（字典 `home_position_support` 驱动；字典缺失时回落纯函数里的兜底表）。 */
export function useHomePositionSupportLabel(): (status: string | null | undefined) => string {
  const labels = useDictLabelMap(DICT_CODE_HOME_POSITION_SUPPORT, HOME_POSITION_SUPPORT_LABEL_FALLBACK);
  return (status: string | null | undefined) => homePositionSupportLabel(status, labels.value);
}
