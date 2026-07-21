<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import dayjs from "dayjs";
import { Message } from "@arco-design/web-vue";
import {
    ArrowDown,
    ArrowUp,
    Copy,
    Filter,
    RefreshCcw,
    Search,
    ShieldCheck,
    Wifi
} from "lucide-vue-next";
import {
    MOCK_DEVICES,
    MOCK_HEALTH,
    MOCK_MESSAGES,
    MOCK_SESSIONS,
    type TraceMessage,
    type TraceSession
} from "./fixtures";
import {
    formatDuration,
    formatFullTime,
    formatTime,
    isKeyHeader,
    methodChainTone,
    methodLabel,
    methodTone,
    parsePayload,
    sessionStateLabel,
    statusTone
} from "./helpers";

type FilterScope = "all" | "anomaly";
type DetailTab = "structured" | "raw" | "sdp" | "xml";

const anchorTime = dayjs("2026-07-21T15:00:00Z");
const filters = ref({
    range: [anchorTime.subtract(15, "minute").format("YYYY-MM-DD HH:mm:ss"), anchorTime.format("YYYY-MM-DD HH:mm:ss")] as string[],
    deviceIds: [] as string[],
    scope: "all" as FilterScope,
    keyword: ""
});
const sessionLoading = ref(false);
const messageLoading = ref(false);
const detailLoading = ref(false);
const selectedCallId = ref<string>("");
const selectedEventId = ref<string>("");
const detailTab = ref<DetailTab>("structured");

onMounted(() => {
    const first = MOCK_SESSIONS.find(s => s.anomaly) || MOCK_SESSIONS[0];
    if (first) {
        selectedCallId.value = first.callId;
        const msgs = MOCK_MESSAGES[first.callId];
        if (msgs && msgs[0]) selectedEventId.value = msgs[0].eventId;
    }
});

const filteredSessions = computed<TraceSession[]>(() => {
    return MOCK_SESSIONS.filter(session => {
        if (filters.value.scope === "anomaly" && !session.anomaly) return false;
        if (filters.value.deviceIds.length && !filters.value.deviceIds.includes(session.deviceId)) return false;
        const keyword = filters.value.keyword.trim().toLowerCase();
        if (keyword) {
            const hay = `${session.callId} ${session.deviceId} ${session.deviceLabel}`.toLowerCase();
            if (!hay.includes(keyword)) return false;
        }
        return true;
    });
});

const currentSession = computed<TraceSession | null>(() =>
    filteredSessions.value.find(s => s.callId === selectedCallId.value) || null
);

const currentMessages = computed<TraceMessage[]>(() =>
    currentSession.value ? MOCK_MESSAGES[currentSession.value.callId] || [] : []
);

const currentMessage = computed<TraceMessage | null>(() =>
    currentMessages.value.find(m => m.eventId === selectedEventId.value) || null
);

const parsedPayload = computed(() => currentMessage.value ? parsePayload(currentMessage.value.payload) : null);

const availableTabs = computed<DetailTab[]>(() => {
    const tabs: DetailTab[] = ["structured", "raw"];
    if (parsedPayload.value?.bodyType === "sdp") tabs.push("sdp");
    if (parsedPayload.value?.bodyType === "xml") tabs.push("xml");
    return tabs;
});

const stats = computed(() => {
    const list = filteredSessions.value;
    return {
        total: list.length,
        anomaly: list.filter(s => s.anomaly).length,
        registerFail: list.filter(s => s.scenario === "register-fail").length,
        pending: list.filter(s => s.scenario === "invite-pending").length
    };
});

const tabLabel: Record<DetailTab, string> = { structured: "概要", raw: "原文", sdp: "SDP", xml: "XML" };

function selectSession(session: TraceSession) {
    if (selectedCallId.value === session.callId) return;
    messageLoading.value = true;
    selectedCallId.value = session.callId;
    setTimeout(() => {
        const msgs = MOCK_MESSAGES[session.callId] || [];
        selectedEventId.value = msgs[0]?.eventId || "";
        detailTab.value = "structured";
        messageLoading.value = false;
    }, 120);
}

function selectMessage(message: TraceMessage) {
    if (selectedEventId.value === message.eventId) return;
    detailLoading.value = true;
    selectedEventId.value = message.eventId;
    detailTab.value = "structured";
    setTimeout(() => { detailLoading.value = false; }, 60);
}

