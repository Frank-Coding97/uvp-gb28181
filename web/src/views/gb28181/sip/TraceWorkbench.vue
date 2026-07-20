<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import dayjs from "dayjs";
import { Message } from "@arco-design/web-vue";
import { Copy, Eye, RefreshCw, Search, ShieldCheck, X } from "lucide-vue-next";
import { listDevices, type DeviceVO } from "@/views/gb28181/device-mgmt/api";
import {
    fetchTraceHealth,
    getTraceMessage,
    listTraceMessages,
    listTraceSessionMessages,
    listTraceSessions,
    stopTraceCapture,
    type TraceDirection,
    type TraceHealth,
    type TraceMessageDetail,
    type TraceMessageSummary,
    type TraceSessionSummary
} from "@/api/gb28181-trace";

type WorkbenchView = "text" | "session";

const route = useRoute();
const router = useRouter();
const loading = ref(false);
const detailLoading = ref(false);
const health = ref<TraceHealth>({ state: "disabled", queueDepth: 0, queueCapacity: 0, dropped: 0 });
const messages = ref<TraceMessageSummary[]>([]);
const sessions = ref<TraceSessionSummary[]>([]);
const sessionMessages = ref<TraceMessageSummary[]>([]);
const selectedSession = ref<TraceSessionSummary | null>(null);
const sessionLoading = ref(false);
const nextCursor = ref("");
const selectedId = ref("");
const detail = ref<TraceMessageDetail | null>(null);
const viewMode = ref<WorkbenchView>(route.query.view === "session" ? "session" : "text");
const stoppingCapture = ref(false);
const mobileDetailOpen = ref(false);
const detailTab = ref("raw");
const sensitiveDialogOpen = ref(false);
const sensitiveLoading = ref(false);
const sensitivePurpose = ref("");
const deviceOptions = ref<DeviceVO[]>([]);
let requestGeneration = 0;
const messageColumns = [
    { title: "时间", dataIndex: "occurredAt", width: 166, slotName: "occurredAt" },
    { title: "方向", dataIndex: "direction", width: 68, slotName: "direction" },
    { title: "方法 / 状态", width: 108, slotName: "messageType" },
    { title: "设备", dataIndex: "deviceId", width: 172, ellipsis: true, tooltip: true },
    { title: "Call-ID", dataIndex: "callId", width: 218, ellipsis: true, tooltip: true },
    { title: "CSeq", width: 100, slotName: "cseq" }
];

const now = dayjs();
const filters = reactive({
    range: [
        typeof route.query.from === "string" ? dayjs(route.query.from).format("YYYY-MM-DD HH:mm:ss") : now.subtract(15, "minute").format("YYYY-MM-DD HH:mm:ss"),
        typeof route.query.to === "string" ? dayjs(route.query.to).format("YYYY-MM-DD HH:mm:ss") : now.format("YYYY-MM-DD HH:mm:ss")
    ] as string[],
    deviceId: typeof route.query.deviceId === "string" ? route.query.deviceId : "",
    deviceIds: typeof route.query.deviceIds === "string" ? route.query.deviceIds.split(",").filter(Boolean) : [],
    direction: (typeof route.query.direction === "string" ? route.query.direction : "") as TraceDirection | "",
    method: typeof route.query.method === "string" ? route.query.method : "",
    statusRange: typeof route.query.statusRange === "string" ? route.query.statusRange : "",
    statusCode: typeof route.query.statusCode === "string" ? route.query.statusCode : "",
    callId: typeof route.query.callId === "string" ? route.query.callId : ""
});

const healthLabel = computed(() => ({ ready: "运行正常", degraded: "存储降级", disabled: "功能未启用" })[health.value.state]);
const healthColor = computed(() => ({ ready: "green", degraded: "orange", disabled: "gray" })[health.value.state]);
const canQuery = computed(() => health.value.state !== "disabled");
const captureID = computed(() => typeof route.query.captureId === "string" ? route.query.captureId : "");
const captureEndsAt = computed(() => typeof route.query.captureEndsAt === "string" ? route.query.captureEndsAt : "");

function toISO(value: string) {
    return dayjs(value).toISOString();
}

