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
    <div class="scheduler-log">
        <a-page-header
            title="调度日志"
            subtitle="Scheduler.Pick 决策审计,保留 7 天,异步落库"
            :show-back="false"
        >
            <template #extra>
                <a-space>
                    <a-select
                        v-model="limit"
                        :options="limitOptions"
                        :style="{ width: '110px' }"
                        @change="refresh"
                    />
                    <a-switch
                        :model-value="autoRefresh"
                        checked-text="自动 30s"
                        unchecked-text="手动"
                        @change="(v: boolean | string | number) => toggleAutoRefresh(Boolean(v))"
                    />
                    <a-button @click="refresh">刷新</a-button>
                </a-space>
            </template>
        </a-page-header>

        <a-card style="margin: 16px">
            <a-table
                :data="logs"
                :loading="loading"
                row-key="id"
                :pagination="false"
                :row-class-name="rowClass"
                size="small"
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
                            <span v-else style="color: #aaa">—</span>
                        </template>
                    </a-table-column>
                    <a-table-column title="命中节点">
                        <template #cell="{ record }">
                            <span v-if="record.nodeName">
                                {{ record.nodeName }}
                                <span style="color: #86909c">(id={{ record.nodeID }})</span>
                            </span>
                            <span v-else style="color: #aaa">—</span>
                        </template>
                    </a-table-column>
                    <a-table-column title="StreamID" :width="200">
                        <template #cell="{ record }">
                            <span v-if="record.streamID">{{ record.streamID }}</span>
                            <span v-else style="color: #aaa">—</span>
                        </template>
                    </a-table-column>
                    <a-table-column title="设备/通道" :width="220">
                        <template #cell="{ record }">
                            <span v-if="record.deviceID">
                                {{ record.deviceID }}
                                <span style="color: #86909c" v-if="record.channelID">
                                    / {{ record.channelID }}
                                </span>
                            </span>
                            <span v-else style="color: #aaa">—</span>
                        </template>
                    </a-table-column>
                    <a-table-column title="错误">
                        <template #cell="{ record }">
                            <a-tag v-if="record.errorMessage" color="red" size="small">
                                {{ record.errorMessage }}
                            </a-tag>
                            <span v-else style="color: #00b42a">成功</span>
                        </template>
                    </a-table-column>
                </template>
            </a-table>
        </a-card>
    </div>
</template>

<style scoped>
.scheduler-log {
    height: 100%;
    overflow: auto;
}

:deep(tr.row-error > td),
:deep(.arco-table-tr.row-error > .arco-table-td),
:deep(tr.row-error .arco-table-td) {
    background-color: #ffece8 !important;
}
</style>
