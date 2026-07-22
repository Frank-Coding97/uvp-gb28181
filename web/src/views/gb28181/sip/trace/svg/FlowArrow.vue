<script setup lang="ts">
import { computed } from "vue";
import { resolveArrowColor, type FlowArrowDirection } from "./flow-arrow-colors";

interface Props {
    from: { x: number; y: number };
    to: { x: number; y: number };
    label: string;
    direction: FlowArrowDirection;
    statusCode?: number;
    highlighted?: boolean;
    labelOffset?: number;
}

const props = withDefaults(defineProps<Props>(), {
    highlighted: false,
    labelOffset: -4
});

const color = computed(() => resolveArrowColor(props.direction, props.statusCode));
const strokeWidth = computed(() => (props.highlighted ? 2.5 : 1.4));
const dashArray = computed(() => (props.direction === "response" ? "4 3" : "none"));

const headPoints = computed(() => {
    const { from, to } = props;
    const dx = to.x - from.x;
    const dy = to.y - from.y;
    const len = Math.hypot(dx, dy) || 1;
    const ux = dx / len;
    const uy = dy / len;
    const size = 6;
    const baseX = to.x - ux * size;
    const baseY = to.y - uy * size;
    const nx = -uy;
    const ny = ux;
    const halfWidth = 3;
    const p1x = baseX + nx * halfWidth;
    const p1y = baseY + ny * halfWidth;
    const p2x = baseX - nx * halfWidth;
    const p2y = baseY - ny * halfWidth;
    return `${to.x},${to.y} ${p1x},${p1y} ${p2x},${p2y}`;
});

const labelPos = computed(() => ({
    x: (props.from.x + props.to.x) / 2,
    y: (props.from.y + props.to.y) / 2 + props.labelOffset
}));
</script>

<template>
    <g class="flow-arrow" :class="{ 'flow-arrow--highlighted': highlighted }" :data-direction="direction">
        <line
            :x1="from.x"
            :y1="from.y"
            :x2="to.x"
            :y2="to.y"
            :stroke="color"
            :stroke-width="strokeWidth"
            :stroke-dasharray="dashArray"
        />
        <polygon :points="headPoints" :fill="color" />
        <text
            :x="labelPos.x"
            :y="labelPos.y"
            text-anchor="middle"
            font-size="10"
            :fill="color"
        >{{ label }}</text>
    </g>
</template>
