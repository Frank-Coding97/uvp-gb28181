export interface BanTTLStep {
  score: number;
  ttl: number;
}

function durationLabel(seconds: number) {
  if (seconds >= 3600 && seconds % 3600 === 0) return `${seconds / 3600} 小时`;
  if (seconds >= 60 && seconds % 60 === 0) return `${seconds / 60} 分钟`;
  return `${seconds} 秒`;
}

export function formatRemaining(createdAt: string, ttl: number, now = Date.now()) {
  if (!Number.isFinite(ttl) || ttl <= 0) return "未设置到期时间";
  const ttlMs = ttl >= 1_000_000_000 ? ttl / 1_000_000 : ttl * 1_000;
  const remaining = Math.max(0, new Date(createdAt).getTime() + ttlMs - now);
  if (remaining <= 0) return "已过期";
  const minutes = Math.floor(remaining / 60000);
  return minutes >= 60 ? `剩余 ${Math.floor(minutes / 60)} 小时` : `剩余 ${Math.max(1, minutes)} 分钟`;
}

export function formatAutomaticBanTTL(steps: BanTTLStep[]) {
  const finite = steps.map(step => step.ttl).filter(ttl => Number.isFinite(ttl) && ttl > 0).sort((left, right) => left - right);
  if (!finite.length) return "策略读取中";
  if (finite.length === 1) return durationLabel(finite[0]);
  return `${durationLabel(finite[0])}起，最高 ${durationLabel(finite[finite.length - 1])}`;
}
