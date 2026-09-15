<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import VChart from "@visactor/vchart";
import { ArrowDownToLine, ArrowUpFromLine, CircleHelp, Database, RefreshCw } from "@lucide/vue";
import {
    getTrafficCoverage,
    getTrafficRealtime,
    getTrafficSessions,
    getTrafficSummary,
    getTrafficTrend,
    type TrafficCoverage,
    type TrafficRealtime,
    type TrafficSession,
    type TrafficSummary,
    type TrafficTrendPoint
} from "../trafficApi";
import { coverageLabel, formatBytes, latestRequestGuard } from "../trafficState";

type TrafficRangeMode = "7d" | "24h";

const props = defineProps<{ deviceId: string; channelId?: string }>();
const loading = ref(false);
const error = ref("");
const rangeMode = ref<TrafficRangeMode>("7d");
const summary = ref<TrafficSummary | null>(null);
const trend = ref<TrafficTrendPoint[]>([]);
const realtime = ref<TrafficRealtime[]>([]);
const coverage = ref<TrafficCoverage | null>(null);
const chartHost = ref<HTMLElement | null>(null);
const guard = latestRequestGuard();
let chart: VChart | null = null;
let resizeObserver: ResizeObserver | null = null;
let abortController: AbortController | null = null;

const detailVisible = ref(false);
const detailLoading = ref(false);
const detailError = ref("");
const detailList = ref<TrafficSession[]>([]);
const detailTotal = ref(0);
const detailPage = ref(1);
const detailPageSize = ref(10);
const selectedBucket = ref<TrafficTrendPoint | null>(null);
let detailAbortController: AbortController | null = null;
let detailRequestVersion = 0;

function defaultTrafficRange() {
    const to = new Date();
    const from = new Date(to);
    from.setUTCDate(from.getUTCDate() - 6);
    return { from: from.toISOString().slice(0, 10), to: to.toISOString().slice(0, 10), granularity: "day" as const };
}

function trafficParams() {
    const scope = { deviceId: props.deviceId, channelId: props.channelId || undefined };
    return rangeMode.value === "24h"
        ? { ...scope, granularity: "hour" as const }
        : { ...scope, ...defaultTrafficRange() };
}

const realtimeSummary = computed(() => realtime.value.reduce((result, item) => ({
    speed: result.speed + item.upstreamBytesPerSecond,
    downstreamSpeed: result.downstreamSpeed + item.estimatedDownstreamBytesPerSec
}), { speed: 0, downstreamSpeed: 0 }));

const trendTitle = computed(() => rangeMode.value === "24h" ? "24小时流量趋势" : "7天流量趋势");
const chartAriaLabel = computed(() => `${trendTitle.value}，上行和下行堆叠柱、总流量折线；点击柱形查看明细`);
const detailTitle = computed(() => selectedBucket.value ? `流量明细 · ${formatBucket(selectedBucket.value)}` : "流量明细");

function formatBucket(point: TrafficTrendPoint) {
    const value = point.bucket || point.date;
    if (rangeMode.value === "7d") return value.slice(5);
    const time = new Date(value);
    if (Number.isNaN(time.getTime())) return value;
    return new Intl.DateTimeFormat("zh-CN", {
        month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false,
        timeZone: "Asia/Shanghai"
    }).format(time).replace("/", "-");
}

function formatDateTime(value?: string | null) {
    if (!value) return "—";
    const time = new Date(value);
    return Number.isNaN(time.getTime()) ? "—" : time.toLocaleString("zh-CN", { hour12: false });
}

function formatDuration(seconds: number) {
    if (!seconds) return "—";
    if (seconds < 60) return `${seconds} 秒`;
    const minutes = Math.floor(seconds / 60);
    return minutes < 60 ? `${minutes} 分钟` : `${Math.floor(minutes / 60)} 小时 ${minutes % 60} 分`;
}

function addWithOriginalOffset(value: string, milliseconds: number) {
    const offset = value.match(/([+-])(\d{2}):(\d{2})$/);
    if (!offset) return new Date(new Date(value).getTime() + milliseconds).toISOString();
    const offsetMinutes = (offset[1] === "-" ? -1 : 1) * (Number(offset[2]) * 60 + Number(offset[3]));
    const shifted = new Date(new Date(value).getTime() + milliseconds + offsetMinutes * 60000);
    return `${shifted.toISOString().slice(0, 19)}${offset[0]}`;
}

function bucketRange(point: TrafficTrendPoint) {
    const value = point.bucket || point.date;
    if (rangeMode.value === "24h") return { from: value, to: addWithOriginalOffset(value, 60 * 60 * 1000) };
    const from = `${value.slice(0, 10)}T00:00:00+08:00`;
    return { from, to: addWithOriginalOffset(from, 24 * 60 * 60 * 1000) };
}

