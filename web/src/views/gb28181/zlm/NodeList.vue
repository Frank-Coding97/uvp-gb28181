<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from "vue";
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
import StatCard from "./components/StatCard.vue";
import LifecycleDot from "./components/LifecycleDot.vue";
import HealthBadge from "./components/HealthBadge.vue";

const router = useRouter();
const nodes = ref<ZLMNode[]>([]);
const loading = ref(false);
const drawerVisible = ref(false);
const refreshTimer = ref<ReturnType<typeof setInterval> | null>(null);

// 搜索 + 过滤
const search = ref("");
const filterStates = ref<string[]>([]);
const filterHealth = ref<string[]>([]);

// 操作 loading(按节点 id 隔离,多节点并发不互相影响)
const opLoading = ref<Record<number, string | null>>({});

// 批量选中
const selectedIds = ref<number[]>([]);

// ====== 健康度推导(基于现有 nearCapacity / state 字段) ======

function healthOf(n: ZLMNode): "healthy" | "warning" | "critical" | "unknown" {
    if (n.state === "offline") return "critical";
    if (n.state === "maintenance") return "unknown";
    // active
    return n.nearCapacity ? "warning" : "healthy";
}

function healthReason(n: ZLMNode): string {
    if (n.state === "offline") return "节点离线";
    if (n.state === "active" && n.nearCapacity) return "接近容量";
    return "";
}

// ====== KPI 顶部数据 ======

const totalNodes = computed(() => nodes.value.length);
const onlineCount = computed(() => nodes.value.filter((n) => n.state === "active").length);
const offlineCount = computed(() => nodes.value.filter((n) => n.state === "offline").length);
const maintenanceCount = computed(() => nodes.value.filter((n) => n.state === "maintenance").length);
const totalStreams = computed(() =>
    nodes.value.reduce((sum, n) => sum + (n.stats?.mediaSourceCount || 0), 0)
);
const totalSessions = computed(() =>
    nodes.value.reduce((sum, n) => sum + (n.stats?.sessionCount || 0), 0)
);
const healthRatio = computed(() => {
    if (totalNodes.value === 0) return 0;
    const healthy = nodes.value.filter((n) => healthOf(n) === "healthy").length;
    return Math.round((healthy / totalNodes.value) * 100);
});

// 过滤后的列表
const filteredNodes = computed(() => {
    const q = search.value.trim().toLowerCase();
    return nodes.value.filter((n) => {
        if (q && !n.name.toLowerCase().includes(q) && !n.host.toLowerCase().includes(q)) return false;
        if (filterStates.value.length && !filterStates.value.includes(n.state)) return false;
        if (filterHealth.value.length && !filterHealth.value.includes(healthOf(n))) return false;
        return true;
    });
});

// 选中节点(给底部 toolbar 用)
const selectedNodes = computed(() => nodes.value.filter((n) => selectedIds.value.includes(n.id)));

// ====== 工具函数 ======

const ZERO_TIME = "0001-01-01T00:00:00Z";

function isZeroTime(s?: string): boolean {
    return !s || s === ZERO_TIME || s.startsWith("0001-01-01");
}

function relTime(s?: string): string {
    if (!s || isZeroTime(s)) return "—";
    const ts = new Date(s).getTime();
    if (Number.isNaN(ts)) return "—";
    const diff = Math.floor((Date.now() - ts) / 1000);
    if (diff < 5) return "刚刚";
    if (diff < 60) return `${diff} 秒前`;
    if (diff < 3600) return `${Math.floor(diff / 60)} 分钟前`;
    if (diff < 86400) return `${Math.floor(diff / 3600)} 小时前`;
    return `${Math.floor(diff / 86400)} 天前`;
}

function cpuPctOf(n: ZLMNode): number {
    const net = n.stats?.netThreadLoadAvg || 0;
    const work = n.stats?.workThreadLoadAvg || 0;
    return Math.round((net * 0.6 + work * 0.4) * 100);
}

// ====== 加载 ======

