import type { TraceMessageSummary } from "@/api/gb28181-trace";

/**
 * 心跳记录页签的数据整形（纯函数，不碰网络）。
 *
 * 数据源是 **SIP 报文轨迹**（`gb_sip_trace_message`，business_code=keepalive），
 * 一次心跳在库里是两行：inbound 的 MESSAGE + 平台回的应答。
 * 平台是否接受这次心跳，**只体现在应答码上**：
 *   200 = 收下了；403 = 收下了但判定注册已过期（会回 4xx 逼设备重新注册）；
 *   其它 4xx/5xx = 其它拒收；没有应答 = 平台压根没回过。
 * 所以「结果」列必须由应答码推出，不能只看「收到了报文」。
 */

export type HeartbeatWindowKey = "1h" | "24h" | "7d";

export interface HeartbeatWindow {
  key: HeartbeatWindowKey;
  label: string;
  ms: number;
}

// 窗口上限受后端 MaxTraceQueryRange(8 天) 与报文保留期(retention_days) 双重约束。
export const HEARTBEAT_WINDOWS: HeartbeatWindow[] = [
  { key: "1h", label: "近 1 小时", ms: 60 * 60 * 1000 },
  { key: "24h", label: "近 24 小时", ms: 24 * 60 * 60 * 1000 },
  { key: "7d", label: "近 7 天", ms: 7 * 24 * 60 * 60 * 1000 }
];

export type HeartbeatResultKind = "ok" | "registration_expired" | "rejected" | "no_response" | "pending_response";

export interface HeartbeatRow {
  /** Call-ID 或（无 Call-ID 时的）报文 ID，用于列表 key */
  key: string;
  receivedAt: string;
  resultCode: number;
  kind: HeartbeatResultKind;
  latencyMs: number | null;
  /** 与「更早一条心跳」的间隔；最早一条或首屏边界上取不到则为 null */
  intervalMs: number | null;
  sourceAddr: string;
  transport: string;
  userAgent: string;
  callId: string;
  malformed: boolean;
}

export interface HeartbeatSummary {
  total: number;
  abnormal: number;
  latestAt: string | null;
  averageIntervalMs: number | null;
}

export function heartbeatWindowRange(key: HeartbeatWindowKey, now: Date = new Date()) {
  const window = HEARTBEAT_WINDOWS.find(item => item.key === key) ?? HEARTBEAT_WINDOWS[1];
  const to = now.getTime();
  return { from: new Date(to - window.ms).toISOString(), to: new Date(to).toISOString(), ms: window.ms };
}

function timestamp(value: string | undefined | null): number {
  if (!value) return Number.NaN;
  return new Date(value).getTime();
}

/** 应答码 → 结果分类。没有应答（平台未回）永远算异常。 */
export function classifyHeartbeat(resultCode: number, responded: boolean): HeartbeatResultKind {
  if (!responded || resultCode <= 0) return "no_response";
  if (resultCode === 200) return "ok";
  if (resultCode === 403) return "registration_expired";
  if (resultCode >= 300) return "rejected";
  // 1xx/2xx 非 200（如 202）在实际心跳里不出现，按正常处理但保留真实码
  return "ok";
}

export function heartbeatResultLabel(row: Pick<HeartbeatRow, "kind" | "resultCode">): string {
  switch (row.kind) {
    case "ok":
      return row.resultCode === 200 ? "正常接收" : `已应答 ${row.resultCode}`;
    case "registration_expired":
      return "注册已过期";
    case "rejected":
      return "被拒收";
    case "pending_response":
      return "应答待确认";
    default:
      return "无应答";
  }
}

/** 报文轨迹是**批量落库**的（flush_interval_ms），刚收到的心跳应答可能还没写进去。 */
export const RESULT_PENDING_WINDOW_MS = 5000;

/**
 * 把 inbound（每次心跳一条）与 outbound（平台应答）配成一行。
 * 只认带状态码的 outbound 报文：心跳的应答就是最终响应，重复应答取最早一条。
 */