async function loadDetails() {
    if (!selectedBucket.value) return;
    detailAbortController?.abort();
    detailAbortController = new AbortController();
    const version = ++detailRequestVersion;
    detailLoading.value = true;
    detailError.value = "";
    try {
        const result = await getTrafficSessions({
            deviceId: props.deviceId,
            channelId: props.channelId || undefined,
            ...bucketRange(selectedBucket.value),
            page: detailPage.value,
            pageSize: detailPageSize.value
        }, detailAbortController.signal);
        if (version !== detailRequestVersion) return;
        if (result.code !== 0) throw new Error(result.message || "流量明细加载失败");
        detailList.value = result.data?.list || [];
        detailTotal.value = result.data?.total || 0;
    } catch (cause: unknown) {
        if (version !== detailRequestVersion || detailAbortController.signal.aborted) return;
        detailError.value = cause instanceof Error ? cause.message : "流量明细加载失败";
        detailList.value = [];
        detailTotal.value = 0;
    } finally {
        if (version === detailRequestVersion) detailLoading.value = false;
    }
}

function openDetails(point: TrafficTrendPoint) {
    selectedBucket.value = point;
    detailPage.value = 1;
    detailVisible.value = true;
    void loadDetails();
}

function onDetailPageChange(page: number) {
    detailPage.value = page;
    void loadDetails();
}