async function refresh() {
    loading.value = true;
    try {
        const res = await listZLMNodes();
        if (res.code === 0) nodes.value = res.data.list || [];
    } catch (e: any) {
        Message.error(e?.message || "加载失败");
    } finally {
        loading.value = false;
    }
}

onMounted(() => {
    refresh();
    refreshTimer.value = setInterval(refresh, 30_000);
});
onUnmounted(() => {
    if (refreshTimer.value) clearInterval(refreshTimer.value);
});

// ====== 操作 ======

function gotoDetail(node: ZLMNode) {
    router.push(`/gb28181/zlm/nodes/${node.id}`);
}

function openCreate() {
    drawerVisible.value = true;
}

async function withOp<T>(node: ZLMNode, op: string, fn: () => Promise<T>) {
    opLoading.value[node.id] = op;
    try {
        await fn();
    } finally {
        opLoading.value[node.id] = null;
    }
}

async function handleMaintenance(node: ZLMNode) {
    Modal.warning({
        title: "切到维护态?",
        content: `节点 ${node.name} 将不再接受新流,旧流自然结束。`,
        okText: "确认",
        cancelText: "取消",
        hideCancel: false,
        onOk: async () => {
            await withOp(node, "maintenance", async () => {
                try {
                    await setZLMNodeMaintenance(node.id);
                    Message.success("已切到维护态");
                    refresh();
                } catch (e: any) {
                    Message.error(e?.message || "操作失败");
                }
            });
        }
    });
}

async function handleActivate(node: ZLMNode) {
    await withOp(node, "activate", async () => {
        try {
            await activateZLMNode(node.id);
            Message.success("已激活");
            refresh();
        } catch (e: any) {
            Message.error(e?.message || "操作失败");
        }
    });
}

async function handleReprobe(node: ZLMNode) {
    await withOp(node, "reprobe", async () => {
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
        }
    });
}

async function handleKick(node: ZLMNode) {
    Modal.warning({
        title: "驱逐全部会话?",
        content: `将断开节点 ${node.name} 的所有连接,正在播放的客户端会立刻断流。`,
        okText: "驱逐",
        cancelText: "取消",
        hideCancel: false,
        onOk: async () => {
            await withOp(node, "kick", async () => {
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
            });
        }
    });
}

async function handleRestart(node: ZLMNode) {
    Modal.warning({
        title: "重启 ZLM 服务?",
        content: `将重启节点 ${node.name} 的 ZLM 进程,所有流将中断,客户端需自行重连。`,
        okText: "重启",
        cancelText: "取消",
        hideCancel: false,
        onOk: async () => {
            await withOp(node, "restart", async () => {
                try {
                    const res = await restartZLMNode(node.id, 5000);
                    if (res.code === 0) {
                        Message.success("已发送重启指令,5 秒后刷新");
                    } else {
                        Message.error(res.message || "重启失败");
                    }
                    setTimeout(() => refresh(), 5000);
                } catch (e: any) {
                    Message.error(e?.message || "重启失败");
                }
            });
        }
    });
}

async function handleDelete(node: ZLMNode) {
    Modal.warning({
        title: "删除节点?",
        content: `节点 ${node.name} 将被从注册表删除。仅维护态可删,流数必须为 0。`,
        okText: "删除",
        cancelText: "取消",
        hideCancel: false,
        onOk: async () => {
            await withOp(node, "delete", async () => {
                try {
                    await deleteZLMNode(node.id);
                    Message.success("已删除");
                    refresh();
                } catch (e: any) {
                    Message.error(e?.response?.data?.message || "删除失败,可能需要先切维护态");
                }
            });
        }
    });
}

// 批量操作
async function handleBatchMaintenance() {
    Modal.warning({
        title: `批量切维护态?`,
        content: `将把 ${selectedNodes.value.length} 个节点切到维护态。`,
        onOk: async () => {
            await Promise.all(selectedNodes.value.map((n) => setZLMNodeMaintenance(n.id).catch(() => null)));
            Message.success(`已操作 ${selectedNodes.value.length} 个节点`);
            selectedIds.value = [];
            refresh();
        }
    });
}

