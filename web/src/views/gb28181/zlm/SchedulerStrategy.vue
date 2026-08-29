<script setup lang="ts">
import { ref, computed, onMounted } from "vue";
import { Message } from "@arco-design/web-vue";
import {
    getScheduler,
    switchScheduler,
    type SchedulerAlgorithm
} from "@/api/gb28181-zlm";

// 算法元数据(说明 + 适用场景)
const algorithmMeta: Record<SchedulerAlgorithm, { title: string; desc: string }> = {
    roundrobin: {
        title: "轮询(RoundRobin)",
        desc: "依次分配,节点能力相近时最公平。默认算法,无状态依赖。"
    },
    weighted: {
        title: "加权轮询(Weighted)",
        desc: "按节点 weight 比例分配,适合异构集群(CPU/带宽不同的机器)。"
    },
    leastload: {
        title: "最小负载(LeastLoad)",
        desc: "选 NetThreadLoadAvg×0.6 + WorkThreadLoadAvg×0.4 最低的节点。需心跳数据。"
    }
};

const current = ref<SchedulerAlgorithm | "">("");
const selected = ref<SchedulerAlgorithm | "">("");
const available = ref<SchedulerAlgorithm[]>([]);
const loading = ref(false);
const saving = ref(false);

// 当前算法标题(空时返"未装配")
const currentTitle = computed(() => {
    const c = current.value;
    if (!c) return "";
    return algorithmMeta[c as SchedulerAlgorithm]?.title || c;
});

async function refresh() {
    loading.value = true;
    try {
        const res = await getScheduler();
        if (res.code === 0) {
            current.value = res.data.algorithm;
            selected.value = res.data.algorithm || "roundrobin";
            available.value = res.data.available;
        }
    } catch (e: any) {
        Message.error(e?.message || "加载失败");
    } finally {
        loading.value = false;
    }
}

async function handleSave() {
    if (!selected.value || selected.value === current.value) return;
    saving.value = true;
    try {
        const algo = selected.value as SchedulerAlgorithm;
        const res = await switchScheduler(algo);
        if (res.code === 0) {
            Message.success(`已切换到 ${algorithmMeta[algo].title}`);
            await refresh();
        } else {
            Message.error(res.message || "切换失败");
        }
    } catch (e: any) {
        Message.error(e?.message || "切换失败");
    } finally {
        saving.value = false;
    }
}

onMounted(refresh);
</script>

<template>
    <div class="snow-fill">
        <div class="snow-fill-inner uvp-page-shell-flat scheduler-shell">
            <div class="scheduler-strategy">
                <div class="strategy-toolbar">
                    <div class="strategy-summary">
                        <span class="summary-label">当前算法</span>
                        <a-tag v-if="current" color="arcoblue" class="summary-tag">
                            {{ currentTitle }}
                        </a-tag>
                        <span v-else class="summary-empty">未装配</span>
                    </div>
                    <div class="strategy-actions">
                        <a-button class="uvp-refresh-btn" @click="refresh" :loading="loading">
                            <template #icon><icon-refresh /></template>
                            刷新
                        </a-button>
                        <a-button
                            type="primary"
                            :loading="saving"
                            :disabled="!selected || selected === current"
                            @click="handleSave"
                        >
                            保存
                        </a-button>
                    </div>
                </div>

                <a-spin :loading="loading" class="strategy-panel">
                    <div class="strategy-hint">
                        <span>选择 ZLM 节点调度算法</span>
                        <span class="strategy-hint__sub">保存后立即生效,无需重启</span>
                    </div>

                    <a-radio-group v-model="selected" direction="vertical" class="algo-radios">
                        <label
                            v-for="algo in available"
                            :key="algo"
                            class="algo-card"
                            :class="{ 'algo-card--active': selected === algo }"
                        >
                            <a-radio :value="algo" class="algo-radio-row">
                                <div class="algo-meta">
                                    <div class="algo-title">{{ algorithmMeta[algo]?.title || algo }}</div>
                                    <div class="algo-desc">{{ algorithmMeta[algo]?.desc || "" }}</div>
                                </div>
                            </a-radio>
                            <a-tag v-if="current === algo" size="small" color="green" bordered>当前生效</a-tag>
                        </label>
                    </a-radio-group>
                </a-spin>
            </div>
        </div>
    </div>
</template>

<style scoped>
.scheduler-shell {
    padding: 0;
    overflow: hidden;
}

.scheduler-strategy {
    height: 100%;
    overflow: auto;
    min-width: 0;
    box-sizing: border-box;
    padding: 4px 8px;
}

.strategy-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    margin-bottom: 16px;
    min-width: 0;
}

.strategy-summary {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
}

.summary-label {
    color: var(--uvp-text-tertiary);
    font-size: 12px;
}

.summary-tag {
    min-height: 26px;
    padding: 0 10px;
    line-height: 24px;
}

.summary-empty {
    color: var(--uvp-text-tertiary);
    font-size: 12px;
}

.strategy-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    justify-content: flex-end;
}

.strategy-actions :deep(.arco-btn) {
    box-sizing: border-box;
    border-radius: 10px;
}

.strategy-panel {
    display: block;
    width: 100%;
    min-width: 0;
    box-sizing: border-box;
    overflow: hidden;
    padding: 16px 18px 18px;
    background: var(--uvp-panel-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: var(--uvp-panel-radius);
    box-shadow: var(--uvp-panel-shadow);
}

.strategy-hint {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 14px;
    color: var(--uvp-text-secondary);
    font-size: 13px;
    flex-wrap: wrap;
    min-width: 0;
}

.strategy-hint__sub {
    color: var(--uvp-text-tertiary);
    font-size: 12px;
    white-space: nowrap;
}

.algo-radios {
    height: 100%;
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
}

.algo-card {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    width: 100%;
    min-width: 0;
    box-sizing: border-box;
    padding: 14px 16px;
    background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 12px;
    transition:
        border-color 0.18s ease,
        box-shadow 0.18s ease,
        background-color 0.18s ease;
    cursor: pointer;
}

.algo-card:hover {
    border-color: rgb(37 99 235 / 24%);
    box-shadow: 0 10px 20px -18px rgb(37 99 235 / 28%);
}

.algo-card--active {
    background: rgb(37 99 235 / 7%);
    border-color: rgb(37 99 235 / 32%);
    box-shadow: 0 10px 20px -18px rgb(37 99 235 / 34%);
}

.algo-radio-row {
    flex: 1;
    min-width: 0;
    margin: 0;
}

.algo-meta {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
}

.algo-title {
    color: var(--uvp-text-primary);
    font-size: 14px;
    font-weight: 600;
    line-height: 1.4;
}

.algo-desc {
    color: var(--uvp-text-tertiary);
    font-size: 12px;
    line-height: 1.4;
}

.algo-radio-row :deep(.arco-radio) {
    display: flex;
    width: 100%;
    align-items: flex-start;
    gap: 12px;
    min-width: 0;
}

.algo-radio-row :deep(.arco-radio-mask) {
    flex-shrink: 0;
    margin-top: 2px;
}

.algo-radio-row :deep(.arco-radio-label) {
    flex: 1;
    min-width: 0;
}

@media (max-width: 768px) {
    .strategy-toolbar,
    .strategy-hint,
    .algo-card {
        flex-direction: column;
        align-items: flex-start;
    }

    .strategy-actions {
        width: 100%;
        justify-content: flex-start;
    }

    .strategy-hint__sub {
        white-space: normal;
    }
}
</style>
