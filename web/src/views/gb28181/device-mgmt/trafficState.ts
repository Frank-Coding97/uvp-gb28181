export function formatBytes(value: number | null | undefined): string {
  const bytes = Number(value || 0);
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const scaled = bytes / 1024 ** index;
  return `${scaled >= 100 || index === 0 ? scaled.toFixed(0) : scaled.toFixed(1)} ${units[index]}`;
}

export function latestRequestGuard() {
  let version = 0;
  return {
    next() {
      version += 1;
      return version;
    },
    current(value: number) {
      return value === version;
    },
    cancel() {
      version += 1;
    }
  };
}

export function coverageLabel(value?: string): string {
  if (value === "partial") return "存在采集缺口";
  if (value === "not_started") return "尚未开始统计";
  return "数据完整";
}

export interface RealtimeLike {
  upstreamBytesPerSecond: number;
  estimatedDownstreamBytesPerSec: number;
  downstreamBytesPerSecond?: number;
  downstreamMeasured?: boolean;
}

// 单条流的下行速率口径:节点(二开 ZLM)支持实测时用实测值,否则回退到
// 「上行码速 × 观看人数」的估算值 —— 估算值不计入已结算历史,两者绝不能混加
export function realtimeDownstreamSpeed(item: RealtimeLike): number {
  return item.downstreamMeasured ? (item.downstreamBytesPerSecond ?? 0) : item.estimatedDownstreamBytesPerSec;
}

export function summarizeRealtime(list: RealtimeLike[]) {
  return list.reduce(
    (result, item) => ({
      speed: result.speed + item.upstreamBytesPerSecond,
      downstreamSpeed: result.downstreamSpeed + realtimeDownstreamSpeed(item)
    }),
    { speed: 0, downstreamSpeed: 0 }
  );
}

// 只有全部流都是实测下行时,实时说明才不再标注「估算」
export function downstreamAllMeasured(list: RealtimeLike[]): boolean {
  return list.length > 0 && list.every(item => item.downstreamMeasured);
}
