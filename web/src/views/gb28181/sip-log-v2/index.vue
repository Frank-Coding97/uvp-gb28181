<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import dayjs from "dayjs";
import { Message } from "@arco-design/web-vue";
import { AlertTriangle, Clock, ListChecks, RefreshCcw, Search, ShieldAlert, Table2, TerminalSquare, Wifi } from "lucide-vue-next";
import DetailPanel from "./components/DetailPanel.vue";
import SessionDetail from "./components/SessionDetail.vue";
import TableView from "./components/TableView.vue";
import TerminalView from "./components/TerminalView.vue";
import { formatFullTime } from "./helpers";
import { listDevices, type DeviceVO } from "@/views/gb28181/device-mgmt/api";
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
    type TraceSessionStats,
    type TraceSessionSummary
} from "@/api/gb28181-trace";

type FilterScope = "all" | "anomaly";
type ViewMode = "table" | "terminal";

const filters = ref({
    range: [dayjs().subtract(15, "minute").format("YYYY-MM-DD HH:mm:ss"), dayjs().format("YYYY-MM-DD HH:mm:ss")] as string[],
    deviceIds: [] as string[],
    scope: "all" as FilterScope,
    keyword: ""
});

const sessions = ref<TraceSessionSummary[]>([]);
const sessionLoading = ref(false);
const stats = ref<TraceSessionStats>({ total: 0, anomaly: 0, registerFail: 0, invitePending: 0 });
const health = ref<TraceHealth>({ state: "disabled", queueDepth: 0, queueCapacity: 0, dropped: 0 });
const devices = ref<DeviceVO[]>([]);
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

function buildSessionQuery(): { from: string; to: string; deviceIds?: string; keyword?: string; anomaly?: boolean; limit: number } {
    return {
        from: dayjs(filters.value.range[0]).toISOString(),
        to: dayjs(filters.value.range[1]).toISOString(),
        deviceIds: filters.value.deviceIds.length ? filters.value.deviceIds.join(",") : undefined,
        keyword: filters.value.keyword.trim() || undefined,
        anomaly: filters.value.scope === "anomaly" ? true : undefined,
        limit: 200
    };
}

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
    try {
        const response = await fetchTraceSessionStats(buildSessionQuery());
        if (response.code === 0) {
            stats.value = response.data || { total: 0, anomaly: 0, registerFail: 0, invitePending: 0 };
        }
    } catch {
        // 统计接口失败不阻断主流程
    }
}

async function loadHealth() {
    try {
        const response = await fetchTraceHealth();
        if (response.code === 0) health.value = response.data;
    } catch {
        health.value = { state: "degraded", queueDepth: 0, queueCapacity: 0, dropped: 0, lastError: "健康接口不可用" };
    }
}

async function loadDevices() {
    try {
        const response = await listDevices({ page: 1, pageSize: 200 });
        if (response.code === 0) devices.value = response.data?.list || [];
    } catch {
        devices.value = [];
    }
}

async function loadDrillMessages() {
    if (!drillCallId.value) return;
    detailLoading.value = true;
    try {
        const response = await listTraceSessionMessages(drillCallId.value, {
            from: dayjs(filters.value.range[0]).toISOString(),
            to: dayjs(filters.value.range[1]).toISOString(),
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
    filters.value.deviceIds = [];
    filters.value.scope = "all";
    filters.value.keyword = "";
}

function search() {
    return refresh();
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
    await Promise.all([loadDevices(), loadHealth(), loadSessions(), loadStats()]);
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
                    <span :class="['head-badge', `health-${health.state}`]">
                        <Wifi :size="12" />
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
                            v-for="v in ([{key:'table',label:'表格',icon:Table2},{key:'terminal',label:'实时',icon:TerminalSquare}] as const)"
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

            <!-- 统计卡片(时序详情页时隐藏) -->
            <div v-if="!drillCallId" class="stat-band">
                <button type="button" :class="['stat-card', { active: filters.scope === 'all' }]" @click="filters.scope = 'all'">
                    <span class="stat-icon icon-total"><ListChecks :size="18" /></span>
                    <div class="stat-content">
                        <span class="stat-num">{{ stats.total }}</span>
                        <span class="stat-label">全部会话</span>
                    </div>
                </button>
                <button type="button" :class="['stat-card', 'warning', { active: filters.scope === 'anomaly' }]" @click="filters.scope = 'anomaly'">
                    <span class="stat-icon icon-anomaly"><AlertTriangle :size="18" /></span>
                    <div class="stat-content">
                        <span class="stat-num warning">{{ stats.anomaly }}</span>
                        <span class="stat-label">异常会话</span>
                    </div>
                </button>
                <div class="stat-card readonly">
                    <span class="stat-icon icon-danger"><ShieldAlert :size="18" /></span>
                    <div class="stat-content">
                        <span class="stat-num danger">{{ stats.registerFail }}</span>
                        <span class="stat-label">注册失败</span>
                    </div>
                </div>
                <div class="stat-card readonly">
                    <span class="stat-icon icon-pending"><Clock :size="18" /></span>
                    <div class="stat-content">
                        <span class="stat-num warning">{{ stats.invitePending }}</span>
                        <span class="stat-label">点播卡住</span>
                    </div>
                </div>
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
                    />
                    <a-select
                        v-model="filters.deviceIds"
                        multiple
                        allow-clear
                        allow-search
                        :max-tag-count="1"
                        placeholder="全部设备"
                        style="width: 200px"
                    >
                        <a-option v-for="d in devices" :key="d.id" :value="d.deviceId">{{ d.name || d.alias || d.deviceId }}</a-option>
                    </a-select>
                    <a-input
                        v-model="filters.keyword"
                        allow-clear
                        placeholder="搜索 Call-ID 或设备编号"
                        style="width: 260px"
                        @press-enter="() => {}"
                    >
                        <template #prefix><Search :size="14" /></template>
                    </a-input>
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
                    <TerminalView
                        v-else
                        :device-ids="filters.deviceIds"
                    />
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
.head-badge.health-ok { color: #059669; background: rgb(5 150 105 / 10%); }
.head-meta { color: var(--uvp-text-tertiary); font-size: 12px; }

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
    border-radius: 12px;
    text-align: left;
    cursor: pointer;
    transition: border-color 120ms, box-shadow 120ms, transform 120ms;
}
.stat-card:not(.readonly):hover { border-color: var(--uvp-brand); transform: translateY(-1px); }
.stat-card.active { border-color: var(--uvp-brand); box-shadow: inset 0 0 0 1px var(--uvp-brand); }
.stat-card.warning.active { border-color: var(--uvp-warning); box-shadow: inset 0 0 0 1px var(--uvp-warning); }
.stat-card.readonly { cursor: default; }

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
.stat-label { color: var(--uvp-text-tertiary); font-size: 12px; }

/* s-layout-search 内嵌样式微调 */
.sip-log-search { margin-bottom: 0; }

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
</style>
