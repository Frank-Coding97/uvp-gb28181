<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute } from "vue-router";
import dayjs from "dayjs";
import { Message } from "@arco-design/web-vue";
import { AlertTriangle, CircleCheck, CircleOff, Clock, ListChecks, RefreshCcw, Search, ShieldAlert, Table2, TerminalSquare } from "lucide-vue-next";
import DetailPanel from "./components/DetailPanel.vue";
import SessionDetail from "./components/SessionDetail.vue";
import TableView from "./components/TableView.vue";
import TerminalView from "./components/TerminalView.vue";
import { formatFullTime } from "./helpers";
import {
    buildTraceStreamUrl,
    fetchTraceHealth,
    fetchTraceSessionStats,
    getTraceMessage,
    listTraceSessionMessages,
    listTraceSessions,
    type TraceHealth,
    type TraceMessageDetail,
    type TraceMessageSummary,
    type TraceDiagnosisCode,
    type TraceSessionQuery,
    type TraceSessionStats,
    type TraceSessionSummary
} from "@/api/gb28181-trace";

type FilterScope = "all" | "anomaly" | "register_failure" | "play_stuck";
type ViewMode = "table" | "terminal";
type TraceWindowMinutes = 5 | 10 | 30 | 60;
type TraceWindowPreset = TraceWindowMinutes | "custom";

const route = useRoute();

const DEFAULT_TRACE_WINDOW_MINUTES: TraceWindowMinutes = 5;
const traceWindowOptions: Array<{ value: TraceWindowMinutes; label: string }> = [
    { value: 5, label: "最近 5 分钟" },
    { value: 10, label: "最近 10 分钟" },
    { value: 30, label: "最近 30 分钟" },
    { value: 60, label: "最近 1 小时" }
];

function createLatestTraceRange(minutes: TraceWindowMinutes): string[] {
    const now = dayjs();
    return [now.subtract(minutes, "minute").format("YYYY-MM-DD HH:mm:ss"), now.format("YYYY-MM-DD HH:mm:ss")];
}

const filters = ref({
    range: [] as string[],
    scope: "all" as FilterScope,
    diagnosisCode: "" as TraceDiagnosisCode | "",
    keyword: ""
});
const traceWindowPreset = ref<TraceWindowPreset>(DEFAULT_TRACE_WINDOW_MINUTES);
const autoTraceRange = computed(() => traceWindowPreset.value !== "custom");

const sessions = ref<TraceSessionSummary[]>([]);
const sessionLoading = ref(false);
const stats = ref<TraceSessionStats | null>(null);
const statsLoading = ref(false);
const statsError = ref(false);
const health = ref<TraceHealth>({ state: "disabled", queueDepth: 0, queueCapacity: 0, dropped: 0 });
const detailLoading = ref(false);
const selectedEventId = ref<string>("");
const currentMessageDetail = ref<TraceMessageDetail | null>(null);
const viewMode = ref<ViewMode>("table");

// 会话时序详情:表格视图点击某行进入,详情内部 CallId
const drillCallId = ref<string>("");
const drillMessages = ref<TraceMessageSummary[]>([]);

const drillSession = computed<TraceSessionSummary | null>(() => {
    if (!drillCallId.value) return null;
    return sessions.value.find(s => s.callId === drillCallId.value) || null;
});

// 详情面板显示条件:只有会话时序详情页需要
const showDetailPanel = computed(() => !!drillCallId.value);

function buildBaseSessionQuery(): TraceSessionQuery {
    const [from, to] = autoTraceRange.value
        ? createLatestTraceRange(traceWindowPreset.value as TraceWindowMinutes)
        : filters.value.range;
    return {
        from: dayjs(from).toISOString(),
        to: dayjs(to).toISOString(),
        keyword: filters.value.keyword.trim() || undefined,
        limit: 200
    };
}

