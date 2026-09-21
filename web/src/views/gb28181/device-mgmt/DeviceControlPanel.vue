<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { AlertTriangle, Circle, ShieldCheck, Square } from "@lucide/vue";
import { controlDevice, getDeviceStatus, getPtzOperation, type DeviceAlarmResolution, type DeviceFactState } from "@/api/gb28181";
import FactChannelPicker from "./FactChannelPicker.vue";
import type { FactChannelOption } from "./deviceFactChannel";

/**
 * 设备控制（GB/T 28181 DeviceControl，表 A.4）—— 2026-09-20 从播放控制台侧栏「高级」搬来。
 *
 * 搬来三条：开始 / 停止设备端录制 · 布防 / 撤防 · 报警复位。
 * 搬家的理由不是"换个位置"，是**归属**：它们是设备侧动作，不是"这一路流"的动作。
 * 控制台是"某路通道正在播"的工作区，关掉弹窗它们就没了；设备详情抽屉是"某台设备"的
 * 常驻入口，任何布局下都能对设备下发（2026-09-20 起设备状态/存储卡也在这里）。
 *
 * ⛔ 接口是**通道级**的（`POST /channel/:id/device-control`），设备级入口必须先把目标落到
 *    一个具体通道上 —— 沿用「设备状态」「存储卡」两页的 FactChannelPicker，共用同一个选择。
 *    报警类动作在设备侧还要解析出 134 报警输入通道，那是服务端的事，前端只展示它的结论。
 *
 * ⛔ `POST` 返回 200 只是**报文发出去了**，不是设备执行完了：服务端为这五个动作记了
 *    operation 且 `responseRequired=true`，必须轮询到终态才有结论。
 *    ⇒ 文案分三层：等待设备应答 / 设备已确认 / 结果未知（可重试）。不许把 200 写成"成功"。
 *
 * 轮询参数与三条收敛规则是从控制台 `PlayConsoleLinked.vue`（2026-09-20 之前那版）**原样搬来**的，
 * 别按"看起来更合理"自由发挥，否则会丢掉两个真实状态：
 *   ① **服务端没给 deadline** 是一个独立状态（`deadlineMs === null`），只能补读一次就收敛为未知；
 *      不能兜底成"自己造一个 15 秒截止时间"，那样"服务端没给截止时间"永远观测不到。
 *   ② `responseRequired === false` 的命令是**单向**的：状态 `sent` 即止，不轮询、也不许说"已成功"。
 */
type ActionKey = "record_start" | "record_stop" | "guard_set" | "guard_reset" | "alarm_reset";
type RecordState = "on" | "off" | "unknown";
type GuardState = "armed" | "disarmed" | "alarm" | "unknown";
type Phase = "queued" | "sent" | "accepted" | "rejected" | "timeout" | "unknown" | "cancelled";
type StatusLevel = "info" | "warn" | "error";

const props = defineProps<{
  /** 下发目标通道；null 表示还没解析出通道。 */
  channelId: number | null;
  channelOptions: FactChannelOption[];
  /** 本页当前是否显示 —— 只在首次显示时静默读一次缓存事实，给按钮定"正反"。 */
  active: boolean;
  /** 是否具备设备控制权限（gb28181:device:control）。 */
  canControl: boolean;
  channelsLoading?: boolean;
}>();

const emit = defineEmits<{ (event: "update:channelId", value: number | null): void }>();

const OPERATION_POLL_INTERVAL_MS = 1000;
/** 读到截止时间那一刻也该给设备结果一个落地窗口，否则卡在边界上的成功会被判成超时。 */
const OPERATION_FINAL_READ_GRACE_MS = 250;
/** 服务端没给截止时间时的单次补读上限：宁可说"结果未知"，也不能把按钮一直按住。 */
const OPERATION_NO_DEADLINE_READ_TIMEOUT_MS = 5000;
/** 还没到终态的两种状态 —— 要继续等，不是结论。 */
const PENDING_PHASES = new Set<Phase>(["queued", "sent"]);
/** 真正的失败终态；`accepted` 不在此列（它是成功，单独处理）。 */
const FAILED_PHASES = new Set<Phase>(["rejected", "timeout", "unknown", "cancelled"]);
const RECORD_ACTIONS: ActionKey[] = ["record_start", "record_stop"];
const GUARD_ACTIONS: ActionKey[] = ["guard_set", "guard_reset"];

