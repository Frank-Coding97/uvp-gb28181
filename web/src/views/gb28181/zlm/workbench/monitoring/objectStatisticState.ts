import type { ZLMObjectStatistics, ZLMRuntimeTrendSample } from "@/api/gb28181-zlm-runtime";

export function objectStatisticTrend(
  history: ZLMRuntimeTrendSample[],
  key: keyof ZLMObjectStatistics
): number[] {
  return history
    .map(sample => sample.objectStatistics?.[key])
    .filter((value): value is number => typeof value === "number" && Number.isFinite(value));
}