function buildSessionQuery(): TraceSessionQuery {
    return {
        ...buildBaseSessionQuery(),
        anomaly: filters.value.scope === "anomaly" ? true : undefined,
        diagnosisCategory: filters.value.scope === "register_failure" || filters.value.scope === "play_stuck"
            ? filters.value.scope
            : undefined,
        diagnosisCode: filters.value.diagnosisCode || undefined
    };
}

const diagnosisCodeOptions = computed<Array<{ value: TraceDiagnosisCode; label: string }>>(() => {
    if (filters.value.scope === "register_failure") {
        return [
            ["digest_failure", "摘要认证失败"], ["nonce_invalid", "Nonce 无效"],
            ["nonce_expired", "Nonce 已过期"], ["nonce_replay", "Nonce 重放"],
            ["server_id_mismatch", "平台 ID 不匹配"], ["device_not_preallocated", "设备未预分配"],
            ["invalid_request", "注册请求无效"], ["internal_error", "平台内部错误"],
            ["timeout", "注册超时"], ["undetermined", "待判断"]
        ].map(([value, label]) => ({ value: value as TraceDiagnosisCode, label }));
    }
    if (filters.value.scope === "play_stuck") {
        return [
            { value: "signaling_timeout", label: "信令响应超时" },
            { value: "media_timeout", label: "信令成功，媒体未就绪" }
        ];
    }
    return [];
});

async function loadSessions() {
    sessionLoading.value = true;
    try {
        const response = await listTraceSessions(buildSessionQuery());
        if (response.code === 0) {
            sessions.value = response.data.items || [];
        } else {
            Message.warning(response.message || "SIP 会话查询失败");
        }
    } catch (e: any) {
        Message.error(e?.message || "SIP 会话查询失败");
    } finally {
        sessionLoading.value = false;
    }
}

async function loadStats() {
    statsLoading.value = true;
    statsError.value = false;
    stats.value = null;
    try {
        const response = await fetchTraceSessionStats(buildBaseSessionQuery());
        if (response.code === 0) {
            stats.value = response.data;
        } else {
            statsError.value = true;
        }
    } catch {
        statsError.value = true;
    } finally {
        statsLoading.value = false;
    }
}

function statValue(field: keyof Pick<TraceSessionStats, "total" | "anomaly" | "registerFail" | "playStuck">): number | string {
    if (statsLoading.value || statsError.value || !stats.value) return "--";
    return stats.value[field];
}

const diagnosisIncomplete = computed(() =>
    health.value.state === "degraded" ||
    health.value.dropped > 0 ||
    health.value.diagnosis?.state === "degraded" ||
    (health.value.diagnosis?.dropped || 0) > 0 ||
    (health.value.diagnosis?.failed || 0) > 0
);

async function loadHealth() {
    try {
        const response = await fetchTraceHealth();
        if (response.code === 0) health.value = response.data;
    } catch {
        health.value = { state: "degraded", queueDepth: 0, queueCapacity: 0, dropped: 0, lastError: "健康接口不可用" };
    }
}

async function loadDrillMessages() {
    if (!drillCallId.value) return;
    detailLoading.value = true;
    try {
        const [from, to] = autoTraceRange.value
            ? createLatestTraceRange(traceWindowPreset.value as TraceWindowMinutes)
            : filters.value.range;
        const response = await listTraceSessionMessages(drillCallId.value, {
            from: dayjs(from).toISOString(),
            to: dayjs(to).toISOString(),
            limit: 500
        });
        if (response.code === 0) {
            const items = response.data.items || [];
            drillMessages.value = items.slice().sort(
                (a, b) => new Date(a.occurredAt).getTime() - new Date(b.occurredAt).getTime()
            );
            // 进入详情后默认选中第一条报文
            if (drillMessages.value[0]) {
                selectedEventId.value = drillMessages.value[0].eventId;
                await loadMessageDetail(selectedEventId.value);
            }
        }
    } catch (e: any) {
        Message.error(e?.message || "会话报文加载失败");
    } finally {
        detailLoading.value = false;
    }
}

