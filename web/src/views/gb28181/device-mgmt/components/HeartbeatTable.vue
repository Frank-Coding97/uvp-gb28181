<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { fetchTraceHealth, listTraceMessages, type TraceMessageQuery, type TraceMessageSummary } from "@/api/gb28181-trace";
import {
  HEARTBEAT_WINDOWS,
  buildHeartbeatRows,
  buildHeartbeatTimeline,
  formatAxisTime,
  formatBucketSpan,
  formatDuration,
  formatHeartbeatTime,
  heartbeatDriftWarning,
  heartbeatResultLabel,
  heartbeatWindowRange,
  summarizeHeartbeats,
  type HeartbeatBucket,
  type HeartbeatRow,
  type HeartbeatWindowKey
} from "../heartbeat";

/**
 * 心跳记录（证据向）：把平台实际收到的每一条心跳、以及平台给它的应答码列成表，
 * 上方再给一条连续性条带 —— 条带负责「一眼看出哪段断了」，表格负责「断的那一刻到底是什么」。
 *
 * 数据源是 SIP 报文轨迹里的 keepalive 报文（一问一答两行）：
 * 一次心跳 = inbound MESSAGE + 平台应答，应答码是「平台到底认没认这次心跳」的唯一凭据。
 * 所以本页只做读展示，不落任何新表 —— 报文轨迹本来就在记，重复存一份只会让两边对不上。
 */
const props = defineProps<{
  deviceId: string;
  /** 平台记账里的心跳时间（gb_device.keepalive_time），用来识别「收到了却没算数」 */
  accountedKeepaliveAt?: string | null;
}>();

/** 表格每次多显示的条数（数据已全量在内存里，翻页不再发请求） */
const PAGE_SIZE = 20;
/** 单次请求上限，与后端 MaxMessagePageSize 对齐 */
const FETCH_LIMIT = 500;
/** 条带最多覆盖的跨度：再大就退化成一堆糊在一起的像素，改成只显示表格 */
const MAX_TIMELINE_SPAN_MS = 26 * 60 * 60 * 1000;

const windowKey = ref<HeartbeatWindowKey>("24h");
const rows = ref<HeartbeatRow[]>([]);
const loading = ref(false);
const error = ref("");
const disabledReason = ref("");
const visibleCount = ref(PAGE_SIZE);
const truncated = ref(false);
const requestVersion = ref(0);
/** 点条带某格后放大到的时间段；null 表示用顶部选的窗口 */
const drilledRange = ref<{ from: string; to: string } | null>(null);
/**
 * 当前窗口内一条心跳都没有时，往前探到的「最后一次心跳」。
 *
 * 设备昨天傍晚掉线、今天才来看，24 小时窗口里可能就是空的 ——
 * 这时候只说「没有心跳」等于什么都没说，得把上次见到它的时刻捞出来。
 */
const lastBeforeWindow = ref<{ at: string; gapMs: number } | null>(null);
/** 往前探是否已经出结果：区分「还在探」和「探过、7 天内真没有」 */
const lastProbeDone = ref(false);

const activeRange = computed(() => {
  const from = drilledRange.value ? new Date(drilledRange.value.from).getTime() : Number.NaN;
  const to = drilledRange.value ? new Date(drilledRange.value.to).getTime() : Number.NaN;
  if (Number.isFinite(from) && Number.isFinite(to) && to > from) {
    return { from: drilledRange.value!.from, to: drilledRange.value!.to, ms: to - from };
  }
  return heartbeatWindowRange(windowKey.value);
});

const windowLabel = computed(() => HEARTBEAT_WINDOWS.find(item => item.key === windowKey.value)?.label ?? "所选时间范围");

/** 跨度太大时不给条带：一格几十像素的假精度比没有更误导 */
const timeline = computed(() => {
  const range = activeRange.value;
  if (range.ms > MAX_TIMELINE_SPAN_MS) return null;
  return buildHeartbeatTimeline(rows.value, new Date(range.from).getTime(), new Date(range.to).getTime());
});

const visibleRows = computed(() => rows.value.slice(0, visibleCount.value));
const hasMoreRows = computed(() => rows.value.length > visibleCount.value);
const summary = computed(() => summarizeHeartbeats(rows.value));
const driftWarning = computed(() =>
  heartbeatDriftWarning(summary.value.latestAt, props.accountedKeepaliveAt, summary.value.averageIntervalMs)
);