function queryParams(cursor = "") {
    const statusBounds = statusRangeBounds(filters.statusRange);
    return {
        from: toISO(filters.range[0]),
        to: toISO(filters.range[1]),
        deviceId: filters.deviceId.trim() || undefined,
        deviceIds: filters.deviceIds.length ? filters.deviceIds.join(",") : undefined,
        direction: filters.direction || undefined,
        method: filters.method.trim().toUpperCase() || undefined,
        statusCode: filters.statusCode ? Number(filters.statusCode) : undefined,
        statusMin: statusBounds?.[0],
        statusMax: statusBounds?.[1],
        callId: filters.callId.trim() || undefined,
        cursor: cursor || undefined,
        limit: 100
    };
}

function statusRangeBounds(value: string): [number, number] | undefined {
    if (!value) return undefined;
    const code = Number(value.slice(0, 1));
    return Number.isInteger(code) && code >= 1 && code <= 6 ? [code * 100, code * 100 + 99] : undefined;
}

async function syncURL() {
    const params = queryParams();
    await router.replace({
        query: {
            ...route.query,
            view: viewMode.value,
            from: params.from,
            to: params.to,
            deviceId: params.deviceId,
            deviceIds: params.deviceIds,
            direction: params.direction,
            method: params.method,
            statusCode: params.statusCode?.toString(),
            statusRange: filters.statusRange || undefined,
            callId: params.callId
        }
    });
}

