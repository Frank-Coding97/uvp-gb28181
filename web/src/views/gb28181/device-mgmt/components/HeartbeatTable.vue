<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { fetchTraceHealth, listTraceMessages, type TraceMessageSummary } from "@/api/gb28181-trace";
import {
  HEARTBEAT_WINDOWS,
  buildHeartbeatRows,
  formatDuration,
  formatHeartbeatTime,
  heartbeatDriftWarning,
  heartbeatResultLabel,
  heartbeatWindowRange,
  summarizeHeartbeats,
  type HeartbeatRow,
  type HeartbeatWindowKey
} from "../heartbeat";

/**
 * 心跳记录（证据向）：把平台实际收到的每一条心跳、以及平台给它的应答码列成表。
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

const PAGE_SIZE = 20;
const OUTBOUND_LIMIT = 200;

const windowKey = ref<HeartbeatWindowKey>("24h");
const rows = ref<HeartbeatRow[]>([]);
const loading = ref(false);
const loadingMore = ref(false);
const error = ref("");
const loadMoreError = ref("");
const disabledReason = ref("");
const nextCursor = ref("");
/** 上一页最旧一条的时间，用来给「跨页第一条」算间隔 */
const oldestLoadedAt = ref<string | null>(null);
const requestVersion = ref(0);

const summary = computed(() => summarizeHeartbeats(rows.value));
const hasMore = computed(() => Boolean(nextCursor.value));
const driftWarning = computed(() =>
  heartbeatDriftWarning(summary.value.latestAt, props.accountedKeepaliveAt, summary.value.averageIntervalMs)
);

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

async function loadResponses(inbound: TraceMessageSummary[]) {
  const times = inbound.map(item => new Date(item.occurredAt).getTime()).filter(value => !Number.isNaN(value));
  if (times.length === 0) return [];
  // 应答总在请求之后几毫秒内，所以按这批心跳的时间跨度前后各放宽 10 秒即可覆盖；
  // ⛔ 不做「按 Call-ID 逐条查」——一次 20 条就是 20 个请求。
  const from = new Date(Math.min(...times) - 10_000).toISOString();
  const to = new Date(Math.max(...times) + 10_000).toISOString();
  const result = await listTraceMessages({
    from,
    to,
    deviceId: props.deviceId,
    businessCode: "keepalive",
    direction: "outbound",
    limit: OUTBOUND_LIMIT
  });
  if (result.code !== 0) throw new Error(result.message || "心跳应答查询失败");
  return result.data?.items ?? [];
}

async function loadPage(mode: "initial" | "more") {
  if (disabledReason.value) return;
  if (mode === "more" && (!hasMore.value || loading.value || loadingMore.value)) return;
  const version = requestVersion.value + 1;
  requestVersion.value = version;
  if (mode === "initial") {
    loading.value = true;
    error.value = "";
  } else {
    loadingMore.value = true;
    loadMoreError.value = "";
  }
  try {
    const range = heartbeatWindowRange(windowKey.value);
    const inboundResult = await listTraceMessages({
      from: range.from,
      to: range.to,
      deviceId: props.deviceId,
      businessCode: "keepalive",
      direction: "inbound",
      limit: PAGE_SIZE,
      cursor: mode === "more" ? nextCursor.value : undefined
    });
    if (version !== requestVersion.value) return;
    if (inboundResult.code !== 0) throw new Error(inboundResult.message || "心跳记录加载失败");
    const inboundItems = inboundResult.data?.items ?? [];
    const olderAt = mode === "more" ? oldestLoadedAt.value : null;
    const outboundItems = inboundItems.length ? await loadResponses(inboundItems) : [];
    if (version !== requestVersion.value) return;
    const built = buildHeartbeatRows(inboundItems, outboundItems, olderAt);
    if (mode === "more") {
      const seen = new Set(rows.value.map(row => row.key));
      rows.value = [...rows.value, ...built.filter(row => !seen.has(row.key))];
    } else {
      rows.value = built;
    }
    nextCursor.value = inboundResult.data?.nextCursor || "";
    if (built.length) oldestLoadedAt.value = built[built.length - 1].receivedAt;
  } catch (cause: unknown) {
    if (version !== requestVersion.value) return;
    const message = cause instanceof Error ? cause.message : "心跳记录加载失败";
    if (mode === "initial") {
      error.value = message;
      rows.value = [];
    } else {
      // 追加失败绝不能静默：否则看起来像「后面没有更多心跳了」。
      loadMoreError.value = message;
    }
  } finally {
    if (version === requestVersion.value) {
      loading.value = false;
      loadingMore.value = false;
    }
  }
}

function reload() {
  rows.value = [];
  nextCursor.value = "";
  oldestLoadedAt.value = null;
  void loadPage("initial");
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

watch(windowKey, () => reload());
watch(
  () => props.deviceId,
  () => reload()
);

onMounted(async () => {
  await probeTraceState();
  if (!disabledReason.value) void loadPage("initial");
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
          <span class="heartbeat-stat"
            >最近心跳 <strong class="mono">{{ formatHeartbeatTime(summary.latestAt) }}</strong></span
          >
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

      <a-table
        v-else
        class="uvp-data-table heartbeat-grid"
        :data="rows"
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
        <span v-if="loadMoreError" class="heartbeat-foot-error">{{ loadMoreError }}</span>
        <span v-else-if="loadingMore">正在加载更早的心跳…</span>
        <span v-else-if="!hasMore">已展示所选时间内全部 {{ rows.length }} 条</span>
        <span v-else>按时间倒序，每次多取 {{ PAGE_SIZE }} 条</span>
        <a-button v-if="hasMore" size="small" class="uvp-refresh-btn" :loading="loadingMore" @click="loadPage('more')">
          加载更早
        </a-button>
      </div>
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