/** 尾部断档说明：设备「昨天掉线、今天来看」时，这一句就是答案 */
const gapNote = computed(() => {
  const current = timeline.value;
  if (!current) return "";
  if (current.total === 0) return "";
  if (!current.gapIsNotable || current.lastAt === null) return "";
  const silence = Date.now() - current.lastAt;
  return `最后一条心跳在 ${formatHeartbeatTime(new Date(current.lastAt).toISOString())}，距今已 ${formatDuration(
    silence
  )} 没有新的心跳。`;
});

// 列定义放脚本里（与 sip-log 的表格同一写法）：列顺序/宽度集中一处，测试也能直接断言列。
const columns = [
  { title: "收到时间", dataIndex: "receivedAt", slotName: "received", width: 164 },
  { title: "平台判定", dataIndex: "kind", slotName: "result", width: 112 },
  { title: "应答耗时", dataIndex: "latencyMs", slotName: "latency", width: 92 },
  { title: "间隔", dataIndex: "intervalMs", slotName: "interval", width: 92 },
  { title: "来源地址", dataIndex: "sourceAddr", slotName: "source", width: 168 },
  { title: "传输", dataIndex: "transport", width: 70 },
  { title: "客户端", dataIndex: "userAgent", slotName: "agent", width: 180, ellipsis: true, tooltip: true }
];

function isAbnormal(row: HeartbeatRow) {
  // 「应答待确认」不算异常：那是刚落库、应答还没写进去的正常中间态。
  return row.kind !== "ok" && row.kind !== "pending_response";
}

function rowClass(record: HeartbeatRow) {
  return isAbnormal(record) ? "heartbeat-row--abnormal" : "";
}

/** 一次请求最多 FETCH_LIMIT 条：心跳是每分钟一条，24 小时就有 1440 条，必须自己翻页取全。 */
async function fetchPages(params: TraceMessageQuery, maxPages: number, failureText: string) {
  const items: TraceMessageSummary[] = [];
  let cursor = "";
  for (let page = 0; page < maxPages; page += 1) {
    const result = await listTraceMessages({ ...params, limit: FETCH_LIMIT, cursor: cursor || undefined });
    if (result.code !== 0) throw new Error(result.message || failureText);
    items.push(...(result.data?.items ?? []));
    cursor = result.data?.nextCursor ?? "";
    if (!cursor) return { items, truncated: false };
  }
  // 翻到上限还没到底：宁可少显示，也不能悄悄把「更早还有」当成「没有了」。
  return { items, truncated: true };
}

async function loadResponses(inbound: TraceMessageSummary[]) {
  const times = inbound.map(item => new Date(item.occurredAt).getTime()).filter(value => !Number.isNaN(value));
  if (times.length === 0) return { items: [], truncated: false };
  // 应答总在请求之后几毫秒内，所以按这批心跳的时间跨度前后各放宽 10 秒即可覆盖；
  // ⛔ 不做「按 Call-ID 逐条查」——一次几百条就是几百个请求。
  const from = new Date(Math.min(...times) - 10_000).toISOString();
  const to = new Date(Math.max(...times) + 10_000).toISOString();
  return fetchPages(
    { from, to, deviceId: props.deviceId, businessCode: "keepalive", direction: "outbound" },
    // 应答条数约等于请求条数，留一点余量；7 天档只取一页，避免开机就发十几个请求。
    activeRange.value.ms > MAX_TIMELINE_SPAN_MS ? 1 : 6,
    "心跳应答查询失败"
  );
}

