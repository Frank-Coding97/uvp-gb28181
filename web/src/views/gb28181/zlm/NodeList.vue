<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { useRouter } from "vue-router";
import {
    listZLMNodes,
    deleteZLMNode,
    setZLMNodeMaintenance,
    activateZLMNode,
    type ZLMNode
} from "@/api/gb28181-zlm";
import NodeForm from "./NodeForm.vue";
import NodeStateBadge from "./components/NodeStateBadge.vue";

const router = useRouter();
const nodes = ref<ZLMNode[]>([]);
const loading = ref(false);
const drawerVisible = ref(false);

let refreshTimer: ReturnType<typeof setInterval> | null = null;

const totalStreams = computed(() =>
    nodes.value.reduce((sum, n) => sum + (n.stats?.mediaSourceCount || 0), 0)
);

async function refresh() {
    loading.value = true;
    try {
        const res = await listZLMNodes();
        if (res.code === 0) {
            nodes.value = res.data.list || [];
        }
    } catch (e: any) {
        Message.error(e?.message || "加载失败");
    } finally {
        loading.value = false;
    }
}

function openCreate() {
    drawerVisible.value = true;
}

async function handleMaintenance(node: ZLMNode) {
    Modal.warning({
        title: "切到维护态?",
        content: `节点 ${node.name} 将不再接受新流,旧流自然结束`,
        okText: "确认",
        cancelText: "取消",
        hideCancel: false,
        onOk: async () => {
            try {
                await setZLMNodeMaintenance(node.id);
                Message.success("已切到维护态");
                refresh();
            } catch (e: any) {
                Message.error(e?.message || "操作失败");
            }
        }
    });
}

async function handleActivate(node: ZLMNode) {
    try {
        await activateZLMNode(node.id);
        Message.success("已激活");
        refresh();
    } catch (e: any) {
        Message.error(e?.message || "操作失败");
    }
}

async function handleDelete(node: ZLMNode) {
    Modal.warning({
        title: "删除节点?",
        content: `节点 ${node.name} 将被从注册表删除。仅维护态可删,流数必须为 0。`,
        okText: "删除",
        cancelText: "取消",
        hideCancel: false,
        onOk: async () => {
            try {
                await deleteZLMNode(node.id);
                Message.success("已删除");
                refresh();
            } catch (e: any) {
                Message.error(e?.response?.data?.message || "删除失败,可能需要先切维护态");
            }
        }
    });
}

function gotoDetail(node: ZLMNode) {
    router.push(`/gb28181/zlm/nodes/${node.id}`);
}

onMounted(() => {
    refresh();
    refreshTimer = setInterval(refresh, 30_000);
});
onUnmounted(() => {
    if (refreshTimer) clearInterval(refreshTimer);
});
</script>

<template>
    <div class="zlm-node-list">
        <a-page-header title="流媒体节点" subtitle="ZLMediaKit 多节点集群管理" :show-back="false">
            <template #extra>
                <a-space>
                    <a-statistic title="节点数" :value="nodes.length" />
                    <a-statistic title="当前流数(总)" :value="totalStreams" />
                    <a-button type="primary" @click="openCreate">+ 添加节点</a-button>
                </a-space>
            </template>
        </a-page-header>

        <a-card style="margin: 16px">
            <a-table
                :data="nodes"
                :loading="loading"
                row-key="id"
                :pagination="false"
            >
                <template #columns>
                    <a-table-column title="名称" data-index="name" />
                    <a-table-column title="Host:Port">
                        <template #cell="{ record }">{{ record.host }}:{{ record.apiPort }}</template>
                    </a-table-column>
                    <a-table-column title="状态">
                        <template #cell="{ record }">
                            <NodeStateBadge :state="record.state" />
                        </template>
                    </a-table-column>
                    <a-table-column title="权重" data-index="weight" />
                    <a-table-column title="当前流">
                        <template #cell="{ record }">{{ record.stats?.mediaSourceCount || 0 }}</template>
                    </a-table-column>
                    <a-table-column title="心跳">
                        <template #cell="{ record }">
                            <span v-if="record.stats?.lastHeartbeatAt && record.stats.lastHeartbeatAt !== '0001-01-01T00:00:00Z'">
                                {{ new Date(record.stats.lastHeartbeatAt).toLocaleString() }}
                            </span>
                            <span v-else style="color: #aaa">—</span>
                        </template>
                    </a-table-column>
                    <a-table-column title="操作" :width="280">
                        <template #cell="{ record }">
                            <a-space>
                                <a-button size="small" @click="gotoDetail(record)">详情</a-button>
                                <a-button
                                    v-if="record.state === 'active'"
                                    size="small"
                                    status="warning"
                                    @click="handleMaintenance(record)"
                                >
                                    隔离
                                </a-button>
                                <a-button
                                    v-if="record.state === 'maintenance'"
                                    size="small"
                                    type="primary"
                                    @click="handleActivate(record)"
                                >
                                    激活
                                </a-button>
                                <a-button
                                    size="small"
                                    status="danger"
                                    @click="handleDelete(record)"
                                >
                                    删除
                                </a-button>
                            </a-space>
                        </template>
                    </a-table-column>
                </template>
            </a-table>
        </a-card>

        <NodeForm v-model:visible="drawerVisible" @created="refresh" />
    </div>
</template>

<style scoped>
.zlm-node-list {
    height: 100%;
    overflow: auto;
}
</style>