async function loadMessageDetail(eventId: string) {
    if (!eventId) {
        currentMessageDetail.value = null;
        return;
    }
    detailLoading.value = true;
    try {
        const response = await getTraceMessage(eventId);
        if (response.code === 0) {
            currentMessageDetail.value = response.data;
        } else {
            currentMessageDetail.value = null;
        }
    } catch {
        currentMessageDetail.value = null;
    } finally {
        detailLoading.value = false;
    }
}

const currentMessage = computed<TraceMessageDetail | null>(() => currentMessageDetail.value);

function onSelectSession(session: TraceSessionSummary) {
    drillCallId.value = session.callId;
}

function onBackToList() {
    drillCallId.value = "";
    drillMessages.value = [];
    currentMessageDetail.value = null;
    selectedEventId.value = "";
}

async function onSelectMessage(message: TraceMessageSummary) {
    if (selectedEventId.value === message.eventId) return;
    selectedEventId.value = message.eventId;
    await loadMessageDetail(message.eventId);
}

function stepMessage(delta: 1 | -1) {
    // 时序详情内步进受限于当前会话报文
    if (drillCallId.value && drillMessages.value.length) {
        const idx = drillMessages.value.findIndex(m => m.eventId === selectedEventId.value);
        if (idx === -1) return;
        const next = drillMessages.value[idx + delta];
        if (next) onSelectMessage(next);
        return;
    }
}

async function refresh() {
    await Promise.all([loadSessions(), loadStats(), loadHealth()]);
    if (drillCallId.value) await loadDrillMessages();
}

function resetFilters() {
    traceWindowPreset.value = DEFAULT_TRACE_WINDOW_MINUTES;
    filters.value.range = [];
    filters.value.scope = "all";
    filters.value.diagnosisCode = "";
    filters.value.keyword = "";
}

function onRangeChange(range?: string[]) {
    if (range?.length === 2) {
        traceWindowPreset.value = "custom";
        return;
    }
    traceWindowPreset.value = DEFAULT_TRACE_WINDOW_MINUTES;
    filters.value.range = [];
}

async function selectTimePreset(value: TraceWindowMinutes) {
    traceWindowPreset.value = value;
    filters.value.range = [];
    await refresh();
}

function search() {
    return refresh();
}

function selectScope(scope: FilterScope) {
    filters.value.scope = scope;
    filters.value.diagnosisCode = "";
    return loadSessions();
}

// 进入详情页时加载报文
watch(drillCallId, async (newVal) => {
    if (newVal) await loadDrillMessages();
});

// 表格视图的实时刷新:挂 SSE 订阅只监听事件,收到 message 事件 debounce 500ms 触发一次表格刷新
// 不消费 payload,只用作"数据变化"的触发信号,避免前端聚合逻辑跟后端 session_day 视图不一致
const liveEnabled = ref(true);
let liveSource: EventSource | null = null;
let liveDebounceTimer: number | null = null;

function scheduleLiveRefresh() {
    if (liveDebounceTimer !== null) return; // 已经在等,不重复排
    liveDebounceTimer = window.setTimeout(async () => {
        liveDebounceTimer = null;
        // 仅在表格视图且没进详情页时才刷新
        if (viewMode.value === "table" && !drillCallId.value) {
            await Promise.all([loadSessions(), loadStats()]);
        }
    }, 500);
}

function openLiveSubscription() {
    closeLiveSubscription();
    if (!liveEnabled.value) return;
    // 只订阅事件不用消费,filter 传空拿全量,只在收到 message 时触发刷新
    liveSource = new EventSource(buildTraceStreamUrl());
    liveSource.addEventListener("message", () => scheduleLiveRefresh());
    // ping / ready / error 都不处理,浏览器自动重连
}

