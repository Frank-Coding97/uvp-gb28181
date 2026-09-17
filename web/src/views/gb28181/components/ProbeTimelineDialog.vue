<script setup lang="ts">
/**
 * ProbeTimelineDialog - 逐帧健康检测「帧到达时间线」详情弹窗
 *
 * 为什么要有这个弹窗:侧栏那一栏只有约 250px 可用宽,而一次 60 秒采样能到四千多帧。
 * 侧栏只放按时间分桶的概览,逐帧散点放在这里。
 *
 * 两个不能省的取舍:
 * 1. 横轴用真实到达时间 relativeTimeMs,不是帧序号 —— 这样「某段时间一帧都没到」
 *    直接表现为空白区,而等距柱状图会把帧间隔信息抹平。
 * 2. 视频与音频分成两条轨道、各带独立纵轴 —— 视频 I 帧可达几十 KB、音频帧只有
 *    几百字节,共用一根纵轴会把音频压成一条贴底直线。
 */
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import VChart from "@visactor/vchart";
import { RotateCcw } from "@lucide/vue";
import type { ProbeSnapshot } from "@/api/gb28181";
import { buildProbeDiagnosis } from "../probeDiagnosis";
import { buildProbeOverview, probeBucketHeight } from "../probeOverview";

const props = defineProps<{
    visible: boolean;
    snapshot: ProbeSnapshot | null;
}>();

const emit = defineEmits<{ (event: "update:visible", value: boolean): void }>();

// VChart 渲染到 canvas,读不到 CSS 变量,颜色只能写死。
const VIDEO_COLOR = "#378ADD";
const KEYFRAME_COLOR = "#BA7517";
const AUDIO_COLOR = "#1D9E75";

type ProbePoint = { time: number; size: number };
type AxisRange = { min: number; max: number };

const frames = computed(() => props.snapshot?.timeline ?? []);
const durationMs = computed(() => Math.max(0, props.snapshot?.summary?.sampleDurationMs ?? 0));
const overview = computed(() => buildProbeOverview(props.snapshot));
// 诊断结论跟着快照走,与曲线同源。后端 health 里已经带了 status / issues / 阈值,
// 这里只做摊平与判读(见 probeDiagnosis.ts),不在组件里重算任何指标。
const diagnosis = computed(() => buildProbeDiagnosis(props.snapshot));

function toKb(bytes: number) {
    return Math.round((bytes / 1024) * 100) / 100;
}

const videoPoints = computed<ProbePoint[]>(() =>
    frames.value
        .filter(frame => frame.trackType === "video")
        .map(frame => ({ time: frame.relativeTimeMs, size: toKb(frame.frameSize) }))
);
const videoKeyPoints = computed<ProbePoint[]>(() =>
    frames.value
        .filter(frame => frame.trackType === "video" && frame.keyFrame)
        .map(frame => ({ time: frame.relativeTimeMs, size: toKb(frame.frameSize) }))
);
const audioPoints = computed<ProbePoint[]>(() =>
    frames.value
        .filter(frame => frame.trackType === "audio")
        .map(frame => ({ time: frame.relativeTimeMs, size: toKb(frame.frameSize) }))
);

/**
 * 纵轴范围。floorAtZero 为真时从 0 起(看绝对量级,保住 I 帧尖峰);
 * 为假时贴住数据放大(音频帧大小几乎恒定,从 0 起会看不出波动)。
 */
function axisRange(points: ProbePoint[], floorAtZero: boolean): AxisRange {
    if (!points.length) return { min: 0, max: 1 };
    let min = points[0].size;
    let max = points[0].size;
    for (const point of points) {
        if (point.size < min) min = point.size;
        if (point.size > max) max = point.size;
    }
    if (!(max > min)) return { min: 0, max: Math.max(1, max * 1.2) };
    const span = max - min;
    return {
        min: floorAtZero ? 0 : Math.max(0, min - span * 0.15),
        max: Math.round((max + span * 0.12) * 100) / 100,
    };
}

const videoAxis = computed(() => axisRange(videoPoints.value, true));
const audioAxis = computed(() => axisRange(audioPoints.value, false));

