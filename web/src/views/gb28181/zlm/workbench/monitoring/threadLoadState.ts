import type { ZLMThreadLoad } from "@/api/gb28181-zlm-runtime";

export type EventThreadTone = "normal" | "warning" | "danger";

export interface EventThreadLoadSummary {
  count: number;
  average: number;
  peak: number;
  highCount: number;
}

export interface EventThreadLoadDistributionBucket {
  key: EventThreadTone;
  label: string;
  range: string;
  count: number;
  ratio: number;
}

function normalizedLoad(load: number): number {
  return Number.isFinite(load) ? Math.min(100, Math.max(0, load)) : 0;
}

export function summarizeEventThreadLoads(loads: readonly ZLMThreadLoad[]): EventThreadLoadSummary {
  if (loads.length === 0) return { count: 0, average: 0, peak: 0, highCount: 0 };
  const values = loads.map(thread => normalizedLoad(thread.load));
  return {
    count: values.length,
    average: Math.round(values.reduce((sum, load) => sum + load, 0) / values.length),
    peak: Math.round(Math.max(...values)),
    highCount: values.filter(load => load >= 80).length
  };
}

export function eventThreadTone(load: number): EventThreadTone {
  const normalized = normalizedLoad(load);
  if (normalized >= 80) return "danger";
  if (normalized >= 50) return "warning";
  return "normal";
}

export function eventThreadIndex(thread: ZLMThreadLoad, fallbackIndex: number): string {
  return thread.name.match(/(\d+)\s*$/)?.[1] ?? String(fallbackIndex);
}

export function eventThreadLoadCSSValue(load: number): string {
  return String(normalizedLoad(load) / 100);
}

export function eventThreadLoadCSSPercent(load: number): string {
  return `${normalizedLoad(load)}%`;
}

export function eventThreadLoadDistribution(
  loads: readonly ZLMThreadLoad[]
): EventThreadLoadDistributionBucket[] {
  const total = loads.length;
  const count = (tone: EventThreadTone) => loads.filter(thread => eventThreadTone(thread.load) === tone).length;
  const buckets: Array<Omit<EventThreadLoadDistributionBucket, "ratio">> = [
    { key: "normal", label: "正常", range: "0–49%", count: count("normal") },
    { key: "warning", label: "关注", range: "50–79%", count: count("warning") },
    { key: "danger", label: "高负载", range: "80–100%", count: count("danger") }
  ];
  return buckets.map(bucket => ({
    ...bucket,
    ratio: total === 0 ? 0 : Math.round(bucket.count * 100 / total)
  }));
}

export function busiestEventThreads(
  loads: readonly ZLMThreadLoad[],
  limit = 5
): ZLMThreadLoad[] {
  return [...loads]
    .sort((left, right) => normalizedLoad(right.load) - normalizedLoad(left.load))
    .slice(0, Math.max(0, Math.floor(limit)));
}

export function eventThreadHeatmapColumns(count: number): number {
  if (count <= 16) return Math.max(1, count);
  if (count <= 48) return 16;
  if (count <= 96) return 24;
  return 32;
}