async function handleBatchActivate() {
    await Promise.all(selectedNodes.value.map((n) => activateZLMNode(n.id).catch(() => null)));
    Message.success(`已激活 ${selectedNodes.value.length} 个节点`);
    selectedIds.value = [];
    refresh();
}
</script>

<template>
    <div class="zlm-node-list">
        <!-- 页面头 -->
        <header class="page-header">
            <div class="title-block">
                <div class="breadcrumb">
                    <span class="crumb">流媒体集群</span>
                    <span class="sep">/</span>
                    <span class="crumb-current">节点</span>
                </div>
                <h1 class="title">流媒体节点</h1>
                <p class="subtitle">ZLMediaKit 多节点集群管理 · 实时心跳 · 自动调度</p>
            </div>
            <div class="actions">
                <a-button @click="refresh" :loading="loading">
                    <template #icon><icon-refresh /></template>
                    刷新
                </a-button>
                <a-button type="primary" @click="openCreate">
                    <template #icon><icon-plus /></template>
                    添加节点
                </a-button>
            </div>
        </header>

        <!-- 顶部 KPI 条(4 卡阶梯式) -->
        <section class="kpi-row">
            <StatCard
                title="节点总数"
                :value="totalNodes"
                :trend="offlineCount > 0 ? `${offlineCount} 离线 · ${maintenanceCount} 维护` : `全部在线`"
                :trend-type="offlineCount > 0 ? 'danger' : 'up'"
                accent="brand"
            />
            <StatCard
                title="活跃流"
                :value="totalStreams"
                :trend="`${onlineCount} 个节点上`"
                accent="accent"
            />
            <StatCard
                title="会话数"
                :value="totalSessions"
                trend="TCP + UDP 累计"
            />
            <StatCard
                title="健康度"
                :value="healthRatio"
                unit="%"
                :trend="healthRatio === 100 ? '集群健康' : '存在告警'"
                :trend-type="healthRatio === 100 ? 'up' : 'down'"
                :accent="healthRatio === 100 ? 'accent' : 'warning'"
            />
        </section>

        <!-- 过滤栏 -->
        <section class="filter-bar">
            <a-input-search
                v-model="search"
                placeholder="搜索节点名或 Host"
                allow-clear
                class="search"
            />
            <a-select
                v-model="filterStates"
                multiple
                placeholder="状态"
                allow-clear
                class="filter-select"
                :options="[
                    { label: '活跃', value: 'active' },
                    { label: '维护', value: 'maintenance' },
                    { label: '离线', value: 'offline' }
                ]"
            />
            <a-select
                v-model="filterHealth"
                multiple
                placeholder="健康度"
                allow-clear
                class="filter-select"
                :options="[
                    { label: 'Healthy', value: 'healthy' },
                    { label: 'Warning', value: 'warning' },
                    { label: 'Critical', value: 'critical' },
                    { label: 'Unknown', value: 'unknown' }
                ]"
            />
            <span class="filter-meta">{{ filteredNodes.length }} / {{ nodes.length }} 节点</span>
        </section>

        <!-- 节点稀疏表 -->
        <section class="node-table-wrap">
            <a-table
                :data="filteredNodes"
                :loading="loading"
                row-key="id"
                :pagination="false"
                :row-selection="{
                    type: 'checkbox',
                    showCheckedAll: true
                }"
                v-model:selected-keys="selectedIds"
                class="node-table"
            >
                <template #columns>
                    <a-table-column title="节点" :width="220">
                        <template #cell="{ record }">
                            <div class="cell-node" @click="gotoDetail(record)">
                                <div class="cell-node-name">{{ record.name }}</div>
                                <div class="cell-node-host">{{ record.host }}:{{ record.apiPort }}</div>
                            </div>
                        </template>
                    </a-table-column>
                    <a-table-column title="状态" :width="100">
                        <template #cell="{ record }">
                            <LifecycleDot :state="record.state" />
                        </template>
                    </a-table-column>
                    <a-table-column title="健康度" :width="160">
                        <template #cell="{ record }">
                            <HealthBadge :health="healthOf(record)" :reason="healthReason(record)" />
                        </template>
                    </a-table-column>
                    <a-table-column title="权重" :width="160">
                        <template #cell="{ record }">
                            <div class="cell-weight">
                                <span class="weight-num zlm-numeric">{{ record.weight }}</span>
                                <div class="weight-bar">
                                    <div class="weight-bar-fill" :style="{ width: `${record.weight}%` }" />
                                </div>
                            </div>
                        </template>
                    </a-table-column>
                    <a-table-column title="流 / 会话" :width="120">
                        <template #cell="{ record }">
                            <div class="cell-numeric zlm-numeric">
                                <span v-if="record.state === 'offline'" class="muted">—</span>
                                <span v-else>
                                    <strong>{{ record.stats?.mediaSourceCount || 0 }}</strong>
                                    <span class="sep"> / </span>
                                    <span class="dim">{{ record.stats?.sessionCount || 0 }}</span>
                                </span>
                            </div>
                        </template>
                    </a-table-column>
                    <a-table-column title="CPU" :width="100">
                        <template #cell="{ record }">
                            <span
                                v-if="record.state === 'offline'"
                                class="muted"
                            >—</span>
                            <span
                                v-else
                                class="cpu-pct zlm-numeric"
                                :class="{
                                    'cpu-high': cpuPctOf(record) >= 80,
                                    'cpu-mid': cpuPctOf(record) >= 60 && cpuPctOf(record) < 80
                                }"
                            >{{ cpuPctOf(record) }}%</span>
                        </template>
                    </a-table-column>
                    <a-table-column title="心跳" :width="120">
                        <template #cell="{ record }">
                            <a-tooltip
                                v-if="!isZeroTime(record.stats?.lastHeartbeatAt)"
                                :content="record.stats?.lastHeartbeatAt"
                            >
                                <span class="rel-time">{{ relTime(record.stats?.lastHeartbeatAt) }}</span>
                            </a-tooltip>
                            <span v-else class="muted">从未上报</span>
                        </template>
                    </a-table-column>
                    <a-table-column title="操作" :width="280">
                        <template #cell="{ record }">
                            <div class="cell-ops">
                                <a-button size="small" @click="gotoDetail(record)">详情</a-button>
                                <a-button
                                    v-if="record.state === 'active'"
                                    size="small"
                                    :loading="opLoading[record.id] === 'maintenance'"
                                    @click="handleMaintenance(record)"
                                >
                                    隔离
                                </a-button>
                                <a-button
                                    v-if="record.state === 'maintenance'"
                                    size="small"
                                    type="primary"
                                    :loading="opLoading[record.id] === 'activate'"
                                    @click="handleActivate(record)"
                                >
                                    激活
                                </a-button>
                                <a-button
                                    v-if="record.state === 'offline'"
                                    size="small"
                                    type="primary"
                                    :loading="opLoading[record.id] === 'reprobe'"
                                    @click="handleReprobe(record)"
                                >
                                    重新探测
                                </a-button>
                                <a-dropdown trigger="click" position="br">
                                    <a-button size="small">
                                        更多
                                        <template #icon><icon-down /></template>
                                    </a-button>
                                    <template #content>
                                        <a-doption
                                            v-if="record.state !== 'offline'"
                                            @click="handleKick(record)"
                                        >驱逐全部会话</a-doption>
                                        <a-doption
                                            v-if="record.state !== 'offline'"
                                            @click="handleRestart(record)"
                                        >重启 ZLM</a-doption>
                                        <a-doption
                                            class="danger"
                                            @click="handleDelete(record)"
                                        >删除节点</a-doption>
                                    </template>
                                </a-dropdown>
                            </div>
                        </template>
                    </a-table-column>
                </template>

                <template #empty>
                    <div class="empty">
                        <icon-cloud class="empty-icon" />
                        <div class="empty-title">还没有 ZLM 节点</div>
                        <div class="empty-sub">点右上角"添加节点"接入第一个 ZLMediaKit 实例</div>
                    </div>
                </template>
            </a-table>
        </section>

        <!-- 批量操作 toolbar(选中节点时浮起底部) -->
        <transition name="slide-up">
            <div v-if="selectedIds.length > 0" class="batch-bar">
                <div class="batch-info">
                    <span class="count">{{ selectedIds.length }}</span> 个节点已选中
                </div>
                <div class="batch-ops">
                    <a-button @click="handleBatchMaintenance">批量切维护</a-button>
                    <a-button type="primary" @click="handleBatchActivate">批量激活</a-button>
                    <a-button @click="selectedIds = []">取消</a-button>
                </div>
            </div>
        </transition>

        <NodeForm v-model:visible="drawerVisible" @created="refresh" />
    </div>