/* ── 时间范围刷选 ── */
// zoomStart/zoomEnd 是选区框的实时位置,拖拽时跟着鼠标走,只驱动那个选框;
// committed* 是「已经出图的横轴范围」,只在松手或重置时更新。
// 之所以要分成两份:重建图表是一次全量重绘(最多上万点 × 2 条轨道),跟着
// mousemove 每帧重画会明显卡顿,所以拖拽期间不出图,松手才提交一次。
const zoomStart = ref(0);
const zoomEnd = ref(1);
const committedStart = ref(0);
const committedEnd = ref(1);
const brushEl = ref<HTMLElement | null>(null);
let brushAnchor = 0;
let brushing = false;

const xMin = computed(() => Math.round(committedStart.value * durationMs.value));
const xMax = computed(() => Math.max(Math.round(committedEnd.value * durationMs.value), xMin.value + 20));
const isZoomed = computed(() => committedStart.value > 0.001 || committedEnd.value < 0.999);

function brushRatio(event: MouseEvent) {
    const el = brushEl.value;
    if (!el) return 0;
    const rect = el.getBoundingClientRect();
    return Math.min(1, Math.max(0, (event.clientX - rect.left) / Math.max(1, rect.width)));
}

function onBrushDown(event: MouseEvent) {
    if (!durationMs.value) return;
    brushing = true;
    brushAnchor = brushRatio(event);
    zoomStart.value = brushAnchor;
    zoomEnd.value = brushAnchor;
    window.addEventListener("mousemove", onBrushMove);
    window.addEventListener("mouseup", onBrushUp);
}

function onBrushMove(event: MouseEvent) {
    if (!brushing) return;
    const ratio = brushRatio(event);
    zoomStart.value = Math.min(brushAnchor, ratio);
    zoomEnd.value = Math.max(brushAnchor, ratio);
}

function onBrushUp() {
    if (!brushing) return;
    brushing = false;
    window.removeEventListener("mousemove", onBrushMove);
    window.removeEventListener("mouseup", onBrushUp);
    // 单击(选区近似为零)视作取消选择,回到全量范围。
    if (zoomEnd.value - zoomStart.value < 0.012) {
        resetZoom();
        return;
    }
    // 提交这次选区,由 watch(xMin/xMax) 统一发起那一次重建。
    committedStart.value = zoomStart.value;
    committedEnd.value = zoomEnd.value;
}

function resetZoom() {
    zoomStart.value = 0;
    zoomEnd.value = 1;
    committedStart.value = 0;
    committedEnd.value = 1;
}

const selectionStyle = computed(() => ({
    left: `${zoomStart.value * 100}%`,
    width: `${Math.max(0.6, (zoomEnd.value - zoomStart.value) * 100)}%`,
}));

/* ── 文案 ── */
const durationText = computed(() =>
    durationMs.value >= 1000 ? `${(durationMs.value / 1000).toFixed(1)} 秒` : `${durationMs.value} ms`
);

/** 节奏对照格里的毫秒值。后端算不出该项时下发 null,显示破折号而不是 0 —— 两者含义完全不同。 */
function msText(value: number | null) {
    return value == null ? "—" : `${value.toFixed(1)} ms`;
}

const summaryText = computed(() => {
    const data = overview.value;
    if (!data) return "这次检测没有取到逐帧数据。";
    const parts = [`采样 ${durationText.value}`, `采集 ${data.totalFrames} 帧`];
    if (data.truncated) parts.push(`明细只含末尾 ${data.sampledFrames} 帧`);
    if (data.stallCount > 0) {
        parts.push(`${data.stallCount} 处到达断档,最长 ${Math.round(data.maxGapMs)} ms`);
    } else {
        parts.push("未发现到达断档");
    }
    return parts.join(" · ");
});

const rangeText = computed(() =>
    isZoomed.value ? `${xMin.value} – ${xMax.value} ms` : "全部"
);

const hasFrames = computed(() => frames.value.length > 0);

/* ── VChart 生命周期 ── */
const videoHost = ref<HTMLElement | null>(null);
const audioHost = ref<HTMLElement | null>(null);
let videoChart: VChart | null = null;
let audioChart: VChart | null = null;
let videoResize: ResizeObserver | null = null;
let audioResize: ResizeObserver | null = null;

function formatAxisValue(value: number) {
    return value >= 10 ? String(Math.round(value)) : String(Math.round(value * 10) / 10);
}