function closeLiveSubscription() {
    if (liveSource) {
        liveSource.close();
        liveSource = null;
    }
    if (liveDebounceTimer !== null) {
        clearTimeout(liveDebounceTimer);
        liveDebounceTimer = null;
    }
}

// 表格视图 + 未进入详情页 + liveEnabled 时才订阅;切换视图/进详情/关开关都清理
watch([viewMode, drillCallId, liveEnabled], () => {
    if (viewMode.value === "table" && !drillCallId.value && liveEnabled.value) {
        openLiveSubscription();
    } else {
        closeLiveSubscription();
    }
}, { immediate: false });

onMounted(async () => {
    // 从 URL 读取预填参数(来自设备管理页的诊断按钮)
    const { deviceIds, from, to } = route.query;
    if (deviceIds && typeof deviceIds === "string") {
        filters.value.keyword = deviceIds;
    }
    if (from && typeof from === "string" && to && typeof to === "string") {
        filters.value.range = [from, to];
        traceWindowPreset.value = "custom";
    }

    await Promise.all([loadHealth(), loadSessions(), loadStats()]);
    // 初始进入表格视图,启动实时订阅
    if (viewMode.value === "table" && liveEnabled.value) {
        openLiveSubscription();
    }
});

onBeforeUnmount(() => {
    closeLiveSubscription();
});
</script>