function stepMessage(delta: 1 | -1) {
    if (!currentMessages.value.length) return;
    const idx = currentMessages.value.findIndex(m => m.eventId === selectedEventId.value);
    if (idx < 0) return;
    const next = Math.min(Math.max(idx + delta, 0), currentMessages.value.length - 1);
    selectMessage(currentMessages.value[next]);
}

async function copyPayload() {
    if (!currentMessage.value) return;
    try {
        await navigator.clipboard.writeText(currentMessage.value.payload);
        Message.success("已复制到剪贴板");
    } catch {
        Message.warning("复制失败,请手动选中");
    }
}

function refresh() {
    sessionLoading.value = true;
    setTimeout(() => { sessionLoading.value = false; }, 400);
}

function resetFilters() {
    filters.value.deviceIds = [];
    filters.value.scope = "all";
    filters.value.keyword = "";
}
</script>

<template>
    <div class="snow-fill">
        <div class="snow-fill-inner uvp-page-shell-flat sip-log-shell">
            <header class="page-head">
                <div class="head-left">
                    <span class="page-title">SIP 日志</span>
                    <span class="head-badge health-ok">
                        <Wifi :size="12" />
                        采集正常
                    </span>
                    <span class="head-meta">最后写入 {{ formatFullTime(MOCK_HEALTH.lastSuccessAt) }}</span>
                </div>
                <div class="head-right">
                    <a-tooltip content="刷新数据">
                        <button class="icon-btn" :disabled="sessionLoading" @click="refresh">
                            <RefreshCcw :size="15" :class="{ spin: sessionLoading }" />
                        </button>
                    </a-tooltip>
                </div>
            </header>

            <section class="stat-band">
                <button type="button" :class="['stat-card', { active: filters.scope === 'all' }]" @click="filters.scope = 'all'">
                    <span class="stat-num">{{ stats.total }}</span>
                    <span class="stat-label">全部会话</span>
                </button>
                <button type="button" :class="['stat-card', 'warning', { active: filters.scope === 'anomaly' }]" @click="filters.scope = 'anomaly'">
                    <span class="stat-num warning">{{ stats.anomaly }}</span>
                    <span class="stat-label">异常会话</span>
                </button>
                <div class="stat-card readonly">
                    <span class="stat-num danger">{{ stats.registerFail }}</span>
                    <span class="stat-label">注册失败</span>
                </div>
                <div class="stat-card readonly">
                    <span class="stat-num warning">{{ stats.pending }}</span>
                    <span class="stat-label">点播卡住</span>
                </div>
            </section>

            <section class="filter-band">
                <a-range-picker
                    v-model="filters.range"
                    show-time
                    value-format="YYYY-MM-DD HH:mm:ss"
                    format="MM-DD HH:mm"
                    class="filter-range"
                />
                <a-select
                    v-model="filters.deviceIds"
                    multiple
                    allow-clear
                    allow-search
                    :max-tag-count="1"
                    placeholder="全部设备"
                    class="filter-device"
                >
                    <a-option v-for="d in MOCK_DEVICES" :key="d.id" :value="d.deviceId">{{ d.label }}</a-option>
                </a-select>
                <a-input
                    v-model="filters.keyword"
                    allow-clear
                    placeholder="搜索 Call-ID 或设备编号"
                    class="filter-keyword"
                >
                    <template #prefix><Search :size="14" /></template>
                </a-input>
                <button class="reset-btn" type="button" @click="resetFilters">
                    <Filter :size="14" />
                    重置
                </button>
            </section>

            <main class="main-grid">
                <section class="pane sessions-pane" aria-label="会话列表">
                    <div class="pane-head">
                        <span class="pane-title">会话</span>
                        <span class="pane-meta">{{ filteredSessions.length }} 条</span>
                    </div>
                    <div class="pane-body">
                        <a-spin :loading="sessionLoading" class="pane-spin">
                            <button
                                v-for="session in filteredSessions"
                                :key="session.callId"
                                type="button"
                                :class="['session-card', { active: selectedCallId === session.callId, anomaly: session.anomaly && selectedCallId !== session.callId }]"
                                @click="selectSession(session)"
                            >
                                <div class="session-card-top">
                                    <span class="device-label">{{ session.deviceLabel }}</span>
                                    <span :class="['session-state', `state-${sessionStateLabel(session).tone}`]">
                                        {{ sessionStateLabel(session).label }}
                                    </span>
                                </div>
                                <div class="method-chain">
                                    <span
                                        v-for="(step, idx) in session.methodSequence"
                                        :key="idx"
                                        :class="['chain-item', `chain-${methodChainTone(step)}`]"
                                    >{{ step }}</span>
                                </div>
                                <span class="call-id mono">{{ session.callId }}</span>
                                <div class="session-card-meta">
                                    <span>{{ formatTime(session.firstAt) }}</span>
                                    <span class="dot-sep">·</span>
                                    <span>{{ session.messageCount }} 条报文</span>
                                    <span class="dot-sep">·</span>
                                    <span>{{ formatDuration(session.durationMs) }}</span>
                                </div>
                            </button>
                            <a-empty v-if="!filteredSessions.length" description="当前筛选范围无会话" />
                        </a-spin>
                    </div>
                </section>

                <section class="pane messages-pane" aria-label="报文时序">
                    <div class="pane-head">
                        <span class="pane-title">
                            报文时序
                            <span v-if="currentSession" class="head-inline-meta">
                                · {{ currentSession.messageCount }} 条 · {{ formatDuration(currentSession.durationMs) }}
                            </span>
                        </span>
                        <span v-if="currentSession?.hasAuthChallenge" class="pane-meta">
                            <ShieldCheck :size="12" />
                            经过鉴权
                        </span>
                    </div>
                    <div class="pane-body">
                        <a-spin :loading="messageLoading" class="pane-spin">
                            <div v-if="currentMessages.length" class="message-list">
                                <button
                                    v-for="msg in currentMessages"
                                    :key="msg.eventId"
                                    type="button"
                                    :class="['message-row', msg.direction, { active: selectedEventId === msg.eventId }]"
                                    @click="selectMessage(msg)"
                                >
                                    <span class="msg-time mono">{{ formatTime(msg.occurredAt) }}</span>
                                    <span :class="['dir-chip', msg.direction]">
                                        <component :is="msg.direction === 'inbound' ? ArrowDown : ArrowUp" :size="11" />
                                        {{ msg.direction === 'inbound' ? '收' : '发' }}
                                    </span>
                                    <span :class="['method-chip', `chip-${msg.statusCode ? statusTone(msg.statusCode) : methodTone(msg.method)}`]">
                                        {{ methodLabel(msg) }}
                                    </span>
                                    <span class="msg-cseq mono">CSeq {{ msg.cseq }} {{ msg.cseqMethod }}</span>
                                    <span class="msg-transport">{{ msg.transport }}</span>
                                </button>
                            </div>
                            <a-empty v-else description="选择左侧会话查看报文" />
                        </a-spin>
                    </div>
                </section>

                <aside class="pane detail-pane" aria-label="报文详情">
                    <div class="pane-head">
                        <span class="pane-title">
                            报文详情
                            <span v-if="currentMessage" class="head-inline-meta">
                                · CSeq {{ currentMessage.cseq }} {{ currentMessage.cseqMethod }}
                            </span>
                        </span>
                        <div class="pane-actions">
                            <a-tooltip content="上一条 ↑"><button class="step-btn" :disabled="!currentMessage" @click="stepMessage(-1)"><ArrowUp :size="13" /></button></a-tooltip>
                            <a-tooltip content="下一条 ↓"><button class="step-btn" :disabled="!currentMessage" @click="stepMessage(1)"><ArrowDown :size="13" /></button></a-tooltip>
                            <a-tooltip content="复制原文"><button class="step-btn" :disabled="!currentMessage" @click="copyPayload"><Copy :size="13" /></button></a-tooltip>
                        </div>
                    </div>
                    <div v-if="currentMessage" class="detail-tabs">
                        <button
                            v-for="tab in availableTabs"
                            :key="tab"
                            type="button"
                            :class="['detail-tab', { active: detailTab === tab }]"
                            @click="detailTab = tab"
                        >{{ tabLabel[tab] }}</button>
                    </div>
                    <div class="pane-body detail-body">
                        <a-spin :loading="detailLoading" class="pane-spin">
                            <div v-if="currentMessage && parsedPayload" class="detail-content">
                                <div v-if="detailTab === 'structured'" class="structured-view">
                                    <div class="detail-block">
                                        <span class="block-label">起始行</span>
                                        <code class="start-line-text mono">{{ parsedPayload.startLine }}</code>
                                    </div>
                                    <div class="detail-block">
                                        <span class="block-label">头部字段</span>
                                        <dl class="header-list">
                                            <template v-for="(h, idx) in parsedPayload.headers" :key="idx">
                                                <dt :class="{ 'key-header': isKeyHeader(h.name) }">{{ h.name }}</dt>
                                                <dd :class="['mono', { 'key-header-val': isKeyHeader(h.name) }]">{{ h.value }}</dd>
                                            </template>
                                        </dl>
                                    </div>
                                    <div v-if="parsedPayload.bodyType !== 'empty'" class="detail-block">
                                        <span class="block-label">正文 · {{ parsedPayload.bodyType.toUpperCase() }}</span>
                                        <pre class="body-code mono">{{ parsedPayload.body }}</pre>
                                    </div>
                                </div>
                                <pre v-else-if="detailTab === 'raw'" class="raw-code mono">{{ currentMessage.payload }}</pre>
                                <pre v-else class="raw-code mono">{{ parsedPayload.body }}</pre>
                            </div>
                            <a-empty v-else description="选中报文后查看详情" />
                        </a-spin>
                    </div>
                </aside>
            </main>
        </div>
    </div>