function renderChart() {
    if (!chartHost.value) return;
    chart?.release();
    chart = null;
    const trafficData = trend.value.flatMap(point => [
        { bucket: point.bucket || point.date, label: formatBucket(point), kind: "上行", bytes: point.upstreamBytes },
        { bucket: point.bucket || point.date, label: formatBucket(point), kind: "下行", bytes: point.downstreamBytes }
    ]);
    const totalData = trend.value.map(point => ({ bucket: point.bucket || point.date, label: formatBucket(point), kind: "总流量", bytes: point.totalBytes }));
    const reducedMotion = typeof window !== "undefined" && window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
    chart = new VChart({
        type: "common",
        background: "transparent",
        data: [
            { id: "traffic-bars", values: trafficData },
            { id: "traffic-total", values: totalData }
        ],
        color: ["#0f766e", "#2563eb", "#d97706"],
        series: [
            {
                type: "bar",
                data: { id: "traffic-bars" },
                xField: "label",
                yField: "bytes",
                seriesField: "kind",
                stack: true,
                barMaxWidth: rangeMode.value === "24h" ? 22 : 38,
                stackCornerRadius: 4,
                bar: { style: { fillOpacity: 0.84, cursor: "pointer" }, state: { hover: { fillOpacity: 1 } } }
            },
            {
                type: "line",
                data: { id: "traffic-total" },
                xField: "label",
                yField: "bytes",
                seriesField: "kind",
                line: { style: { lineWidth: 2.2 } },
                point: { visible: rangeMode.value === "7d", style: { size: 6, lineWidth: 2, stroke: "#fff" } }
            }
        ],
        legends: { visible: true, orient: "top", position: "start" },
        axes: [
            { orient: "left", label: { formatMethod: (value: number) => formatBytes(value) } },
            { orient: "bottom", label: { autoRotate: false, autoHide: true } }
        ],
        tooltip: {
            activeType: "dimension",
            dimension: { content: [{ key: (datum: { kind: string }) => datum.kind, value: (datum: { bytes: number }) => formatBytes(datum.bytes) }] }
        },
        animation: !reducedMotion,
        padding: { left: 8, right: 12, top: 8, bottom: 4 }
    } as never, { dom: chartHost.value });
    chart.on("click", { level: "mark", type: "bar" }, params => {
        const bucket = String(params.datum?.bucket || "");
        const point = trend.value.find(item => (item.bucket || item.date) === bucket);
        if (point) openDetails(point);
    });
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
    const params = trafficParams();
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

watch(() => [props.deviceId, props.channelId, rangeMode.value], () => {
    detailVisible.value = false;
    void load();
}, { immediate: true });

onBeforeUnmount(() => {
    guard.cancel();
    abortController?.abort();
    detailAbortController?.abort();
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
                <div class="metric-card metric-card--up">
                    <span class="metric-icon"><ArrowUpFromLine :size="16" /></span>
                    <div><span>已结算上行<a-tooltip content="设备或摄像机发送到平台流媒体服务器的数据量" position="top"><button class="metric-help" type="button" aria-label="说明：已结算上行"><CircleHelp :size="12" /></button></a-tooltip></span><strong>{{ formatBytes(summary.upstreamBytes) }}</strong></div>
                </div>
                <div class="metric-card metric-card--down">
                    <span class="metric-icon"><ArrowDownToLine :size="16" /></span>
                    <div><span>已结算下行<a-tooltip content="平台流媒体服务器发送给浏览器、客户端等观看端的数据量" position="top"><button class="metric-help" type="button" aria-label="说明：已结算下行"><CircleHelp :size="12" /></button></a-tooltip></span><strong>{{ formatBytes(summary.downstreamBytes) }}</strong></div>
                </div>
                <div class="metric-card metric-card--total">
                    <span class="metric-icon"><Database :size="16" /></span>
                    <div><span>已结算总量<a-tooltip content="上行流量与下行流量之和" position="top"><button class="metric-help" type="button" aria-label="说明：已结算总量"><CircleHelp :size="12" /></button></a-tooltip></span><strong>{{ formatBytes(summary.totalBytes) }}</strong></div>
                </div>
            </div>
            <div v-if="coverage" class="coverage-line" :class="coverage.coverage">
                <span>{{ coverageLabel(coverage.coverage) }}</span>
                <span v-if="coverage.statisticsStartedAt">统计自 {{ new Date(coverage.statisticsStartedAt).toLocaleString() }} 开始</span>
            </div>
            <div class="trend-chart-head">
                <div><strong>{{ trendTitle }}</strong><span>点击柱形查看该时段详细台账</span></div>
                <div class="range-tabs" role="tablist" aria-label="流量统计时间范围">
                    <button type="button" role="tab" data-range="7d" :aria-selected="rangeMode === '7d'" :class="{ active: rangeMode === '7d' }" @click="rangeMode = '7d'">7天</button>
                    <button type="button" role="tab" data-range="24h" :aria-selected="rangeMode === '24h'" :class="{ active: rangeMode === '24h' }" @click="rangeMode = '24h'">24小时</button>
                </div>
            </div>
            <div v-if="trend.length" ref="chartHost" class="traffic-chart" role="img" :aria-label="chartAriaLabel"></div>
            <div v-if="trend.length" class="sr-only-buckets" aria-label="流量趋势明细入口">
                <button v-for="point in trend" :key="point.bucket || point.date" type="button" @click="openDetails(point)">查看 {{ formatBucket(point) }} 流量明细</button>
            </div>
            <a-empty v-else-if="!loading" :description="coverage?.coverage === 'not_started' ? '尚未开始统计' : '暂无流量历史'" />
            <div v-if="realtime.length" class="estimate-note">当前上行 {{ formatBytes(realtimeSummary.speed) }}/s · 估算下行 {{ formatBytes(realtimeSummary.downstreamSpeed) }}/s，估算值不计入已结算历史。</div>
        </a-spin>

        <a-modal v-model:visible="detailVisible" modal-class="uvp-system-dialog traffic-detail-dialog" :title="detailTitle" :width="960" :footer="false" unmount-on-close>
            <div v-if="detailError" class="detail-error" role="alert"><span>{{ detailError }}</span><a-button size="small" @click="loadDetails">重试</a-button></div>
            <a-table v-else :data="detailList" :loading="detailLoading" :pagination="false" row-key="id" :scroll="{ x: 900 }">
                <template #columns>
                    <a-table-column title="通道" data-index="channelCode" :width="190" />
                    <a-table-column title="方向" :width="82"><template #cell="{ record }"><span class="direction-tag" :class="record.direction">{{ record.direction === 'upstream' ? '上行' : '下行' }}</span></template></a-table-column>
                    <a-table-column title="媒体类型" data-index="mediaKind" :width="90" />
                    <a-table-column title="已结算流量" :width="110" align="right"><template #cell="{ record }">{{ formatBytes(record.settledTotalBytes) }}</template></a-table-column>
                    <a-table-column title="时长" :width="105"><template #cell="{ record }">{{ formatDuration(record.durationSeconds) }}</template></a-table-column>
                    <a-table-column title="开始时间" :width="180"><template #cell="{ record }">{{ formatDateTime(record.startedAt || record.createdAt) }}</template></a-table-column>
                    <a-table-column title="结束时间" :width="180"><template #cell="{ record }">{{ formatDateTime(record.endedAt) }}</template></a-table-column>
                </template>
                <template #empty><a-empty description="该时段暂无流量会话" /></template>
            </a-table>
            <footer v-if="detailTotal > 0" class="detail-pagination"><span>共 {{ detailTotal }} 条</span><a-pagination :current="detailPage" :page-size="detailPageSize" :total="detailTotal" show-jumper @change="onDetailPageChange" /></footer>
        </a-modal>
    </section>
</template>

<style scoped>
.traffic-trend { min-width: 0; min-height: 420px; }
.traffic-loading { display: block; min-height: 420px; }
.metric-strip { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; padding: 10px 0; }
.metric-card { display: grid; grid-template-columns: 32px minmax(0, 1fr); align-items: center; gap: 9px; min-width: 0; min-height: 68px; padding: 10px; border: 1px solid var(--color-border-2); border-radius: 9px; background: var(--color-bg-2); }
.metric-icon { display: grid; place-items: center; width: 32px; height: 32px; color: rgb(var(--primary-6)); border-radius: 8px; background: rgb(var(--primary-1)); }
.metric-card--up .metric-icon { color: rgb(var(--success-6)); background: rgb(var(--success-1)); }
.metric-card--total .metric-icon { color: rgb(var(--warning-6)); background: rgb(var(--warning-1)); }
.metric-card > div > span, .metric-card small { display: block; color: var(--color-text-3); font-size: 11px; }
.metric-help { display: inline-grid; place-items: center; width: 18px; height: 18px; padding: 0; margin-left: 3px; color: var(--color-text-3); vertical-align: -5px; cursor: help; background: transparent; border: 0; border-radius: 50%; transition: color 180ms ease, background-color 180ms ease; }
.metric-help:hover { color: rgb(var(--primary-6)); background: var(--color-fill-2); }
.metric-help:focus-visible { color: rgb(var(--primary-6)); outline: 2px solid rgb(var(--primary-6)); outline-offset: 1px; }
.metric-card strong { display: inline-block; margin-top: 3px; overflow: hidden; color: var(--color-text-1); font-size: 17px; font-weight: 650; text-overflow: ellipsis; white-space: nowrap; }
.metric-card small { display: inline; margin-left: 4px; }
.coverage-line { display: flex; gap: 12px; justify-content: space-between; padding: 2px 2px 8px; color: var(--color-text-3); font-size: 12px; }
.coverage-line.partial { color: rgb(var(--warning-6)); }
.coverage-line.not_started { color: var(--color-text-2); }
.trend-chart-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 6px 2px 2px; }
.trend-chart-head > div:first-child { display: grid; gap: 2px; }
.trend-chart-head strong { color: var(--color-text-1); font-size: 13px; }
.trend-chart-head span { color: var(--color-text-3); font-size: 11px; }
.range-tabs { display: inline-flex; padding: 3px; border: 1px solid var(--color-border-2); border-radius: 8px; background: var(--color-fill-2); }
.range-tabs button { min-width: 62px; min-height: 30px; padding: 0 12px; color: var(--color-text-2); font: inherit; font-size: 12px; font-weight: 600; cursor: pointer; background: transparent; border: 0; border-radius: 6px; transition: color 180ms ease, background-color 180ms ease, box-shadow 180ms ease; }
.range-tabs button:hover { color: rgb(var(--primary-6)); background: var(--color-fill-1); }
.range-tabs button:focus-visible { outline: 2px solid rgb(var(--primary-6)); outline-offset: 2px; }
.range-tabs button.active { color: rgb(var(--primary-6)); background: var(--color-bg-2); box-shadow: 0 1px 3px rgb(0 0 0 / 10%); }
.traffic-chart { width: 100%; height: 286px; min-height: 286px; cursor: pointer; }
.sr-only-buckets { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border: 0; }
.estimate-note { padding: 8px 2px; color: var(--color-text-3); font-size: 12px; }
.traffic-state { display: flex; min-height: 360px; align-items: center; justify-content: center; gap: 12px; }
.traffic-state.error, .detail-error { color: rgb(var(--danger-6)); }
.detail-error { display: flex; min-height: 220px; align-items: center; justify-content: center; gap: 10px; }
.direction-tag { display: inline-flex; padding: 2px 8px; color: rgb(var(--primary-6)); border-radius: 999px; background: rgb(var(--primary-1)); }
.direction-tag.upstream { color: rgb(var(--success-6)); background: rgb(var(--success-1)); }
.detail-pagination { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding-top: 14px; color: var(--color-text-3); font-size: 12px; }
@media (max-width: 900px) { .metric-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); } .metric-card:last-child { grid-column: span 2; } }
@media (max-width: 768px) { .coverage-line, .trend-chart-head, .detail-pagination { align-items: flex-start; flex-direction: column; gap: 6px; } .range-tabs { width: 100%; } .range-tabs button { flex: 1; } }
@media (max-width: 480px) { .metric-strip { grid-template-columns: 1fr; } .metric-card:last-child { grid-column: auto; } }
@media (prefers-reduced-motion: reduce) { .traffic-trend *, .traffic-trend *::before, .traffic-trend *::after { animation-duration: 0.01ms !important; transition-duration: 0.01ms !important; } }
</style>
