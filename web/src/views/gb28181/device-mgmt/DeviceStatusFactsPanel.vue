<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { Activity, RefreshCcw, ShieldCheck } from "@lucide/vue";
import {
  getDeviceStatus,
  getPtzOperation,
  type DeviceAlarmFact,
  type DeviceAlarmResolution,
  type DeviceFactState,
  type DeviceReportedFacts,
  type DeviceStatusResult,
  type PTZResourceFreshness
} from "@/api/gb28181";
import FactChannelPicker from "./FactChannelPicker.vue";
import type { FactChannelOption } from "./deviceFactChannel";

/**
 * 设备状态事实（GB/T 28181 DeviceStatus / A.2.4.x）—— 从播放控制台搬到设备详情抽屉。
 *
 * ⛔ 查询目标是**通道**（`/channel/:id/device-status`）：设备在应答里既报通道的录像状态，
 *    又报自己的在线/自检/编码/时间偏差/报警输入数量，平台分别按通道与设备落库。
 *    所以这一页在设备级入口上要带一个「查哪个通道」的选择器（见 FactChannelPicker）。
 *
 * ⛔ `refresh=true` 在服务端只是**发起**一次查询（SIP 应答是异步的），拿回来的还是上一次的事实：
 *    必须轮询 operation 到终态，再重读一次事实 —— 这就是下面那套 poll 存在的唯一理由。
 *    不带 refresh 的读取是纯缓存读，不发任何 SIP 报文（打开这一页时的默认动作）。
 */
type RecordState = "on" | "off" | "unknown";
type GuardState = "armed" | "disarmed" | "alarm" | "unknown";

const props = defineProps<{
  /** 查询目标通道；null 表示还没解析出通道。 */
  channelId: number | null;
  channelOptions: FactChannelOption[];
  /** 本页当前是否显示 —— 只在该页首次显示时做一次静默读缓存，避免打开抽屉就替用户查两遍。 */
  active: boolean;
  /** 是否具备查看权限（gb28181:ptz:view）。 */
  canView: boolean;
  channelsLoading?: boolean;
}>();

const emit = defineEmits<{ (event: "update:channelId", value: number | null): void }>();

const recordState = ref<RecordState>("unknown");
const guardState = ref<GuardState>("unknown");
const alarmResolution = ref<DeviceAlarmResolution | null>(null);
/** 设备在应答里自报的事实；整段为 null 表示这条应答没带这些项，界面显示「未上报」而不是「关闭」。 */
const deviceReport = ref<DeviceReportedFacts | null>(null);
const alarmFacts = ref<DeviceAlarmFact[]>([]);
const freshness = ref<PTZResourceFreshness>("unknown");
const error = ref("");
const pending = ref(false);
/** 是否读到过一份结果 —— 用来区分「还没读过」与「设备没报」。 */
const loaded = ref(false);

const DEVICE_STATUS_POLL_DELAYS_MS = [1000, 2000, 4000, 8000] as const;
const DEVICE_STATUS_DEFAULT_DEADLINE_MS = DEVICE_STATUS_POLL_DELAYS_MS.reduce((total, delay) => total + delay, 0);
const DEVICE_STATUS_FACT_READ_INSURANCE_MS = 5000;
const DEVICE_STATUS_TERMINAL_PHASES = new Set(["accepted", "rejected", "timeout", "unknown", "cancelled"]);

interface TrackedOperation {
  operationId: string;
  terminal: boolean;
  deadlineAt: number;
  error: string;
}

let pollTimer: number | null = null;
let pollAttempt = 0;
let generation = 0;
let operations = new Map<string, TrackedOperation>();

/** 当前上下文是否还成立：换通道 / 换一次查询 / 组件卸载都会让在途结果作废。 */
function isStale(channelId: number, currentGeneration: number) {
  return currentGeneration !== generation || props.channelId !== channelId;
}

function clearPolling(invalidate = true) {
  if (pollTimer !== null) window.clearTimeout(pollTimer);
  pollTimer = null;
  pollAttempt = 0;
  if (invalidate) generation += 1;
  operations.clear();
  pending.value = false;
}

function normalizeRecordState(value: DeviceFactState | boolean | number | null | undefined): RecordState {
  if (value === true || value === 1) return "on";
  if (value === false || value === 0) return "off";
  const normalized = String(value ?? "")
    .trim()
    .toLowerCase();
  if (["on", "1", "true", "record", "recording", "start", "started"].includes(normalized)) return "on";
  if (["off", "0", "false", "stop", "stopped", "idle"].includes(normalized)) return "off";
  return "unknown";
}

