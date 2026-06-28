<script setup lang="ts">
/**
 * 极简 SVG sparkline(不依赖 ECharts)
 *
 * 用法:<Sparkline :data="streamHistory" :width="100" :height="30" color="brand" />
 *
 * data:可以是 number[] 或 {value: number}[];自动适配。
 * 单点时显示一个圆点;空时显示 dashed baseline。
 */
import { computed } from "vue";

type RawPoint = number | { value: number };

const props = defineProps<{
    data: RawPoint[];
    width?: number;
    height?: number;
    color?: "brand" | "accent" | "warning" | "danger" | "info" | string;
    /** 是否填充曲线下方区域 */
    fill?: boolean;
}>();

const W = computed(() => props.width ?? 100);
const H = computed(() => props.height ?? 30);

const strokeColor = computed(() => {
    switch (props.color) {
        case "brand":
            return "var(--zlm-brand-500)";
        case "accent":
            return "var(--zlm-accent-500)";
        case "warning":
            return "var(--zlm-warn-500)";
        case "danger":
            return "var(--zlm-danger-500)";
        case "info":
            return "var(--zlm-info-500)";
        default:
            return props.color || "var(--zlm-brand-500)";
    }
});

const normalized = computed(() => {
    return props.data
        .map((p) => (typeof p === "number" ? p : p?.value ?? 0))
        .filter((v) => typeof v === "number" && !Number.isNaN(v));
});

const points = computed(() => {
    const vals = normalized.value;
    if (vals.length < 2) return "";
    const max = Math.max(...vals, 1);
    const min = Math.min(...vals, 0);
    const range = max - min || 1;
    const stepX = W.value / (vals.length - 1);
    return vals
        .map((v, i) => {
            const x = i * stepX;
            const y = H.value - ((v - min) / range) * H.value;
            return `${x.toFixed(2)},${y.toFixed(2)}`;
        })
        .join(" ");
});

const fillPath = computed(() => {
    if (!props.fill) return "";
    const vals = normalized.value;
    if (vals.length < 2) return "";
    const max = Math.max(...vals, 1);
    const min = Math.min(...vals, 0);
    const range = max - min || 1;
    const stepX = W.value / (vals.length - 1);
    const pathPoints = vals.map((v, i) => {
        const x = i * stepX;
        const y = H.value - ((v - min) / range) * H.value;
        return `${x.toFixed(2)},${y.toFixed(2)}`;
    });
    return `M 0,${H.value} L ${pathPoints.join(" L ")} L ${W.value},${H.value} Z`;
});

const lastPoint = computed(() => {
    const vals = normalized.value;
    if (vals.length === 0) return null;
    if (vals.length === 1) {
        return { x: W.value / 2, y: H.value / 2 };
    }
    const max = Math.max(...vals, 1);
    const min = Math.min(...vals, 0);
    const range = max - min || 1;
    const stepX = W.value / (vals.length - 1);
    const i = vals.length - 1;
    return {
        x: i * stepX,
        y: H.value - ((vals[i] - min) / range) * H.value
    };
});

const isEmpty = computed(() => normalized.value.length === 0);
</script>

<template>
    <svg :viewBox="`0 0 ${W} ${H}`" :width="W" :height="H" class="sparkline" preserveAspectRatio="none">
        <line
            v-if="isEmpty"
            :x1="0"
            :x2="W"
            :y1="H / 2"
            :y2="H / 2"
            stroke="var(--zlm-border)"
            stroke-dasharray="2,2"
            stroke-width="1"
        />
        <path
            v-if="fill && fillPath"
            :d="fillPath"
            :fill="strokeColor"
            fill-opacity="0.1"
        />
        <polyline
            v-if="points"
            :points="points"
            fill="none"
            :stroke="strokeColor"
            stroke-width="1.5"
            stroke-linejoin="round"
            stroke-linecap="round"
            vector-effect="non-scaling-stroke"
        />
        <circle
            v-if="lastPoint && normalized.length >= 1"
            :cx="lastPoint.x"
            :cy="lastPoint.y"
            r="2"
            :fill="strokeColor"
        />
    </svg>
</template>

<style scoped>
.sparkline {
    display: block;
}
</style>