interface PollContext {
  action: ActionKey;
  operationId: string;
  /** null = 服务端**没给**截止时间（不是"还没拿到"），走单次补读上限。 */
  deadlineMs: number | null;
  timer: number | null;
  readTimer: number | null;
}

const recordState = ref<RecordState>("unknown");
const guardState = ref<GuardState>("unknown");
const alarmResolution = ref<DeviceAlarmResolution | null>(null);

/** 去重键：录制 / 布撤防 / 复位各自成组 —— 同时"开始录制 + 布防"允许，同组内互斥。 */
const pendingGroups = ref<string[]>([]);
const statusText = ref<Record<string, string>>({});
const statusLevel = ref<Record<string, StatusLevel>>({});
const errorText = ref<Record<string, string>>({});

const polls = new Map<string, PollContext>();
let generation = 0;

function groupOf(action: ActionKey) {
  if (RECORD_ACTIONS.includes(action)) return "record";
  if (GUARD_ACTIONS.includes(action)) return "guard";
  return "alarm_reset";
}

/**
 * 这个动作需不需要等设备结论。
 *
 * 五个动作在服务端默认都记 operation 且 `responseRequired=true`；只有服务端**明确**说
 * `false` 才按单向处理。没有 `responseRequired` 时兜底为 `true`（要等）—— 兜底成"不用等"
 * 会把 200 说成"已生效"，方向恰好相反。
 */
function requiresDeviceResult(responseRequired?: boolean) {
  if (typeof responseRequired === "boolean") return responseRequired;
  return true;
}

/** 上下文是否还成立：换通道 / 换一轮动作 / 组件卸载都会让在途结果作废。 */
function isStale(channelId: number, token: number) {
  return token !== generation || props.channelId !== channelId;
}

function clearPoll(group: string) {
  const context = polls.get(group);
  if (context?.timer !== null && context?.timer !== undefined) window.clearTimeout(context.timer);
  if (context?.readTimer !== null && context?.readTimer !== undefined) window.clearTimeout(context.readTimer);
  polls.delete(group);
}

function clearPolls(invalidate = true) {
  for (const group of [...polls.keys()]) clearPoll(group);
  if (invalidate) generation += 1;
  pendingGroups.value = [];
}

function setPendingGroup(group: string, next: boolean) {
  const set = new Set(pendingGroups.value);
  if (next) set.add(group);
  else set.delete(group);
  pendingGroups.value = [...set];
}

function isPending(action: ActionKey) {
  return pendingGroups.value.includes(groupOf(action));
}

function setStatus(group: string, message: string, error = "", level: StatusLevel = "info") {
  statusText.value = { ...statusText.value, [group]: message };
  statusLevel.value = { ...statusLevel.value, [group]: level };
  errorText.value = { ...errorText.value, [group]: error };
}

/**
 * 收敛为"结果未知"。⛔ 它**不是**一句普通提示：`data-level` 会被置成 `warn`，
 * 视觉上与"等待设备应答"分开 —— 否则操作员会把"我不知道设备做没做"读成"正在做"。
 */
