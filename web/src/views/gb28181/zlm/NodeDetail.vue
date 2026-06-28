<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Message } from "@arco-design/web-vue";
import {
    getZLMNode,
    testZLMNodeConnection,
    activateZLMNode,
    setZLMNodeMaintenance,
    kickZLMNodeSessions,
    restartZLMNode,
    type ZLMNode
} from "@/api/gb28181-zlm";
import StatCard from "./components/StatCard.vue";
import LifecycleDot from "./components/LifecycleDot.vue";
import HealthBadge from "./components/HealthBadge.vue";
import Sparkline from "./components/Sparkline.vue";
import NodeConfig from "./NodeConfig.vue";

interface HistoryPoint {
    time: number;
    value: number;
}

const route = useRoute();
const router = useRouter();
const node = ref<ZLMNode | null>(null);
const loading = ref(false);
const activeTab = ref<"overview" | "config">("overview");
let refreshTimer: ReturnType<typeof setInterval> | null = null;

const HISTORY_MAX = 30;
const streamHistory = ref<HistoryPoint[]>([]);
const sessionHistory = ref<HistoryPoint[]>([]);
const cpuHistory = ref<HistoryPoint[]>([]);

const nodeId = Number(route.params.id);

function pushHistory(buf: HistoryPoint[], value: number): HistoryPoint[] {
    const next = [...buf, { time: Date.now(), value }];
    if (next.length > HISTORY_MAX) next.shift();
    return next;
}