</template>

<style scoped>
.sip-log-shell {
    display: flex;
    flex-direction: column;
    min-width: 0;
    padding: 0;
    overflow: hidden;
    background: var(--uvp-shell-muted);
}

.mono {
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
}

/* Header */
.page-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 20px 10px;
}
.head-left { display: flex; align-items: center; gap: 12px; min-width: 0; }
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
.head-badge.health-ok {
    color: #059669;
    background: rgb(5 150 105 / 10%);
}
.head-meta { color: var(--uvp-text-tertiary); font-size: 12px; }
.head-right { display: flex; gap: 6px; }
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
    transition: color 120ms, border-color 120ms;
}
.icon-btn:hover:not(:disabled) { color: var(--uvp-brand); border-color: var(--uvp-brand); }
.icon-btn:disabled { cursor: wait; opacity: 0.6; }
.spin { animation: rotate 900ms linear infinite; }
@keyframes rotate { to { transform: rotate(360deg); } }

/* Stat band */
.stat-band {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 10px;
    padding: 0 20px 12px;
}
.stat-card {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
    padding: 12px 16px;
    color: var(--uvp-text-secondary);
    background: var(--uvp-panel-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 12px;
    text-align: left;
    cursor: pointer;
    transition: border-color 120ms, box-shadow 120ms;
}
.stat-card:not(.readonly):hover { border-color: var(--uvp-brand); }
.stat-card.active { border-color: var(--uvp-brand); box-shadow: inset 0 0 0 1px var(--uvp-brand); }
.stat-card.warning.active { border-color: var(--uvp-warning); box-shadow: inset 0 0 0 1px var(--uvp-warning); }
.stat-card.readonly { cursor: default; }
.stat-num { color: var(--uvp-text-primary); font-size: 22px; font-weight: 600; line-height: 1.1; }
.stat-num.danger { color: var(--uvp-danger); }
.stat-num.warning { color: var(--uvp-warning); }
.stat-label { color: var(--uvp-text-tertiary); font-size: 12px; }

/* Filter band */
.filter-band {
    display: grid;
    grid-template-columns: 280px 200px 1fr auto;
    gap: 10px;
    padding: 0 20px 14px;
}
.filter-range, .filter-device, .filter-keyword { width: 100%; min-width: 0; }
.reset-btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 32px;
    padding: 0 12px;
    color: var(--uvp-text-secondary);
    background: var(--uvp-panel-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 8px;
    font-size: 13px;
    cursor: pointer;
    transition: color 120ms, border-color 120ms;
}
.reset-btn:hover { color: var(--uvp-brand); border-color: var(--uvp-brand); }

/* Main grid */
.main-grid {
    display: grid;
    grid-template-columns: 340px 1fr 440px;
    flex: 1;
    min-height: 0;
    gap: 10px;
    margin: 0 20px 20px;
    overflow: hidden;
}
.pane {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
    background: var(--uvp-panel-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 12px;
}
.pane-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    min-height: 44px;
    padding: 0 14px;
    background: var(--uvp-list-toolbar-bg);
    border-bottom: 1px solid var(--uvp-panel-border);
}
.pane-title { color: var(--uvp-text-primary); font-size: 13px; font-weight: 600; }
.head-inline-meta { color: var(--uvp-text-tertiary); font-size: 12px; font-weight: normal; }
.pane-meta { display: inline-flex; align-items: center; gap: 4px; color: var(--uvp-text-tertiary); font-size: 12px; }
.pane-body { display: flex; flex: 1; min-height: 0; overflow: auto; }
.pane-spin { display: flex; flex: 1; min-width: 0; min-height: 0; width: 100%; }
.pane-spin :deep(.arco-spin-children) { display: flex; flex: 1; flex-direction: column; min-width: 0; }
.pane-spin :deep(.arco-empty) { margin: auto; }

/* Session cards */
.session-card {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 12px 14px;
    color: var(--uvp-text-secondary);
    background: transparent;
    border: 0;
    border-bottom: 1px solid var(--uvp-panel-border);
    text-align: left;
    cursor: pointer;
    transition: background 120ms;
}
.session-card:hover { background: var(--uvp-table-row-hover-bg); }
.session-card.active { background: var(--uvp-brand-soft); box-shadow: inset 3px 0 0 var(--uvp-brand); }
.session-card.anomaly { box-shadow: inset 3px 0 0 var(--uvp-warning); }
.session-card-top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
}
.device-label {
    flex: 1;
    overflow: hidden;
    color: var(--uvp-text-primary);
    font-size: 13px;
    font-weight: 600;
    text-overflow: ellipsis;
    white-space: nowrap;
}
.session-state {
    display: inline-flex;
    align-items: center;
    height: 20px;
    padding: 0 8px;
    font-size: 11px;
    font-weight: 500;
    border-radius: 10px;
    flex: 0 0 auto;
}
.state-success { color: #059669; background: rgb(5 150 105 / 10%); }
.state-warning { color: var(--uvp-warning); background: var(--uvp-warning-soft); }
.state-danger { color: var(--uvp-danger); background: var(--uvp-danger-soft); }
.state-info { color: var(--uvp-brand); background: var(--uvp-brand-soft); }
.state-neutral { color: var(--uvp-text-tertiary); background: color-mix(in srgb, var(--uvp-text-tertiary) 12%, transparent); }

.method-chain {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    align-items: center;
}
.chain-item {
    display: inline-flex;
    align-items: center;
    height: 20px;
    padding: 0 6px;
    font-size: 11px;
    font-weight: 500;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    line-height: 1;
    border-radius: 4px;
}
.chain-success { color: #059669; background: rgb(5 150 105 / 10%); }
.chain-warning { color: var(--uvp-warning); background: var(--uvp-warning-soft); }
.chain-danger { color: var(--uvp-danger); background: var(--uvp-danger-soft); }
.chain-info { color: var(--uvp-brand); background: var(--uvp-brand-soft); }
.chain-neutral { color: var(--uvp-text-tertiary); background: color-mix(in srgb, var(--uvp-text-tertiary) 12%, transparent); }

.call-id {
    display: block;
    overflow: hidden;
    color: var(--uvp-text-tertiary);
    font-size: 11px;
    text-overflow: ellipsis;
    white-space: nowrap;
}
.session-card-meta {
    display: flex;
    align-items: center;
    gap: 4px;
    color: var(--uvp-text-tertiary);
    font-size: 11px;
}
.dot-sep { color: var(--uvp-panel-border); }

/* Message list */
.message-list { display: flex; flex-direction: column; width: 100%; }
.message-row {
    display: grid;
    grid-template-columns: 96px 52px 72px 1fr auto;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    color: var(--uvp-text-secondary);
    background: transparent;
    border: 0;
    border-bottom: 1px solid var(--uvp-panel-border);
    cursor: pointer;
    text-align: left;
    transition: background 120ms;
}
.message-row:hover { background: var(--uvp-table-row-hover-bg); }
.message-row.active { background: var(--uvp-brand-soft); box-shadow: inset 3px 0 0 var(--uvp-brand); }
.msg-time { color: var(--uvp-text-primary); font-size: 12px; }
.dir-chip {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    height: 20px;
    padding: 0 6px;
    font-size: 11px;
    font-weight: 500;
    border-radius: 4px;
}
.dir-chip.inbound { color: var(--uvp-brand-cyan); background: color-mix(in srgb, var(--uvp-brand-cyan) 14%, transparent); }
.dir-chip.outbound { color: var(--uvp-brand); background: var(--uvp-brand-soft); }
.method-chip {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    height: 22px;
    padding: 0 8px;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 12px;
    font-weight: 600;
    border-radius: 4px;
}
.chip-success { color: #059669; background: rgb(5 150 105 / 10%); }
.chip-warning { color: var(--uvp-warning); background: var(--uvp-warning-soft); }
.chip-danger { color: var(--uvp-danger); background: var(--uvp-danger-soft); }
.chip-info { color: var(--uvp-brand); background: var(--uvp-brand-soft); }
.chip-neutral { color: var(--uvp-text-tertiary); background: color-mix(in srgb, var(--uvp-text-tertiary) 12%, transparent); }
.msg-cseq { color: var(--uvp-text-tertiary); font-size: 12px; }
.msg-transport {
    padding: 2px 6px;
    color: var(--uvp-text-tertiary);
    font-size: 10px;
    border: 1px solid var(--uvp-panel-border);
    border-radius: 3px;
}

/* Detail pane */
.pane-actions { display: inline-flex; align-items: center; gap: 4px; }
.step-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    padding: 0;
    color: var(--uvp-text-secondary);
    background: transparent;
    border: 1px solid var(--uvp-panel-border);
    border-radius: 6px;
    cursor: pointer;
}
.step-btn:hover:not(:disabled) { color: var(--uvp-brand); border-color: var(--uvp-brand); }
.step-btn:disabled { opacity: 0.4; cursor: not-allowed; }

.detail-tabs {
    display: flex;
    gap: 4px;
    padding: 8px 12px 0;
    border-bottom: 1px solid var(--uvp-panel-border);
}
.detail-tab {
    padding: 6px 12px 8px;
    color: var(--uvp-text-tertiary);
    background: transparent;
    border: 0;
    border-bottom: 2px solid transparent;
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
    transition: color 120ms, border-color 120ms;
}
.detail-tab:hover { color: var(--uvp-text-secondary); }
.detail-tab.active { color: var(--uvp-brand); border-bottom-color: var(--uvp-brand); }

.detail-body { padding: 12px 14px 18px; }
.detail-content { display: flex; flex-direction: column; gap: 14px; width: 100%; }
.detail-block { display: flex; flex-direction: column; gap: 6px; }
.block-label {
    color: var(--uvp-text-tertiary);
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.6px;
}
.start-line-text {
    padding: 8px 10px;
    color: var(--uvp-text-primary);
    background: var(--uvp-list-toolbar-bg);
    border-radius: 6px;
    font-size: 12px;
    line-height: 1.55;
    word-break: break-all;
}
.header-list {
    display: grid;
    grid-template-columns: 130px 1fr;
    gap: 4px 12px;
    margin: 0;
}
.header-list dt {
    color: var(--uvp-text-tertiary);
    font-size: 12px;
    line-height: 1.6;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}
.header-list dd {
    margin: 0;
    color: var(--uvp-text-secondary);
    font-size: 12px;
    line-height: 1.6;
    word-break: break-all;
}
.header-list dt.key-header { color: var(--uvp-text-primary); font-weight: 600; }
.header-list dd.key-header-val { color: var(--uvp-text-primary); }
.body-code, .raw-code {
    margin: 0;
    padding: 10px 12px;
    color: var(--uvp-text-primary);
    background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 6px;
    font-size: 12px;
    line-height: 1.65;
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 100%;
    overflow: auto;
}
.raw-code { white-space: pre; }

@media (max-width: 1400px) {
    .main-grid { grid-template-columns: 300px 1fr 380px; }
}
@media (max-width: 1200px) {
    .filter-band { grid-template-columns: 1fr 1fr; }
    .filter-range, .filter-device, .filter-keyword { grid-column: auto; }
    .filter-range { grid-column: span 2; }
    .main-grid { grid-template-columns: 280px 1fr; grid-template-rows: 1fr auto; }
    .detail-pane { grid-column: span 2; }
}
</style>
