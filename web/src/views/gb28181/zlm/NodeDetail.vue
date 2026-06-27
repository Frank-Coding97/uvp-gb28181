<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Message } from "@arco-design/web-vue";
import {
    getZLMNode,
    testZLMNodeConnection,
    activateZLMNode,
    type ZLMNode
} from "@/api/gb28181-zlm";
import NodeStateBadge from "./components/NodeStateBadge.vue";
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

// 历史采样:每 30s 推一条,最多保留 30 条(15 分钟窗口)
const HISTORY_MAX = 30;
const streamHistory = ref<HistoryPoint[]>([]);
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
                // CPU 负载 = 网络 + 工作线程负载加权(各占 50%)
                const cpu = ((stats.netThreadLoadAvg || 0) + (stats.workThreadLoadAvg || 0)) / 2;
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

const reprobing = ref(false);

// 节点离线时:手工探测一次,若恢复 → 自动 activate 拉回调度池
async function reprobe() {
    if (!node.value) return;
    reprobing.value = true;
    try {
        const res = await testZLMNodeConnection(node.value.id);
        if (res.code === 0 && res.data?.online) {
            // 节点已可达,尝试激活(若仍 offline 状态)
            if (node.value.state === "offline") {
                const act = await activateZLMNode(node.value.id);
                if (act.code === 0) {
                    Message.success("节点已恢复并重新加入调度池");
                } else {
                    Message.warning("探测可达,但激活失败,请手动激活");
                }
            } else {
                Message.success(`节点在线,http.port=${res.data.httpPort || "?"}`);
            }
            await refresh();
        } else {
            Message.error(`节点仍不可达: ${res.data?.error || "未知"}`);
        }
    } catch (e: any) {
        Message.error(e?.message || "探测失败");
    } finally {
        reprobing.value = false;
    }
}

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