export function buildHeartbeatRows(
  inbound: TraceMessageSummary[],
  outbound: TraceMessageSummary[],
  previousReceivedAt?: string | null,
  now: number = Date.now()
): HeartbeatRow[] {
  const responses = new Map<string, TraceMessageSummary>();
  for (const item of outbound) {
    if (!item.callId || item.statusCode <= 0) continue;
    const existing = responses.get(item.callId);
    if (!existing || timestamp(item.occurredAt) < timestamp(existing.occurredAt)) {
      responses.set(item.callId, item);
    }
  }

  const rows = inbound
    .filter(item => item.direction === "inbound")
    .slice()
    .sort((left, right) => timestamp(right.occurredAt) - timestamp(left.occurredAt))
    .map((item): HeartbeatRow => {
      const response = item.callId ? responses.get(item.callId) : undefined;
      const received = timestamp(item.occurredAt);
      const respondedAt = response ? timestamp(response.occurredAt) : Number.NaN;
      const resultCode = response ? response.statusCode : 0;
      // 刚落库的心跳可能「应答还没写进去」，这段时间内不能判成无应答 —— 那是假的异常。
      const kind: HeartbeatResultKind =
        !response && now - received < RESULT_PENDING_WINDOW_MS
          ? "pending_response"
          : classifyHeartbeat(resultCode, Boolean(response));
      return {
        key: item.callId || item.eventId,
        receivedAt: item.occurredAt,
        resultCode,
        kind,
        latencyMs: Number.isNaN(respondedAt) ? null : Math.max(0, respondedAt - received),
        intervalMs: null,
        sourceAddr: item.remoteAddr || "-",
        transport: item.transport || "-",
        userAgent: item.userAgent || "-",
        callId: item.callId,
        malformed: item.malformed
      } satisfies HeartbeatRow;
    });

  // 间隔按「相邻两条」算：列表是倒序，所以第 i 条要跟更早的第 i+1 条比；
  // 最后一行的「更早一条」在上一页里，由 previousReceivedAt 传进来（首屏没有就是 null）。
  for (let index = 0; index < rows.length; index += 1) {
    const older = index + 1 < rows.length ? timestamp(rows[index + 1].receivedAt) : timestamp(previousReceivedAt);
    if (Number.isNaN(older)) continue;
    rows[index].intervalMs = Math.max(0, timestamp(rows[index].receivedAt) - older);
  }
  return rows;
}

export function summarizeHeartbeats(rows: HeartbeatRow[]): HeartbeatSummary {
  if (rows.length === 0) {
    return { total: 0, abnormal: 0, latestAt: null, averageIntervalMs: null };
  }
  const intervals = rows.map(row => row.intervalMs).filter((value): value is number => value !== null && value > 0);
  const average = intervals.length === 0 ? null : Math.round(intervals.reduce((sum, value) => sum + value, 0) / intervals.length);
  return {
    total: rows.length,
    // 「应答待确认」是刚落库、应答还没写进去的正常状态，不能算异常，否则刷新一次吓一次。
    abnormal: rows.filter(row => row.kind !== "ok" && row.kind !== "pending_response").length,
    latestAt: rows[0].receivedAt,
    averageIntervalMs: average
  };
}

/** 毫秒 → 「1 分 05 秒」这类可读间隔 */
export function formatDuration(ms: number | null | undefined): string {
  if (ms === null || ms === undefined || Number.isNaN(ms)) return "-";
  if (ms < 1000) return `${Math.round(ms)} 毫秒`;
  const totalSeconds = Math.round(ms / 1000);
  if (totalSeconds < 60) return `${totalSeconds} 秒`;
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes < 60) return seconds ? `${minutes} 分 ${String(seconds).padStart(2, "0")} 秒` : `${minutes} 分`;
  const hours = Math.floor(minutes / 60);
  const restMinutes = minutes % 60;
  return restMinutes ? `${hours} 小时 ${restMinutes} 分` : `${hours} 小时`;
}

export function formatHeartbeatTime(value?: string | null): string {
  if (!value || value.startsWith("0001-01-01")) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "-";
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(
    date.getMinutes()
  )}:${pad(date.getSeconds())}`;
}

/**
 * 「报文收到了、但平台没算进心跳」的判据。
 *
 * 这正是线上踩过的那种死锁：设备每分钟发心跳、平台每分钟回 200，
 * 但心跳被判定为来源不符而丢弃 ⇒ `gb_device.keepalive_time` 停在过去，
 * 而报文库里最新一条却是「刚刚」。两边一对比就能看出来，是最硬的证据。
 */
export function heartbeatDriftWarning(
  latestReceivedAt: string | null,
  accountedKeepaliveAt: string | null | undefined,
  intervalMs: number | null
): string | null {
  if (!latestReceivedAt) return null;
  if (!accountedKeepaliveAt || accountedKeepaliveAt.startsWith("0001-01-01")) return null;
  const latest = timestamp(latestReceivedAt);
  const accounted = timestamp(accountedKeepaliveAt);
  if (Number.isNaN(latest) || Number.isNaN(accounted)) return null;
  // 容忍 3 个心跳周期，最少 3 分钟：避免刚开窗、刚恢复时的抖动误报。
  const tolerance = Math.max(3 * 60 * 1000, (intervalMs ?? 0) * 3);
  if (latest - accounted <= tolerance) return null;
  return `报文已收到（${formatHeartbeatTime(latestReceivedAt)}），但设备心跳时间停在 ${formatHeartbeatTime(
    accountedKeepaliveAt
  )}，说明平台收到报文却没有计入心跳`;
}
