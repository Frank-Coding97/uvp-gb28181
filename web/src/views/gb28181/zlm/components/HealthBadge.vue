<script setup lang="ts">
/**
 * 健康度胶囊(独立于生命周期的健康指示)
 *
 * 用法:
 *   <HealthBadge :health="record.health" :reason="record.nearCapacity ? '接近容量' : ''" />
 *   <HealthBadge health="warning" reason="接近容量" />
 *
 * 健康度对应:
 *   healthy   → 绿色胶囊 "Healthy"
 *   warning   → 黄色胶囊 "Warning"(可附 reason)
 *   critical  → 红色胶囊 "Critical"(可附 reason)
 *   unknown / undefined / null → 灰色 "—"(无健康信号,如离线节点)
 */
import { computed } from "vue";

export type HealthLevel = "healthy" | "warning" | "critical" | "unknown" | string;

const props = defineProps<{
    health?: HealthLevel | null;
    reason?: string;
}>();

const cfg = computed(() => {
    switch (props.health) {
        case "healthy":
            return {
                bg: "var(--zlm-success-50)",
                color: "var(--zlm-success-600)",
                border: "var(--zlm-success-500)",
                text: "Healthy"
            };
        case "warning":
            return {
                bg: "var(--zlm-warn-50)",
                color: "var(--zlm-warn-600)",
                border: "var(--zlm-warn-500)",
                text: "Warning"
            };
        case "critical":
            return {
                bg: "var(--zlm-danger-50)",
                color: "var(--zlm-danger-600)",
                border: "var(--zlm-danger-500)",
                text: "Critical"
            };
        default:
            return null;
    }
});
</script>

<template>
    <span v-if="cfg" class="health-badge" :style="{ background: cfg.bg, color: cfg.color, borderColor: cfg.border }">
        <span class="text">{{ cfg.text }}</span>
        <span v-if="reason" class="reason">· {{ reason }}</span>
    </span>
    <span v-else class="health-empty">—</span>
</template>

<style scoped>
@import "@/styles/zlm-tokens.css";

.health-badge {
    display: inline-flex;
    align-items: center;
    gap: var(--zlm-space-1);
    padding: 2px 8px;
    border-radius: var(--zlm-radius-full);
    border: 1px solid transparent;
    font-size: var(--zlm-fs-caption);
    font-weight: var(--zlm-fw-medium);
    line-height: 1.4;
    white-space: nowrap;
}

.health-badge .reason {
    color: inherit;
    opacity: 0.85;
}

.health-empty {
    color: var(--zlm-text-4);
    font-size: var(--zlm-fs-body);
}
</style>