<template>
    <div class="snow-fill">
        <div class="snow-fill-inner uvp-page-shell-flat sip-log-shell">
            <!-- 顶部工具栏 -->
            <div class="toolbar">
                <div class="toolbar-left">
                    <span class="page-title">SIP 日志</span>
                    <span :class="['head-badge', `health-${health.state}`]" role="status" aria-live="polite" aria-atomic="true">
                        <CircleCheck v-if="health.state === 'ready'" class="health-icon-ready" :size="13" aria-hidden="true" />
                        <AlertTriangle v-else-if="health.state === 'degraded'" class="health-icon-degraded" :size="13" aria-hidden="true" />
                        <CircleOff v-else class="health-icon-disabled" :size="13" aria-hidden="true" />
                        {{ health.state === 'ready' ? '采集正常' : health.state === 'degraded' ? '存储降级' : '未启用' }}
                    </span>
                    <span v-if="health.lastSuccessAt" class="head-meta">最后写入 {{ formatFullTime(health.lastSuccessAt) }}</span>
                </div>
                <div class="toolbar-right">
                    <!-- 实时刷新开关:只在表格视图列表模式下有效 -->
                    <label
                        v-if="viewMode === 'table' && !drillCallId"
                        class="live-toggle"
                        :class="{ active: liveEnabled }"
                    >
                        <input v-model="liveEnabled" type="checkbox" />
                        <span class="live-dot" />
                        实时
                    </label>
                    <div class="view-switch" v-if="!drillCallId">
                        <button
                            v-for="v in ([{key:'table',label:'表格',icon:Table2},{key:'terminal',label:'终端',icon:TerminalSquare}] as const)"
                            :key="v.key"
                            type="button"
                            :class="['view-btn', { active: viewMode === v.key }]"
                            @click="viewMode = v.key"
                        >
                            <component :is="v.icon" :size="14" />
                            {{ v.label }}
                        </button>
                    </div>
                    <a-tooltip content="刷新">
                        <button class="icon-btn" :disabled="sessionLoading" @click="refresh">
                            <RefreshCcw :size="15" :class="{ spin: sessionLoading }" />
                        </button>
                    </a-tooltip>
                </div>
            </div>

            <div v-if="diagnosisIncomplete" class="diagnosis-warning" role="status">
                <AlertTriangle :size="14" aria-hidden="true" />
                诊断数据可能不完整，原始 SIP 报文仍可查看
            </div>

            <!-- 统计卡片(时序详情页时隐藏) -->
            <div v-if="!drillCallId" class="stat-band">
                <button type="button" :class="['stat-card', { active: filters.scope === 'all' }]" @click="selectScope('all')">
                    <span class="stat-icon icon-total"><ListChecks :size="18" /></span>
                    <div class="stat-content">
                        <span :class="['stat-num', { error: statsError }]">{{ statValue('total') }}</span>
                        <span class="stat-label">全部会话</span>
                    </div>
                </button>
                <button type="button" :class="['stat-card', 'warning', { active: filters.scope === 'anomaly' }]" @click="selectScope('anomaly')">
                    <span class="stat-icon icon-anomaly"><AlertTriangle :size="18" /></span>
                    <div class="stat-content">
                        <span :class="['stat-num', 'warning', { error: statsError }]">{{ statValue('anomaly') }}</span>
                        <span class="stat-label">异常会话</span>
                    </div>
                </button>
                <button type="button" :class="['stat-card', 'danger', { active: filters.scope === 'register_failure' }]" @click="selectScope('register_failure')">
                    <span class="stat-icon icon-danger"><ShieldAlert :size="18" /></span>
                    <div class="stat-content">
                        <span :class="['stat-num', 'danger', { error: statsError }]">{{ statValue('registerFail') }}</span>
                        <span class="stat-label">注册失败</span>
                    </div>
                </button>
                <button type="button" :class="['stat-card', 'warning', { active: filters.scope === 'play_stuck' }]" @click="selectScope('play_stuck')">
                    <span class="stat-icon icon-pending"><Clock :size="18" /></span>
                    <div class="stat-content">
                        <span :class="['stat-num', 'warning', { error: statsError }]">{{ statValue('playStuck') }}</span>
                        <span class="stat-label">点播卡住</span>
                    </div>
                </button>
            </div>

            <!-- 筛选栏(时序详情页时隐藏)- 走系统 s-layout-search 封装 -->
            <s-layout-search v-if="!drillCallId" class="sip-log-search">
                <template #fields>
                    <a-range-picker
                        v-model="filters.range"
                        show-time
                        value-format="YYYY-MM-DD HH:mm:ss"
                        format="MM-DD HH:mm"
                        style="width: 320px"
                        allow-clear
                        @change="onRangeChange"
                    />
                    <a-select
                        v-model="traceWindowPreset"
                        class="window-preset-select"
                        style="width: 160px"
                        @change="selectTimePreset"
                    >
                        <template #prefix><Clock :size="14" stroke-width="2.2" aria-hidden="true" /></template>
                        <a-option v-for="item in traceWindowOptions" :key="item.value" :value="item.value">
                            {{ item.label }}
                        </a-option>
                        <a-option v-if="traceWindowPreset === 'custom'" value="custom" disabled>
                            自定义区间
                        </a-option>
                    </a-select>
                    <a-input
                        v-model="filters.keyword"
                        allow-clear
                        placeholder="搜索设备 ID、名称或 Call-ID"
                        style="width: 300px"
                        @press-enter="search"
                    >
                        <template #prefix><Search :size="14" /></template>
                    </a-input>
                    <a-select
                        v-if="diagnosisCodeOptions.length"
                        v-model="filters.diagnosisCode"
                        allow-clear
                        placeholder="全部失败类型"
                        style="width: 220px"
                        @change="loadSessions"
                    >
                        <a-option v-for="item in diagnosisCodeOptions" :key="item.value" :value="item.value">
                            {{ item.label }}
                        </a-option>
                    </a-select>
                </template>
                <template #actions>
                    <a-button type="primary" @click="search">
                        <template #icon><icon-search /></template>
                        <span>查询</span>
                    </a-button>
                    <a-button @click="resetFilters">
                        <template #icon><icon-refresh /></template>
                        <span>重置</span>
                    </a-button>
                </template>
            </s-layout-search>

            <!-- 主体:视图区(左) + 可选的详情面板(右) -->
            <div :class="['workspace', { 'has-detail': showDetailPanel }]">
                <div class="view-slot">
                    <!-- 会话时序详情:表格点某会话进入 -->
                    <SessionDetail
                        v-if="drillCallId && drillSession"
                        :session="drillSession"
                        :messages="drillMessages"
                        :selected-event-id="selectedEventId"
                        @back="onBackToList"
                        @select-message="onSelectMessage"
                    />
                    <TableView
                        v-else-if="viewMode === 'table'"
                        :sessions="sessions"
                        :selected-call-id="drillCallId"
                        :loading="sessionLoading"
                        @select-session="onSelectSession"
                    />
                    <TerminalView v-else />
                </div>
                <DetailPanel
                    v-if="showDetailPanel"
                    :message="currentMessage"
                    :loading="detailLoading"
                    @step="stepMessage"
                />
            </div>
        </div>
    </div>