</template>

<style scoped>
@import "@/styles/zlm-tokens.css";

.zlm-node-list {
    height: 100%;
    overflow: auto;
    background: var(--zlm-bg);
    padding: var(--zlm-space-6);
    font-family: var(--zlm-font-body);
    color: var(--zlm-text-2);
}

/* === 页面头 === */
.page-header {
    display: flex;
    align-items: flex-end;
    justify-content: space-between;
    gap: var(--zlm-space-4);
    margin-bottom: var(--zlm-space-6);
}

.title-block {
    flex: 1;
    min-width: 0;
}

.breadcrumb {
    font-size: var(--zlm-fs-caption);
    color: var(--zlm-text-3);
    margin-bottom: var(--zlm-space-2);
}

.breadcrumb .sep {
    margin: 0 var(--zlm-space-2);
    color: var(--zlm-text-4);
}

.breadcrumb .crumb-current {
    color: var(--zlm-text-2);
    font-weight: var(--zlm-fw-medium);
}

.title {
    font-size: var(--zlm-fs-h1);
    font-weight: var(--zlm-fw-semibold);
    color: var(--zlm-text-1);
    margin: 0;
    line-height: 1.2;
}

.subtitle {
    font-size: var(--zlm-fs-body);
    color: var(--zlm-text-3);
    margin: var(--zlm-space-1) 0 0;
}

