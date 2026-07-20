<script setup lang="ts">
import { provide, ref } from "vue";
import { SVG_CANVAS_HOVER_KEY, type SvgHoverCoord } from "./svg-canvas-context";

interface Props {
    viewBox: string;
    width?: string | number;
    height?: string | number;
    preserveAspectRatio?: string;
}

const props = withDefaults(defineProps<Props>(), {
    width: "100%",
    height: "100%",
    preserveAspectRatio: "xMidYMid meet"
});

const hover = ref<SvgHoverCoord | null>(null);
provide(SVG_CANVAS_HOVER_KEY, hover);

function onMove(evt: MouseEvent) {
    const svg = evt.currentTarget as SVGSVGElement | null;
    if (!svg) return;
    const rect = svg.getBoundingClientRect();
    if (!rect.width || !rect.height) return;

    const [minX, minY, vbW, vbH] = props.viewBox.split(/\s+/).map(Number);
    const scaleX = vbW / rect.width;
    const scaleY = vbH / rect.height;
    hover.value = {
        x: (evt.clientX - rect.left) * scaleX + minX,
        y: (evt.clientY - rect.top) * scaleY + minY
    };
}

function onLeave() {
    hover.value = null;
}
</script>

<template>
    <svg
        :viewBox="viewBox"
        :width="width"
        :height="height"
        :preserveAspectRatio="preserveAspectRatio"
        @mousemove="onMove"
        @mouseleave="onLeave"
    >
        <slot />
    </svg>
</template>
