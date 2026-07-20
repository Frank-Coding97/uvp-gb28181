<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { RefreshCw } from "lucide-vue-next";
import { useTraceFilters } from "./composables/useTraceFilters";
import { useTraceHealth } from "./composables/useTraceHealth";
import type { WorkbenchView } from "./composables/traceFilterTypes";
import type { TraceRecord } from "./api/traceApi";
import TraceTextView from "./views/TraceTextView.vue";
import TraceSessionView from "./views/TraceSessionView.vue";
import TraceDetailPanel from "./components/TraceDetailPanel.vue";

const router = useRouter();
const { state, setView } = useTraceFilters();
const { health, loading: healthLoading, refresh: refreshHealth } = useTraceHealth();
const selectedRecord = ref<TraceRecord | null>(null);

const healthLabel = computed(
    () =>
        ({ ready: "运行正常", degraded: "存储降级", disabled: "功能未启用" }[health.value.state] ?? health.value.state)
);
const healthColor = computed(
    () => ({ ready: "green", degraded: "orange", disabled: "gray" }[health.value.state] ?? "gray")
);
const canQuery = computed(() => health.value.state !== "disabled");

const viewOptions: Array<{ value: WorkbenchView; label: string }> = [
    { value: "text", label: "文本流" },
    { value: "session", label: "时序图" },
    { value: "matrix", label: "通信矩阵" }
];

const globalRefreshing = ref(false);
async function refreshAll() {
    globalRefreshing.value = true;
    try {
        await refreshHealth();
        // 子视图挂 refresh 后自行拉数据(P3+ 挂上)
    } finally {
        globalRefreshing.value = false;
    }
}

function jumpDeviceMgmt() {
    router.push({ path: "/gb28181/device-mgmt" });
}

onMounted(() => {
    // filter 状态已由 useTraceFilters 消费 URL 完成初始化
});
</script>

<template>
    <div class="snow-fill">
        <div class="snow-fill-inner uvp-page-shell-flat trace-shell">
            <header class="trace-toolbar">
                <div class="trace-status">
                    <span class="trace-title">SIP 日志</span>
                    <a-radio-group
                        :model-value="state.view"
                        type="button"
                        size="small"
                        @change="(value: WorkbenchView) => setView(value)"
                    >
                        <a-radio v-for="opt in viewOptions" :key="opt.value" :value="opt.value">
                            {{ opt.label }}
                        </a-radio>
                    </a-radio-group>
                    <a-tag :color="healthColor" bordered>{{ healthLabel }}</a-tag>
                    <span v-if="health.queueCapacity" class="status-detail">
                        队列 {{ health.queueDepth }}/{{ health.queueCapacity }}
                    </span>
                    <span v-if="health.dropped" class="status-loss">已丢失 {{ health.dropped }}</span>
                </div>
                <a-tooltip content="刷新">
                    <a-button class="icon-command" :loading="globalRefreshing || healthLoading" @click="refreshAll">
                        <template #icon><RefreshCw :size="16" /></template>
                    </a-button>
                </a-tooltip>
            </header>

            <a-alert v-if="health.state === 'disabled'" type="info" class="trace-alert">
                SIP Trace 功能未启用,请在 config 里配置 gb28181.trace 相关项
            </a-alert>
            <a-alert v-else-if="health.state === 'degraded'" type="warning" class="trace-alert">
                {{ health.lastError || "ClickHouse 当前不可用,日志可能存在缺口" }}
            </a-alert>

            <div v-if="state.captureId" class="capture-band">
                <span>设备诊断捕获进行中</span>
                <span v-if="state.captureEndsAt" class="status-detail">预计结束 {{ state.captureEndsAt }}</span>
            </div>

            <main v-if="canQuery" class="trace-workspace">
                <section class="view-active" data-testid="active-view">
                    <TraceTextView v-if="state.view === 'text'" @select="selectedRecord = $event" />
                    <TraceSessionView v-else-if="state.view === 'session'" @select="selectedRecord = $event" />
                    <template v-else-if="state.view === 'matrix'">
                        <a-empty description="通信矩阵视图(T-6 落地)" />
                    </template>
                </section>
                <aside class="detail-panel">
                    <TraceDetailPanel :record="selectedRecord" />
                </aside>
            </main>
            <main v-else class="trace-workspace">
                <a-empty description="SIP Trace 未启用,查询暂不可用">
                    <template #image>
                        <RefreshCw :size="32" />
                    </template>
                    <a-button type="primary" @click="jumpDeviceMgmt">回设备管理</a-button>
                </a-empty>
            </main>
        </div>
    </div>
</template>

<style scoped>
.trace-shell {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 16px 20px;
    height: 100%;
    box-sizing: border-box;
}
.trace-toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
}
.trace-status {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
}
.trace-title {
    font-size: 15px;
    font-weight: 600;
}
.status-detail {
    color: #6b7280;
    font-size: 12px;
}
.status-loss {
    color: #d14343;
    font-size: 12px;
    font-weight: 600;
}
.trace-alert {
    margin: 0;
}
.capture-band {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 8px 12px;
    background: rgba(230, 236, 245, 0.6);
    border-radius: 6px;
}
.trace-workspace {
    flex: 1;
    min-height: 0;
    display: flex;
    gap: 12px;
}
.view-active {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}
.detail-panel {
    width: 400px;
    flex-shrink: 0;
    border-left: 1px solid #e5e6eb;
    overflow: hidden;
}
.view-placeholder {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
}
</style>
