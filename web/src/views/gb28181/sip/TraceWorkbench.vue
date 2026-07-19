<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import dayjs from "dayjs";
import { Message } from "@arco-design/web-vue";
import { Copy, RefreshCw, Search, X } from "lucide-vue-next";
import {
    fetchTraceHealth,
    getTraceMessage,
    listTraceMessages,
    type TraceDirection,
    type TraceHealth,
    type TraceMessageDetail,
    type TraceMessageSummary
} from "@/api/gb28181-trace";

const route = useRoute();
const router = useRouter();
const loading = ref(false);
const detailLoading = ref(false);
const health = ref<TraceHealth>({ state: "disabled", queueDepth: 0, queueCapacity: 0, dropped: 0 });
const messages = ref<TraceMessageSummary[]>([]);
const nextCursor = ref("");
const selectedId = ref("");
const detail = ref<TraceMessageDetail | null>(null);
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
        typeof route.query.from === "string" ? dayjs(route.query.from).format("YYYY-MM-DD HH:mm:ss") : now.subtract(30, "minute").format("YYYY-MM-DD HH:mm:ss"),
        typeof route.query.to === "string" ? dayjs(route.query.to).format("YYYY-MM-DD HH:mm:ss") : now.format("YYYY-MM-DD HH:mm:ss")
    ] as string[],
    deviceId: typeof route.query.deviceId === "string" ? route.query.deviceId : "",
    direction: (typeof route.query.direction === "string" ? route.query.direction : "") as TraceDirection | "",
    method: typeof route.query.method === "string" ? route.query.method : "",
    statusCode: typeof route.query.statusCode === "string" ? route.query.statusCode : "",
    callId: typeof route.query.callId === "string" ? route.query.callId : ""
});

const healthLabel = computed(() => ({ ready: "运行正常", degraded: "存储降级", disabled: "功能未启用" })[health.value.state]);
const healthColor = computed(() => ({ ready: "green", degraded: "orange", disabled: "gray" })[health.value.state]);
const canQuery = computed(() => health.value.state !== "disabled");

function toISO(value: string) {
    return dayjs(value).toISOString();
}

function queryParams(cursor = "") {
    return {
        from: toISO(filters.range[0]),
        to: toISO(filters.range[1]),
        deviceId: filters.deviceId.trim() || undefined,
        direction: filters.direction || undefined,
        method: filters.method.trim().toUpperCase() || undefined,
        statusCode: filters.statusCode ? Number(filters.statusCode) : undefined,
        callId: filters.callId.trim() || undefined,
        cursor: cursor || undefined,
        limit: 100
    };
}

async function syncURL() {
    const params = queryParams();
    await router.replace({
        query: {
            ...route.query,
            from: params.from,
            to: params.to,
            deviceId: params.deviceId,
            direction: params.direction,
            method: params.method,
            statusCode: params.statusCode?.toString(),
            callId: params.callId
        }
    });
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
    loading.value = true;
    try {
        if (reset) {
            nextCursor.value = "";
            messages.value = [];
            detail.value = null;
            selectedId.value = "";
            await syncURL();
        }
        const response = await listTraceMessages(queryParams(reset ? "" : nextCursor.value));
        if (response.code !== 0) throw new Error(response.message || "查询失败");
        const page = response.data;
        messages.value = reset ? page.items : [...messages.value, ...page.items];
        nextCursor.value = page.nextCursor || "";
    } catch (error: any) {
        Message.error(error?.message || "SIP 日志查询失败");
        await loadHealth();
    } finally {
        loading.value = false;
    }
}

async function refreshAll() {
    await loadHealth();
    await search();
}