async function reload() {
  if (disabledReason.value) return;
  const version = requestVersion.value + 1;
  requestVersion.value = version;
  loading.value = true;
  error.value = "";
  truncated.value = false;
  lastBeforeWindow.value = null;
  lastProbeDone.value = false;
  try {
    const range = activeRange.value;
    const inboundPage = await fetchPages(
      { from: range.from, to: range.to, deviceId: props.deviceId, businessCode: "keepalive", direction: "inbound" },
      range.ms > MAX_TIMELINE_SPAN_MS ? 1 : 6,
      "心跳记录加载失败"
    );
    if (version !== requestVersion.value) return;
    const outboundPage = inboundPage.items.length ? await loadResponses(inboundPage.items) : { items: [], truncated: false };
    if (version !== requestVersion.value) return;
    // 游标翻页有可能重复返回同一条（时间边界上），按报文 ID 去重后再配对。
    const seen = new Set<string>();
    const uniqueInbound = inboundPage.items.filter(item => {
      if (seen.has(item.eventId)) return false;
      seen.add(item.eventId);
      return true;
    });
    rows.value = buildHeartbeatRows(uniqueInbound, outboundPage.items);
    visibleCount.value = PAGE_SIZE;
    truncated.value = inboundPage.truncated;
    // 窗口内空的时候才往前探：这多半就是「设备早就掉了、今天才发现」，
    // 不把上次见到它的时刻捞出来，用户只能看到一片空白。
    if (rows.value.length === 0 && !drilledRange.value) {
      await probeLastHeartbeat(range.from);
    }
  } catch (cause: unknown) {
    if (version !== requestVersion.value) return;
    error.value = cause instanceof Error ? cause.message : "心跳记录加载失败";
    rows.value = [];
  } finally {
    if (version === requestVersion.value) loading.value = false;
  }
}

/**
 * 往前找最后一次心跳：倒序取 1 条就够，不需要把 7 天的报文全拉一遍。
 * 上限按报文保留期（默认 7 天）取，再往前也查不到东西。
 */
async function probeLastHeartbeat(before: string) {
  try {
    const from = new Date(new Date(before).getTime() - 7 * 24 * 60 * 60 * 1000).toISOString();
    const result = await listTraceMessages({
      from,
      to: before,
      deviceId: props.deviceId,
      businessCode: "keepalive",
      direction: "inbound",
      limit: 1
    });
    const item = result.code === 0 ? result.data?.items?.[0] : undefined;
    if (!item) return;
    const at = new Date(item.occurredAt);
    if (Number.isNaN(at.getTime())) return;
    lastBeforeWindow.value = { at: item.occurredAt, gapMs: Date.now() - at.getTime() };
  } catch {
    // 往前探只是补充信息：探不到就不显示，不能因此把主流程报成失败。
  } finally {
    lastProbeDone.value = true;
  }
}

/** 跳到「最后一次心跳」那一刻看：前后各留半小时，正好能看清它是在哪一格停下的 */
function jumpToLastHeartbeat() {
  const probe = lastBeforeWindow.value;
  if (!probe) return;
  const at = new Date(probe.at).getTime();
  drilledRange.value = {
    from: new Date(at - 30 * 60_000).toISOString(),
    to: new Date(at + 30 * 60_000).toISOString()
  };
  void reload();
}

function bucketState(bucket: HeartbeatBucket) {
  if (bucket.abnormal > 0) return "bad";
  if (bucket.total > 0) return "ok";
  return "empty";
}

/** 最后一条心跳落在哪一格：给它单独描边，让「断点」是一个确定的格子而不是一片模糊的灰 */
function isLastBucket(bucket: HeartbeatBucket) {
  const lastAt = timeline.value?.lastAt;
  if (lastAt === null || lastAt === undefined) return false;
  return lastAt >= bucket.from && lastAt < bucket.to;
}

function bucketTitle(bucket: HeartbeatBucket) {
  const span = activeRange.value.ms;
  const head = `${formatAxisTime(bucket.from, span)} 起的 ${formatBucketSpan(timeline.value?.bucketMs ?? 0)}`;
  if (bucket.total === 0) return `${head}：没有收到心跳`;
  const tail = bucket.abnormal > 0 ? `，其中异常 ${bucket.abnormal} 条` : "";
  return `${head}：收到 ${bucket.total} 条心跳${tail}`;
}

/** 点一格就把窗口收窄到这一格，粒度自动细化到 1 分钟 —— 这就是「看每分钟」的入口 */
function drillInto(bucket: HeartbeatBucket) {
  const padding = Math.min(bucket.to - bucket.from, 30_000);
  drilledRange.value = {
    from: new Date(bucket.from - padding).toISOString(),
    to: new Date(bucket.to + padding).toISOString()
  };
  void reload();
}

function clearDrillDown() {
  drilledRange.value = null;
  void reload();
}

