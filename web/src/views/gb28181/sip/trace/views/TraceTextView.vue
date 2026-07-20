<script setup lang="ts">
import { ref, computed, onMounted, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { useTraceFilters } from "../composables/useTraceFilters";
import { fetchTraceRecords, type TraceRecord } from "../api/traceApi";
import { classifyMessage } from "../semantic/semanticLabel";
import { parseTimeRange } from "../composables/traceFilterTypes";

const { state } = useTraceFilters();

const emit = defineEmits<{
    select: [record: TraceRecord];
}>();

const timeRange = computed(() => parseTimeRange(state.range));

const records = ref<TraceRecord[]>([]);
const loading = ref(false);
const total = ref(0);

const rows = computed(() =>
    records.value.map((r) => {
        const label = classifyMessage({ method: r.method, body: r.body });
        return {
            ...r,
            label: label.label,
            category: label.category,
            timestamp: new Date(r.ts).toLocaleTimeString("zh-CN", { hour12: false })
        };
    })
);

async function loadRecords() {
    loading.value = true;
    try {
        const resp = await fetchTraceRecords({
            startTime: timeRange.value?.start,
            endTime: timeRange.value?.end,
            deviceId: state.deviceId || undefined,
            callId: state.callId || undefined,
            direction: state.direction || undefined,
            method: state.method || undefined,
            limit: 50,
            offset: 0
        });
        records.value = resp.records;
        total.value = resp.total;
    } catch (err) {
        Message.error("加载 trace 失败: " + (err instanceof Error ? err.message : String(err)));
    } finally {
        loading.value = false;
    }
}

function copyRaw(record: TraceRecord) {
    const raw = `${record.method} ${record.statusCode ? record.statusCode : ""}\nFrom: ${record.from}\nTo: ${record.to}\nCall-ID: ${record.callId}\n\n${record.body || ""}`;
    navigator.clipboard.writeText(raw).then(
        () => Message.success("已复制原始消息"),
        () => Message.error("复制失败")
    );
}

onMounted(() => {
    loadRecords();
});

watch(
    () => [state.range, state.deviceId, state.callId, state.direction, state.method],
    () => {
        loadRecords();
    },
    { deep: true }
);
</script>

<template>
    <div class="trace-text-view">
        <a-spin :loading="loading" class="trace-spin">
            <div class="trace-scroll-container">
                <div
                    v-for="row in rows"
                    :key="row.id"
                    class="trace-row"
                    @click="emit('select', row)"
                    @contextmenu.prevent="copyRaw(row)"
                >
                    <span class="trace-ts">{{ row.timestamp }}</span>
                    <a-tag :color="row.direction === 'inbound' ? 'blue' : 'green'" size="small">
                        {{ row.direction === "inbound" ? "←" : "→" }}
                    </a-tag>
                    <span class="trace-method">{{ row.method }}</span>
                    <span v-if="row.statusCode" class="trace-status">{{ row.statusCode }}</span>
                    <a-tag size="small" class="trace-label">{{ row.label }}</a-tag>
                    <span class="trace-peer">{{ row.from }} → {{ row.to }}</span>
                    <span class="trace-call-id">{{ row.callId }}</span>
                </div>
            </div>
        </a-spin>
        <div class="trace-footer">共 {{ total }} 条 · 右键复制原始消息</div>
    </div>
</template>

<style scoped>
.trace-text-view {
    display: flex;
    flex-direction: column;
    height: 100%;
    gap: 8px;
}
.trace-spin {
    flex: 1;
    min-height: 0;
}
.trace-scroll-container {
    height: 100%;
    overflow-y: auto;
    padding: 8px;
    background: #fafafa;
    border-radius: 6px;
}
.trace-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    font-size: 13px;
    font-family: "SF Mono", Monaco, monospace;
    border-bottom: 1px solid #e5e7eb;
    cursor: pointer;
}
.trace-row:hover {
    background: rgba(var(--primary-6), 0.08);
}
.trace-ts {
    color: #6b7280;
    min-width: 80px;
}
.trace-method {
    font-weight: 600;
    min-width: 80px;
}
.trace-status {
    color: #059669;
    font-weight: 600;
}
.trace-label {
    min-width: 80px;
}
.trace-peer {
    color: #4b5563;
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}
.trace-call-id {
    color: #9ca3af;
    font-size: 12px;
}
.trace-footer {
    padding: 8px 12px;
    color: #6b7280;
    font-size: 12px;
    text-align: right;
}
</style>
