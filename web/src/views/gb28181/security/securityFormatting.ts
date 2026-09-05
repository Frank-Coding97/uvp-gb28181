export interface BanTTLStep {
  score: number;
  ttl: number;
}

const securityReasonLabels: Record<string, string> = {
  unknown_method: "未知 SIP 方法",
  unknown_invite_rate: "短窗口未授权 INVITE",
  unauthorized_invite_accumulation: "10 分钟累计 10 次未授权 INVITE",
  server_id_mismatch: "Server-ID 不匹配",
  digest_failure: "Digest 鉴权失败",
  nonce_invalid: "Nonce 无效",
  nonce_expired: "Nonce 已过期",
  nonce_replay: "Nonce 重放",
  nonce_stale: "Nonce 已陈旧",
  unregistered_message: "未注册 MESSAGE",
  packet_too_large: "SIP 报文过大",
  connection_rate: "连接速率过高",
  manual_blacklist: "手动黑名单",
  active_ban: "已有自动封禁"
};
const inviteRateScore = 20;

export function formatSecurityReason(reason: string) {
  return securityReasonLabels[reason] || reason;
}

export function formatShortWindowInviteRule(window?: number, banScore?: number) {
  if (typeof window !== "number" || !Number.isFinite(window) || window <= 0 || typeof banScore !== "number" || !Number.isFinite(banScore) || banScore <= 0) {
    return "短期 INVITE 规则读取中";
  }
  return `${window} 秒内 ${Math.ceil(banScore / inviteRateScore)} 次未授权 INVITE`;
}

function durationLabel(seconds: number) {
  if (seconds >= 3600 && seconds % 3600 === 0) return `${seconds / 3600} 小时`;
  if (seconds >= 60 && seconds % 60 === 0) return `${seconds / 60} 分钟`;
  return `${seconds} 秒`;
}

export function formatRemaining(createdAt: string, ttl: number, now = Date.now(), permanent = false) {
  if (permanent) return "永久封禁";
  if (!Number.isFinite(ttl) || ttl <= 0) return "未设置到期时间";
  const ttlMs = ttl >= 1_000_000_000 ? ttl / 1_000_000 : ttl * 1_000;
  const remaining = Math.max(0, new Date(createdAt).getTime() + ttlMs - now);
  if (remaining <= 0) return "已过期";
  const minutes = Math.floor(remaining / 60000);
  return minutes >= 60 ? `剩余 ${Math.floor(minutes / 60)} 小时` : `剩余 ${Math.max(1, minutes)} 分钟`;
}

export function formatAutomaticBanTTL(steps: BanTTLStep[], permanentAutoBan = false, banScore?: number) {
  const hasPermanentStep = permanentAutoBan && steps.some(step => step.score === banScore && step.ttl === 0);
  if (hasPermanentStep) return "永久封禁（需人工解封）";
  const finite = steps.map(step => step.ttl).filter(ttl => Number.isFinite(ttl) && ttl > 0).sort((left, right) => left - right);
  if (!finite.length) return "策略读取中";
  if (finite.length === 1) return durationLabel(finite[0]);
  return `${durationLabel(finite[0])}起，最高 ${durationLabel(finite[finite.length - 1])}`;
}
