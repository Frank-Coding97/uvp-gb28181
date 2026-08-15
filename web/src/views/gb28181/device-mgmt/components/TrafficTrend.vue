<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import VChart from "@visactor/vchart";
import { RefreshCw } from "lucide-vue-next";
import { getTrafficCoverage, getTrafficRealtime, getTrafficSummary, getTrafficTrend, type TrafficCoverage, type TrafficRealtime, type TrafficSummary, type TrafficTrendPoint } from "../trafficApi";
import { coverageLabel, formatBytes, latestRequestGuard } from "../trafficState";

const props = defineProps<{ deviceId: string; channelId?: string }>();
const loading = ref(false);
const error = ref("");
const summary = ref<TrafficSummary | null>(null);
const trend = ref<TrafficTrendPoint[]>([]);
const realtime = ref<TrafficRealtime[]>([]);
const coverage = ref<TrafficCoverage | null>(null);
const chartHost = ref<HTMLElement | null>(null);
const guard = latestRequestGuard();
let chart: VChart | null = null;
let resizeObserver: ResizeObserver | null = null;
let abortController: AbortController | null = null;

const realtimeSummary = computed(() => realtime.value.reduce((result, item) => ({
    upstream: result.upstream + item.inProgressUpstreamBytes,
    speed: result.speed + item.upstreamBytesPerSecond,
    viewers: result.viewers + item.readerCount,
    downstreamSpeed: result.downstreamSpeed + item.estimatedDownstreamBytesPerSec
}), { upstream: 0, speed: 0, viewers: 0, downstreamSpeed: 0 }));

function renderChart() {
    if (!chartHost.value) return;
    chart?.release();
    chart = null;
    const data = trend.value.flatMap(point => [
        { date: point.date, kind: "上行", bytes: point.upstreamBytes },
        { date: point.date, kind: "下行", bytes: point.downstreamBytes },
        { date: point.date, kind: "总流量", bytes: point.totalBytes }
    ]);
    chart = new VChart({
        type: "line",
        data: [{ id: "traffic", values: data }],
        xField: "date",
        yField: "bytes",
        seriesField: "kind",
        color: ["#0f766e", "#2563eb", "#d97706"],
        line: { style: { lineWidth: 2 } },
        area: { visible: true, style: { fillOpacity: 0.1 } },
        point: { visible: trend.value.length <= 14 },
        legends: { visible: true, orient: "top", position: "start" },
        axes: [{ orient: "left", label: { formatMethod: (value: number) => formatBytes(value) } }, { orient: "bottom" }],
        tooltip: { mark: { content: [{ key: (datum: { kind: string }) => datum.kind, value: (datum: { bytes: number }) => formatBytes(datum.bytes) }] } },
        padding: { left: 8, right: 12, top: 8, bottom: 4 }
    } as never, { dom: chartHost.value });
    chart.renderSync();
    resizeObserver?.disconnect();
    resizeObserver = new ResizeObserver(entries => {
        const bounds = entries[0]?.contentRect;
        if (bounds && bounds.width > 0 && bounds.height > 0) chart?.resize(bounds.width, bounds.height);
    });
    resizeObserver.observe(chartHost.value);
}

async function load() {
    abortController?.abort();
    abortController = new AbortController();
    const version = guard.next();
    loading.value = true;
    error.value = "";
    const params = { deviceId: props.deviceId, channelId: props.channelId || undefined };
    try {
        const [summaryResult, trendResult, realtimeResult, coverageResult] = await Promise.all([
            getTrafficSummary(params, abortController.signal),
            getTrafficTrend(params, abortController.signal),
            getTrafficRealtime(params, abortController.signal),
            getTrafficCoverage(params, abortController.signal)
        ]);
        if (!guard.current(version)) return;
        for (const result of [summaryResult, trendResult, realtimeResult, coverageResult]) {
            if (result.code !== 0) throw new Error(result.message || "流量统计加载失败");
        }
        summary.value = summaryResult.data;
        trend.value = trendResult.data?.list || [];
        realtime.value = realtimeResult.data?.list || [];
        coverage.value = coverageResult.data;
        await nextTick();
        renderChart();
    } catch (cause: unknown) {
        if (!guard.current(version) || abortController.signal.aborted) return;
        error.value = cause instanceof Error ? cause.message : "流量统计加载失败";
        summary.value = null;
        trend.value = [];
    } finally {
        if (guard.current(version)) loading.value = false;
    }
}

watch(() => [props.deviceId, props.channelId], load, { immediate: true });
onBeforeUnmount(() => {
    guard.cancel();
    abortController?.abort();
    resizeObserver?.disconnect();
    chart?.release();
});
</script>