function buildSpec(points: ProbePoint[], keyPoints: ProbePoint[], axis: AxisRange, color: string) {
    const series: Record<string, unknown>[] = [
        {
            type: "scatter",
            data: { id: "points" },
            xField: "time",
            yField: "size",
            size: 5,
            point: { style: { fill: color, fillOpacity: 0.85 } },
        },
    ];
    if (keyPoints.length) {
        series.push({
            type: "scatter",
            data: { id: "keys" },
            xField: "time",
            yField: "size",
            size: 10,
            shape: "diamond",
            point: { style: { fill: KEYFRAME_COLOR } },
        });
    }
    return {
        type: "common",
        background: "transparent",
        animation: false,
        padding: { left: 6, right: 14, top: 8, bottom: 6 },
        data: [
            { id: "points", values: points },
            { id: "keys", values: keyPoints },
        ],
        series,
        axes: [
            {
                orient: "left",
                type: "linear",
                min: axis.min,
                max: axis.max,
                label: { formatMethod: (value: number) => formatAxisValue(value) },
            },
            {
                orient: "bottom",
                type: "linear",
                min: xMin.value,
                max: xMax.value,
                label: { formatMethod: (value: number) => `${Math.round(value)} ms` },
            },
        ],
        legends: { visible: false },
        tooltip: { visible: false },
    };
}

/**
 * 渲染(或重新渲染)一张图表。
 *
 * 这里刻意不用 updateSpecSync 做增量更新:VChart 1.13.10 的 scatter 序列走增量
 * 更新时会在内部 transform 管线抛 `Cannot read properties of undefined
 * (reading 'forEach')`,同一份 spec 把 series.type 换成 line/bar 就正常,说明是
 * scatter 这条路径的限制而不是 spec 写错。缩放只改横轴范围,实例上没有需要保留的
 * 交互状态,图表本身也是 animation:false,因此直接销毁重建 —— 与首次挂载走完全
 * 相同的代码路径,实测在同一宿主上重建可正常出图且不残留 canvas。
 */
function renderChart(host: HTMLElement | null, chart: VChart | null, spec: unknown): VChart | null {
    if (!host) return chart;
    chart?.release();
    const created = new VChart(spec as never, { dom: host, autoFit: false });
    created.renderSync();
    return created;
}

function observe(host: HTMLElement | null, getChart: () => VChart | null) {
    if (!host || typeof ResizeObserver === "undefined") return null;
    // 记录上一次真正下发的尺寸。VChart 的 resize 只认整数像素,把入参取整后再比较,
    // 亚像素抖动(0.5px 之类)就不会反复触发重绘 —— 否则一次重绘又会改 canvas 尺寸,
    // 回来再触发观察器,空转成 ResizeObserver loop 告警。
    let lastW = 0;
    let lastH = 0;
    const observer = new ResizeObserver(entries => {
        const bounds = entries[0]?.contentRect;
        const chart = getChart();
        if (!bounds || bounds.width <= 0 || bounds.height <= 0 || !chart) return;
        const width = Math.round(bounds.width);
        const height = Math.round(bounds.height);
        if (width === lastW && height === lastH) return;
        lastW = width;
        lastH = height;
        chart.resize(width, height);
    });
    observer.observe(host);
    return observer;
}

function syncCharts() {
    if (!props.visible) return;
    videoChart = renderChart(
        videoHost.value,
        videoChart,
        buildSpec(videoPoints.value, videoKeyPoints.value, videoAxis.value, VIDEO_COLOR)
    );
    if (!videoResize) videoResize = observe(videoHost.value, () => videoChart);
    audioChart = renderChart(
        audioHost.value,
        audioChart,
        buildSpec(audioPoints.value, [], audioAxis.value, AUDIO_COLOR)
    );
    if (!audioResize) audioResize = observe(audioHost.value, () => audioChart);
}

function releaseCharts() {
    videoResize?.disconnect();
    audioResize?.disconnect();
    videoResize = null;
    audioResize = null;
    videoChart?.release();
    audioChart?.release();
    videoChart = null;
    audioChart = null;
}

watch(
    () => props.visible,
    async visible => {
        if (!visible) {
            releaseCharts();
            return;
        }
        resetZoom();
        await nextTick();
        syncCharts();
    }
);

