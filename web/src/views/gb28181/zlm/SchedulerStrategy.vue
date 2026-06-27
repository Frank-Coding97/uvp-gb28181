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
    <div class="scheduler-strategy">
        <a-page-header
            title="调度算法"
            subtitle="ZLM 节点选路策略,实时生效(无需重启)"
            :show-back="false"
        />

        <a-card style="margin: 16px" :loading="loading">
            <a-descriptions :column="1" bordered>
                <a-descriptions-item label="当前算法">
                    <a-tag color="arcoblue" size="medium" v-if="current">
                        {{ currentTitle }}
                    </a-tag>
                    <span v-else style="color: #86909c">未装配</span>
                </a-descriptions-item>
            </a-descriptions>

            <a-divider />

            <a-radio-group v-model="selected" direction="vertical" class="algo-radios">
                <a-radio
                    v-for="algo in available"
                    :key="algo"
                    :value="algo"
                    class="algo-radio-row"
                >
                    <div class="algo-meta">
                        <div class="algo-title">{{ algorithmMeta[algo]?.title || algo }}</div>
                        <div class="algo-desc">{{ algorithmMeta[algo]?.desc || "" }}</div>
                    </div>
                </a-radio>
            </a-radio-group>

            <a-space style="margin-top: 24px">
                <a-button
                    type="primary"
                    :loading="saving"
                    :disabled="!selected || selected === current"
                    @click="handleSave"
                >
                    保存
                </a-button>
                <a-button @click="refresh">重置</a-button>
            </a-space>
        </a-card>
    </div>
</template>

<style scoped>
.scheduler-strategy {
    height: 100%;
    overflow: auto;
}

.algo-radios {
    display: flex;
    flex-direction: column;
    gap: 16px;
}

.algo-radio-row {
    padding: 8px 0;
}

.algo-meta {
    display: inline-flex;
    flex-direction: column;
    margin-left: 4px;
}

.algo-title {
    font-weight: 600;
    line-height: 1.4;
}

.algo-desc {
    font-size: 12px;
    color: #86909c;
    line-height: 1.4;
    margin-top: 2px;
}
</style>
