<script setup lang="ts">
import { ref, onMounted, onUnmounted } from "vue";
import { Message } from "@arco-design/web-vue";
import {
    listSchedulerLogs,
    type SchedulerLogEntry
} from "@/api/gb28181-zlm";

const logs = ref<SchedulerLogEntry[]>([]);
const loading = ref(false);
const limit = ref(100);
const autoRefresh = ref(true);

const limitOptions = [
    { label: "50 条", value: 50 },
    { label: "100 条", value: 100 },
    { label: "200 条", value: 200 },
    { label: "500 条", value: 500 }
];

let refreshTimer: ReturnType<typeof setInterval> | null = null;

async function refresh() {
    loading.value = true;
    try {
        const res = await listSchedulerLogs(limit.value);
        if (res.code === 0) {
            logs.value = res.data.list || [];
        }
    } catch (e: any) {
        Message.error(e?.message || "加载失败");
    } finally {
        loading.value = false;
    }
}

function toggleAutoRefresh(val: boolean) {
    autoRefresh.value = val;
    if (refreshTimer) {
        clearInterval(refreshTimer);
        refreshTimer = null;
    }
    if (val) {
        refreshTimer = setInterval(refresh, 30_000);
    }
}

function formatTime(s: string): string {
    if (!s) return "—";
    try {
        return new Date(s).toLocaleString();
    } catch {
        return s;
    }
}

function rowClass(record: SchedulerLogEntry): string {
    return record.errorMessage ? "row-error" : "";
}

onMounted(() => {
    refresh();
    if (autoRefresh.value) {
        refreshTimer = setInterval(refresh, 30_000);
    }
});
onUnmounted(() => {
    if (refreshTimer) clearInterval(refreshTimer);
});
</script>

<template>
    <div class="snow-fill">
        <div class="snow-fill-inner uvp-page-shell-flat scheduler-log-shell">
            <div class="scheduler-log">
                <s-layout-search class="scheduler-log-search">
                    <template #fields>
                    <a-select
                        v-model="limit"
                        :options="limitOptions"
                        style="width: 126px"
                        placeholder="日志条数"
                        @change="refresh"
                    />
                        <span class="log-count">{{ logs.length }} 条日志</span>
                    </template>
                    <template #actions>
                    <a-switch
                            class="auto-refresh"
                        :model-value="autoRefresh"
                        checked-text="自动 30s"
                        unchecked-text="手动"
                        @change="(v: boolean | string | number) => toggleAutoRefresh(Boolean(v))"
                    />
                        <a-button class="uvp-refresh-btn" @click="refresh" :loading="loading">
                            <template #icon><icon-refresh /></template>
                            刷新
                        </a-button>
                    </template>
                </s-layout-search>

                <div class="scheduler-log-table">
            <a-table
                :data="logs"
                :loading="loading"
                row-key="id"
                :pagination="false"
                :row-class-name="rowClass"
                size="small"
                        class="uvp-data-table"
            >
                <template #columns>
                    <a-table-column title="时间" :width="180">
                        <template #cell="{ record }">{{ formatTime(record.happenedAt) }}</template>
                    </a-table-column>
                    <a-table-column title="算法" :width="100">
                        <template #cell="{ record }">
                            <a-tag v-if="record.algorithm" size="small" color="arcoblue">
                                {{ record.algorithm }}
                            </a-tag>
                                    <span v-else class="muted">—</span>
                        </template>
                    </a-table-column>
                    <a-table-column title="命中节点">
                        <template #cell="{ record }">
                            <span v-if="record.nodeName">
                                {{ record.nodeName }}
                                        <span class="muted">(id={{ record.nodeID }})</span>
                            </span>
                                    <span v-else class="muted">—</span>
                        </template>
                    </a-table-column>
                    <a-table-column title="StreamID" :width="200">
                        <template #cell="{ record }">
                            <span v-if="record.streamID">{{ record.streamID }}</span>
                                    <span v-else class="muted">—</span>
                        </template>
                    </a-table-column>
                    <a-table-column title="设备/通道" :width="220">
                        <template #cell="{ record }">
                            <span v-if="record.deviceID">
                                {{ record.deviceID }}
                                        <span class="muted" v-if="record.channelID">
                                    / {{ record.channelID }}
                                </span>
                            </span>
                                    <span v-else class="muted">—</span>
                        </template>
                    </a-table-column>
                    <a-table-column title="错误">
                        <template #cell="{ record }">
                            <a-tag v-if="record.errorMessage" color="red" size="small">
                                {{ record.errorMessage }}
                            </a-tag>
                                    <span v-else class="success-text">成功</span>
                        </template>
                    </a-table-column>
                </template>
            </a-table>
                </div>
            </div>
        </div>
    </div>
</template>

<style scoped>
.scheduler-log-shell {
    padding: 4px 8px;
    overflow: hidden;
}

.scheduler-log {
    height: 100%;
    overflow: auto;
}

.scheduler-log-search {
    margin-bottom: 16px;
}

.scheduler-log-search :deep(.uvp-search-panel__fields) {
    flex-wrap: nowrap;
}

.scheduler-log-search :deep(.uvp-search-panel__actions) {
    gap: 10px;
}

.scheduler-log-search :deep(.arco-select-view),
.scheduler-log-search :deep(.arco-input-wrapper) {
    box-sizing: border-box;
    height: 44px;
    min-height: 44px;
    background: var(--uvp-search-control-bg) !important;
    border: 1px solid var(--uvp-search-secondary-btn-border) !important;
    border-radius: 10px !important;
    box-shadow: var(--uvp-search-control-shadow) !important;
}

.scheduler-log-search :deep(.arco-select-view.arco-select-view-focus),
.scheduler-log-search :deep(.arco-select-view:focus-within),
.scheduler-log-search :deep(.arco-input-wrapper:focus-within) {
    border-color: var(--uvp-brand) !important;
    box-shadow: var(--uvp-search-control-focus-shadow) !important;
}

.scheduler-log-search :deep(.arco-select-view-input::placeholder),
.scheduler-log-search :deep(.arco-input::placeholder) {
    color: var(--uvp-text-tertiary) !important;
    opacity: 1;
}

.scheduler-log-search :deep(.arco-btn) {
    box-sizing: border-box;
    height: 44px;
    min-height: 44px;
    border-radius: 10px;
}

.auto-refresh {
    min-width: 92px;
}

.log-count {
    display: inline-flex;
    align-items: center;
    height: 44px;
    color: var(--uvp-text-tertiary);
    font-size: 12px;
    white-space: nowrap;
}

.scheduler-log-table {
    overflow: hidden;
    background: var(--uvp-panel-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: var(--uvp-panel-radius);
    box-shadow: var(--uvp-panel-shadow);
}

.scheduler-log-table :deep(.arco-table-container) {
    border-radius: inherit;
}

.scheduler-log-table :deep(.arco-table-td) {
    font-size: 14px;
    line-height: 22px;
}

.muted {
    color: var(--uvp-text-tertiary);
}

.success-text {
    color: #16845f;
    font-weight: 500;
}

:deep(tr.row-error > td),
:deep(.arco-table-tr.row-error > .arco-table-td),
:deep(tr.row-error .arco-table-td) {
    background-color: rgb(248 113 113 / 10%) !important;
}

@media (max-width: 768px) {
    .scheduler-log-search :deep(.uvp-search-panel__fields) {
        flex-wrap: wrap;
    }

    .scheduler-log-search :deep(.arco-select) {
        width: 100% !important;
    }
}
</style>