function showMore() {
  visibleCount.value += PAGE_SIZE;
}

async function probeTraceState() {
  try {
    const health = await fetchTraceHealth();
    if (health.code === 0 && health.data?.state === "disabled") {
      disabledReason.value = "平台未启用 SIP 报文记录，心跳明细不可用";
    }
  } catch {
    // 健康探测只是补充说明：探不到就按正常流程去拉数据，让真实错误在列表里报出来。
  }
}

watch(windowKey, () => {
  drilledRange.value = null;
  void reload();
});
watch(
  () => props.deviceId,
  () => {
    drilledRange.value = null;
    void reload();
  }
);

onMounted(async () => {
  await probeTraceState();
  if (!disabledReason.value) void reload();
});
</script>

<template>
  <div class="heartbeat-panel">
    <div class="runtime-filter">
      <span>时间范围</span>
      <div class="runtime-filter-control">
        <a-select v-model="windowKey" :style="{ width: '190px' }">
          <a-option v-for="item in HEARTBEAT_WINDOWS" :key="item.key" :value="item.key">{{ item.label }}</a-option>
        </a-select>
        <small>取自平台收到的 SIP 心跳报文；受报文保留期限制（默认 7 天，超出会被清理）</small>
      </div>
    </div>

    <div v-if="disabledReason" class="heartbeat-state" role="alert">
      <span>{{ disabledReason }}</span>
    </div>

    <template v-else>
      <div class="heartbeat-head">
        <div class="heartbeat-stats">
          <span class="heartbeat-stat"
            >已加载 <strong>{{ summary.total }}</strong> 条</span
          >
          <span class="heartbeat-stat" :class="{ danger: summary.abnormal > 0 }">
            异常 <strong>{{ summary.abnormal }}</strong> 条
          </span>
          <span class="heartbeat-stat"
            >平均间隔 <strong>{{ formatDuration(summary.averageIntervalMs) }}</strong></span
          >
          <span class="heartbeat-stat">
            最近心跳 <strong class="mono">{{ formatHeartbeatTime(summary.latestAt) }}</strong>
          </span>
        </div>
        <a-button size="small" class="uvp-refresh-btn" :loading="loading" @click="reload">刷新</a-button>
      </div>

      <div v-if="driftWarning" class="heartbeat-drift" role="alert">
        <span>{{ driftWarning }}</span>
      </div>

      <div v-if="error" class="heartbeat-state error" role="alert">
        <span>{{ error }}</span>
        <a-button size="small" class="uvp-refresh-btn" @click="reload">重试</a-button>
      </div>

      <template v-else>
        <section v-if="timeline" class="heartbeat-timeline">
          <div class="heartbeat-timeline-head">
            <span class="heartbeat-timeline-title">心跳连续性</span>
            <span class="heartbeat-timeline-meta">
              <template v-if="drilledRange">
                <a-link @click="clearDrillDown">返回{{ windowLabel }}</a-link>
                <span>·</span>
              </template>
              <span>一格 {{ formatBucketSpan(timeline.bucketMs) }}，共 {{ timeline.buckets.length }} 格</span>
              <span>·</span>
              <span>点任意一格放大到该时段</span>
            </span>
          </div>
          <div class="heartbeat-strip" role="group" aria-label="心跳连续性条带">
            <button
              v-for="bucket in timeline.buckets"
              :key="bucket.index"
              type="button"
              class="heartbeat-cell"
              :data-state="bucketState(bucket)"
              :data-last="isLastBucket(bucket) ? 'true' : undefined"
              :aria-label="bucketTitle(bucket)"
              :title="bucketTitle(bucket)"
              @click="drillInto(bucket)"
            />
          </div>
          <div class="heartbeat-axis">
            <span>{{ formatAxisTime(timeline.from, activeRange.ms) }}</span>
            <span>{{ formatAxisTime(timeline.from + activeRange.ms / 2, activeRange.ms) }}</span>
            <span>{{ formatAxisTime(timeline.to, activeRange.ms) }}</span>
          </div>
          <div class="heartbeat-legend">
            <span><i data-state="ok" />正常接收</span>
            <span><i data-state="bad" />被拒收或无应答</span>
            <span><i data-state="empty" />无心跳</span>
            <span><i data-state="last" />最后一条</span>
          </div>
          <p v-if="timeline.total === 0" class="heartbeat-gap danger">
            <span>所选时间范围内一条心跳都没有：设备可能在这之前就已离线，或报文已被保留期清理。</span>
            <span v-if="lastBeforeWindow">
              再往前找，最后一次心跳在 <strong class="mono">{{ formatHeartbeatTime(lastBeforeWindow.at) }}</strong
              >，距今 {{ formatDuration(lastBeforeWindow.gapMs) }}。
              <a-link @click="jumpToLastHeartbeat">查看那一刻</a-link>
            </span>
            <span v-else-if="lastProbeDone">再往前 7 天也没有找到心跳记录。</span>
          </p>
          <p v-else-if="gapNote" class="heartbeat-gap danger">{{ gapNote }}</p>
        </section>

        <a-table
          class="uvp-data-table heartbeat-grid"
          :data="visibleRows"
          :columns="columns"
          :loading="loading"
          :pagination="false"
          :row-class="rowClass"
          :scroll="{ y: 300 }"
          row-key="key"
          size="small"
          :bordered="{ wrapper: false, cell: false }"
        >
          <template #empty>
            <div class="heartbeat-empty">
              <a-empty>
                <template #description>
                  <div class="heartbeat-empty-hint">
                    <strong>所选时间范围内没有心跳报文</strong>
                    <span>设备未注册成功、已经离线，或报文记录被保留期清掉了，都会是这个结果。</span>
                  </div>
                </template>
              </a-empty>
            </div>
          </template>
          <template #received="{ record }">
            <span class="mono">{{ formatHeartbeatTime(record.receivedAt) }}</span>
          </template>
          <template #result="{ record }">
            <span class="heartbeat-result" :data-kind="record.kind">{{ heartbeatResultLabel(record) }}</span>
          </template>
          <template #latency="{ record }">{{ formatDuration(record.latencyMs) }}</template>
          <template #interval="{ record }">{{ formatDuration(record.intervalMs) }}</template>
          <template #source="{ record }">
            <span class="mono">{{ record.sourceAddr }}</span>
          </template>
          <template #agent="{ record }">{{ record.userAgent }}</template>
        </a-table>

        <div v-if="rows.length" class="heartbeat-foot">
          <span v-if="truncated"> 时间范围内报文过多，仅加载了最近 {{ rows.length }} 条；要看完整明细请把时间范围调小 </span>
          <span v-else-if="hasMoreRows">按时间倒序，共 {{ rows.length }} 条，已显示 {{ visibleRows.length }} 条</span>
          <span v-else>已展示所选时间内全部 {{ rows.length }} 条</span>
          <a-button v-if="hasMoreRows" size="small" class="uvp-refresh-btn" @click="showMore">加载更早</a-button>
        </div>
      </template>
    </template>
  </div>