watch([xMin, xMax, videoAxis, audioAxis], () => {
    // 横轴只跟着 committed* 变,拖拽中的 mousemove 不会走到这里。
    void nextTick(syncCharts);
}, { flush: "post" });

onBeforeUnmount(releaseCharts);
</script>

<template>
    <a-modal
        :visible="props.visible"
        :width="980"
        title="帧到达时间线"
        modal-class="uvp-system-dialog probe-timeline-modal"
        :footer="false"
        :mask-closable="true"
        unmount-on-close
        @cancel="emit('update:visible', false)"
        @update:visible="emit('update:visible', $event)"
    >
        <div class="ptl" data-testid="probe-timeline-dialog">
            <p class="ptl-summary" data-testid="probe-timeline-summary">{{ summaryText }}</p>

            <!-- 诊断结论层。放在曲线之前:先给「是什么问题、该往哪儿查」,再让用户去图里
                 核对是哪一秒。这一层的数据全部来自后端 health,前端不重算指标。 -->
            <section v-if="diagnosis" class="ptl-diagnosis" data-testid="probe-timeline-diagnosis">
                <header class="ptl-diag-head">
                    <span class="ptl-diag-status" :class="diagnosis.status" data-testid="probe-timeline-status">{{ diagnosis.statusLabel }}</span>
                    <strong data-testid="probe-timeline-verdict">{{ diagnosis.verdict.title }}</strong>
                </header>
                <p class="ptl-diag-detail">{{ diagnosis.verdict.detail }}</p>

                <ul v-if="diagnosis.issues.length" class="ptl-diag-issues" data-testid="probe-timeline-issues">
                    <li v-for="issue in diagnosis.issues" :key="issue.code" :class="issue.severity">
                        <span class="ptl-diag-issue-title">{{ issue.title }}</span>
                        <em v-if="issue.evidence" class="ptl-diag-evidence">{{ issue.evidence }}</em>
                        <span class="ptl-diag-focus">{{ issue.focus }}</span>
                    </li>
                </ul>
                <p v-else class="ptl-diag-clean">未发现异常帧间隔</p>

                <div class="ptl-diag-rhythm">
                    <div>
                        <span>编码节奏 · 源端出帧</span>
                        <strong>{{ msText(diagnosis.rhythm.encodeMs) }}</strong>
                        <em>DTS 间隔均值</em>
                    </div>
                    <div>
                        <span>到达节奏 · 链路送达</span>
                        <strong>{{ msText(diagnosis.rhythm.arrivalMs) }}</strong>
                        <em>{{ diagnosis.rhythm.label }}</em>
                    </div>
                </div>
            </section>

            <template v-if="hasFrames">
                <section class="ptl-lane">
                    <header class="ptl-lane-head">
                        <span>视频轨 · 帧大小 (KB)</span>
                        <em>菱形 = 关键帧</em>
                    </header>
                    <div ref="videoHost" class="ptl-canvas ptl-canvas-video" data-testid="probe-timeline-video"></div>
                </section>

                <section class="ptl-lane">
                    <header class="ptl-lane-head">
                        <span>音频轨 · 帧大小 (KB)</span>
                        <em>纵轴已按数据放大，便于看波动</em>
                    </header>
                    <div ref="audioHost" class="ptl-canvas ptl-canvas-audio" data-testid="probe-timeline-audio"></div>
                </section>

                <section class="ptl-brush-section">
                    <header class="ptl-lane-head">
                        <span>时间范围</span>
                        <span class="ptl-brush-tools">
                            <em data-testid="probe-timeline-range">{{ rangeText }}</em>
                            <button
                                type="button"
                                class="ptl-reset"
                                data-testid="probe-timeline-reset"
                                :disabled="!isZoomed"
                                @click="resetZoom"
                            >
                                <RotateCcw :size="12" />重置
                            </button>
                        </span>
                    </header>
                    <div
                        ref="brushEl"
                        class="ptl-brush-track"
                        role="slider"
                        tabindex="0"
                        aria-label="拖拽选择要细看的时间范围"
                        :aria-valuemin="0"
                        :aria-valuemax="durationMs"
                        :aria-valuenow="xMin"
                        data-testid="probe-timeline-brush"
                        @mousedown="onBrushDown"
                    >
                        <span
                            v-for="(bucket, index) in overview?.buckets ?? []"
                            :key="index"
                            class="ptl-brush-bar"
                            :class="{ stalled: bucket.stalled, empty: bucket.count === 0 }"
                            :style="{ height: `${probeBucketHeight(bucket)}%` }"
                        ></span>
                        <span class="ptl-brush-selection" :style="selectionStyle"></span>
                    </div>
                    <p class="ptl-hint">在色条上拖拽可以放大到某一段时间，单击空白处回到全部范围。</p>
                </section>

                <div class="ptl-legend">
                    <span><i class="video"></i>视频帧</span>
                    <span><i class="key"></i>关键帧</span>
                    <span><i class="audio"></i>音频帧</span>
                    <span><i class="stall"></i>到达断档区间</span>
                </div>
            </template>

            <div v-else class="ptl-empty">
                <span>这次检测没有取到逐帧数据</span>
            </div>
        </div>
    </a-modal>