function normalizeGuardState(value: DeviceFactState | boolean | number | null | undefined): GuardState {
  if (value === true || value === 1) return "armed";
  if (value === false || value === 0) return "disarmed";
  const normalized = String(value ?? "")
    .trim()
    .toLowerCase();
  if (normalized === "alarm") return "alarm";
  if (["armed", "on", "1", "true", "guard", "set", "defended"].includes(normalized)) return "armed";
  if (["disarmed", "off", "0", "false", "reset", "unset"].includes(normalized)) return "disarmed";
  return "unknown";
}

function applyResult(result: DeviceStatusResult) {
  const state = result.state || null;
  recordState.value = normalizeRecordState(state?.recordState ?? result.recordState);
  guardState.value = normalizeGuardState(result.alarmResolution?.state ?? state?.guardState ?? result.guardState);
  alarmResolution.value = result.alarmResolution || null;
  alarmFacts.value = Array.isArray(result.alarmFacts) ? result.alarmFacts : [];
  deviceReport.value = result.deviceReport || null;
  freshness.value = result.freshness || state?.freshness || "unknown";
  error.value = result.refreshError || "";
  loaded.value = true;
}

function resetFacts() {
  recordState.value = "unknown";
  guardState.value = "unknown";
  alarmResolution.value = null;
  alarmFacts.value = [];
  deviceReport.value = null;
  freshness.value = "unknown";
  error.value = "";
  loaded.value = false;
}

function operationErrors() {
  return [...operations.values()]
    .map(operation => operation.error)
    .filter(Boolean)
    .join("；");
}

function refreshOperationIds(result: DeviceStatusResult) {
  const ids = [
    result.refreshOperationIds?.record,
    result.refreshOperationIds?.alarm,
    result.recordRefreshOperationId,
    result.alarmRefreshOperationId,
    result.refreshOperationId
  ];
  return [...new Set(ids.filter((id): id is string => typeof id === "string" && id.trim().length > 0))];
}

/**
 * 轮询一次 operation，并带**剩余截止时间**兜底：operation 接口自己挂住时也要能收尾。
 * ⛔ 计时器必须挂在闭包里按次持有 —— 同时轮询 record / alarm 两个 operation 是常态，
 *    共用一个模块级计时器会导致先返回的那个把另一个的截止兜底清掉。
 */
function readOperationWithDeadline(channelId: number, tracked: TrackedOperation) {
  const remainingMs = tracked.deadlineAt - Date.now();
  if (remainingMs <= 0) return Promise.reject(new Error("设备状态查询超时，结果未知"));
  return new Promise<Awaited<ReturnType<typeof getPtzOperation>>>((resolve, reject) => {
    let settled = false;
    const deadlineTimer = window.setTimeout(() => {
      if (settled) return;
      settled = true;
      reject(new Error("设备状态查询超时，结果未知"));
    }, remainingMs);
    getPtzOperation(channelId, tracked.operationId).then(
      response => {
        if (settled) return;
        settled = true;
        window.clearTimeout(deadlineTimer);
        resolve(response);
      },
      (requestError: unknown) => {
        if (settled) return;
        settled = true;
        window.clearTimeout(deadlineTimer);
        reject(requestError);
      }
    );
  });
}

/** 收尾：把终态错误汇总出来，再补读一次事实（补读是尽力而为，不能把按钮一直按在「查询中」）。 */
async function finishRefresh(channelId: number, currentGeneration: number) {
  const operationError = operationErrors();
  if (isStale(channelId, currentGeneration)) return;
  clearPolling(false);
  freshness.value = "unknown";
  error.value = operationError;

  let insuranceTimer: number | null = null;
  const insurance = new Promise<never>((_, reject) => {
    insuranceTimer = window.setTimeout(
      () => reject(new Error("设备状态最终补读超时，结果未知")),
      DEVICE_STATUS_FACT_READ_INSURANCE_MS
    );
  });
  try {
    const response = await Promise.race([getDeviceStatus(channelId, false), insurance]);
    if (isStale(channelId, currentGeneration)) return;
    if (response.code !== 0 || !response.data) throw new Error(response.message || "读取设备状态失败");
    applyResult(response.data);
    error.value = [error.value, operationError].filter(Boolean).join("；");
  } catch (readError: any) {
    if (isStale(channelId, currentGeneration)) return;
    error.value = [operationError, readError?.message || "读取设备状态失败"].filter(Boolean).join("；");
  } finally {
    if (insuranceTimer !== null) window.clearTimeout(insuranceTimer);
  }
}