.actions {
    display: flex;
    gap: var(--zlm-space-2);
}

/* === KPI 条 === */
.kpi-row {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: var(--zlm-space-4);
    margin-bottom: var(--zlm-space-6);
}

/* === 过滤栏 === */
.filter-bar {
    display: flex;
    align-items: center;
    gap: var(--zlm-space-3);
    margin-bottom: var(--zlm-space-4);
    padding: var(--zlm-space-3) var(--zlm-space-4);
    background: var(--zlm-card);
    border-radius: var(--zlm-radius-lg);
    border: 1px solid var(--zlm-border);
}

.search {
    width: 280px;
    flex-shrink: 0;
}

.filter-select {
    width: 160px;
}

.filter-meta {
    margin-left: auto;
    font-size: var(--zlm-fs-caption);
    color: var(--zlm-text-3);
}

/* === 节点表 === */
.node-table-wrap {
    background: var(--zlm-card);
    border-radius: var(--zlm-radius-lg);
    border: 1px solid var(--zlm-border);
    overflow: hidden;
}

.node-table :deep(.arco-table-th) {
    background: var(--zlm-bg);
    font-weight: var(--zlm-fw-medium);
    font-size: var(--zlm-fs-caption);
    color: var(--zlm-text-3);
    text-transform: none;
    letter-spacing: 0.02em;
}

.node-table :deep(.arco-table-td) {
    padding: 10px 16px !important;
    height: 56px;
    font-size: var(--zlm-fs-body);
    color: var(--zlm-text-2);
}

.node-table :deep(.arco-table-tr:hover .arco-table-td) {
    background: var(--zlm-card-hover);
}

/* 节点名 cell */
.cell-node {
    cursor: pointer;
    line-height: 1.3;
}

.cell-node-name {
    font-size: var(--zlm-fs-body);
    font-weight: var(--zlm-fw-semibold);
    color: var(--zlm-text-1);
    transition: color var(--zlm-dur-fast) var(--zlm-ease-out);
}