</template>

<style scoped>
/* ⛔ 这几个 grid 容器必须显式写 `grid-template-columns: minmax(0, 1fr)`。
   默认的 auto 轨道按内容定宽,而图表宿主的内容就是 VChart 那张带 px 宽度的 canvas ——
   轨道会跟着 canvas 一起长,canvas 又被 ResizeObserver 按宿主宽度重设,于是形成正反馈:
   实测每轮 +2px(就是 .ptl-canvas 那圈 1px 边框,它还是 content-box),无限变宽。 */
.ptl { display: grid; grid-template-columns: minmax(0, 1fr); gap: 12px; padding: 2px 2px 0; }
.ptl-summary { margin: 0; color: var(--uvp-text-secondary); font-size: 12px; line-height: 1.5; }
/* 诊断区只是文字与小块,没有图表宿主,但容器契约跟 .ptl 保持一致,免得以后往里塞
   图表时又踩到 auto 轨道那个反馈环(见文件顶部 .ptl 的说明)。 */
.ptl-diagnosis {
    display: grid; grid-template-columns: minmax(0, 1fr); gap: 7px;
    padding: 11px 12px; background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border); border-radius: 8px;
}
.ptl-diag-head { display: flex; align-items: center; gap: 8px; min-width: 0; }
.ptl-diag-head > strong { color: var(--uvp-text-primary); font-size: 12.5px; font-weight: 600; }
/* 状态徽章分四档:后端只给 ok/warning/error,unknown 是快照缺字段时的兜底,
   不假装健康。 */
.ptl-diag-status {
    flex: 0 0 auto; padding: 2px 8px; border-radius: 999px;
    color: var(--uvp-text-tertiary); background: var(--uvp-panel-bg);
    border: 1px solid var(--uvp-panel-border); font-size: 11px; font-weight: 600;
}
.ptl-diag-status.ok {
    color: var(--uvp-brand-cyan); background: color-mix(in srgb, var(--uvp-brand-cyan) 12%, transparent);
    border-color: color-mix(in srgb, var(--uvp-brand-cyan) 32%, transparent);
}
.ptl-diag-status.warning { color: var(--uvp-warning); background: var(--uvp-warning-soft); border-color: var(--uvp-warning-border); }
.ptl-diag-status.error { color: var(--uvp-danger); background: var(--uvp-danger-soft); border-color: var(--uvp-danger-border); }
.ptl-diag-detail { margin: 0; color: var(--uvp-text-secondary); font-size: 12px; line-height: 1.55; }
.ptl-diag-issues { display: grid; grid-template-columns: minmax(0, 1fr); gap: 5px; margin: 0; padding: 0; list-style: none; }
/* 左细边区分严重程度;标题与证据同一行,排查方向另起一行占满,长文案不会被挤成竖条。 */
.ptl-diag-issues > li {
    display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 3px 8px;
    align-items: baseline; padding: 6px 9px;
    background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border);
    border-left-width: 2px; border-radius: 6px;
}
.ptl-diag-issues > li.warning { border-left-color: var(--uvp-warning); }
.ptl-diag-issues > li.error { border-left-color: var(--uvp-danger); }
.ptl-diag-issue-title { color: var(--uvp-text-primary); font-size: 11.5px; font-weight: 600; }
.ptl-diag-evidence { color: var(--uvp-text-secondary); font-family: ui-monospace, Menlo, monospace; font-size: 11px; font-style: normal; }
.ptl-diag-focus { grid-column: 1 / -1; color: var(--uvp-text-tertiary); font-size: 11px; }
.ptl-diag-clean { margin: 0; color: var(--uvp-text-tertiary); font-size: 11.5px; }
.ptl-diag-rhythm { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 6px; }
.ptl-diag-rhythm > div {
    display: grid; gap: 3px; padding: 7px 9px;
    background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: 6px;
}
.ptl-diag-rhythm span { color: var(--uvp-text-tertiary); font-size: 10.5px; }
.ptl-diag-rhythm strong { color: var(--uvp-text-primary); font-family: ui-monospace, Menlo, monospace; font-size: 13px; font-weight: 650; }
.ptl-diag-rhythm em { color: var(--uvp-text-tertiary); font-size: 10.5px; font-style: normal; }
.ptl-lane { display: grid; grid-template-columns: minmax(0, 1fr); gap: 5px; min-width: 0; }
.ptl-lane-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; color: var(--uvp-text-secondary); font-size: 11.5px; }
.ptl-lane-head > span { font-weight: 600; }
.ptl-lane-head > em { color: var(--uvp-text-tertiary); font-size: 11px; font-style: normal; }
/* min-width:0 + border-box + overflow:hidden 三重保险:即使 canvas 一时比宿主宽,
   也不会反过来把宿主撑大(那正是反馈环的入口)。 */