async function loadDevices() {
    try {
        const response = await listDevices({ page: 1, pageSize: 200 });
        if (response.code === 0) deviceOptions.value = response.data?.list || [];
    } catch {
        deviceOptions.value = [];
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

async function search(reset = true) {
    if (!filters.range?.[0] || !filters.range?.[1]) {
        Message.warning("请选择时间范围");
        return;
    }
    if (!canQuery.value) return;
    const generation = ++requestGeneration;
    loading.value = true;
    try {
        if (reset) {
            nextCursor.value = "";
            messages.value = [];
            sessions.value = [];
            sessionMessages.value = [];
            selectedSession.value = null;
            detail.value = null;
            selectedId.value = "";
            await syncURL();
        }
        if (viewMode.value === "session") {
            const params = queryParams();
            const response = await listTraceSessions({
                from: params.from,
                to: params.to,
                deviceId: params.deviceId,
                deviceIds: params.deviceIds,
                callId: params.callId,
                limit: 200
            });
            if (response.code !== 0) throw new Error(response.message || "查询失败");
            if (generation !== requestGeneration) return;
            sessions.value = response.data.items || [];
            selectedSession.value = null;
            sessionMessages.value = [];
        } else {
            const response = await listTraceMessages(queryParams(reset ? "" : nextCursor.value));
            if (response.code !== 0) throw new Error(response.message || "查询失败");
            if (generation !== requestGeneration) return;
            const page = response.data;
            messages.value = reset ? page.items : [...messages.value, ...page.items];
            nextCursor.value = page.nextCursor || "";
        }
    } catch (error: any) {
        Message.error(error?.message || "SIP 日志查询失败");
        await loadHealth();
    } finally {
        loading.value = false;
    }
}

async function switchView(value: WorkbenchView) {
    if (viewMode.value === value) return;
    viewMode.value = value;
    detail.value = null;
    selectedId.value = "";
    await search();
}

async function selectSession(session: TraceSessionSummary) {
    selectedSession.value = session;
    mobileDetailOpen.value = true;
    detail.value = null;
    sessionMessages.value = [];
    if (!session.originalAvailable) return;
    sessionLoading.value = true;
    try {
        const response = await listTraceSessionMessages(session.callId, {
            from: dayjs(session.firstAt).subtract(1, "second").toISOString(),
            to: dayjs(session.lastAt).add(1, "second").toISOString(),
            deviceId: session.deviceId || undefined,
            limit: 500
        });
        if (response.code !== 0) throw new Error(response.message || "会话报文读取失败");
        sessionMessages.value = [...(response.data.items || [])].sort((a, b) => Date.parse(a.occurredAt) - Date.parse(b.occurredAt));
    } catch (error: any) {
        Message.error(error?.message || "会话报文读取失败");
    } finally {
        sessionLoading.value = false;
    }
}

async function stopCaptureWindow() {
    if (!captureID.value) return;
    stoppingCapture.value = true;
    try {
        const response = await stopTraceCapture(captureID.value);
        if (response.code !== 0) throw new Error(response.message || "停止失败");
        const query = { ...route.query };
        delete query.captureId;
        delete query.captureEndsAt;
        await router.replace({ query });
        Message.success("诊断窗口已停止");
    } catch (error: any) {
        Message.error(error?.message || "停止诊断窗口失败");
    } finally {
        stoppingCapture.value = false;
    }
}

async function refreshAll() {
    await loadHealth();
    await search();
}

async function selectMessage(record: TraceMessageSummary) {
    selectedId.value = record.eventId;
    mobileDetailOpen.value = true;
    detailTab.value = "raw";
    detailLoading.value = true;
    try {
        const response = await getTraceMessage(record.eventId);
        if (response.code !== 0) throw new Error(response.message || "读取失败");
        detail.value = response.data;
    } catch (error: any) {
        Message.error(error?.message || "报文已过期或不可用");
        detail.value = null;
    } finally {
        detailLoading.value = false;
    }
}

function resetFilters() {
    const current = dayjs();
    filters.range = [current.subtract(15, "minute").format("YYYY-MM-DD HH:mm:ss"), current.format("YYYY-MM-DD HH:mm:ss")];
    filters.deviceId = "";
    filters.deviceIds = [];
    filters.direction = "";
    filters.method = "";
    filters.statusRange = "";
    filters.statusCode = "";
    filters.callId = "";
    void search();
}

async function loadSensitiveDetail() {
    const purpose = sensitivePurpose.value.trim();
    if (!purpose || !selectedId.value) {
        Message.warning("请填写敏感报文查看用途");
        return;
    }
    sensitiveLoading.value = true;
    try {
        const response = await getTraceMessage(selectedId.value, { sensitive: true, purpose });
        if (response.code !== 0) throw new Error(response.message || "敏感报文读取失败");
        detail.value = response.data;
        sensitiveDialogOpen.value = false;
        Message.success("敏感报文已按用途授权查看");
    } catch (error: any) {
        Message.error(error?.message || "敏感报文读取失败");
    } finally {
        sensitiveLoading.value = false;
    }
}

async function copyPayload() {
    if (!detail.value?.payload) return;
    try {
        await navigator.clipboard.writeText(detail.value.payload);
        Message.success("报文已复制");
    } catch {
        Message.warning("复制失败");
    }
}

function messageLabel(record: TraceMessageSummary) {
    return record.statusCode ? String(record.statusCode) : record.method || record.cseqMethod || "未知";
}

function formatTime(value: string) {
    return dayjs(value).format("MM-DD HH:mm:ss.SSS");
}

function sessionStateLabel(session: TraceSessionSummary) {
    if (!session.originalAvailable) return "原文已过期";
    if (session.missingResponse) return "缺少响应";
    if (session.finalStatus >= 300) return `异常 ${session.finalStatus}`;
    return session.finalStatus ? `完成 ${session.finalStatus}` : "进行中";
}

const detailContent = computed(() => {
    const payload = detail.value?.payload || "";
    if (detailTab.value === "headers") return payload.split("\r\n\r\n", 1)[0] || "未检测到 SIP 头部";
    if (detailTab.value === "sdp") return payload.includes("v=0") ? payload.slice(payload.indexOf("v=0")) : "未检测到 SDP 内容";
    if (detailTab.value === "xml") return payload.includes("<") ? payload.slice(payload.indexOf("<")) : "未检测到 XML 内容";
    return payload;
});

onMounted(async () => {
    await loadDevices();
    await loadHealth();
    if (canQuery.value) await search(false);
});
</script>

<template>
    <div class="snow-fill">
        <div class="snow-fill-inner uvp-page-shell-flat trace-shell">
            <header class="trace-toolbar">
                <div class="trace-status">
                    <span class="trace-title">SIP 日志</span>
                    <a-radio-group :model-value="viewMode" type="button" size="small" @change="(value: WorkbenchView) => switchView(value)">
                        <a-radio value="text">文本</a-radio>
                        <a-radio value="session">会话</a-radio>
                    </a-radio-group>
                    <a-tag :color="healthColor" bordered>{{ healthLabel }}</a-tag>
                    <span v-if="health.queueCapacity" class="status-detail">队列 {{ health.queueDepth }}/{{ health.queueCapacity }}</span>
                    <span v-if="health.dropped" class="status-loss">已丢失 {{ health.dropped }}</span>
                    <span v-if="health.currentGap" class="status-loss">缺口 {{ health.currentGap.eventCount }} 条</span>
                </div>
                <a-tooltip content="刷新">
                    <a-button class="icon-command" :loading="loading" @click="refreshAll">
                        <template #icon><RefreshCw :size="16" /></template>
                    </a-button>
                </a-tooltip>
            </header>

            <a-alert v-if="health.state === 'disabled'" type="info" class="trace-alert">SIP Trace 功能未启用</a-alert>
            <a-alert v-else-if="health.state === 'degraded'" type="warning" class="trace-alert">
                {{ health.lastError || "ClickHouse 当前不可用，日志可能存在缺口" }}
                <span v-if="health.currentGap">；缺口自 {{ formatTime(health.currentGap.startedAt) }} 起累计 {{ health.currentGap.eventCount }} 条</span>
            </a-alert>
            <div v-if="captureID" class="capture-band" role="status">
                <ShieldCheck :size="16" aria-hidden="true" />
                <span>诊断窗口进行中</span>
                <span v-if="captureEndsAt" class="status-detail">预计结束 {{ formatTime(captureEndsAt) }}</span>
                <a-button size="small" status="danger" :loading="stoppingCapture" @click="stopCaptureWindow">停止窗口</a-button>
            </div>

            <section class="filter-band" :class="{ 'session-filter': viewMode === 'session' }" aria-label="SIP 日志筛选">
                <a-range-picker
                    v-model="filters.range"
                    show-time
                    value-format="YYYY-MM-DD HH:mm:ss"
                    format="YYYY-MM-DD HH:mm:ss"
                    class="range-control"
                />
                <a-select
                    v-model="filters.deviceIds"
                    multiple
                    allow-clear
                    allow-search
                    :max-tag-count="2"
                    placeholder="设备（可多选）"
                    class="filter-control"
                >
                    <a-option v-for="device in deviceOptions" :key="device.id" :value="device.deviceId">
                        {{ device.name || device.alias || device.deviceId }}
                    </a-option>
                </a-select>
                <a-select v-if="viewMode === 'text'" v-model="filters.direction" allow-clear placeholder="方向" class="short-control">
                    <a-option value="inbound">接收</a-option>
                    <a-option value="outbound">发送</a-option>
                </a-select>
                <a-input v-if="viewMode === 'text'" v-model="filters.method" allow-clear placeholder="方法" class="short-control" @press-enter="search()" />
                <a-select v-if="viewMode === 'text'" v-model="filters.statusRange" allow-clear placeholder="状态范围" class="short-control">
                    <a-option value="1xx">1xx 临时</a-option>
                    <a-option value="2xx">2xx 成功</a-option>
                    <a-option value="3xx">3xx 重定向</a-option>
                    <a-option value="4xx">4xx 客户端错误</a-option>
                    <a-option value="5xx">5xx 服务端错误</a-option>
                    <a-option value="6xx">6xx 全局失败</a-option>
                </a-select>
                <a-input v-model="filters.callId" allow-clear placeholder="Call-ID" class="call-id-control" @press-enter="search()" />
                <a-button type="primary" :disabled="!canQuery" :loading="loading" @click="search()">
                    <template #icon><Search :size="15" /></template>
                    查询
                </a-button>
                <a-tooltip content="重置筛选">
                    <a-button class="icon-command" @click="resetFilters">
                        <template #icon><X :size="16" /></template>
                    </a-button>
                </a-tooltip>
            </section>

            <main class="trace-workspace" :class="{ 'session-layout': viewMode === 'session' }">
                <section class="message-pane" aria-label="SIP 报文列表">
                    <a-table
                        v-if="viewMode === 'text'"
                        class="trace-table"
                        :data="messages"
                        :columns="messageColumns"
                        :loading="loading"
                        :pagination="false"
                        :bordered="false"
                        size="small"
                        row-key="eventId"
                        :scroll="{ x: 800, y: 520 }"
                        @row-click="selectMessage"
                    >
                        <template #empty>
                            <a-empty :description="canQuery ? '当前筛选范围无日志' : 'SIP Trace 功能未启用'" />
                        </template>
                        <template #occurredAt="{ record }"><span class="mono time-cell">{{ formatTime(record.occurredAt) }}</span></template>
                        <template #direction="{ record }">
                            <span :class="['direction-mark', record.direction]">{{ record.direction === 'inbound' ? '接收' : '发送' }}</span>
                        </template>
                        <template #messageType="{ record }"><strong class="message-method">{{ messageLabel(record) }}</strong></template>
                        <template #cseq="{ record }"><span class="mono">{{ record.cseq }} {{ record.cseqMethod }}</span></template>
                    </a-table>
                    <a-spin v-if="viewMode === 'session'" :loading="loading" class="session-list">
                        <button
                            v-for="session in sessions"
                            :key="`${session.day}-${session.deviceId}-${session.callId}`"
                            type="button"
                            :class="['session-row', { active: selectedSession?.callId === session.callId, anomaly: session.anomaly }]"
                            @click="selectSession(session)"
                        >
                            <span class="session-row-main">
                                <strong class="mono session-call-id">{{ session.callId }}</strong>
                                <span class="session-methods">{{ session.methods.filter(Boolean).join(' · ') || '未知方法' }}</span>
                            </span>
                            <span class="session-row-meta">
                                <span>{{ formatTime(session.lastAt) }}</span>
                                <span>{{ session.messageCount }} 条</span>
                                <a-tag :color="session.anomaly ? 'orange' : session.originalAvailable ? 'green' : 'gray'" size="small">
                                    {{ sessionStateLabel(session) }}
                                </a-tag>
                            </span>
                        </button>
                        <a-empty v-if="!loading && !sessions.length" description="当前筛选范围无会话" />
                    </a-spin>
                    <div class="list-footer">
                        <span>{{ viewMode === 'text' ? `已加载 ${messages.length} 条` : `${sessions.length} 个会话` }}</span>
                        <a-button v-if="viewMode === 'text' && nextCursor" size="small" :loading="loading" @click="search(false)">加载更多</a-button>
                    </div>
                </section>

                <aside :class="['detail-pane', { 'mobile-open': mobileDetailOpen }]" aria-label="SIP 报文详情">
                    <section v-if="viewMode === 'session'" class="session-flow" aria-label="SIP 会话时序">
                        <div class="session-flow-head">
                            <div>
                                <strong>会话时序</strong>
                                <span v-if="selectedSession" class="mono">{{ selectedSession.callId }}</span>
                            </div>
                            <a-tag v-if="selectedSession && !selectedSession.originalAvailable" color="gray">原始报文已过期</a-tag>
                        </div>
                        <a-spin :loading="sessionLoading" class="session-flow-body">
                            <div v-if="sessionMessages.length" class="flow-lanes">
                                <div class="lane-label lane-left">设备</div>
                                <div class="lane-label lane-right">平台</div>
                                <button
                                    v-for="node in sessionMessages"
                                    :key="node.eventId"
                                    type="button"
                                    :class="['flow-node', node.direction, { active: selectedId === node.eventId }]"
                                    @click="selectMessage(node)"
                                >
                                    <span class="flow-time mono">{{ formatTime(node.occurredAt) }}</span>
                                    <span class="flow-line"><span class="flow-arrow">{{ node.direction === 'inbound' ? '→' : '←' }}</span></span>
                                    <strong>{{ messageLabel(node) }}</strong>
                                    <span class="flow-cseq mono">{{ node.cseq }} {{ node.cseqMethod }}</span>
                                </button>
                            </div>
                            <a-empty v-else :description="selectedSession ? selectedSession.originalAvailable ? '会话无可用报文' : '原始报文已过期' : '选择一个会话'" />
                        </a-spin>
                    </section>
                    <div class="detail-head">
                        <div class="detail-heading">
                            <span>报文详情</span>
                            <a-tag v-if="detail" size="small" bordered>{{ messageLabel(detail) }}</a-tag>
                        </div>
                        <div class="detail-actions">
                            <a-tooltip content="按用途查看敏感字段">
                                <a-button class="icon-command" :disabled="!detail" aria-label="查看敏感报文" @click="sensitiveDialogOpen = true">
                                    <template #icon><Eye :size="16" /></template>
                                </a-button>
                            </a-tooltip>
                            <a-tooltip content="复制当前报文">
                                <a-button class="icon-command" :disabled="!detail" aria-label="复制报文" @click="copyPayload">
                                    <template #icon><Copy :size="16" /></template>
                                </a-button>
                            </a-tooltip>
                            <a-button class="mobile-close" aria-label="关闭详情" @click="mobileDetailOpen = false">
                                <template #icon><X :size="16" /></template>
                            </a-button>
                        </div>
                    </div>
                    <a-spin :loading="detailLoading" class="detail-body">
                        <template v-if="detail">
                            <a-tabs v-model:active-key="detailTab" class="detail-tabs" size="small">
                                <a-tab-pane key="raw" title="Raw" />
                                <a-tab-pane key="headers" title="Headers" />
                                <a-tab-pane key="sdp" title="SDP" />
                                <a-tab-pane key="xml" title="XML" />
                            </a-tabs>
                            <pre class="sip-payload">{{ detailContent }}</pre>
                        </template>
                        <a-empty v-else description="选择一条报文查看详情" />
                    </a-spin>
                </aside>
            </main>
        </div>
    </div>
    <a-modal v-model:visible="sensitiveDialogOpen" title="敏感报文查看" :confirm-loading="sensitiveLoading" @ok="loadSensitiveDetail">
        <a-alert type="warning" class="sensitive-warning">敏感字段仅用于当前诊断，不会写入列表或浏览器历史。</a-alert>
        <a-form-item label="查看用途" required>
            <a-textarea v-model="sensitivePurpose" :max-length="200" show-word-limit placeholder="例如：定位设备注册失败" />
        </a-form-item>
    </a-modal>
</template>

<style scoped>
.trace-shell { display: flex; flex-direction: column; min-width: 0; padding: 0; overflow: hidden; }
.trace-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; min-height: 40px; padding: 4px 8px 8px; }
.trace-status { display: flex; align-items: center; gap: 9px; min-width: 0; }
.trace-title { color: var(--uvp-text-primary); font-size: 15px; font-weight: 600; }
.status-detail { color: var(--uvp-text-tertiary); font-size: 12px; }
.status-loss { color: rgb(var(--warning-6)); font-size: 12px; font-weight: 500; }
.trace-alert { margin: 0 8px 8px; }
.capture-band { display: flex; align-items: center; gap: 10px; min-height: 38px; margin: 0 8px 8px; padding: 4px 10px 4px 12px; color: var(--uvp-text-secondary); font-size: 12px; background: color-mix(in srgb, var(--uvp-brand-cyan) 9%, var(--uvp-panel-bg)); border: 1px solid color-mix(in srgb, var(--uvp-brand-cyan) 30%, var(--uvp-panel-border)); }
.filter-band { display: grid; grid-template-columns: 310px 160px 96px 96px 96px minmax(170px, 1fr) auto auto; align-items: center; gap: 8px; padding: 10px 8px; background: var(--uvp-list-toolbar-bg); border-block: 1px solid var(--uvp-divider); }
.filter-band.session-filter { grid-template-columns: 310px 180px minmax(200px, 1fr) auto auto; }
.range-control, .filter-control, .short-control, .call-id-control { width: 100%; min-width: 0; }
.icon-command { width: 32px; min-width: 32px; height: 32px; padding: 0; }
.trace-workspace { display: grid; grid-template-columns: minmax(520px, 1.35fr) minmax(360px, 1fr); flex: 1; min-height: 0; margin: 8px; overflow: hidden; border: 1px solid var(--uvp-panel-border); background: var(--uvp-panel-bg); }
.trace-workspace.session-layout { grid-template-columns: minmax(360px, 0.8fr) minmax(520px, 1.4fr); }
.message-pane { display: flex; flex-direction: column; min-width: 0; min-height: 0; border-right: 1px solid var(--uvp-divider); }
.trace-table { flex: 1; min-height: 0; }
.trace-table :deep(.arco-table-tr) { cursor: pointer; }
.trace-table :deep(.arco-table-tr:hover .arco-table-td) { background: var(--uvp-brand-soft); }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace; }
.time-cell { color: var(--uvp-text-secondary); font-size: 12px; }
.direction-mark { display: inline-flex; align-items: center; gap: 5px; font-size: 12px; font-weight: 600; }
.direction-mark::before { width: 6px; height: 6px; content: ""; border-radius: 50%; }
.direction-mark.inbound { color: var(--uvp-brand-cyan); }
.direction-mark.inbound::before { background: var(--uvp-brand-cyan); }
.direction-mark.outbound { color: var(--uvp-brand); }
.direction-mark.outbound::before { background: var(--uvp-brand); }
.message-method { color: var(--uvp-text-primary); font-size: 12px; }
.list-footer { display: flex; align-items: center; justify-content: space-between; min-height: 38px; padding: 4px 10px; color: var(--uvp-text-tertiary); font-size: 12px; border-top: 1px solid var(--uvp-divider); }
.session-list { display: flex; flex: 1; min-height: 0; overflow: auto; }
.session-list :deep(.arco-spin-children) { width: 100%; }
.session-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; width: 100%; height: 68px; min-height: 68px; max-height: 68px; padding: 10px 12px; color: var(--uvp-text-secondary); text-align: left; background: transparent; border: 0; border-bottom: 1px solid var(--uvp-divider); cursor: pointer; }
.session-row:hover, .session-row.active { background: var(--uvp-brand-soft); }
.session-row.active { box-shadow: inset 3px 0 0 var(--uvp-brand); }
.session-row.anomaly { box-shadow: inset 3px 0 0 rgb(var(--warning-6)); }
.session-row-main, .session-row-meta { display: flex; flex-direction: column; min-width: 0; }
.session-row-main { flex: 1; gap: 5px; }
.session-row-meta { align-items: flex-end; flex: 0 0 auto; gap: 3px; color: var(--uvp-text-tertiary); font-size: 11px; }
.session-call-id { overflow: hidden; color: var(--uvp-text-primary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.session-methods { overflow: hidden; color: var(--uvp-text-tertiary); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.detail-pane { display: flex; flex-direction: column; min-width: 0; min-height: 0; }
.detail-actions { display: inline-flex; align-items: center; gap: 4px; }
.mobile-close { display: none; }
.detail-tabs { flex: 0 0 auto; padding: 0 12px; }
.detail-tabs :deep(.arco-tabs-content) { display: none; }
.sensitive-warning { margin-bottom: 14px; }
.session-flow { display: flex; flex: 1 1 58%; flex-direction: column; min-height: 220px; overflow: hidden; border-bottom: 1px solid var(--uvp-divider); }
.session-flow-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; min-height: 44px; padding: 6px 12px; border-bottom: 1px solid var(--uvp-divider); }
.session-flow-head > div { display: flex; align-items: center; gap: 10px; min-width: 0; }
.session-flow-head strong { color: var(--uvp-text-primary); font-size: 13px; }
.session-flow-head .mono { overflow: hidden; color: var(--uvp-text-tertiary); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.session-flow-body { display: flex; flex: 1; min-height: 0; overflow: auto; }
.session-flow-body :deep(.arco-spin-children) { display: flex; flex: 1; min-width: 0; min-height: 0; }
.session-flow-body :deep(.arco-empty) { margin: auto; }
.flow-lanes { position: relative; display: grid; grid-template-columns: 1fr 72px 1fr; align-content: start; width: 100%; min-width: 0; padding: 34px 14px 18px; }
.flow-lanes::before { position: absolute; top: 28px; bottom: 12px; left: 50%; width: 1px; content: ""; background: var(--uvp-divider); }
.lane-label { position: absolute; top: 8px; color: var(--uvp-text-tertiary); font-size: 11px; font-weight: 600; }
.lane-left { left: 18%; }
.lane-right { right: 18%; }
.flow-node { z-index: 1; display: grid; grid-template-columns: minmax(100px, 1fr) 72px minmax(68px, 0.7fr); grid-column: 1 / -1; align-items: center; min-height: 48px; padding: 4px 6px; color: var(--uvp-text-secondary); background: transparent; border: 0; cursor: pointer; }
.flow-node:hover, .flow-node.active { background: var(--uvp-brand-soft); }
.flow-node strong { color: var(--uvp-text-primary); font-size: 12px; }
.flow-time, .flow-cseq { overflow: hidden; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.flow-node.inbound .flow-time, .flow-node.inbound .flow-cseq { text-align: left; }
.flow-node.outbound .flow-time, .flow-node.outbound .flow-cseq { text-align: right; }
.flow-node.outbound .flow-time { grid-column: 3; grid-row: 1; }
.flow-node.outbound .flow-line { grid-column: 2; grid-row: 1; }
.flow-node.outbound strong { grid-column: 1; grid-row: 1; text-align: right; }
.flow-node.inbound strong { text-align: left; }
.flow-line { position: relative; display: flex; align-items: center; justify-content: center; height: 1px; background: var(--uvp-panel-border); }
.flow-arrow { display: inline-flex; align-items: center; justify-content: center; width: 20px; height: 20px; color: var(--uvp-text-secondary); font-size: 15px; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-divider); border-radius: 50%; }
.flow-cseq { display: none; }
.detail-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; min-height: 44px; padding: 5px 10px 5px 14px; border-bottom: 1px solid var(--uvp-divider); }
.detail-heading { display: flex; align-items: center; gap: 8px; min-width: 0; color: var(--uvp-text-primary); font-size: 13px; font-weight: 600; }
.detail-body { display: flex; flex: 1; min-height: 0; }
.detail-body :deep(.arco-spin-children) { display: flex; flex: 1; min-width: 0; min-height: 0; }
.detail-body :deep(.arco-empty) { margin: auto; }
.sip-payload { width: 100%; min-width: 0; min-height: 0; box-sizing: border-box; margin: 0; padding: 14px 16px 24px; overflow: auto; color: var(--uvp-text-primary); background: var(--color-fill-1); font: 12px/1.65 ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace; white-space: pre; tab-size: 4; }
.trace-shell :deep(button:focus-visible), .trace-shell :deep(input:focus-visible), .trace-shell :deep(.arco-select-view:focus-visible) { outline: 2px solid var(--uvp-brand); outline-offset: 2px; }

@media (max-width: 1080px) {
    .filter-band { grid-template-columns: minmax(280px, 1.5fr) repeat(3, minmax(96px, 0.5fr)); }
    .filter-band.session-filter { grid-template-columns: minmax(280px, 1.4fr) minmax(160px, 0.8fr) minmax(180px, 1fr) auto auto; }
    .filter-control, .call-id-control { grid-column: span 2; }
    .trace-workspace, .trace-workspace.session-layout { grid-template-columns: 1fr; overflow: auto; }
    .message-pane { min-height: 470px; border-right: 0; border-bottom: 1px solid var(--uvp-divider); }
    .detail-pane { min-height: 420px; }
}

@media (max-width: 720px) {
    .filter-band { grid-template-columns: 1fr 1fr; }
    .filter-band.session-filter { grid-template-columns: 1fr 1fr; }
    .range-control, .filter-control, .call-id-control { grid-column: 1 / -1; }
    .trace-workspace { margin: 6px 0 0; border-inline: 0; }
    .trace-status { flex-wrap: wrap; }
    .detail-pane { position: fixed; z-index: 40; inset: 0; width: 100%; min-height: 100%; background: var(--uvp-panel-bg); transform: translateX(100%); transition: transform 180ms ease; }
    .detail-pane.mobile-open { transform: translateX(0); }
    .mobile-close { display: inline-flex; }
    .detail-head { padding-inline: 14px; }
}

@media (prefers-reduced-motion: reduce) {
    .detail-pane { transition: none; }
}
</style>
