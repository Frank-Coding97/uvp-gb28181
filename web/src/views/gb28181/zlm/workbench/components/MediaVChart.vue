<script setup lang="ts">
import { computed, getCurrentInstance, nextTick, onActivated, onBeforeUnmount, onDeactivated, onMounted, ref, watch } from "vue";
import VChart from "@visactor/vchart";
import type { MediaChartSpec } from "../chart/overviewChart";

export type MediaChartStatus = "ready" | "empty" | "unknown" | "unavailable" | "partial";

const props = withDefaults(defineProps<{
  title: string;
  spec?: MediaChartSpec | null;
  status?: MediaChartStatus;
  statusText?: string;
  summary?: string;
  warning?: string | null;
  asOf?: string | null;
  sampledLabel?: string;
  legendLabel?: string;
  showSummary?: boolean;
  active?: boolean;
}>(), {
  spec: null,
  status: "ready",
  statusText: "当前暂无图表数据",
  summary: "暂无可读的图表摘要",
  warning: null,
  asOf: null,
  sampledLabel: "",
  legendLabel: "",
  showSummary: true,
  active: true
});

const chartHost = ref<HTMLElement | null>(null);
let chart: VChart | null = null;
let resizeObserver: ResizeObserver | null = null;
let syncVersion = 0;
const instance = getCurrentInstance();
const idPrefix = `media-vchart-${instance?.uid ?? Math.random().toString(36).slice(2)}`;
const summaryId = `${idPrefix}-summary`;

const effectiveStatus = computed<MediaChartStatus>(() => {
  if (!props.spec && props.status === "ready") return "unknown";
  return props.status;
});

const canRender = computed(() => props.active && !!props.spec && (effectiveStatus.value === "ready" || effectiveStatus.value === "partial"));
const stateText = computed(() => {
  if (effectiveStatus.value === "empty") return props.statusText || "当前筛选没有数据";
  if (effectiveStatus.value === "unknown") return props.statusText || "当前数据状态未知";
  if (effectiveStatus.value === "unavailable") return props.statusText || "当前数据暂不可用";
  if (effectiveStatus.value === "partial") return props.statusText || "当前为部分采样数据";
  return props.statusText;
});
const chartAriaLabel = computed(() => `${props.title}，${props.summary || stateText.value}`);