function scheduleRefresh(channelId: number, currentGeneration: number) {
  const activeOperations = [...operations.values()].filter(operation => !operation.terminal);
  if (activeOperations.length === 0) {
    void finishRefresh(channelId, currentGeneration);
    return;
  }
  if (pollTimer !== null) window.clearTimeout(pollTimer);
  const baseDelay = DEVICE_STATUS_POLL_DELAYS_MS[Math.min(pollAttempt, DEVICE_STATUS_POLL_DELAYS_MS.length - 1)];
  const nearestDeadline = Math.min(...activeOperations.map(operation => operation.deadlineAt));
  const delay = Math.max(0, Math.min(baseDelay, nearestDeadline - Date.now()));
  pollTimer = window.setTimeout(async () => {
    pollTimer = null;
    if (isStale(channelId, currentGeneration)) return;
    const targets = [...operations.values()].filter(operation => !operation.terminal);
    const results = await Promise.all(
      targets.map(async tracked => {
        try {
          const response = await readOperationWithDeadline(channelId, tracked);
          if (response.code !== 0 || !response.data || response.data.operationId !== tracked.operationId) {
            throw new Error(response.message || "设备状态查询结果不匹配");
          }
          return { tracked, operation: response.data, failure: "" };
        } catch (pollError: any) {
          return { tracked, operation: null, failure: pollError?.message || "设备状态查询暂时不可用" };
        }
      })
    );
    if (isStale(channelId, currentGeneration)) return;

    const now = Date.now();
    for (const result of results) {
      const tracked = result.tracked;
      if (result.operation) {
        const serverDeadline = result.operation.deadlineAt ? Date.parse(result.operation.deadlineAt) : Number.NaN;
        if (Number.isFinite(serverDeadline)) tracked.deadlineAt = serverDeadline;
        tracked.terminal = DEVICE_STATUS_TERMINAL_PHASES.has(result.operation.status);
        tracked.error =
          tracked.terminal && result.operation.status !== "accepted"
            ? result.operation.errorCode || result.operation.errorMessage || `设备状态查询${result.operation.status}`
            : "";
      } else {
        tracked.error = result.failure;
      }
      if (!tracked.terminal && now >= tracked.deadlineAt) {
        tracked.terminal = true;
        tracked.error = tracked.error || "设备状态查询超时，结果未知";
      }
    }

    pollAttempt += 1;
    scheduleRefresh(channelId, currentGeneration);
  }, delay);
}

/**
 * @param refresh true = 发起一次真实查询（向设备发报文）并轮询到终态；false = 只读平台缓存。
 */
async function load(channelId = props.channelId, refresh = false) {
  if (!props.canView || !channelId) return;
  clearPolling();
  const currentGeneration = generation;
  // 向设备查询时保留上一份事实（刷新不是清空），只有换通道才清。
  if (refresh) pending.value = true;
  error.value = "";
  try {
    const response = await getDeviceStatus(channelId, refresh);
    if (isStale(channelId, currentGeneration)) return;
    if (response.code !== 0 || !response.data) throw new Error(response.message || "读取设备状态失败");
    applyResult(response.data);
    const ids = refresh ? refreshOperationIds(response.data) : [];
    if (ids.length > 0) {
      const fallbackDeadline = Date.now() + DEVICE_STATUS_DEFAULT_DEADLINE_MS;
      operations = new Map(
        ids.map(operationId => [operationId, { operationId, terminal: false, deadlineAt: fallbackDeadline, error: "" }])
      );
      pending.value = true;
      pollAttempt = 0;
      scheduleRefresh(channelId, currentGeneration);
      return;
    }
  } catch (loadError: any) {
    if (isStale(channelId, currentGeneration)) return;
    error.value = loadError?.message || "读取设备状态失败";
    freshness.value = "unknown";
  }
  pending.value = false;
}

/* 打开这一页 / 换通道时读一次缓存（不发 SIP 报文）。 */
watch(
  [() => props.channelId, () => props.active, () => props.canView],
  ([channelId, active, canView], previous) => {
    const previousChannelId = previous?.[0] ?? null;
    if (!active || !canView || !channelId) return;
    if (channelId === previousChannelId && loaded.value) return;
    clearPolling();
    resetFacts();
    void load(channelId, false);
  },
  { immediate: true }
);

onBeforeUnmount(() => clearPolling());

const hasChannel = computed(() => typeof props.channelId === "number" && props.channelId > 0);
const refreshDisabled = computed(() => !props.canView || !hasChannel.value || pending.value || props.channelOptions.length === 0);
const refreshTitle = computed(() => {
  if (!props.canView) return "当前账号没有设备状态查看权限";
  if (!hasChannel.value) return "该设备下暂无通道，无法查询";
  return statusText();
});

