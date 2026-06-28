<script setup lang="ts">
/**
 * 阶梯式 KPI 卡(替代 a-statistic,避免 "Invalid Date" 老 bug)
 *
 * 排版:title (12px caption) → value (36px display,可带 unit) → suffix (12px caption 趋势/比较)
 *
 * 用法:
 *   <StatCard title="当前流数" :value="128" trend="+12 vs 1h" trend-type="up" />
 *   <StatCard title="网络线程" :value="0.32" unit="%" :is-percent="true" />
 *   <StatCard title="Uptime" :value-text="'3d 14h'" />  <!-- 非数字值用 value-text -->
 *
 * 不引 ECharts 时 sparkline slot 可塞自定义 SVG。
 */
import { computed } from "vue";

const props = defineProps<{
    title: string;
    /** 数字值;为 percent 时 value 应是 0-1 浮点,组件自动 *100 */
    value?: number;
    /** 直接传文本(给 Uptime / 时长 等非数字 KPI) */
    valueText?: string;
    /** 单位,显示在数字右侧小字 */
    unit?: string;
    /** value 是 0-1 浮点,自动 ×100 显示 */
    isPercent?: boolean;
    /** 小数位数,默认数字 0 / 百分比 1 */
    digits?: number;
    /** 趋势文本,如 "+12 vs 1h" / "-3.5% vs 昨日" */
    trend?: string;
    /** 趋势类型,影响颜色 */
    trendType?: "up" | "down" | "neutral" | "danger";
    /** 强调 value 颜色(brand / accent / warning / danger) */
    accent?: "brand" | "accent" | "warning" | "danger" | "default";
}>();

const displayValue = computed(() => {
    if (props.valueText !== undefined) return props.valueText;
    if (props.value === undefined || props.value === null) return "—";
    const v = props.isPercent ? props.value * 100 : props.value;
    const d = props.digits ?? (props.isPercent ? 1 : 0);
    if (Number.isNaN(v)) return "—";
    return v.toFixed(d);
});

const valueColor = computed(() => {
    switch (props.accent) {
        case "brand":
            return "var(--zlm-brand-600)";
        case "accent":
            return "var(--zlm-accent-600)";
        case "warning":
            return "var(--zlm-warn-600)";
        case "danger":
            return "var(--zlm-danger-600)";
        default:
            return "var(--zlm-text-1)";
    }
});

const trendColor = computed(() => {
    switch (props.trendType) {
        case "up":
            return "var(--zlm-success-600)";
        case "down":
            return "var(--zlm-danger-600)";
        case "danger":
            return "var(--zlm-danger-600)";
        default:
            return "var(--zlm-text-3)";
    }
});
</script>

<template>
    <div class="stat-card">
        <div class="title">{{ title }}</div>
        <div class="value-row">
            <span class="value zlm-numeric" :style="{ color: valueColor }">{{ displayValue }}</span>
            <span v-if="unit" class="unit">{{ unit }}</span>
        </div>
        <div v-if="trend || $slots.spark" class="footer">
            <span v-if="trend" class="trend" :style="{ color: trendColor }">{{ trend }}</span>
            <div v-if="$slots.spark" class="spark">
                <slot name="spark" />
            </div>
        </div>
    </div>
</template>

<style scoped>
@import "@/styles/zlm-tokens.css";

.stat-card {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: var(--zlm-space-4);
    background: var(--zlm-card);
    border-radius: var(--zlm-radius-lg);
    border: 1px solid var(--zlm-border);
    font-family: var(--zlm-font-display);
    transition: border-color var(--zlm-dur-base) var(--zlm-ease-out);
    min-height: 96px;
}

.stat-card:hover {
    border-color: var(--zlm-border-strong);
}

.title {
    font-size: var(--zlm-fs-caption);
    color: var(--zlm-text-3);
    font-weight: var(--zlm-fw-medium);
    letter-spacing: 0.02em;
}

.value-row {
    display: flex;
    align-items: baseline;
    gap: 6px;
    line-height: 1;
}

.value {
    font-size: var(--zlm-fs-display);
    font-weight: var(--zlm-fw-semibold);
    letter-spacing: -0.02em;
}

.unit {
    font-size: 14px;
    color: var(--zlm-text-3);
    font-weight: var(--zlm-fw-medium);
}

.footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--zlm-space-2);
    margin-top: auto;
}

.trend {
    font-size: var(--zlm-fs-caption);
    font-weight: var(--zlm-fw-medium);
    line-height: 1;
}

.spark {
    height: 28px;
    flex: 1;
    min-width: 0;
    display: flex;
    justify-content: flex-end;
    align-items: center;
}
</style>
