<script setup lang="ts">
import { computed } from "vue";
import SvgCanvas from "./SvgCanvas.vue";
import FlowArrow from "./FlowArrow.vue";

const rows = 5;
const callsPerRow = 50;
const arrowStep = 12;

const arrows = computed(() => {
    const out: Array<{
        from: { x: number; y: number };
        to: { x: number; y: number };
        label: string;
        direction: "request" | "response";
        statusCode?: number;
    }> = [];
    for (let r = 0; r < rows; r++) {
        const y = 40 + r * 60;
        for (let i = 0; i < callsPerRow; i++) {
            const x = 20 + i * arrowStep;
            const goingRight = i % 2 === 0;
            const from = { x: goingRight ? x : x + 10, y };
            const to = { x: goingRight ? x + 10 : x, y };
            const direction: "request" | "response" = goingRight ? "request" : "response";
            const statusCode = direction === "response" ? [200, 302, 404, 500][i % 4] : undefined;
            out.push({ from, to, label: goingRight ? "REQ" : String(statusCode || ""), direction, statusCode });
        }
    }
    return out;
});
</script>

<template>
    <div class="spike-demo">
        <header class="spike-demo__head">
            <h2>SVG 时序图 SPIKE · 250 元素渲染验证</h2>
            <p>rows={{ rows }} × callsPerRow={{ callsPerRow }} = {{ rows * callsPerRow }} 箭头</p>
            <p class="spike-demo__hint">用 Chrome Performance 录制 hover 全程,断言帧率 ≥ 55fps · Long task ≤ 50ms</p>
        </header>
        <SvgCanvas :view-box="`0 0 ${20 + callsPerRow * arrowStep + 20} ${40 + rows * 60}`" width="1200" height="360">
            <line
                v-for="r in rows"
                :key="`swim-${r}`"
                :x1="0"
                :y1="40 + (r - 1) * 60"
                :x2="20 + callsPerRow * arrowStep + 20"
                :y2="40 + (r - 1) * 60"
                stroke="#e5e7eb"
                stroke-width="0.5"
            />
            <FlowArrow
                v-for="(a, idx) in arrows"
                :key="idx"
                :from="a.from"
                :to="a.to"
                :label="a.label"
                :direction="a.direction"
                :status-code="a.statusCode"
            />
        </SvgCanvas>
    </div>
</template>

<style scoped>
.spike-demo {
    padding: 16px;
}
.spike-demo__head {
    margin-bottom: 12px;
}
.spike-demo__hint {
    color: #6b7280;
    font-size: 12px;
}
</style>