function prefersReducedMotion(): boolean {
  return typeof window !== "undefined" && typeof window.matchMedia === "function" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

function normalizedSpec(spec: MediaChartSpec): MediaChartSpec {
  const reduced = prefersReducedMotion();
  if (reduced) {
    return {
      ...spec,
      animation: false,
      animationAppear: false,
      animationEnter: false,
      animationUpdate: false,
      animationExit: false
    };
  }
  return {
    animation: true,
    animationAppear: { duration: 220 },
    animationEnter: { duration: 220 },
    animationUpdate: { duration: 220 },
    animationExit: { duration: 180 },
    ...spec
  };
}

function disconnectResizeObserver() {
  resizeObserver?.disconnect();
  resizeObserver = null;
}

function releaseChart() {
  disconnectResizeObserver();
  if (!chart) return;
  chart.release();
  chart = null;
}

function observeResize() {
  if (!chartHost.value || typeof ResizeObserver === "undefined") return;
  disconnectResizeObserver();
  resizeObserver = new ResizeObserver(entries => {
    const bounds = entries[0]?.contentRect;
    if (!bounds || bounds.width <= 0 || bounds.height <= 0 || !chart) return;
    chart.resize(bounds.width, bounds.height);
  });
  resizeObserver.observe(chartHost.value);
}

function mountChart(spec: MediaChartSpec) {
  if (!chartHost.value) return;
  chart = new VChart(normalizedSpec(spec) as never, { dom: chartHost.value, autoFit: false });
  chart.on("layoutStart", clearHoverState);
  chart.renderSync();
  observeResize();
}

function clearHoverState() {
  if (!chart) return;
  chart.hideTooltip();
  for (const component of chart.getComponents()) {
    if ("hideCrosshair" in component && typeof component.hideCrosshair === "function") component.hideCrosshair();
  }
}

function syncChart() {
  syncVersion += 1;
  const version = syncVersion;
  if (!canRender.value || !props.spec || !chartHost.value) {
    releaseChart();
    return;
  }
  const nextSpec = normalizedSpec(props.spec);
  if (!chart) {
    mountChart(props.spec);
    return;
  }
  // A stale watcher should never update a released chart after deactivation.
  if (version !== syncVersion || !props.active) return;
  // Crosshair caches reference the old series while updateSpec rebuilds its data.
  clearHoverState();
  chart.updateSpecSync(nextSpec as never, true);
  chart.renderSync();
  observeResize();
}

watch(
  () => [props.spec, props.status, props.active],
  syncChart,
  { deep: true, flush: "post" }
);

onMounted(syncChart);
onActivated(() => void nextTick(syncChart));
onDeactivated(releaseChart);
onBeforeUnmount(releaseChart);
</script>

<template>
  <section class="media-vchart" :data-status="effectiveStatus" :aria-busy="canRender ? 'false' : undefined">
    <header class="media-vchart__header">
      <div>
        <h3>{{ title }}</h3>
        <p v-if="sampledLabel || asOf" class="media-vchart__meta">
          <span v-if="sampledLabel">{{ sampledLabel }}</span>
          <span v-if="sampledLabel && asOf"> · </span>
          <span v-if="asOf">最新 {{ asOf }}</span>
        </p>
      </div>
      <div v-if="legendLabel || effectiveStatus === 'partial'" class="media-vchart__header-aside">
        <span v-if="legendLabel" class="media-vchart__legend"><i aria-hidden="true" />{{ legendLabel }}</span>
        <span v-if="effectiveStatus === 'partial'" class="media-vchart__badge">部分数据</span>
      </div>
    </header>

    <div v-if="canRender" ref="chartHost" class="media-vchart__canvas" role="img" tabindex="0" :aria-label="chartAriaLabel" :aria-describedby="summaryId" />
    <div v-else class="media-vchart__state" role="status" :aria-label="`${title}：${stateText}`">
      <span class="media-vchart__state-icon" aria-hidden="true">{{ effectiveStatus === 'empty' ? '∅' : effectiveStatus === 'unavailable' ? '!' : '—' }}</span>
      <strong>{{ stateText }}</strong>
    </div>
    <p :id="summaryId" class="media-vchart__summary" :class="{ 'media-vchart__summary--sr-only': !showSummary }">{{ summary || stateText }}</p>
    <p v-if="warning" class="media-vchart__warning" role="status">{{ warning }}</p>
  </section>
</template>

<style scoped>
.media-vchart { min-width: 0; padding: 14px 16px 12px; color: var(--zlm-text-2, var(--color-text-2)); background: var(--uvp-panel-bg, var(--color-bg-2)); border: 1px solid var(--uvp-panel-border, var(--color-border-2)); border-radius: var(--uvp-panel-radius, 10px); box-shadow: var(--uvp-panel-shadow, 0 2px 8px rgb(0 0 0 / 4%)); }
.media-vchart__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; margin-bottom: 8px; }
.media-vchart h3 { margin: 0; color: var(--zlm-text-1, var(--color-text-1)); font-size: 14px; font-weight: 600; }
.media-vchart__meta { margin: 3px 0 0; color: var(--zlm-text-3, var(--color-text-2)); font-size: 12px; line-height: 1.35; }
.media-vchart__header-aside { display: flex; flex: none; align-items: center; gap: 8px; }
.media-vchart__legend { display: inline-flex; align-items: center; gap: 6px; color: var(--zlm-text-3, var(--color-text-2)); font-size: 12px; white-space: nowrap; }
.media-vchart__legend i { width: 8px; height: 8px; background: var(--zlm-brand-500, rgb(var(--primary-6))); border-radius: 50%; }
.media-vchart__badge { flex: none; padding: 2px 7px; color: var(--zlm-warn-600, rgb(var(--warning-6))); background: var(--zlm-warn-50, rgb(var(--warning-1))); border-radius: 999px; font-size: 12px; }
.media-vchart__canvas { width: 100%; min-height: 220px; }
.media-vchart__canvas:focus-visible { outline: 2px solid var(--zlm-brand-500, rgb(var(--primary-6))); outline-offset: 2px; }
.media-vchart__state { display: flex; min-height: 220px; flex-direction: column; align-items: center; justify-content: center; gap: 8px; color: var(--zlm-text-3, var(--color-text-3)); text-align: center; }
.media-vchart__state strong { color: var(--zlm-text-2, var(--color-text-2)); font-size: 13px; font-weight: 500; }
.media-vchart__state-icon { display: grid; width: 30px; height: 30px; place-items: center; color: var(--zlm-text-4, var(--color-text-4)); border: 1px solid var(--uvp-panel-border, var(--color-border-2)); border-radius: 50%; font-size: 17px; }
.media-vchart[data-status="unavailable"] .media-vchart__state-icon { color: var(--zlm-danger-600, rgb(var(--danger-6))); border-color: var(--zlm-danger-500, rgb(var(--danger-5))); }
.media-vchart[data-status="partial"] .media-vchart__summary { color: var(--zlm-warn-600, rgb(var(--warning-6))); }
.media-vchart__summary { min-height: 18px; margin: 7px 0 0; color: var(--zlm-text-3, var(--color-text-2)); font-size: 12px; line-height: 1.4; }
.media-vchart__summary--sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border: 0; }
.media-vchart__warning { margin: 3px 0 0; color: var(--zlm-warn-600, rgb(var(--warning-6))); font-size: 12px; line-height: 1.4; }
@media (prefers-reduced-motion: reduce) { .media-vchart__canvas { scroll-behavior: auto; } }
</style>