<template>
    <section class="traffic-trend" aria-label="流量统计">
        <div v-if="error" class="traffic-state error" role="alert">
            <span>{{ error }}</span>
            <a-button size="small" @click="load"><template #icon><RefreshCw :size="14" /></template>重试</a-button>
        </div>
        <a-spin v-else :loading="loading" class="traffic-loading">
            <div v-if="summary" class="metric-strip">
                <div><span>已结算上行</span><strong>{{ formatBytes(summary.upstreamBytes) }}</strong></div>
                <div><span>已结算下行</span><strong>{{ formatBytes(summary.downstreamBytes) }}</strong></div>
                <div><span>已结算总量</span><strong>{{ formatBytes(summary.totalBytes) }}</strong></div>
                <div><span>进行中上行</span><strong>{{ formatBytes(realtimeSummary.upstream) }}</strong><small>实时</small></div>
                <div><span>当前观看</span><strong>{{ realtimeSummary.viewers }}</strong><small>人</small></div>
            </div>
            <div v-if="coverage" class="coverage-line" :class="coverage.coverage">
                <span>{{ coverageLabel(coverage.coverage) }}</span>
                <span v-if="coverage.statisticsStartedAt">统计自 {{ new Date(coverage.statisticsStartedAt).toLocaleString() }} 开始</span>
            </div>
            <div v-if="trend.length" ref="chartHost" class="traffic-chart" role="img" aria-label="按日上行、下行和总流量面积趋势图"></div>
            <a-empty v-else-if="!loading" :description="coverage?.coverage === 'not_started' ? '尚未开始统计' : '暂无流量历史'" />
            <div v-if="trend.length" class="trend-table-wrap" aria-label="流量趋势表格数据">
                <table>
                    <thead><tr><th>日期</th><th>上行</th><th>下行</th><th>总流量</th></tr></thead>
                    <tbody><tr v-for="point in trend" :key="point.date"><td>{{ point.date }}</td><td>{{ formatBytes(point.upstreamBytes) }}</td><td>{{ formatBytes(point.downstreamBytes) }}</td><td>{{ formatBytes(point.totalBytes) }}</td></tr></tbody>
                </table>
            </div>
            <div v-if="realtime.length" class="estimate-note">当前上行 {{ formatBytes(realtimeSummary.speed) }}/s · 估算下行 {{ formatBytes(realtimeSummary.downstreamSpeed) }}/s，估算值不计入已结算历史。</div>
        </a-spin>
    </section>
</template>

<style scoped>
.traffic-trend { min-width: 0; min-height: 420px; }
.traffic-loading { display: block; min-height: 420px; }
.metric-strip { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); border-block: 1px solid var(--color-border-2); }
.metric-strip > div { min-width: 0; padding: 12px; border-right: 1px solid var(--color-border-2); }
.metric-strip > div:last-child { border-right: 0; }
.metric-strip span, .metric-strip small { display: block; color: var(--color-text-3); font-size: 12px; }
.metric-strip strong { display: inline-block; margin-top: 5px; color: var(--color-text-1); font-size: 18px; letter-spacing: 0; }
.coverage-line { display: flex; gap: 12px; justify-content: space-between; padding: 8px 2px; color: var(--color-text-3); font-size: 12px; }
.coverage-line.partial { color: rgb(var(--warning-6)); }
.coverage-line.not_started { color: var(--color-text-2); }
.traffic-chart { width: 100%; height: 250px; min-height: 250px; }
.trend-table-wrap { max-height: 150px; overflow: auto; border-top: 1px solid var(--color-border-2); }
table { width: 100%; border-collapse: collapse; font-size: 12px; }
th, td { padding: 7px 10px; border-bottom: 1px solid var(--color-border-2); text-align: right; white-space: nowrap; }
th:first-child, td:first-child { text-align: left; }
.estimate-note { padding: 9px 2px; color: var(--color-text-3); font-size: 12px; }
.traffic-state { display: flex; min-height: 360px; align-items: center; justify-content: center; gap: 12px; }
.traffic-state.error { color: rgb(var(--danger-6)); }
@media (max-width: 768px) { .metric-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); } .metric-strip > div { border-bottom: 1px solid var(--color-border-2); } .coverage-line { flex-direction: column; gap: 3px; } }
@media (prefers-reduced-motion: reduce) { .traffic-trend *, .traffic-trend *::before, .traffic-trend *::after { animation-duration: 0.01ms !important; transition-duration: 0.01ms !important; } }
</style>