async function refresh() {
    loading.value = true;
    try {
        const res = await getZLMNode(nodeId);
        if (res.code === 0) {
            node.value = res.data;
            const stats = res.data?.stats;
            if (stats) {
                streamHistory.value = pushHistory(streamHistory.value, stats.mediaSourceCount || 0);
                sessionHistory.value = pushHistory(sessionHistory.value, stats.sessionCount || 0);
                const cpu = (stats.netThreadLoadAvg || 0) * 0.6 + (stats.workThreadLoadAvg || 0) * 0.4;
                cpuHistory.value = pushHistory(cpuHistory.value, cpu);
            }
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

// ====== 操作 ======

const opLoading = ref<Record<string, boolean>>({});

async function withOp(op: string, fn: () => Promise<void>) {
    opLoading.value[op] = true;
    try {
        await fn();
    } finally {
        opLoading.value[op] = false;
    }
}

async function handleReprobe() {
    if (!node.value) return;
    await withOp("reprobe", async () => {
        try {
            const res = await testZLMNodeConnection(node.value!.id);
            if (res.code === 0 && res.data?.online) {
                if (node.value!.state === "offline") {
                    const act = await activateZLMNode(node.value!.id);
                    if (act.code === 0) Message.success("节点已恢复并重新加入调度池");
                    else Message.warning("探测可达,但激活失败,请手动激活");
                } else {
                    Message.success(`节点在线,http.port=${res.data.httpPort || "?"}`);
                }
                await refresh();
            } else {
                Message.error(`节点仍不可达: ${res.data?.error || "未知"}`);
            }
        } catch (e: any) {
            Message.error(e?.message || "探测失败");
        }
    });
}

async function handleMaintenance() {
    if (!node.value) return;
    await withOp("maintenance", async () => {
        try {
            await setZLMNodeMaintenance(node.value!.id);
            Message.success("已切到维护态");
            refresh();
        } catch (e: any) {
            Message.error(e?.message || "操作失败");
        }
    });
}

async function handleActivate() {
    if (!node.value) return;
    await withOp("activate", async () => {
        try {
            await activateZLMNode(node.value!.id);
            Message.success("已激活");
            refresh();
        } catch (e: any) {
            Message.error(e?.message || "操作失败");
        }
    });
}

async function handleKick() {
    if (!node.value) return;
    await withOp("kick", async () => {
        try {
            const res = await kickZLMNodeSessions(node.value!.id);
            if (res.code === 0) {
                Message.success(`已驱逐 ${res.data?.count ?? 0} 路会话`);
                refresh();
            } else {
                Message.error(res.message || "驱逐失败");
            }
        } catch (e: any) {
            Message.error(e?.message || "驱逐失败");
        }
    });
}

async function handleRestart() {
    if (!node.value) return;
    await withOp("restart", async () => {
        try {
            const res = await restartZLMNode(node.value!.id, 5000);
            if (res.code === 0) {
                Message.success("已发送重启指令,5 秒后刷新");
                setTimeout(() => refresh(), 5000);
            } else {
                Message.error(res.message || "重启失败");
            }
        } catch (e: any) {
            Message.error(e?.message || "重启失败");
        }
    });
}

// ====== 派生 ======

function healthOf(n: ZLMNode | null) {
    if (!n) return "unknown";
    if (n.state === "offline") return "critical";
    if (n.state === "maintenance") return "unknown";
    return n.nearCapacity ? "warning" : "healthy";
}

function healthReason(n: ZLMNode | null) {
    if (!n) return "";
    if (n.state === "offline") return "节点离线";
    if (n.state === "active" && n.nearCapacity) return "接近容量";
    return "";
}

const cpuPct = computed(() => {
    const s = node.value?.stats;
    if (!s) return 0;
    return Math.round((s.netThreadLoadAvg * 0.6 + s.workThreadLoadAvg * 0.4) * 100);
});

const rtpUsage = computed(() => {
    const n = node.value;
    if (!n) return { used: 0, total: 0, pct: 0 };
    const total = (n.rtpPortEnd || 0) - (n.rtpPortStart || 0);
    const used = n.stats?.mediaSourceCount || 0;
    const pct = total > 0 ? Math.round((used / total) * 100) : 0;
    return { used, total, pct };
});

function fmtTime(s: string | undefined | null): string {
    if (!s) return "—";
    if (s.startsWith("0001-01-01")) return "—";
    const d = new Date(s);
    if (isNaN(d.getTime())) return s;
    const pad = (n: number) => String(n).padStart(2, "0");
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}
</script>

<template>
    <div class="node-detail">
        <!-- 页面头 -->
        <header class="page-header">
            <div class="title-row">
                <button class="back-btn" @click="router.push('/gb28181/zlm/nodes')">
                    <icon-left />
                </button>
                <div class="title-block">
                    <div class="title-row-inline">
                        <h1 class="title">{{ node?.name || "节点详情" }}</h1>
                        <LifecycleDot v-if="node" :state="node.state" />
                        <HealthBadge v-if="node" :health="healthOf(node)" :reason="healthReason(node)" />
                    </div>
                    <p class="subtitle">
                        <span class="mono">{{ node?.host }}:{{ node?.apiPort }}</span>
                        <span class="dim"> · UUID {{ node?.mediaServerUUID }}</span>
                    </p>
                </div>
            </div>
            <div class="actions">
                <a-button
                    v-if="node?.state === 'offline'"
                    type="primary"
                    :loading="opLoading.reprobe"
                    @click="handleReprobe"
                >
                    重新探测
                </a-button>
                <a-button
                    v-if="node?.state === 'active'"
                    :loading="opLoading.maintenance"
                    @click="handleMaintenance"
                >
                    隔离
                </a-button>
                <a-button
                    v-if="node?.state === 'maintenance'"
                    type="primary"
                    :loading="opLoading.activate"
                    @click="handleActivate"
                >
                    激活
                </a-button>
                <a-dropdown
                    v-if="node && node.state !== 'offline'"
                    trigger="click"
                    position="br"
                >
                    <a-button>
                        更多
                        <template #icon><icon-down /></template>
                    </a-button>
                    <template #content>
                        <a-doption :loading="opLoading.kick" @click="handleKick">驱逐全部会话</a-doption>
                        <a-doption :loading="opLoading.restart" @click="handleRestart">重启 ZLM 服务</a-doption>
                    </template>
                </a-dropdown>
                <span class="refresh-hint">每 30s 自动刷新</span>
            </div>
        </header>

        <a-tabs v-model:active-key="activeTab" class="detail-tabs">
            <a-tab-pane key="overview" title="概览">
                <a-spin :loading="loading">
                    <!-- KPI 第一行(4 张主指标) -->
                    <div class="kpi-grid kpi-grid-4">
                        <StatCard
                            title="活跃流"
                            :value="node?.stats?.mediaSourceCount || 0"
                            trend="MediaSource"
                            accent="brand"
                        >
                            <template #spark>
                                <Sparkline
                                    :data="streamHistory"
                                    color="brand"
                                    :width="80"
                                    :height="24"
                                    fill
                                />
                            </template>
                        </StatCard>
                        <StatCard
                            title="会话数"
                            :value="node?.stats?.sessionCount || 0"
                            trend="TCP + UDP"
                            accent="accent"
                        >
                            <template #spark>
                                <Sparkline
                                    :data="sessionHistory"
                                    color="accent"
                                    :width="80"
                                    :height="24"
                                    fill
                                />
                            </template>
                        </StatCard>
                        <StatCard
                            title="网络线程负载"
                            :value="(node?.stats?.netThreadLoadAvg || 0)"
                            :is-percent="true"
                            unit="%"
                            trend="event poller 平均"
                        />
                        <StatCard
                            title="工作线程负载"
                            :value="(node?.stats?.workThreadLoadAvg || 0)"
                            :is-percent="true"
                            unit="%"
                            trend="work poller 平均"
                        />
                    </div>

                    <!-- KPI 第二行(3 张配额/容量) -->
                    <div class="kpi-grid kpi-grid-3">
                        <StatCard
                            title="CPU 综合负载"
                            :value="cpuPct"
                            unit="%"
                            trend="NetThread×0.6 + WorkThread×0.4"
                            :accent="cpuPct >= 80 ? 'danger' : cpuPct >= 60 ? 'warning' : 'default'"
                        />
                        <StatCard
                            title="RTP 端口使用"
                            :value-text="`${rtpUsage.used} / ${rtpUsage.total}`"
                            :trend="`${rtpUsage.pct}% · ${node?.rtpPortStart}-${node?.rtpPortEnd}`"
                        />
                        <StatCard
                            title="权重"
                            :value="node?.weight || 0"
                            trend="加权轮询用"
                        />
                    </div>

                    <!-- 趋势图 2 大卡 -->
                    <div class="trend-row">
                        <div class="trend-card">
                            <div class="trend-header">
                                <span class="trend-title">流数趋势</span>
                                <span class="trend-meta">最近 15 分钟 · 样本 {{ streamHistory.length }}/{{ HISTORY_MAX }}</span>
                            </div>
                            <div class="trend-canvas">
                                <Sparkline
                                    v-if="streamHistory.length >= 1"
                                    :data="streamHistory"
                                    color="brand"
                                    :width="600"
                                    :height="120"
                                    fill
                                />
                                <div v-else class="trend-empty">采样中,等待 30s 后第一个数据点...</div>
                            </div>
                        </div>
                        <div class="trend-card">
                            <div class="trend-header">
                                <span class="trend-title">CPU 负载趋势</span>
                                <span class="trend-meta">最近 15 分钟 · NetThread+WorkThread 加权</span>
                            </div>
                            <div class="trend-canvas">
                                <Sparkline
                                    v-if="cpuHistory.length >= 1"
                                    :data="cpuHistory"
                                    color="warning"
                                    :width="600"
                                    :height="120"
                                    fill
                                />
                                <div v-else class="trend-empty">采样中...</div>
                            </div>
                        </div>
                    </div>

                    <!-- 节点信息 -->
                    <div class="info-block">
                        <h2 class="info-title">节点信息</h2>
                        <div class="info-grid">
                            <div class="info-item">
                                <div class="info-label">ID</div>
                                <div class="info-value">{{ node?.id }}</div>
                            </div>
                            <div class="info-item">
                                <div class="info-label">UUID</div>
                                <div class="info-value mono">{{ node?.mediaServerUUID }}</div>
                            </div>
                            <div class="info-item">
                                <div class="info-label">Host</div>
                                <div class="info-value mono">{{ node?.host }}:{{ node?.apiPort }}</div>
                            </div>
                            <div class="info-item">
                                <div class="info-label">RTP 端口范围</div>
                                <div class="info-value">{{ node?.rtpPortStart }} - {{ node?.rtpPortEnd }}</div>
                            </div>
                            <div class="info-item">
                                <div class="info-label">创建时间</div>
                                <div class="info-value">{{ fmtTime(node?.createdAt) }}</div>
                            </div>
                            <div class="info-item">
                                <div class="info-label">更新时间</div>
                                <div class="info-value">{{ fmtTime(node?.updatedAt) }}</div>
                            </div>
                        </div>
                    </div>
                </a-spin>
            </a-tab-pane>
            <a-tab-pane key="config" title="配置">
                <NodeConfig v-if="nodeId" :node-id="nodeId" />
            </a-tab-pane>
        </a-tabs>
    </div>
</template>

<style scoped>
.node-detail {
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
    justify-content: space-between;
    align-items: flex-start;
    gap: var(--zlm-space-4);
    margin-bottom: var(--zlm-space-6);
}

.title-row {
    display: flex;
    align-items: flex-start;
    gap: var(--zlm-space-3);
    flex: 1;
    min-width: 0;
}

.back-btn {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 36px;
    height: 36px;
    border-radius: var(--zlm-radius-md);
    border: 1px solid var(--zlm-border);
    background: var(--zlm-card);
    color: var(--zlm-text-2);
    cursor: pointer;
    transition: all var(--zlm-dur-fast) var(--zlm-ease-out);
    font-size: 16px;
    margin-top: 2px;
}

.back-btn:hover {
    border-color: var(--zlm-brand-500);
    color: var(--zlm-brand-500);
}

.title-block {
    flex: 1;
    min-width: 0;
}

.title-row-inline {
    display: flex;
    align-items: center;
    gap: var(--zlm-space-3);
    flex-wrap: wrap;
}

.title {
    font-size: var(--zlm-fs-h1);
    font-weight: var(--zlm-fw-semibold);
    color: var(--zlm-text-1);
    margin: 0;
    line-height: 1.2;
}

.subtitle {
    margin: 6px 0 0;
    font-size: var(--zlm-fs-caption);
    color: var(--zlm-text-3);
    word-break: break-all;
}

.subtitle .mono {
    font-family: var(--zlm-font-mono);
    color: var(--zlm-text-2);
}

.subtitle .dim {
    color: var(--zlm-text-4);
}

.actions {
    display: flex;
    align-items: center;
    gap: var(--zlm-space-2);
    flex-shrink: 0;
}

.refresh-hint {
    color: var(--zlm-text-3);
    font-size: var(--zlm-fs-caption);
    margin-left: var(--zlm-space-2);
}

/* === Tabs === */
.detail-tabs :deep(.arco-tabs-nav) {
    background: transparent;
    border-bottom: 1px solid var(--zlm-border);
    padding: 0;
    margin-bottom: var(--zlm-space-6);
}

.detail-tabs :deep(.arco-tabs-tab) {
    font-size: var(--zlm-fs-body);
    font-weight: var(--zlm-fw-medium);
    color: var(--zlm-text-3);
}

.detail-tabs :deep(.arco-tabs-tab-active) {
    color: var(--zlm-brand-600);
}

.detail-tabs :deep(.arco-tabs-tab-active .arco-tabs-tab-title:after) {
    background: var(--zlm-brand-500);
}

/* === KPI 网格 === */
.kpi-grid {
    display: grid;
    gap: var(--zlm-space-4);
    margin-bottom: var(--zlm-space-4);
}

.kpi-grid-4 {
    grid-template-columns: repeat(4, 1fr);
}

.kpi-grid-3 {
    grid-template-columns: repeat(3, 1fr);
}

/* === 趋势图 === */
.trend-row {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--zlm-space-4);
    margin: var(--zlm-space-4) 0;
}

.trend-card {
    background: var(--zlm-card);
    border-radius: var(--zlm-radius-lg);
    border: 1px solid var(--zlm-border);
    padding: var(--zlm-space-4);
    box-shadow: var(--zlm-shadow-sm);
}

.trend-header {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    margin-bottom: var(--zlm-space-3);
}

.trend-title {
    font-size: var(--zlm-fs-body);
    font-weight: var(--zlm-fw-semibold);
    color: var(--zlm-text-1);
}

.trend-meta {
    font-size: var(--zlm-fs-caption);
    color: var(--zlm-text-3);
}

.trend-canvas {
    min-height: 120px;
    display: flex;
    align-items: center;
    justify-content: center;
}

.trend-canvas > svg {
    width: 100%;
    height: 120px;
}

.trend-empty {
    color: var(--zlm-text-4);
    font-size: var(--zlm-fs-caption);
}

/* === 节点信息 === */
.info-block {
    background: var(--zlm-card);
    border-radius: var(--zlm-radius-lg);
    border: 1px solid var(--zlm-border);
    padding: var(--zlm-space-4) var(--zlm-space-6);
    margin-top: var(--zlm-space-4);
}

.info-title {
    font-size: var(--zlm-fs-h2);
    font-weight: var(--zlm-fw-semibold);
    color: var(--zlm-text-1);
    margin: 0 0 var(--zlm-space-4);
}

.info-grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: var(--zlm-space-3) var(--zlm-space-6);
}

.info-item {
    display: flex;
    gap: var(--zlm-space-3);
    padding: var(--zlm-space-2) 0;
    border-bottom: 1px solid var(--zlm-divider);
}

.info-label {
    flex-shrink: 0;
    width: 120px;
    color: var(--zlm-text-3);
    font-size: var(--zlm-fs-body);
}

.info-value {
    color: var(--zlm-text-1);
    font-size: var(--zlm-fs-body);
    word-break: break-all;
    flex: 1;
    min-width: 0;
}

.info-value.mono {
    font-family: var(--zlm-font-mono);
    font-size: 13px;
}
</style>