</template>

<style scoped lang="scss">
.heartbeat-panel {
  display: grid;
  gap: 10px;
  min-width: 0;
}

.runtime-filter {
  display: grid;
  grid-template-columns: 72px minmax(220px, 440px);
  gap: 12px;
  align-items: start;
  padding: 2px 0 8px;
  border-bottom: 1px solid var(--uvp-panel-border);
}

.runtime-filter > span {
  padding-top: 6px;
  font-size: 12px;
  font-weight: 600;
  color: var(--uvp-text-secondary);
}

.runtime-filter-control {
  display: grid;
  gap: 4px;
}

.runtime-filter-control small {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}

.heartbeat-head {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  justify-content: space-between;
}

.heartbeat-stats {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.heartbeat-stat {
  padding: 3px 9px;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 999px;
}

.heartbeat-stat strong {
  font-size: 12px;
  font-weight: 650;
  color: var(--uvp-text-primary);
}

.heartbeat-stat.danger {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
  border-color: var(--uvp-danger-border);
}

.heartbeat-stat.danger strong {
  color: var(--uvp-danger);
}

.heartbeat-drift {
  padding: 7px 10px;
  font-size: 12px;
  line-height: 1.5;
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
  border: 1px solid var(--uvp-warning-border);
  border-radius: 8px;
}

.heartbeat-state {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  min-height: 40px;
  padding: 7px 10px;
  font-size: 12px;
  color: var(--uvp-text-secondary);
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
}

.heartbeat-state.error {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
  border-color: var(--uvp-danger-border);
}

.heartbeat-timeline {
  display: grid;
  gap: 6px;
  padding: 10px 12px 12px;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 10px;
}

.heartbeat-timeline-head {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 10px;
  align-items: baseline;
  justify-content: space-between;
}

.heartbeat-timeline-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--uvp-text-secondary);
}

