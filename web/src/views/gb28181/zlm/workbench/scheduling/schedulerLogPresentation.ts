const SECRET_ASSIGNMENT = /(?:api[-_]?secret|password|passwd|token|authorization|access[-_]?key|secret)\s*[:=]\s*[^\s,;]+/gi;
const URL_VALUE = /https?:\/\/[^\s,;]+/gi;

/** Keep backend diagnostics useful without rendering credentials or internal URLs. */
export function safeSchedulerError(value: unknown, maxLength = 180): string {
  const text = String(value ?? "").trim();
  if (!text) return "调度失败";
  const sanitized = text
    .replace(SECRET_ASSIGNMENT, "敏感参数=[已隐藏]")
    .replace(URL_VALUE, "[内部地址已隐藏]");
  return sanitized.length > maxLength ? `${sanitized.slice(0, maxLength)}…` : sanitized;
}

export function schedulerAlgorithmLabel(value: string): string {
  return ({
    roundrobin: "轮询",
    weighted: "加权轮询",
    leastload: "最小负载"
  } as Record<string, string>)[value] || value || "未知策略";
}