function fmtTime(s: string | undefined | null): string {
    if (!s) return "—";
    // zero value 兜底
    if (s.startsWith("0001-01-01")) return "—";
    const d = new Date(s);
    if (isNaN(d.getTime())) return s;
    const pad = (n: number) => String(n).padStart(2, "0");
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

// SVG sparkline: viewBox 100x30
const SVG_W = 100;
const SVG_H = 30;

function sparklinePoints(history: HistoryPoint[], explicitMax?: number): string {
    if (history.length === 0) return "";
    const values = history.map((p) => p.value);
    const max = explicitMax !== undefined ? explicitMax : Math.max(...values, 1);
    const safeMax = max === 0 ? 1 : max;
    const step = history.length > 1 ? SVG_W / (history.length - 1) : 0;
    return history
        .map((p, i) => {
            const x = i * step;
            const y = SVG_H - (p.value / safeMax) * SVG_H;
            return `${x.toFixed(2)},${y.toFixed(2)}`;
        })
        .join(" ");
}

const streamSparklinePoints = computed(() => sparklinePoints(streamHistory.value));
// CPU 是 0-1 浮点,固定 max=1
const cpuSparklinePoints = computed(() => sparklinePoints(cpuHistory.value, 1));

const streamMax = computed(() =>
    streamHistory.value.length === 0 ? 0 : Math.max(...streamHistory.value.map((p) => p.value))
);
const cpuMaxPct = computed(() => {
    if (cpuHistory.value.length === 0) return "0";
    const m = Math.max(...cpuHistory.value.map((p) => p.value));
    return (m * 100).toFixed(1);
});
</script>

<template>
    <div class="zlm-node-detail">
        <a-page-header
            :title="node?.name || '节点详情'"
            :subtitle="node ? `${node.host}:${node.apiPort}` : ''"
            @back="router.push('/gb28181/zlm/nodes')"
        >
            <template #extra>
                <a-space>
                    <a-button
                        v-if="node?.state === 'offline'"
                        :loading="reprobing"
                        type="primary"
                        @click="reprobe"
                    >
                        重新探测
                    </a-button>
                    <span class="refresh-hint">每 30 秒自动刷新</span>
                    <NodeStateBadge v-if="node" :state="node.state" />
                </a-space>
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
                                <div class="stat-card">
                                    <div class="stat-title">网络线程负载</div>
                                    <div class="stat-value">{{ ((node?.stats?.netThreadLoadAvg || 0) * 100).toFixed(1) }}<span class="stat-suffix">%</span></div>
                                </div>
                            </a-card>
                        </a-col>
                        <a-col :span="6">
                            <a-card>
                                <div class="stat-card">
                                    <div class="stat-title">工作线程负载</div>
                                    <div class="stat-value">{{ ((node?.stats?.workThreadLoadAvg || 0) * 100).toFixed(1) }}<span class="stat-suffix">%</span></div>
                                </div>
                            </a-card>
                        </a-col>
                    </a-row>
                    <a-row :gutter="16" style="margin-top: 16px">
                        <a-col :span="8">
                            <a-card>
                                <div class="stat-card">
                                    <div class="stat-title">内存占用</div>
                                    <div class="stat-value">{{ fmtBytes(node?.stats?.memoryUsageBytes || 0) }}</div>
                                </div>
                            </a-card>
                        </a-col>
                        <a-col :span="8">
                            <a-card>
                                <div class="stat-card">
                                    <div class="stat-title">累计入流量</div>
                                    <div class="stat-value">{{ fmtBytes(node?.stats?.totalBytesIn || 0) }}</div>
                                </div>
                            </a-card>
                        </a-col>
                        <a-col :span="8">
                            <a-card>
                                <div class="stat-card">
                                    <div class="stat-title">累计出流量</div>
                                    <div class="stat-value">{{ fmtBytes(node?.stats?.totalBytesOut || 0) }}</div>
                                </div>
                            </a-card>
                        </a-col>
                    </a-row>

                    <a-row :gutter="16" style="margin-top: 16px">
                        <a-col :span="12">
                            <a-card>
                                <div class="spark-header">
                                    <span class="spark-title">流数趋势(最近 15 分钟)</span>
                                    <span class="spark-meta">峰值 {{ streamMax }} · 样本 {{ streamHistory.length }}/{{ HISTORY_MAX }}</span>
                                </div>
                                <svg
                                    v-if="streamHistory.length > 1"
                                    class="sparkline"
                                    :viewBox="`0 0 ${SVG_W} ${SVG_H}`"
                                    preserveAspectRatio="none"
                                >
                                    <polyline
                                        class="spark-stream"
                                        fill="none"
                                        :points="streamSparklinePoints"
                                    />
                                </svg>
                                <div v-else class="spark-empty">采样中...</div>
                            </a-card>
                        </a-col>
                        <a-col :span="12">
                            <a-card>
                                <div class="spark-header">
                                    <span class="spark-title">CPU 负载趋势(最近 15 分钟)</span>
                                    <span class="spark-meta">峰值 {{ cpuMaxPct }}% · 样本 {{ cpuHistory.length }}/{{ HISTORY_MAX }}</span>
                                </div>
                                <svg
                                    v-if="cpuHistory.length > 1"
                                    class="sparkline"
                                    :viewBox="`0 0 ${SVG_W} ${SVG_H}`"
                                    preserveAspectRatio="none"
                                >
                                    <polyline
                                        class="spark-cpu"
                                        fill="none"
                                        :points="cpuSparklinePoints"
                                    />
                                </svg>
                                <div v-else class="spark-empty">采样中...</div>
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
                        <a-descriptions-item label="创建时间">{{ fmtTime(node?.createdAt) }}</a-descriptions-item>
                        <a-descriptions-item label="更新时间">{{ fmtTime(node?.updatedAt) }}</a-descriptions-item>
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

.refresh-hint {
    color: #86909c;
    font-size: 12px;
}

.stat-card {
    display: flex;
    flex-direction: column;
    gap: 8px;
}

.stat-title {
    color: #86909c;
    font-size: 13px;
}

.stat-value {
    font-size: 26px;
    font-weight: 500;
    color: #1d2129;
    line-height: 1.2;
}

.stat-suffix {
    font-size: 14px;
    color: #86909c;
    margin-left: 4px;
}

.spark-header {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    margin-bottom: 8px;
}

.spark-title {
    font-size: 14px;
    color: #1d2129;
    font-weight: 500;
}

.spark-meta {
    font-size: 12px;
    color: #86909c;
}

.sparkline {
    width: 100%;
    height: 60px;
    display: block;
}

.spark-stream {
    stroke: #165dff;
    stroke-width: 1.2;
    vector-effect: non-scaling-stroke;
}

.spark-cpu {
    stroke: #f7ba1e;
    stroke-width: 1.2;
    vector-effect: non-scaling-stroke;
}

.spark-empty {
    height: 60px;
    display: flex;
    align-items: center;
    justify-content: center;
    color: #c9cdd4;
    font-size: 12px;
}
</style>