async function selectMessage(record: TraceMessageSummary) {
    selectedId.value = record.eventId;
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
    filters.range = [current.subtract(30, "minute").format("YYYY-MM-DD HH:mm:ss"), current.format("YYYY-MM-DD HH:mm:ss")];
    filters.deviceId = "";
    filters.direction = "";
    filters.method = "";
    filters.statusCode = "";
    filters.callId = "";
    void search();
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

onMounted(async () => {
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
                    <a-tag :color="healthColor" bordered>{{ healthLabel }}</a-tag>
                    <span v-if="health.queueCapacity" class="status-detail">队列 {{ health.queueDepth }}/{{ health.queueCapacity }}</span>
                    <span v-if="health.dropped" class="status-loss">已丢失 {{ health.dropped }}</span>
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
            </a-alert>

            <section class="filter-band" aria-label="SIP 日志筛选">
                <a-range-picker
                    v-model="filters.range"
                    show-time
                    value-format="YYYY-MM-DD HH:mm:ss"
                    format="YYYY-MM-DD HH:mm:ss"
                    class="range-control"
                />
                <a-input v-model="filters.deviceId" allow-clear placeholder="设备编码" class="filter-control" @press-enter="search()" />
                <a-select v-model="filters.direction" allow-clear placeholder="方向" class="short-control">
                    <a-option value="inbound">接收</a-option>
                    <a-option value="outbound">发送</a-option>
                </a-select>
                <a-input v-model="filters.method" allow-clear placeholder="方法" class="short-control" @press-enter="search()" />
                <a-input v-model="filters.statusCode" allow-clear placeholder="状态码" class="short-control" @press-enter="search()" />
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

            <main class="trace-workspace">
                <section class="message-pane" aria-label="SIP 报文列表">
                    <a-table
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
                    <div class="list-footer">
                        <span>已加载 {{ messages.length }} 条</span>
                        <a-button v-if="nextCursor" size="small" :loading="loading" @click="search(false)">加载更多</a-button>
                    </div>
                </section>

                <aside class="detail-pane" aria-label="SIP 报文详情">
                    <div class="detail-head">
                        <div class="detail-heading">
                            <span>报文详情</span>
                            <a-tag v-if="detail" size="small" bordered>{{ messageLabel(detail) }}</a-tag>
                        </div>
                        <a-tooltip content="复制完整报文">
                            <a-button class="icon-command" :disabled="!detail" @click="copyPayload">
                                <template #icon><Copy :size="16" /></template>
                            </a-button>
                        </a-tooltip>
                    </div>
                    <a-spin :loading="detailLoading" class="detail-body">
                        <pre v-if="detail" class="sip-payload">{{ detail.payload }}</pre>
                        <a-empty v-else description="选择一条报文查看详情" />
                    </a-spin>
                </aside>
            </main>
        </div>
    </div>
</template>

<style scoped>
.trace-shell { display: flex; flex-direction: column; min-width: 0; padding: 0; overflow: hidden; }
.trace-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; min-height: 40px; padding: 4px 8px 8px; }
.trace-status { display: flex; align-items: center; gap: 9px; min-width: 0; }
.trace-title { color: var(--uvp-text-primary); font-size: 15px; font-weight: 600; }
.status-detail { color: var(--uvp-text-tertiary); font-size: 12px; }
.status-loss { color: rgb(var(--warning-6)); font-size: 12px; font-weight: 500; }
.trace-alert { margin: 0 8px 8px; }
.filter-band { display: grid; grid-template-columns: 310px 160px 96px 96px 96px minmax(170px, 1fr) auto auto; align-items: center; gap: 8px; padding: 10px 8px; background: var(--uvp-list-toolbar-bg); border-block: 1px solid var(--uvp-divider); }
.range-control, .filter-control, .short-control, .call-id-control { width: 100%; min-width: 0; }
.icon-command { width: 32px; min-width: 32px; height: 32px; padding: 0; }
.trace-workspace { display: grid; grid-template-columns: minmax(520px, 1.35fr) minmax(360px, 1fr); flex: 1; min-height: 0; margin: 8px; overflow: hidden; border: 1px solid var(--uvp-panel-border); background: var(--uvp-panel-bg); }
.message-pane { display: flex; flex-direction: column; min-width: 0; min-height: 0; border-right: 1px solid var(--uvp-divider); }
.trace-table { flex: 1; min-height: 0; }
.trace-table :deep(.arco-table-tr) { cursor: pointer; }
.trace-table :deep(.arco-table-tr:hover .arco-table-td) { background: var(--uvp-brand-soft); }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace; }
.time-cell { color: var(--uvp-text-secondary); font-size: 12px; }
.direction-mark { display: inline-flex; align-items: center; gap: 5px; font-size: 12px; font-weight: 600; }
.direction-mark::before { width: 6px; height: 6px; content: ""; border-radius: 50%; }
.direction-mark.inbound { color: #0f766e; }
.direction-mark.inbound::before { background: #14b8a6; }
.direction-mark.outbound { color: #1d4ed8; }
.direction-mark.outbound::before { background: #3b82f6; }
.message-method { color: var(--uvp-text-primary); font-size: 12px; }
.list-footer { display: flex; align-items: center; justify-content: space-between; min-height: 38px; padding: 4px 10px; color: var(--uvp-text-tertiary); font-size: 12px; border-top: 1px solid var(--uvp-divider); }
.detail-pane { display: flex; flex-direction: column; min-width: 0; min-height: 0; }
.detail-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; min-height: 44px; padding: 5px 10px 5px 14px; border-bottom: 1px solid var(--uvp-divider); }
.detail-heading { display: flex; align-items: center; gap: 8px; min-width: 0; color: var(--uvp-text-primary); font-size: 13px; font-weight: 600; }
.detail-body { display: flex; flex: 1; min-height: 0; }
.detail-body :deep(.arco-spin-children) { display: flex; flex: 1; min-width: 0; min-height: 0; }
.detail-body :deep(.arco-empty) { margin: auto; }
.sip-payload { width: 100%; min-width: 0; min-height: 0; box-sizing: border-box; margin: 0; padding: 14px 16px 24px; overflow: auto; color: var(--uvp-text-primary); background: var(--color-fill-1); font: 12px/1.65 ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace; white-space: pre; tab-size: 4; }

@media (max-width: 1080px) {
    .filter-band { grid-template-columns: minmax(280px, 1.5fr) repeat(3, minmax(96px, 0.5fr)); }
    .filter-control, .call-id-control { grid-column: span 2; }
    .trace-workspace { grid-template-columns: 1fr; overflow: auto; }
    .message-pane { min-height: 470px; border-right: 0; border-bottom: 1px solid var(--uvp-divider); }
    .detail-pane { min-height: 420px; }
}

@media (max-width: 720px) {
    .filter-band { grid-template-columns: 1fr 1fr; }
    .range-control, .filter-control, .call-id-control { grid-column: 1 / -1; }
    .trace-workspace { margin: 6px 0 0; border-inline: 0; }
    .trace-status { flex-wrap: wrap; }
}
</style>