.cell-node:hover .cell-node-name {
    color: var(--zlm-brand-600);
}

.cell-node-host {
    font-size: var(--zlm-fs-caption);
    color: var(--zlm-text-3);
    font-family: var(--zlm-font-mono);
    margin-top: 2px;
}

/* 权重 cell */
.cell-weight {
    display: flex;
    align-items: center;
    gap: var(--zlm-space-2);
}

.weight-num {
    font-weight: var(--zlm-fw-semibold);
    color: var(--zlm-text-1);
    min-width: 28px;
}

.weight-bar {
    flex: 1;
    height: 6px;
    background: var(--zlm-divider);
    border-radius: var(--zlm-radius-full);
    overflow: hidden;
}

.weight-bar-fill {
    height: 100%;
    background: var(--zlm-brand-500);
    border-radius: var(--zlm-radius-full);
    transition: width var(--zlm-dur-slow) var(--zlm-ease-out);
}

/* 数字 cell */
.cell-numeric strong {
    color: var(--zlm-text-1);
    font-weight: var(--zlm-fw-semibold);
}
.cell-numeric .sep {
    color: var(--zlm-text-4);
}
.cell-numeric .dim {
    color: var(--zlm-text-3);
}

/* CPU pct */
.cpu-pct {
    font-weight: var(--zlm-fw-medium);
    color: var(--zlm-text-2);
}
.cpu-pct.cpu-mid {
    color: var(--zlm-warn-600);
}
.cpu-pct.cpu-high {
    color: var(--zlm-danger-600);
    font-weight: var(--zlm-fw-semibold);
}

.muted {
    color: var(--zlm-text-4);
}

.rel-time {
    color: var(--zlm-text-3);
    font-size: var(--zlm-fs-body);
}

/* 操作 cell */
.cell-ops {
    display: flex;
    gap: var(--zlm-space-2);
    align-items: center;
}

/* row offline / maintenance 行染色 */
.node-table :deep(.arco-table-tr) {
    transition: background var(--zlm-dur-fast) var(--zlm-ease-out);
}

/* 空状态 */
.empty {
    padding: var(--zlm-space-12) var(--zlm-space-6);
    text-align: center;
    color: var(--zlm-text-3);
}

.empty-icon {
    font-size: 48px;
    color: var(--zlm-text-4);
    margin-bottom: var(--zlm-space-3);
}

.empty-title {
    font-size: var(--zlm-fs-h2);
    font-weight: var(--zlm-fw-semibold);
    color: var(--zlm-text-2);
    margin-bottom: var(--zlm-space-2);
}

.empty-sub {
    font-size: var(--zlm-fs-body);
    color: var(--zlm-text-3);
}

/* === 批量 toolbar(底部浮起) === */
.batch-bar {
    position: fixed;
    bottom: var(--zlm-space-6);
    left: 50%;
    transform: translateX(-50%);
    z-index: 50;
    display: flex;
    align-items: center;
    gap: var(--zlm-space-4);
    background: var(--zlm-text-1);
    color: var(--zlm-text-inverse);
    padding: var(--zlm-space-3) var(--zlm-space-4);
    border-radius: var(--zlm-radius-full);
    box-shadow: var(--zlm-shadow-lg);
}

.batch-info {
    color: var(--zlm-text-inverse);
    font-size: var(--zlm-fs-body);
}

.batch-info .count {
    font-weight: var(--zlm-fw-bold);
    color: var(--zlm-brand-500);
    margin-right: 4px;
}

.batch-ops {
    display: flex;
    gap: var(--zlm-space-2);
}

/* 进出动画 */
.slide-up-enter-active,
.slide-up-leave-active {
    transition: all var(--zlm-dur-base) var(--zlm-ease-out);
}
.slide-up-enter-from,
.slide-up-leave-to {
    transform: translate(-50%, 16px);
    opacity: 0;
}

/* 危险下拉项 */
:deep(.danger) {
    color: var(--zlm-danger-600);
}
</style>
