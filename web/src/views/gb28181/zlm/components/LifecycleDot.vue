<script setup lang="ts">
/**
 * 生命周期状态圆点(8px 实心圆 + 文字)
 *
 * 用法:<LifecycleDot :state="record.state" />
 *
 * 状态对应:
 *   active       → 绿色圆点 + "活跃"
 *   maintenance  → 橙色圆点 + "维护"
 *   offline      → 灰色圆点 + "离线"
 */
import { computed } from "vue";

const props = defineProps<{
    state: "active" | "maintenance" | "offline" | string;
    showText?: boolean; // 默认 true
}>();

const cfg = computed(() => {
    switch (props.state) {
        case "active":
            return { color: "var(--zlm-state-active)", text: "活跃" };
        case "maintenance":
            return { color: "var(--zlm-state-maintenance)", text: "维护" };
        case "offline":
            return { color: "var(--zlm-state-offline)", text: "离线" };
        default:
            return { color: "var(--zlm-text-3)", text: props.state };
    }
});

const showText = computed(() => props.showText !== false);
</script>

<template>
    <span class="lifecycle-dot">
        <span class="dot" :style="{ background: cfg.color }" />
        <span v-if="showText" class="text">{{ cfg.text }}</span>
    </span>
</template>

<style scoped>

.lifecycle-dot {
    display: inline-flex;
    align-items: center;
    gap: var(--zlm-space-2);
    font-size: var(--zlm-fs-body);
    color: var(--zlm-text-2);
    font-family: var(--zlm-font-body);
}

.dot {
    display: inline-block;
    width: 8px;
    height: 8px;
    border-radius: var(--zlm-radius-full);
    flex-shrink: 0;
}

.text {
    line-height: var(--zlm-lh-tight);
}
</style>
