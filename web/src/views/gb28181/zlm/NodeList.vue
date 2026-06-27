<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { useRouter } from "vue-router";
import {
    listZLMNodes,
    deleteZLMNode,
    setZLMNodeMaintenance,
    activateZLMNode,
    testZLMNodeConnection,
    kickZLMNodeSessions,
    restartZLMNode,
    type ZLMNode
} from "@/api/gb28181-zlm";
import NodeForm from "./NodeForm.vue";
import NodeStateBadge from "./components/NodeStateBadge.vue";

const router = useRouter();
const nodes = ref<ZLMNode[]>([]);
const loading = ref(false);
const drawerVisible = ref(false);
const reprobing = ref<Record<number, boolean>>({});

const ZERO_TIME = "0001-01-01T00:00:00Z";

let refreshTimer: ReturnType<typeof setInterval> | null = null;

const totalStreams = computed(() =>
    nodes.value.reduce((sum, n) => sum + (n.stats?.mediaSourceCount || 0), 0)
);

const offlineCount = computed(() =>
    nodes.value.filter((n) => n.state === "offline").length
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

function isZeroTime(s?: string): boolean {
    return !s || s === ZERO_TIME || s.startsWith("0001-01-01");
}

function pickReferenceTime(node: ZLMNode): string | null {
    const heartbeat = node.stats?.lastHeartbeatAt;
    if (heartbeat && !isZeroTime(heartbeat)) {
        // 用心跳时间 / 更新时间二者较新
        if (node.updatedAt && !isZeroTime(node.updatedAt)) {
            const t1 = new Date(heartbeat).getTime();
            const t2 = new Date(node.updatedAt).getTime();
            return t1 >= t2 ? heartbeat : node.updatedAt;
        }
        return heartbeat;
    }
    if (node.updatedAt && !isZeroTime(node.updatedAt)) {
        return node.updatedAt;
    }
    return null;
}

function offlineDuration(node: ZLMNode): string {
    const ref = pickReferenceTime(node);
    if (!ref) return "从未上报";
    const refTs = new Date(ref).getTime();
    if (Number.isNaN(refTs)) return "从未上报";
    const diffSec = Math.floor((Date.now() - refTs) / 1000);
    if (diffSec < 60) return "刚刚";
    if (diffSec < 3600) return `离线 ${Math.floor(diffSec / 60)} 分钟`;
    if (diffSec < 86400) return `离线 ${Math.floor(diffSec / 3600)} 小时`;
    return `离线 ${Math.floor(diffSec / 86400)} 天`;
}

function rowClass(record: ZLMNode): string {
    switch (record.state) {
        case "maintenance":
            return "row-maintenance";
        case "offline":
            return "row-offline";
        default:
            return "";
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

async function handleReprobe(node: ZLMNode) {
    reprobing.value[node.id] = true;
    try {
        const res = await testZLMNodeConnection(node.id);
        if (res.code === 0 && res.data?.online) {
            const act = await activateZLMNode(node.id);
            if (act.code === 0) {
                Message.success("节点已恢复并重新加入调度池");
            } else {
                Message.warning("探测可达,但激活失败,请手动激活");
            }
        } else {
            Message.error(`节点仍不可达: ${res.data?.error || "未知"}`);
        }
        refresh();
    } catch (e: any) {
        Message.error(e?.message || "探测失败");
    } finally {
        reprobing.value[node.id] = false;
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

// 驱逐节点全部会话(高危,Modal 二次确认),仅 active / maintenance 状态可用
async function handleKick(node: ZLMNode) {
    Modal.warning({
        title: "驱逐全部会话?",
        content: `将断开节点 ${node.name} 的所有连接,正在播放的客户端会立刻断流。请确认。`,
        okText: "驱逐",
        cancelText: "取消",
        hideCancel: false,
        onOk: async () => {
            try {
                const res = await kickZLMNodeSessions(node.id);
                if (res.code === 0) {
                    Message.success(`已驱逐 ${res.data?.count ?? 0} 路会话`);
                } else {
                    Message.error(res.message || "驱逐失败");
                }
                refresh();
            } catch (e: any) {
                Message.error(e?.message || "驱逐失败");
            }
        }
    });
}

// 重启 ZLM 服务(高危,所有流中断)
// ZLM 接到 restartServer 立刻重启,需要几秒才能恢复服务,延迟 5s 刷新让心跳重新上报
async function handleRestart(node: ZLMNode) {
    Modal.warning({
        title: "重启 ZLM 服务?",
        content: `将重启节点 ${node.name} 的 ZLM 进程,所有流将中断,客户端需自行重连。请确认。`,
        okText: "重启",
        cancelText: "取消",
        hideCancel: false,
        onOk: async () => {
            try {
                await restartZLMNode(node.id);
                Message.success("已发出重启指令,等待 ZLM 重启完成...");
                // ZLM 重启需要几秒,延迟 refresh 让心跳重新上报
                setTimeout(refresh, 5000);
            } catch (e: any) {
                Message.error(e?.message || "重启失败");
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
                    <a-statistic title="离线节点" :value="offlineCount" :value-style="{ color: offlineCount > 0 ? '#f53f3f' : undefined }" />
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
                :row-class-name="rowClass"
            >
                <template #columns>
                    <a-table-column title="名称" data-index="name" />
                    <a-table-column title="Host:Port">
                        <template #cell="{ record }">{{ record.host }}:{{ record.apiPort }}</template>
                    </a-table-column>
                    <a-table-column title="状态">
                        <template #cell="{ record }">
                            <div class="state-cell">
                                <NodeStateBadge :state="record.state" />
                                <div
                                    v-if="record.state === 'offline'"
                                    class="offline-duration"
                                >
                                    {{ offlineDuration(record) }}
                                </div>
                            </div>
                        </template>
                    </a-table-column>
                    <a-table-column title="权重">
                        <template #cell="{ record }">
                            <div class="weight-cell">
                                <span class="weight-num">{{ record.weight }}</span>
                                <a-progress
                                    :percent="record.weight / 100"
                                    :show-text="false"
                                    size="small"
                                    class="weight-bar"
                                />
                            </div>
                        </template>
                    </a-table-column>
                    <a-table-column title="当前流">
                        <template #cell="{ record }">
                            <span v-if="record.state === 'offline'" style="color: #aaa">—</span>
                            <span v-else>{{ record.stats?.mediaSourceCount || 0 }}</span>
                        </template>
                    </a-table-column>
                    <a-table-column title="心跳">
                        <template #cell="{ record }">
                            <span v-if="record.stats?.lastHeartbeatAt && !record.stats.lastHeartbeatAt.startsWith('0001-01-01')">
                                {{ new Date(record.stats.lastHeartbeatAt).toLocaleString() }}
                            </span>
                            <span v-else style="color: #aaa">—</span>
                        </template>
                    </a-table-column>
                    <a-table-column title="操作" :width="420">
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
                                    v-if="record.state === 'offline'"
                                    size="small"
                                    type="primary"
                                    :loading="reprobing[record.id]"
                                    @click="handleReprobe(record)"
                                >
                                    重新探测
                                </a-button>
                                <!-- 驱逐:active/maintenance 状态可用,offline 时 ZLM 不可达不显示 -->
                                <a-button
                                    v-if="record.state !== 'offline'"
                                    size="small"
                                    status="warning"
                                    class="btn-kick"
                                    @click="handleKick(record)"
                                >
                                    驱逐
                                </a-button>
                                <!-- 重启:同上,offline 时 ZLM 已挂不需要重启 -->
                                <a-button
                                    v-if="record.state !== 'offline'"
                                    size="small"
                                    status="danger"
                                    @click="handleRestart(record)"
                                >
                                    重启
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

.state-cell {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 2px;
}

.offline-duration {
    font-size: 12px;
    color: #86909c;
    line-height: 1.2;
}

.weight-cell {
    display: flex;
    align-items: center;
    gap: 8px;
}

.weight-num {
    min-width: 24px;
    text-align: right;
    font-variant-numeric: tabular-nums;
}

.weight-bar {
    width: 80px;
}

:deep(tr.row-maintenance > td),
:deep(.arco-table-tr.row-maintenance > .arco-table-td),
:deep(tr.row-maintenance .arco-table-td) {
    background-color: #fffbe6 !important;
}

:deep(tr.row-offline > td),
:deep(.arco-table-tr.row-offline > .arco-table-td),
:deep(tr.row-offline .arco-table-td) {
    background-color: #f7f8fa !important;
    color: #86909c;
}

/* 驱逐按钮:橙色,跟"隔离"(默认 warning 黄)区分开,语义上更接近"危险但非毁灭" */
.btn-kick :deep(.arco-btn) {
    background-color: #ff7d00;
    border-color: #ff7d00;
    color: #fff;
}
.btn-kick :deep(.arco-btn:hover) {
    background-color: #f77234;
    border-color: #f77234;
}
</style>
