<template>
    <div class="trace-session-view">
        <div v-if="loading" class="loading">加载中...</div>
        <div v-else-if="error" class="error">{{ error }}</div>
        <div v-else-if="sessions.length === 0" class="empty">暂无会话数据</div>
        <div v-else class="session-list">
            <div v-for="session in sessions" :key="session.callId" class="session-group">
                <div class="session-header" @click="toggleSession(session.callId)">
                    <span class="expand-icon">{{ expandedSessions.has(session.callId) ? '▼' : '▶' }}</span>
                    <span class="call-id">{{ session.callId }}</span>
                    <span class="count">{{ session.records.length }} 条消息</span>
                    <span class="time-range">{{ formatTimeRange(session) }}</span>
                </div>
                <div v-if="expandedSessions.has(session.callId)" class="session-records">
                    <div
                        v-for="record in session.records"
                        :key="record.id"
                        class="record-row"
                        @click="emit('select', record)"
                        @contextmenu.prevent="copyRaw(record)"
                    >
                        <span class="timestamp">{{ formatTimestamp(record.ts) }}</span>
                        <span :class="['direction', record.direction]">
                            {{ record.direction === 'inbound' ? '⬇️' : '⬆️' }}
                        </span>
                        <span class="method">{{ record.method }}</span>
                        <span v-if="record.statusCode" class="status">{{ record.statusCode }}</span>
                        <span class="tag">{{ getSemanticTag(record) }}</span>
                        <span class="endpoints">{{ record.from }} → {{ record.to }}</span>
                    </div>
                </div>
            </div>
        </div>
    </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from "vue";
import { Message } from "@arco-design/web-vue";
import { useTraceFilters } from "../composables/useTraceFilters";
import { fetchTraceRecords } from "../api/traceApi";
import type { TraceRecord } from "../api/traceApi";
import { parseTimeRange } from "../composables/traceFilterTypes";

interface SessionGroup {
    callId: string;
    records: TraceRecord[];
}

const { state } = useTraceFilters();

const emit = defineEmits<{
    select: [record: TraceRecord];
}>();

const loading = ref(false);
const error = ref("");
const allRecords = ref<TraceRecord[]>([]);
const expandedSessions = ref(new Set<string>());

const sessions = computed<SessionGroup[]>(() => {
    const map = new Map<string, TraceRecord[]>();
    allRecords.value.forEach((r: TraceRecord) => {
        if (!map.has(r.callId)) map.set(r.callId, []);
        map.get(r.callId)!.push(r);
    });
    return Array.from(map.entries())
        .map(([callId, records]) => ({
            callId,
            records: records.sort((a, b) => a.ts - b.ts)
        }))
        .sort((a, b) => b.records[0].ts - a.records[0].ts);
});

async function loadRecords() {
    loading.value = true;
    error.value = "";
    try {
        const timeRange = parseTimeRange(state.range);
        if (!timeRange) {
            error.value = "时间范围解析失败";
            return;
        }
        const response = await fetchTraceRecords({
            startTime: timeRange.start,
            endTime: timeRange.end,
            deviceId: state.deviceId,
            callId: state.callId,
            direction: state.direction || undefined,
            method: state.method
        });
        allRecords.value = response.records;
    } catch (err) {
        error.value = err instanceof Error ? err.message : "加载失败";
    } finally {
        loading.value = false;
    }
}

function toggleSession(callId: string) {
    if (expandedSessions.value.has(callId)) {
        expandedSessions.value.delete(callId);
    } else {
        expandedSessions.value.add(callId);
    }
}

function formatTimestamp(ts: number): string {
    return new Date(ts).toLocaleString("zh-CN", {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        hour12: false
    });
}

function formatTimeRange(session: SessionGroup): string {
    if (session.records.length === 0) return "";
    const first = session.records[0].ts;
    const last = session.records[session.records.length - 1].ts;
    const duration = ((last - first) / 1000).toFixed(1);
    return `历时 ${duration}s`;
}