function statusText() {
  if (error.value) return "状态读取失败";
  if (pending.value) return "正在查询设备状态";
  if (!loaded.value) return "尚未读取设备状态";
  if (freshness.value === "fresh") return "设备状态已同步";
  if (freshness.value === "stale") return "设备状态已过期";
  return "设备状态未知";
}

function recordStateText() {
  return recordState.value === "on" ? "设备录制中" : recordState.value === "off" ? "设备未录制" : "未知";
}

function guardStateText(state: GuardState) {
  if (state === "alarm") return "ALARM 报警中";
  return state === "armed" ? "已布防" : state === "disarmed" ? "已撤防" : "未知";
}

/** 设备明确回了 Alarmstatus Num=「0」 时「报警输入：未知」就是错的 —— 设备说的是「我没有报警输入」。 */
function alarmInputText() {
  if (deviceReport.value?.alarmInputCount === 0) return "设备无报警输入";
  return guardStateText(guardState.value);
}

function alarmInputClass() {
  if (deviceReport.value?.alarmInputCount === 0) return "fact-none";
  return `fact-${guardState.value}`;
}

function deviceOnlineText() {
  const state = deviceReport.value?.online;
  if (state === "online") return "在线";
  if (state === "offline") return "离线";
  return "未上报";
}

function selfTestText() {
  const state = deviceReport.value?.selfTest;
  if (state === "ok") return "自检正常";
  if (state === "error") return "自检异常";
  return "未上报";
}

function encodeStateText() {
  const state = deviceReport.value?.encode;
  if (state === "on") return "编码中";
  if (state === "off") return "编码已停";
  return "未上报";
}

/** 平台观测时刻 − 设备自报时刻。设备时间只到秒，±1 秒是量化误差，不当偏差报。 */
function clockSkewText() {
  const seconds = deviceReport.value?.clockSkewSeconds;
  if (seconds === null || seconds === undefined) return "未上报";
  const abs = Math.abs(seconds);
  if (abs < 2) return "与平台一致";
  const unit = abs < 60 ? `${abs} 秒` : abs < 3600 ? `${Math.round(abs / 60)} 分钟` : `${(abs / 3600).toFixed(1)} 小时`;
  return seconds > 0 ? `设备慢 ${unit}` : `设备快 ${unit}`;
}

function factClass(value: unknown, state: unknown = value) {
  if (value === null || value === undefined) return "fact-none";
  return `fact-${state}`;
}

function alarmResolutionWarning() {
  if (alarmResolution.value?.status === "ambiguous") {
    const candidates = alarmResolution.value.candidates
      .map(candidate => candidate.code)
      .filter(Boolean)
      .join("、");
    return `报警目标不明确${candidates ? `（${candidates}）` : ""}，布防、撤防和复位操作将由服务端拒绝`;
  }
  if (alarmResolution.value?.status === "unavailable") {
    if (deviceReport.value?.alarmInputCount === 0) {
      return "设备自报没有报警输入通道，布防、撤防和复位将按注册父设备编码发送，以设备应答为准";
    }
    return "未找到可用的 134 报警输入，布防、撤防和复位将按注册父设备编码发送，以设备应答为准";
  }
  return "";
}

function alarmResolutionTargetText() {
  if (alarmResolution.value?.status !== "resolved" || !alarmResolution.value.targetCode) return "";
  return `报警目标 ${alarmResolution.value.targetCode}`;
}

function alarmFactText(fact: DeviceAlarmFact) {
  return `${fact.targetCode} ${guardStateText(normalizeGuardState(fact.guardState))}`;
}
</script>

