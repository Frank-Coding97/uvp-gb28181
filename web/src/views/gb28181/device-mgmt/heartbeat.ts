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

/** 条带上的一格：一格代表一段时间，格内可能压了多条心跳。 */
export interface HeartbeatBucket {
  index: number;
  from: number;
  to: number;
  /** 这一格里收到的（inbound）心跳条数 */
  total: number;
  /** 其中被判定为异常（403/其它拒收/无应答）的条数 */
  abnormal: number;
}

export interface HeartbeatTimeline {
  from: number;
  to: number;
  bucketMs: number;
  buckets: HeartbeatBucket[];
  /** 窗口内收到的心跳总条数 */
  total: number;
  /**
   * 窗口内「最后一条心跳」的时间（毫秒）。窗口内一条都没有时为 null ——
   * 这正是「设备早就掉线、今天才来看」的形态，必须单独说出来，不能让条带全灰了之。
   */
  lastAt: number | null;
  /** 最后一条心跳之后到窗口末尾的空白时长 */
  gapAfterLastMs: number | null;
  /** 最后一条心跳在条带里的相对位置 0~1（画断点用） */
  lastRatio: number | null;
  /** 这段空白是否值得标出来（太短就是正常抖动，标了反而误导） */
  gapIsNotable: boolean;
}

/**
 * 一格代表多长时间。
 *
 * 心跳通常 1 分钟一条，所以「一格一条」是最直观的：跨度 ≤2 小时时用 1 分钟一格，
 * 窗口里有多少分钟就有多少格。24 小时必须放宽到 10 分钟一格 —— 1440 格在屏幕上
 * 每格不足 0.5 像素，画出来只是一条糊线，那种密度的信息只能靠点击钻取。
 */
export function pickHeartbeatBucketMs(spanMs: number): number {
  const MINUTE = 60_000;
  if (spanMs <= 2 * 60 * MINUTE) return MINUTE;
  if (spanMs <= 6 * 60 * MINUTE) return 5 * MINUTE;
  if (spanMs <= 26 * 60 * MINUTE) return 10 * MINUTE;
  if (spanMs <= 48 * 60 * MINUTE) return 30 * MINUTE;
  return 60 * MINUTE;
}

/**
 * 把心跳摊到等宽时间格上。
 *
 * rows 是倒序（最新在前），这里不假设顺序，自己按时间落格。
 * 落不进窗口的行直接丢弃 —— 条带的横轴必须严格等于用户选的时间范围，
 * 否则「最后一段空白」会被窗口外的数据填掉，看起来像没掉线。
 */
export function buildHeartbeatTimeline(
  rows: HeartbeatRow[],
  fromMs: number,
  toMs: number,
  minNotableGapMs = 3 * 60 * 1000
): HeartbeatTimeline {
  const span = Math.max(0, toMs - fromMs);
  const bucketMs = pickHeartbeatBucketMs(span);
  const count = Math.max(1, Math.ceil(span / bucketMs));
  const buckets: HeartbeatBucket[] = Array.from({ length: count }, (_, index) => ({
    index,
    from: fromMs + index * bucketMs,
    to: fromMs + (index + 1) * bucketMs,
    total: 0,
    abnormal: 0
  }));

  let lastAt: number | null = null;
  let total = 0;
  for (const row of rows) {
    const at = timestamp(row.receivedAt);
    if (Number.isNaN(at) || at < fromMs || at >= toMs) continue;
    const index = Math.min(count - 1, Math.floor((at - fromMs) / bucketMs));
    buckets[index].total += 1;
    total += 1;
    if (row.kind !== "ok" && row.kind !== "pending_response") buckets[index].abnormal += 1;
    if (lastAt === null || at > lastAt) lastAt = at;
  }

  const gapAfterLastMs = lastAt === null ? null : Math.max(0, toMs - lastAt);
  // 阈值取「3 分钟」与「2 格」的较大者：1 小时视图里断 3 分钟就算掉线，
  // 24 小时视图里断 20 分钟仍可能只是网络抖动，不该标红。
  const notable = gapAfterLastMs !== null && span > 0 && gapAfterLastMs > Math.max(minNotableGapMs, bucketMs * 2);
  return {
    from: fromMs,
    to: toMs,
    bucketMs,
    buckets,
    total,
    lastAt,
    gapAfterLastMs,
    lastRatio: lastAt === null || span === 0 ? null : (lastAt - fromMs) / span,
    gapIsNotable: notable
  };
}

/** 一格多长，写成「1 分钟」这种给人看的说法 */
export function formatBucketSpan(ms: number): string {
  if (ms < 60_000) return `${Math.round(ms / 1000)} 秒`;
  const minutes = Math.round(ms / 60_000);
  if (minutes < 60) return `${minutes} 分钟`;
  const hours = Math.round(minutes / 60);
  return `${hours} 小时`;
}

/** 坐标轴刻度：跨天的窗口要带日期，否则「20:15 到 20:15」看不出是两天 */
export function formatAxisTime(ms: number, spanMs: number): string {
  const date = new Date(ms);
  if (Number.isNaN(date.getTime())) return "-";
  const pad = (part: number) => String(part).padStart(2, "0");
  const clock = `${pad(date.getHours())}:${pad(date.getMinutes())}`;
  if (spanMs <= 12 * 60 * 60 * 1000) return clock;
  return `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${clock}`;
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