</template>

<style scoped>
.sip-log-shell {
    display: flex;
    flex-direction: column;
    gap: 12px;
    height: 100%;
    min-height: 0;
}

.toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
}
.toolbar-left { display: flex; align-items: center; gap: 12px; min-width: 0; }
.toolbar-right { display: flex; align-items: center; gap: 10px; }
.page-title { color: var(--uvp-text-primary); font-size: 16px; font-weight: 600; letter-spacing: 0.2px; }
.head-badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    height: 22px;
    padding: 0 8px;
    font-size: 12px;
    font-weight: 500;
    border-radius: 4px;
}
.head-badge.health-ready { color: #047857; background: rgb(5 150 105 / 10%); }
.head-badge.health-degraded { color: #b45309; background: rgb(217 119 6 / 10%); }
.head-badge.health-disabled { color: var(--uvp-text-tertiary); background: var(--uvp-list-toolbar-bg); }
.head-meta { color: var(--uvp-text-tertiary); font-size: 12px; }
.diagnosis-warning {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 30px;
    padding: 0 10px;
    color: #b45309;
    background: rgb(217 119 6 / 8%);
    border: 1px solid rgb(217 119 6 / 24%);
    border-radius: 6px;
    font-size: 12px;
}

/* 实时刷新开关 */
.live-toggle {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 5px 10px;
    color: var(--uvp-text-tertiary);
    background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 8px;
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
    user-select: none;
    transition: color 120ms;
}
.live-toggle input { display: none; }
.live-dot {
    display: inline-block;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: color-mix(in srgb, var(--uvp-text-tertiary) 40%, transparent);
    transition: background 120ms;
}
.live-toggle.active { color: #059669; border-color: color-mix(in srgb, #059669 40%, var(--uvp-panel-border)); }
.live-toggle.active .live-dot {
    background: #10b981;
    box-shadow: 0 0 0 3px rgb(16 185 129 / 20%);
    animation: pulse-live 1.6s ease-in-out infinite;
}
@keyframes pulse-live { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }

.view-switch {
    display: inline-flex;
    padding: 3px;
    background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 8px;
}
.view-btn {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 5px 12px;
    color: var(--uvp-text-secondary);
    background: transparent;
    border: 0;
    border-radius: 6px;
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
    transition: background 120ms, color 120ms;
}
.view-btn:hover { color: var(--uvp-text-primary); }
.view-btn.active {
    color: var(--uvp-brand);
    background: var(--uvp-panel-bg);
    box-shadow: 0 1px 2px rgb(15 23 42 / 8%);
}

.icon-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    padding: 0;
    color: var(--uvp-text-secondary);
    background: var(--uvp-panel-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 8px;
    cursor: pointer;
}
.icon-btn:hover:not(:disabled) { color: var(--uvp-brand); border-color: var(--uvp-brand); }
.icon-btn:disabled { cursor: wait; opacity: 0.6; }
.spin { animation: rotate 900ms linear infinite; }
@keyframes rotate { to { transform: rotate(360deg); } }

.stat-band {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 12px;
}
.stat-card {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 14px 18px;
    color: var(--uvp-text-secondary);
    background: var(--uvp-panel-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 8px;
    text-align: left;
    cursor: pointer;
    transition: border-color 120ms, box-shadow 120ms, transform 120ms;
}
.stat-card:hover { border-color: var(--uvp-brand); transform: translateY(-1px); }
.stat-card.active { border-color: var(--uvp-brand); box-shadow: inset 0 0 0 1px var(--uvp-brand); }
.stat-card.warning.active { border-color: var(--uvp-warning); box-shadow: inset 0 0 0 1px var(--uvp-warning); }
.stat-card.danger.active { border-color: var(--uvp-danger); box-shadow: inset 0 0 0 1px var(--uvp-danger); }

.stat-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 40px;
    height: 40px;
    border-radius: 10px;
    flex: 0 0 auto;
}
.stat-icon.icon-total { color: var(--uvp-brand); background: var(--uvp-brand-soft); }
.stat-icon.icon-anomaly { color: var(--uvp-warning); background: var(--uvp-warning-soft); }
.stat-icon.icon-danger { color: var(--uvp-danger); background: var(--uvp-danger-soft); }
.stat-icon.icon-pending { color: var(--uvp-warning); background: var(--uvp-warning-soft); }

.stat-content { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.stat-num { color: var(--uvp-text-primary); font-size: 22px; font-weight: 600; line-height: 1.1; }
.stat-num.danger { color: var(--uvp-danger); }
.stat-num.warning { color: var(--uvp-warning); }
.stat-num.error { color: var(--uvp-text-tertiary); }
.stat-label { color: var(--uvp-text-tertiary); font-size: 12px; }

/* s-layout-search 内嵌样式微调 */
.sip-log-search { margin-bottom: 0; }
.window-preset-select { flex: 0 0 160px; }
.window-preset-select :deep(.arco-select-view) {
    color: color-mix(in srgb, var(--uvp-brand) 78%, var(--uvp-text-primary));
    background: color-mix(in srgb, var(--uvp-brand) 7%, var(--uvp-panel-bg));
    border-color: color-mix(in srgb, var(--uvp-brand) 24%, var(--uvp-panel-border));
    border-radius: 7px;
    box-shadow: 0 1px 2px rgb(37 99 235 / 6%);
    font-size: 12px;
    font-weight: 550;
}
.window-preset-select :deep(.arco-select-view-prefix) { color: var(--uvp-brand); }

.workspace {
    display: grid;
    grid-template-columns: 1fr;
    flex: 1;
    min-height: 0;
    gap: 10px;
    overflow: hidden;
}
.workspace.has-detail {
    grid-template-columns: 1fr 560px;
}
.view-slot {
    display: grid;
    grid-template-columns: 1fr;
    grid-template-rows: 1fr;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
}
.view-slot > * { min-width: 0; min-height: 0; }

@media (max-width: 1500px) {
    .workspace.has-detail { grid-template-columns: 1fr 480px; }
}
@media (max-width: 1300px) {
    .workspace.has-detail { grid-template-columns: 1fr 420px; }
}
@media (max-width: 1200px) {
    .workspace.has-detail { grid-template-columns: 1fr; grid-template-rows: 1fr auto; }
    .stat-band { grid-template-columns: repeat(2, 1fr); }
}
@media (max-width: 768px) {
    .toolbar { align-items: flex-start; flex-wrap: wrap; }
    .toolbar-left { flex-wrap: wrap; gap: 8px; }
    .toolbar-right { width: 100%; flex-wrap: wrap; justify-content: flex-end; gap: 8px; }
    .head-meta { flex-basis: 100%; }
    .stat-band { gap: 8px; }
    .stat-card { gap: 8px; padding: 10px; }
    .stat-icon { width: 34px; height: 34px; border-radius: 6px; }
    .stat-num { font-size: 19px; }
}
</style>
