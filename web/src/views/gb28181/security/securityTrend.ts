import type { SecurityEventAggregate } from "@/api/gb28181-security";

export type SecurityTrendPeriod = "1h" | "24h" | "7d";
export type SecurityTrendSeries = "识别" | "封禁";

export interface SecurityTrendPoint {
  time: string;
  value: number;
  series: SecurityTrendSeries;
}

interface TrendBucket {
  start: number;
  detected: number;
  blocked: number;
}

const PERIOD_CONFIG: Record<SecurityTrendPeriod, { windowMs: number; bucketMs: number; label: (time: number) => string }> = {
  "1h": { windowMs: 60 * 60 * 1000, bucketMs: 10 * 60 * 1000, label: formatClock },
  "24h": { windowMs: 24 * 60 * 60 * 1000, bucketMs: 2 * 60 * 60 * 1000, label: time => `${formatDate(time)} ${formatClock(time)}` },
  "7d": { windowMs: 7 * 24 * 60 * 60 * 1000, bucketMs: 24 * 60 * 60 * 1000, label: formatDate }
};

export function buildSecurityTrend(events: SecurityEventAggregate[], period: SecurityTrendPeriod, now = Date.now()): { points: SecurityTrendPoint[]; hasData: boolean } {
  const config = PERIOD_CONFIG[period];
  const windowStart = now - config.windowMs;
  const firstBucket = Math.floor(windowStart / config.bucketMs) * config.bucketMs;
  const bucketCount = Math.ceil(config.windowMs / config.bucketMs);
  const buckets = new Map<number, TrendBucket>();

  for (let index = 0; index <= bucketCount; index += 1) {
    const start = firstBucket + index * config.bucketMs;
    buckets.set(start, { start, detected: 0, blocked: 0 });
  }

  for (const event of events) {
    const occurredAt = Date.parse(event.lastSeenAt || event.bucketAt);
    if (!Number.isFinite(occurredAt) || occurredAt < windowStart || occurredAt > now) continue;
    const start = Math.floor(occurredAt / config.bucketMs) * config.bucketMs;
    const bucket = buckets.get(start);
    if (!bucket) continue;
    const count = Math.max(0, Number(event.count) || 0);
    bucket.detected += count;
    if (event.action === "ban") bucket.blocked += count;
  }

  const points = Array.from(buckets.values()).flatMap(bucket => [
    { time: config.label(bucket.start), value: bucket.detected, series: "识别" as const },
    { time: config.label(bucket.start), value: bucket.blocked, series: "封禁" as const }
  ]);
  return { points, hasData: points.some(point => point.value > 0) };
}

function formatClock(timestamp: number) {
  return new Date(timestamp).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: false });
}

function formatDate(timestamp: number) {
  return new Date(timestamp).toLocaleDateString([], { month: "2-digit", day: "2-digit" });
}
