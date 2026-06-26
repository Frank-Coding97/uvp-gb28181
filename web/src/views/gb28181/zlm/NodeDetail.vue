<script setup lang="ts">
import { ref, onMounted, onUnmounted } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Message } from "@arco-design/web-vue";
import { getZLMNode, type ZLMNode } from "@/api/gb28181-zlm";
import NodeStateBadge from "./components/NodeStateBadge.vue";
import NodeConfig from "./NodeConfig.vue";

const route = useRoute();
const router = useRouter();
const node = ref<ZLMNode | null>(null);
const loading = ref(false);
const activeTab = ref<"overview" | "config">("overview");
let refreshTimer: ReturnType<typeof setInterval> | null = null;

const nodeId = Number(route.params.id);

async function refresh() {
    loading.value = true;
    try {
        const res = await getZLMNode(nodeId);
        if (res.code === 0) {
            node.value = res.data;
        }
    } catch (e: any) {
        Message.error(e?.message || "加载失败");
    } finally {
        loading.value = false;
    }
}

onMounted(() => {
    if (!nodeId || Number.isNaN(nodeId)) {
        router.replace("/gb28181/zlm/nodes");
        return;
    }
    refresh();
    refreshTimer = setInterval(refresh, 30_000);
});
onUnmounted(() => {
    if (refreshTimer) clearInterval(refreshTimer);
});

function fmtBytes(n: number): string {
    if (!n) return "0 B";
    const units = ["B", "KB", "MB", "GB", "TB"];
    let i = 0;
    let v = n;
    while (v >= 1024 && i < units.length - 1) {
        v /= 1024;
        i++;
    }
    return `${v.toFixed(2)} ${units[i]}`;
}
</script>

<template>
    <div class="zlm-node-detail">
        <a-page-header
            :title="node?.name || '节点详情'"
            :subtitle="node ? `${node.host}:${node.apiPort}` : ''"
            @back="router.push('/gb28181/zlm/nodes')"
        >
            <template #extra>
                <NodeStateBadge v-if="node" :state="node.state" />
            </template>
        </a-page-header>

        <a-tabs v-model:active-key="activeTab" style="margin: 16px">
            <a-tab-pane key="overview" title="概览">
                <a-spin :loading="loading">
                    <a-row :gutter="16">
                        <a-col :span="6">
                            <a-card>
                                <a-statistic title="当前流数" :value="node?.stats?.mediaSourceCount || 0" />
                            </a-card>
                        </a-col>
                        <a-col :span="6">
                            <a-card>
                                <a-statistic title="会话数" :value="node?.stats?.sessionCount || 0" />
                            </a-card>
                        </a-col>
                        <a-col :span="6">
                            <a-card>
                                <a-statistic
                                    title="网络线程负载"
                                    :value="((node?.stats?.netThreadLoadAvg || 0) * 100).toFixed(1)"
                                    suffix="%"
                                />
                            </a-card>
                        </a-col>
                        <a-col :span="6">
                            <a-card>
                                <a-statistic
                                    title="工作线程负载"
                                    :value="((node?.stats?.workThreadLoadAvg || 0) * 100).toFixed(1)"
                                    suffix="%"
                                />
                            </a-card>
                        </a-col>
                    </a-row>
                    <a-row :gutter="16" style="margin-top: 16px">
                        <a-col :span="8">
                            <a-card>
                                <a-statistic title="内存占用" :value="fmtBytes(node?.stats?.memoryUsageBytes || 0)" />
                            </a-card>
                        </a-col>
                        <a-col :span="8">
                            <a-card>
                                <a-statistic title="累计入流量" :value="fmtBytes(node?.stats?.totalBytesIn || 0)" />
                            </a-card>
                        </a-col>
                        <a-col :span="8">
                            <a-card>
                                <a-statistic title="累计出流量" :value="fmtBytes(node?.stats?.totalBytesOut || 0)" />
                            </a-card>
                        </a-col>
                    </a-row>
                    <a-descriptions style="margin-top: 16px" :column="2" title="节点信息" bordered>
                        <a-descriptions-item label="ID">{{ node?.id }}</a-descriptions-item>
                        <a-descriptions-item label="UUID">{{ node?.mediaServerUUID }}</a-descriptions-item>
                        <a-descriptions-item label="权重">{{ node?.weight }}</a-descriptions-item>
                        <a-descriptions-item label="RTP 端口范围">
                            {{ node?.rtpPortStart }}-{{ node?.rtpPortEnd }}
                        </a-descriptions-item>
                        <a-descriptions-item label="创建时间">{{ node?.createdAt }}</a-descriptions-item>
                        <a-descriptions-item label="更新时间">{{ node?.updatedAt }}</a-descriptions-item>
                    </a-descriptions>
                </a-spin>
            </a-tab-pane>
            <a-tab-pane key="config" title="配置">
                <NodeConfig v-if="nodeId" :node-id="nodeId" />
            </a-tab-pane>
        </a-tabs>
    </div>
</template>

<style scoped>
.zlm-node-detail {
    height: 100%;
    overflow: auto;
}
</style>