function settleUnknown(group: string, message: string) {
  clearPoll(group);
  setPendingGroup(group, false);
  setStatus(group, message, "", "warn");
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

/**
 * 读一次**平台缓存**事实（`refresh=false`，不发任何 SIP 报文），只为了定两个开关的方向。
 * ⛔ 它不负责"最新"：要最新事实请到「设备状态」页点「向设备查询」。
 *    读取失败在这里是**静默**的 —— 它只是按钮文案的旁路输入，不该在控制页上另开一条报错。
 */
async function loadFacts(channelId: number, token: number) {
  const response = await getDeviceStatus(channelId, false);
  if (isStale(channelId, token)) return;
  if (response.code !== 0 || !response.data) return;
  const result = response.data;
  const state = result.state || null;
  recordState.value = normalizeRecordState(state?.recordState ?? result.recordState);
  guardState.value = normalizeGuardState(result.alarmResolution?.state ?? state?.guardState ?? result.guardState);
  alarmResolution.value = result.alarmResolution || null;
}

/** 只有设备明确说"我做了"才允许改本地事实；发出去的 200 不算。 */
function applyAccepted(group: string, action: ActionKey) {
  if (action === "record_start") recordState.value = "on";
  if (action === "record_stop") recordState.value = "off";
  if (action === "guard_set") guardState.value = "armed";
  if (action === "guard_reset") guardState.value = "disarmed";
  setStatus(group, "设备已确认");
}

function parseDeadline(deadlineAt?: string | null): number | null {
  const parsed = deadlineAt ? Date.parse(deadlineAt) : Number.NaN;
  return Number.isFinite(parsed) ? parsed : null;
}

/**
 * 轮询一次 operation，带剩余截止时间兜底：operation 接口自己挂住也要能收尾。
 * ⛔ 计时器挂在**这次读取的闭包里**，并由 context 持引用 —— 录制与布撤防会并发轮询，
 *    共用一个槽位会让先返回的那个把另一个的兜底清掉（与 DeviceStatusFactsPanel 同款坑）。
 */
function readOperation(channelId: number, context: PollContext) {
  const timeoutMs =
    context.deadlineMs === null
      ? OPERATION_NO_DEADLINE_READ_TIMEOUT_MS
      : Math.max(OPERATION_FINAL_READ_GRACE_MS, context.deadlineMs - Date.now() + OPERATION_FINAL_READ_GRACE_MS);
  return new Promise<Awaited<ReturnType<typeof getPtzOperation>>>((resolve, reject) => {
    let settled = false;
    const timer = window.setTimeout(() => {
      if (settled) return;
      settled = true;
      context.readTimer = null;
      reject(
        new Error(
          context.deadlineMs === null
            ? "服务端未返回操作截止时间，补读一次后结果未知"
            : "操作状态读取超过服务端截止时间，结果未知"
        )
      );
    }, timeoutMs);
    context.readTimer = timer;
    getPtzOperation(channelId, context.operationId).then(
      response => {
        if (settled) return;
        settled = true;
        window.clearTimeout(timer);
        context.readTimer = null;
        resolve(response);
      },
      (requestError: unknown) => {
        if (settled) return;
        settled = true;
        window.clearTimeout(timer);
        context.readTimer = null;
        reject(requestError);
      }
    );
  });
}

function schedulePoll(group: string, channelId: number, token: number) {
  const context = polls.get(group);
  if (!context) return;
  if (context.timer !== null) window.clearTimeout(context.timer);
  const delay =
    context.deadlineMs === null
      ? OPERATION_POLL_INTERVAL_MS
      : Math.max(0, Math.min(OPERATION_POLL_INTERVAL_MS, context.deadlineMs - Date.now()));
  context.timer = window.setTimeout(() => {
    context.timer = null;
    void pollOnce(group, channelId, token);
  }, delay);
}

async function pollOnce(group: string, channelId: number, token: number) {
  const context = polls.get(group);
  if (!context || isStale(channelId, token)) return;
  const action = context.action;
  try {
    const response = await readOperation(channelId, context);
    if (isStale(channelId, token) || polls.get(group) !== context) return;
    if (response.code !== 0 || !response.data || response.data.operationId !== context.operationId) {
      settleUnknown(group, "操作状态不匹配，结果未知");
      return;
    }
    const operation = response.data;
    const serverDeadline = parseDeadline(operation.deadlineAt);
    if (serverDeadline !== null) context.deadlineMs = serverDeadline;
    const status = String(operation.status) as Phase;
    if (status === "sent" && !requiresDeviceResult(operation.responseRequired)) {
      clearPoll(group);
      setPendingGroup(group, false);
      setStatus(group, "已下发（该命令无需设备回执）");
      return;
    }
    if (PENDING_PHASES.has(status)) {
      if (context.deadlineMs === null) {
        settleUnknown(group, "服务端未返回操作截止时间，补读一次后结果未知");
        return;
      }
      if (Date.now() >= context.deadlineMs) {
        settleUnknown(group, "操作超过服务端截止时间，结果未知");
        return;
      }
      setStatus(group, "等待设备应答");
      schedulePoll(group, channelId, token);
      return;
    }
    clearPoll(group);
    setPendingGroup(group, false);
    if (status === "accepted") {
      if (!requiresDeviceResult(operation.responseRequired)) {
        setStatus(group, "已下发（该命令无需设备回执）");
        return;
      }
      applyAccepted(group, action);
      return;
    }
    const message = operation.errorCode || operation.errorMessage || `操作${status}`;
    setStatus(group, message, message, "error");
  } catch (readError: any) {
    if (isStale(channelId, token) || polls.get(group) !== context) return;
    settleUnknown(group, `${readError?.message || "操作状态读取失败，结果未知"}，可重试`);
  }
}

async function run(action: ActionKey) {
  const channelId = props.channelId;
  const group = groupOf(action);
  if (!props.canControl || !channelId || isPending(action)) return;
  const token = generation;
  setStatus(group, "正在发送");
  setPendingGroup(group, true);
  try {
    const response = await controlDevice(channelId, {
      action,
      idempotencyKey: `${channelId}-${action}-${Date.now()}`
    });
    if (isStale(channelId, token)) return;
    if (response.code !== 0) throw new Error(response.message || "设备控制失败");
    const operation = response.data;
    const status = String(operation?.status || "sent") as Phase;
    if (status === "accepted") {
      setPendingGroup(group, false);
      if (requiresDeviceResult(operation?.responseRequired)) {
        applyAccepted(group, action);
        return;
      }
      setStatus(group, "已下发（该命令无需设备回执）");
      return;
    }
    if (FAILED_PHASES.has(status)) {
      setPendingGroup(group, false);
      const message = operation?.errorCode || operation?.errorMessage || `操作${status}`;
      setStatus(group, message, message, "error");
      return;
    }
    if (status === "sent" && !requiresDeviceResult(operation?.responseRequired)) {
      setPendingGroup(group, false);
      setStatus(group, "已下发（该命令无需设备回执）");
      return;
    }
    const operationId = operation?.operationId || "";
    if (!operationId) {
      // 服务端没记 operation ⇒ 这条只保证"报文已送达"，没有可轮询的终态。
      setPendingGroup(group, false);
      setStatus(group, "已下发（该命令无需设备回执）");
      return;
    }
    polls.set(group, {
      action,
      operationId,
      deadlineMs: parseDeadline(operation?.deadlineAt),
      timer: null,
      readTimer: null
    });
    setStatus(group, "等待设备应答");
    schedulePoll(group, channelId, token);
  } catch (error: any) {
    if (isStale(channelId, token)) return;
    const message = error?.message || "设备控制失败";
    setPendingGroup(group, false);
    setStatus(group, message, message, "error");
  }
}

/* 打开这一页 / 换通道时读一次缓存事实（不发 SIP 报文）。 */
watch(
  [() => props.channelId, () => props.active, () => props.canControl],
  ([channelId, active, canControl]) => {
    if (!active || !canControl || !channelId) return;
    clearPolls();
    const token = generation;
    void loadFacts(channelId, token);
  },
  { immediate: true }
);

onBeforeUnmount(() => clearPolls());

const hasChannel = computed(() => typeof props.channelId === "number" && props.channelId > 0);
const blockedReason = computed(() => {
  if (!props.canControl) return "当前账号没有设备控制权限";
  if (props.channelOptions.length === 0) return "该设备下暂无通道，无法下发控制命令";
  if (!hasChannel.value) return "请先选择一个目标通道";
  return "";
});

function recordLabel() {
  return recordState.value === "on" ? "停止设备端录制" : "开始设备端录制";
}

function guardLabel() {
  return guardState.value === "armed" ? "撤防" : "布防";
}

function recordStateText() {
  if (recordState.value === "on") return "设备录制中";
  if (recordState.value === "off") return "设备未录制";
  return "录制状态未知";
}

function guardStateText() {
  if (guardState.value === "alarm") return "ALARM 报警中";
  if (guardState.value === "armed") return "已布防";
  if (guardState.value === "disarmed") return "已撤防";
  return "布防状态未知";
}

/** 设备自报没有报警输入时「未知」就是错的 —— 它说的是「我没有报警输入」。 */
function alarmTargetHint() {
  if (alarmResolution.value?.status === "ambiguous") {
    const candidates = alarmResolution.value.candidates
      .map(candidate => candidate.code)
      .filter(Boolean)
      .join("、");
    return `报警目标不明确${candidates ? `（${candidates}）` : ""}，布防、撤防与复位会被服务端拒绝`;
  }
  if (alarmResolution.value?.status === "unavailable") {
    return "未解析出可用的 134 报警输入，命令将按注册父设备编码发送，以设备应答为准";
  }
  if (alarmResolution.value?.status === "resolved" && alarmResolution.value.targetCode) {
    return `报警目标 ${alarmResolution.value.targetCode}`;
  }
  return "";
}

function statusOf(action: ActionKey) {
  return statusText.value[groupOf(action)] || "以设备应答为准";
}

function levelOf(action: ActionKey): StatusLevel {
  return statusLevel.value[groupOf(action)] || "info";
}

function errorOf(action: ActionKey) {
  return errorText.value[groupOf(action)] || "";
}
</script>

<template>
  <div class="control-panel">
    <FactChannelPicker
      :options="props.channelOptions"
      :model-value="props.channelId"
      :loading="props.channelsLoading"
      @update:model-value="value => emit('update:channelId', value)"
    />

    <p v-if="blockedReason" class="fact-empty" data-testid="control-blocked">{{ blockedReason }}</p>

    <section class="fact-group">
      <header class="fact-group-head">
        <span class="fact-group-title"><Circle :size="14" />设备录制</span>
        <span class="fact-group-status" data-testid="control-record-fact">{{ recordStateText() }}</span>
      </header>
      <div class="control-actions">
        <a-button
          size="small"
          :type="recordState === 'on' ? 'default' : 'primary'"
          :loading="isPending('record_start')"
          :disabled="!!blockedReason"
          data-testid="control-record-toggle"
          @click="run(recordState === 'on' ? 'record_stop' : 'record_start')"
        >
          <template #icon>
            <Square v-if="recordState === 'on'" :size="13" />
            <Circle v-else :size="13" />
          </template>
          {{ recordLabel() }}
        </a-button>
        <!-- 状态未知时正反两个动作都摆出来：不猜，让操作员显式选。 -->
        <a-button
          v-if="recordState === 'unknown'"
          size="small"
          :loading="isPending('record_stop')"
          :disabled="!!blockedReason"
          data-testid="control-record-stop"
          @click="run('record_stop')"
        >
          <template #icon><Square :size="13" /></template>
          请求停止设备录制
        </a-button>
      </div>
      <p class="control-hint" :data-level="levelOf('record_start')" data-testid="control-record-status" aria-live="polite">
        {{ statusOf("record_start") }}
      </p>
      <p v-if="errorOf('record_start')" class="fact-error" data-testid="control-record-error">
        {{ errorOf("record_start") }}
      </p>
    </section>

    <section class="fact-group">
      <header class="fact-group-head">
        <span class="fact-group-title"><ShieldCheck :size="14" />布防与复位</span>
        <span class="fact-group-status" data-testid="control-guard-fact">{{ guardStateText() }}</span>
      </header>
      <p v-if="alarmTargetHint()" class="control-hint" data-testid="control-alarm-target">{{ alarmTargetHint() }}</p>
      <div class="control-actions">
        <a-button
          size="small"
          :type="guardState === 'armed' ? 'default' : 'primary'"
          :loading="isPending('guard_set')"
          :disabled="!!blockedReason"
          data-testid="control-guard-toggle"
          @click="run(guardState === 'armed' ? 'guard_reset' : 'guard_set')"
        >
          <template #icon><ShieldCheck :size="13" /></template>
          {{ guardLabel() }}
        </a-button>
        <a-button
          v-if="guardState === 'unknown'"
          size="small"
          :loading="isPending('guard_reset')"
          :disabled="!!blockedReason"
          data-testid="control-guard-reset"
          @click="run('guard_reset')"
        >
          <template #icon><ShieldCheck :size="13" /></template>
          请求撤防
        </a-button>
        <a-button
          size="small"
          :loading="isPending('alarm_reset')"
          :disabled="!!blockedReason"
          data-testid="control-alarm-reset"
          @click="run('alarm_reset')"
        >
          <template #icon><AlertTriangle :size="13" /></template>
          报警复位
        </a-button>
      </div>
      <p class="control-hint" :data-level="levelOf('guard_set')" data-testid="control-guard-status" aria-live="polite">
        {{ statusOf("guard_set") }}
      </p>
      <p v-if="errorOf('guard_set')" class="fact-error" data-testid="control-guard-error">
        {{ errorOf("guard_set") }}
      </p>
    </section>
  </div>
</template>

<style scoped>
.control-panel {
  display: grid;
  gap: 12px;
}
.control-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
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
.control-hint {
  margin: 0;
  font-size: 11.5px;
  line-height: 1.5;
  color: var(--uvp-text-tertiary);
  overflow-wrap: anywhere;
}

/* "结果未知"必须与"等待设备应答"长得不一样 —— 前者操作员要自己去看设备。 */
.control-hint[data-level="warn"] {
  color: var(--uvp-warning);
}
.control-hint[data-level="error"] {
  color: var(--uvp-danger);
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
</style>