<template>
  <div class="fact-panel">
    <FactChannelPicker
      :options="props.channelOptions"
      :model-value="props.channelId"
      :loading="props.channelsLoading"
      @update:model-value="value => emit('update:channelId', value)"
    />

    <p v-if="props.channelOptions.length === 0" class="fact-empty">该设备下暂无通道，无法查询设备状态。</p>

    <section class="fact-group">
      <header class="fact-group-head">
        <span class="fact-group-title"><Activity :size="14" />DeviceStatus 事实</span>
        <span class="fact-group-status" :class="{ pending: pending, error: !!error }">{{ statusText() }}</span>
      </header>

      <div class="fact-field-grid">
        <span class="k">录像</span>
        <span class="v" :class="`fact-${recordState}`" data-testid="fact-record-state">{{ recordStateText() }}</span>
        <span class="k">报警输入</span>
        <span class="v" :class="alarmInputClass()" data-testid="fact-alarm-input">{{ alarmInputText() }}</span>
        <span class="k">编码</span>
        <span class="v" :class="factClass(deviceReport?.encode)">{{ encodeStateText() }}</span>
        <span class="k">设备自检</span>
        <span class="v" :class="factClass(deviceReport?.selfTest)">{{ selfTestText() }}</span>
        <span class="k">设备自报</span>
        <span class="v" :class="factClass(deviceReport?.online)">{{ deviceOnlineText() }}</span>
        <span class="k">时间偏差</span>
        <span class="v" :class="factClass(deviceReport?.clockSkewSeconds)">{{ clockSkewText() }}</span>
      </div>

      <p v-if="error" class="fact-error" data-testid="fact-error">{{ error }}</p>

      <div class="fact-actions">
        <a-button
          size="small"
          :loading="pending"
          :disabled="refreshDisabled"
          :title="refreshTitle"
          data-testid="fact-refresh"
          @click="load(props.channelId, true)"
        >
          <template #icon><RefreshCcw :size="13" /></template>
          {{ pending ? "查询中" : "向设备查询" }}
        </a-button>
        <span class="fact-actions-hint">
          {{ props.channelOptions.length ? "优先读平台缓存；「向设备查询」才会下发 DeviceStatus 报文" : "" }}
        </span>
      </div>
    </section>

    <section class="fact-group">
      <header class="fact-group-head">
        <span class="fact-group-title"><ShieldCheck :size="14" />报警事实</span>
        <span class="fact-group-status">按防区</span>
      </header>
      <p v-if="alarmResolutionTargetText()" class="fact-alarm-target">{{ alarmResolutionTargetText() }}</p>
      <p v-if="alarmResolutionWarning()" class="fact-alarm-warning" data-testid="alarm-resolution-warning">
        {{ alarmResolutionWarning() }}
      </p>
      <div v-if="alarmFacts.length > 0" class="fact-alarm-list" data-testid="alarm-facts">
        <span
          v-for="fact in alarmFacts"
          :key="fact.targetCode"
          class="fact-alarm-chip"
          :class="`fact-${normalizeGuardState(fact.guardState)}`"
          >{{ alarmFactText(fact) }}</span
        >
      </div>
      <p v-else class="fact-empty">设备未上报防区事实。</p>
    </section>
  </div>
</template>

<style scoped>
.fact-panel {
  display: grid;
  gap: 12px;
}
.fact-group {
  display: grid;
  gap: 10px;
  padding: 12px 14px 14px;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 12px;
}
.fact-group-head {
  display: flex;
  gap: 10px;
  align-items: center;
  justify-content: space-between;
}
.fact-group-title {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  font-size: 12px;
  font-weight: 620;
  color: var(--uvp-text-secondary);
}
.fact-group-status {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.fact-group-status.pending {
  color: var(--uvp-warning);
}
.fact-group-status.error {
  color: var(--uvp-danger);
}
.fact-field-grid {
  display: grid;
  grid-template-columns: 92px minmax(0, 1fr);
  gap: 10px 14px;
  align-items: center;
}
.fact-field-grid .k {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.fact-field-grid .v {
  font-size: 13px;
  color: var(--uvp-text-primary);
  overflow-wrap: anywhere;
}
.fact-field-grid .v.fact-on,
.fact-field-grid .v.fact-armed {
  color: var(--uvp-brand-cyan);
}
.fact-field-grid .v.fact-alarm {
  color: var(--uvp-danger);
}
.fact-field-grid .v.fact-unknown {
  color: var(--uvp-warning);
}
.fact-field-grid .v.fact-none {
  color: var(--uvp-text-tertiary);
}
.fact-alarm-target {
  margin: 0;
  font-size: 12px;
  color: var(--uvp-text-secondary);
}
.fact-alarm-warning {
  margin: 0;
  font-size: 11.5px;
  line-height: 1.5;
  color: var(--uvp-warning);
}
.fact-alarm-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.fact-alarm-chip {
  padding: 2px 8px;
  font-size: 11px;
  color: var(--uvp-text-secondary);
  background: var(--uvp-shell-muted);
  border-radius: 999px;
}
.fact-alarm-chip.fact-armed {
  color: var(--uvp-brand-cyan);
}
.fact-alarm-chip.fact-alarm {
  color: var(--uvp-danger);
}
.fact-alarm-chip.fact-unknown {
  color: var(--uvp-warning);
}
.fact-error {
  margin: 0;
  font-size: 11.5px;
  line-height: 1.5;
  color: var(--uvp-danger);
  overflow-wrap: anywhere;
}
.fact-empty {
  margin: 0;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.fact-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
}
.fact-actions-hint {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
</style>
