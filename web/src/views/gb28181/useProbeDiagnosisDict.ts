/**
 * 「逐帧健康检测·诊断结论」的字典注入层。
 *
 * 分工（⛔ 别合并）：
 * - `probeDiagnosis.ts` —— 纯函数，只认"传进来的一张表"，可脱离 Vue 单测。
 * - 本文件 —— 唯一的 Vue 侧出口，从 `sys_dict` 取三张表注入给纯函数。
 *
 * ⛔ 只翻译三处「一码一名」：状态徽章 / 到达节奏 / issue 标题。
 *   同文件里 `verdict`（定责结论）与 `issue.focus`（排查方向）都是**由码值组合出来的
 *   整句结论**，属派生状态，必须留在代码里 —— 那不是字典能表达的"一码一名"。
 *   也正因如此，本文件不合并那两类文案。
 */
import { computed, type ComputedRef } from "vue";
import { useDictLabelMap } from "@/hooks/useDictOptions";
import {
  DICT_CODE_PROBE_ARRIVAL,
  DICT_CODE_PROBE_ISSUE_CODE,
  DICT_CODE_PROBE_STATUS,
  PROBE_ARRIVAL_LABEL_FALLBACK,
  PROBE_ISSUE_TITLE_FALLBACK,
  PROBE_STATUS_LABEL_FALLBACK,
  type ProbeDiagnosisLabels
} from "./probeDiagnosis";

/** 三张展示表打包返回（字典缺失时各自回落纯函数里的兜底表）。 */
export function useProbeDiagnosisLabels(): ComputedRef<ProbeDiagnosisLabels> {
  const status = useDictLabelMap(DICT_CODE_PROBE_STATUS, PROBE_STATUS_LABEL_FALLBACK);
  const arrival = useDictLabelMap(DICT_CODE_PROBE_ARRIVAL, PROBE_ARRIVAL_LABEL_FALLBACK);
  const issueTitle = useDictLabelMap(DICT_CODE_PROBE_ISSUE_CODE, PROBE_ISSUE_TITLE_FALLBACK);
  return computed<ProbeDiagnosisLabels>(() => ({
    status: status.value,
    arrival: arrival.value,
    issueTitle: issueTitle.value
  }));
}