.ptl-canvas {
    width: 100%; min-width: 0; box-sizing: border-box; overflow: hidden;
    background: var(--uvp-list-toolbar-bg); border: 1px solid var(--uvp-panel-border); border-radius: 8px;
}
.ptl-canvas-video { height: 208px; }
.ptl-canvas-audio { height: 132px; }
.ptl-brush-section { display: grid; grid-template-columns: minmax(0, 1fr); gap: 5px; min-width: 0; }
.ptl-brush-tools { display: inline-flex; align-items: center; gap: 8px; }
.ptl-reset {
    display: inline-flex; align-items: center; gap: 4px; padding: 2px 8px;
    color: var(--uvp-text-secondary); background: transparent;
    border: 1px solid var(--uvp-panel-border); border-radius: 6px;
    cursor: pointer; font-size: 11px;
}
.ptl-reset:hover:not(:disabled) { color: var(--uvp-brand); border-color: var(--uvp-brand); }
.ptl-reset:disabled { cursor: not-allowed; opacity: 0.45; }
.ptl-brush-track {
    position: relative; display: flex; align-items: flex-end; gap: 1px;
    height: 34px; padding: 0 1px; overflow: hidden;
    background: var(--uvp-list-toolbar-bg); border: 1px solid var(--uvp-panel-border);
    border-radius: 7px; cursor: crosshair; user-select: none;
}
.ptl-brush-bar {
    flex: 1 1 0; min-width: 0; border-radius: 1px 1px 0 0;
    background: color-mix(in srgb, var(--uvp-brand) 55%, transparent);
}
.ptl-brush-bar.empty { background: var(--uvp-panel-border); }
.ptl-brush-bar.stalled { background: var(--uvp-warning); }
.ptl-brush-selection {
    position: absolute; top: 0; bottom: 0;
    background: color-mix(in srgb, var(--uvp-brand) 18%, transparent);
    border-left: 1px solid var(--uvp-brand); border-right: 1px solid var(--uvp-brand);
    pointer-events: none;
}
.ptl-hint { margin: 0; color: var(--uvp-text-tertiary); font-size: 11px; }
.ptl-legend { display: flex; flex-wrap: wrap; align-items: center; gap: 14px; color: var(--uvp-text-tertiary); font-size: 11px; }
.ptl-legend span { display: inline-flex; align-items: center; gap: 5px; }
.ptl-legend i { width: 8px; height: 8px; border-radius: 50%; }
.ptl-legend i.video { background: #378ADD; }
.ptl-legend i.audio { background: #1D9E75; }
.ptl-legend i.key { background: #BA7517; border-radius: 1px; transform: rotate(45deg); }
.ptl-legend i.stall { width: 12px; height: 3px; background: var(--uvp-warning); border-radius: 1px; }
.ptl-empty {
    display: flex; min-height: 120px; align-items: center; justify-content: center;
    color: var(--uvp-text-tertiary); background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border); border-radius: 8px; font-size: 12px;
}
</style>
