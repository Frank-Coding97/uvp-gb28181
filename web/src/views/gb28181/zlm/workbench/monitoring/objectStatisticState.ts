import type { ZLMNodeRuntime, ZLMObjectStatistics } from "@/api/gb28181-zlm-runtime";

export interface ObjectStatisticSample {
  asOf: string;
  statistics: ZLMObjectStatistics;
}

const OBJECT_STATISTIC_SAMPLE_LIMIT = 60;

export function appendObjectStatisticSample(
  history: ObjectStatisticSample[],
  runtime: ZLMNodeRuntime,
  limit = OBJECT_STATISTIC_SAMPLE_LIMIT
): ObjectStatisticSample[] {
  if (!runtime.metricsComplete || !runtime.metrics.objectStatistics) return history;

  const retained = history.at(-1)?.asOf === runtime.asOf ? history.slice(0, -1) : history;
  const next = [...retained, {
    asOf: runtime.asOf,
    statistics: { ...runtime.metrics.objectStatistics }
  }];
  return next.slice(-Math.max(1, Math.floor(limit)));
}

export function objectStatisticTrend(
  history: ObjectStatisticSample[],
  key: keyof ZLMObjectStatistics
): number[] {
  return history
    .map(sample => sample.statistics[key])
    .filter(Number.isFinite);
}