function getSemanticTag(record: TraceRecord): string {
    if (record.method === "REGISTER") return "注册";
    if (record.method === "MESSAGE") return "信令";
    if (record.method === "INVITE") return "邀请";
    if (record.method === "BYE") return "挂断";
    if (record.method === "ACK") return "应答";
    return record.method;
}

function copyRaw(record: TraceRecord) {
    const raw = `${record.method} ${record.direction === 'inbound' ? 'FROM' : 'TO'} ${record.from}\nCall-ID: ${record.callId}\n\n${record.body || '(no body)'}`;
    navigator.clipboard.writeText(raw);
    Message.success("已复制原始消息");
}

watch(
    () => [state.range, state.deviceId, state.callId, state.direction, state.method],
    () => {
        loadRecords();
    },
    { deep: true }
);

onMounted(() => {
    loadRecords();
});
</script>

<style scoped lang="less">
.trace-session-view {
    height: 100%;
    overflow: auto;
    padding: 12px;
}

.loading,
.error,
.empty {
    text-align: center;
    padding: 40px;
    color: #86909c;
}

.error {
    color: #d14343;
}

.session-list {
    display: flex;
    flex-direction: column;
    gap: 12px;
}

.session-group {
    border: 1px solid #e5e6eb;
    border-radius: 4px;
    overflow: hidden;
}

.session-header {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 12px;
    background: #f7f8fa;
    cursor: pointer;
    user-select: none;
    transition: background 0.2s;

    &:hover {
        background: #eef0f3;
    }

    .expand-icon {
        color: #4e5969;
        font-size: 12px;
        width: 14px;
    }

    .call-id {
        font-family: "SF Mono", Monaco, monospace;
        font-size: 13px;
        font-weight: 500;
        color: #1d2129;
    }

    .count {
        color: #86909c;
        font-size: 12px;
    }

    .time-range {
        color: #86909c;
        font-size: 12px;
        margin-left: auto;
    }
}

.session-records {
    background: #ffffff;
}

.record-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 12px 8px 38px;
    border-top: 1px solid #f2f3f5;
    font-size: 13px;
    cursor: pointer;
    transition: background 0.2s;

    &:hover {
        background: rgba(var(--primary-6), 0.08);
    }

    .timestamp {
        color: #86909c;
        font-family: "SF Mono", Monaco, monospace;
        font-size: 12px;
        width: 150px;
    }

    .direction {
        width: 24px;
        text-align: center;

        &.inbound {
            color: #059669;
        }

        &.outbound {
            color: #0d6efd;
        }
    }

    .method {
        font-weight: 500;
        color: #1d2129;
        width: 80px;
    }

    .status {
        font-family: "SF Mono", Monaco, monospace;
        font-size: 12px;
        color: #4e5969;
        width: 40px;
    }

    .tag {
        padding: 2px 8px;
        border-radius: 3px;
        font-size: 12px;
        background: #e8f3ff;
        color: #0d6efd;
        width: 100px;
        text-align: center;
    }

    .endpoints {
        color: #86909c;
        font-family: "SF Mono", Monaco, monospace;
        font-size: 12px;
        flex: 1;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }
}

/* 深色模式适配 */
body[arco-theme='dark'] {
    .loading,
    .empty {
        color: #9ca3af;
    }

    .session-group {
        border-color: #2e2e30;
    }

    .session-header {
        background: #1e1e20;

        &:hover {
            background: #252527;
        }

        .expand-icon {
            color: #9ca3af;
        }

        .call-id {
            color: #e5e7eb;
        }

        .count,
        .time-range {
            color: #9ca3af;
        }
    }

    .session-records {
        background: #17171a;
    }

    .record-row {
        border-top-color: #2e2e30;

        .method {
            color: #e5e7eb;
        }

        .status {
            color: #9ca3af;
        }

        .tag {
            background: rgba(13, 110, 253, 0.2);
            color: #60a5fa;
        }

        .endpoints {
            color: #9ca3af;
        }
    }
}
</style>