.heartbeat-timeline-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
  align-items: baseline;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}

.heartbeat-strip {
  display: flex;
  gap: 2px;
  height: 34px;
}

.heartbeat-cell {
  position: relative;
  flex: 1 1 0;
  min-width: 0;
  padding: 0;
  cursor: pointer;
  background: var(--uvp-panel-border);
  border: 0;
  border-radius: 2px;
}

.heartbeat-cell[data-state="ok"] {
  background: color-mix(in srgb, var(--uvp-success) 72%, var(--uvp-panel-bg));
}

.heartbeat-cell[data-state="bad"] {
  background: color-mix(in srgb, var(--uvp-danger) 82%, var(--uvp-panel-bg));
}

.heartbeat-cell[data-state="empty"] {
  background: color-mix(in srgb, var(--uvp-text-tertiary) 22%, var(--uvp-panel-bg));
}

.heartbeat-cell[data-last="true"] {
  outline: 2px solid var(--uvp-text-primary);
  outline-offset: 1px;
}

.heartbeat-cell:hover {
  outline: 2px solid var(--uvp-text-secondary);
  outline-offset: 1px;
}

.heartbeat-cell:focus-visible {
  outline: 2px solid var(--uvp-brand);
  outline-offset: 2px;
}

.heartbeat-axis {
  display: flex;
  justify-content: space-between;
  font-family: var(--uvp-font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}

.heartbeat-legend {
  display: flex;
  flex-wrap: wrap;
  gap: 14px;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}

.heartbeat-legend span {
  display: inline-flex;
  gap: 5px;
  align-items: center;
}

.heartbeat-legend i {
  display: inline-block;
  width: 10px;
  height: 10px;
  background: color-mix(in srgb, var(--uvp-text-tertiary) 22%, var(--uvp-panel-bg));
  border-radius: 2px;
}

.heartbeat-legend i[data-state="ok"] {
  background: color-mix(in srgb, var(--uvp-success) 72%, var(--uvp-panel-bg));
}

.heartbeat-legend i[data-state="bad"] {
  background: color-mix(in srgb, var(--uvp-danger) 82%, var(--uvp-panel-bg));
}

.heartbeat-legend i[data-state="last"] {
  background: transparent;
  border: 2px solid var(--uvp-text-primary);
}

.heartbeat-gap {
  margin: 0;
  font-size: 12px;
  line-height: 1.5;
  color: var(--uvp-text-secondary);
}

.heartbeat-gap.danger {
  padding: 6px 9px;
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
  border: 1px solid var(--uvp-danger-border);
  border-radius: 6px;
}

.heartbeat-result {
  display: inline-flex;
  align-items: center;
  padding: 1px 8px;
  font-size: 11px;
  font-weight: 600;
  color: var(--uvp-text-secondary);
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 999px;
}

.heartbeat-result[data-kind="ok"] {
  color: var(--uvp-success);
  background: var(--uvp-success-soft);
  border-color: var(--uvp-success-border);
}

.heartbeat-result[data-kind="registration_expired"],
.heartbeat-result[data-kind="rejected"] {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
  border-color: var(--uvp-danger-border);
}

.heartbeat-result[data-kind="no_response"] {
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
  border-color: var(--uvp-warning-border);
}

.heartbeat-grid :deep(.heartbeat-row--abnormal) .arco-table-td {
  background: color-mix(in srgb, var(--uvp-danger-soft) 55%, transparent);
}

.heartbeat-foot {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}

.heartbeat-foot-error {
  color: var(--uvp-danger);
}

.heartbeat-empty-hint {
  display: grid;
  gap: 4px;
}

.heartbeat-empty-hint strong {
  font-size: 12px;
  font-weight: 600;
  color: var(--uvp-text-secondary);
}

.heartbeat-empty-hint span {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}

.mono {
  font-family: var(--uvp-font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
}
</style>
