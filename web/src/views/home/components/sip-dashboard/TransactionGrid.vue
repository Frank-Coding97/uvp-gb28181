<template>
  <div class="tx-section">
    <div class="tx-section__title">协议事务 · 今日</div>
    <div class="tx-grid">
      <button
        v-for="cell in cells"
        :key="cell.kind"
        type="button"
        class="tx-cell"
        :class="{ 'tx-cell--alert': cell.alert }"
        @click="emit('cellClick', cell.kind)"
      >
        <div class="tx-cell__head">
          <div class="tx-cell__icon">{{ cell.iconAbbr }}</div>
        </div>
        <div class="tx-cell__name">
          {{ cell.labelZh }}
          <span class="tx-cell__en">{{ cell.labelEn }}</span>
        </div>
        <div class="tx-cell__stats">
          <div class="tx-cell__count">{{ formatCount(cell.todayCount) }}</div>
          <div class="tx-cell__rate" :class="rateClass(cell)">
            {{ rateText(cell) }}
          </div>
        </div>
      </button>
      <!-- 不足 8 格补占位,布局稳定 -->
      <div v-for="i in placeholderCount" :key="`p${i}`" class="tx-cell tx-cell--placeholder" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import type { TransactionStat } from "@/api/gb28181";

interface Props {
  transactions: TransactionStat[];
}
const props = defineProps<Props>();
const emit = defineEmits<{
  (e: "cellClick", kind: string): void;
}>();

// 8 类事务的图标缩写(2 字母)
const iconMap: Record<string, string> = {
  REGISTER: "RG",
  KEEPALIVE: "KA",
  CATALOG: "CT",
  INVITE: "IV",
  RECORD: "RI",
  ALARM: "AL",
  PTZ: "PT",
  BYE: "BY"
};

interface Cell extends TransactionStat {
  iconAbbr: string;
}

const cells = computed((): Cell[] =>
  props.transactions.map((t) => ({
    ...t,
    iconAbbr: iconMap[t.kind] ?? "??"
  }))
);

const placeholderCount = computed((): number => Math.max(0, 8 - cells.value.length));

function formatCount(n: number): string {
  if (n === 0) return "—";
  return n.toLocaleString("en-US");
}

function rateText(cell: TransactionStat): string {
  if (cell.todayCount === 0) return "—";
  return `${(cell.successRate * 100).toFixed(1)}%`;
}

function rateClass(cell: TransactionStat): string {
  if (cell.todayCount === 0) return "tx-cell__rate--idle";
  if (cell.successRate < 0.95) return "tx-cell__rate--bad";
  if (cell.successRate < 0.99) return "tx-cell__rate--warn";
  return "";
}
</script>

<style scoped lang="scss">
.tx-section {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.tx-section__title {
  font-size: 11px;
  font-weight: 600;
  color: var(--uvp-text-tertiary);
  letter-spacing: 0;
}

.tx-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 8px;
}

.tx-cell {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-height: 72px;
  padding: 9px 10px;
  overflow: hidden;
  font: inherit;
  color: inherit;
  text-align: left;
  appearance: none;
  cursor: pointer;
  background: var(--dashboard-surface-muted, var(--uvp-list-toolbar-bg));
  border: 1px solid var(--uvp-panel-border);
  border-radius: 6px;
  transition:
    background-color 0.18s ease,
    border-color 0.18s ease,
    box-shadow 0.18s ease;
}

.tx-cell:hover:not(.tx-cell--placeholder) {
  background: var(--uvp-panel-bg);
  border-color: color-mix(in srgb, var(--uvp-brand) 38%, var(--uvp-panel-border));
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--uvp-brand) 8%, transparent);
}

.tx-cell:focus-visible {
  outline: none;
  border-color: var(--uvp-brand);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--uvp-brand) 12%, transparent);
}

.tx-cell--alert {
  background: var(--uvp-danger-soft);
  border-color: var(--uvp-danger-border);
}

.tx-cell--alert::before {
  position: absolute;
  top: 0;
  right: 0;
  left: 0;
  height: 2px;
  content: "";
  background: var(--uvp-danger);
}

.tx-cell--placeholder {
  cursor: default;
  background: transparent;
  border-color: var(--uvp-panel-border);
  border-style: dashed;
  opacity: 0.55;
}

.tx-cell__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.tx-cell__icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  font-size: 11px;
  font-weight: 600;
  color: var(--uvp-brand);
  letter-spacing: 0;
  background: var(--uvp-brand-soft);
  border-radius: 4px;
}

.tx-cell--alert .tx-cell__icon {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
}

.tx-cell__name {
  font-size: 13px;
  font-weight: 500;
  color: var(--uvp-text-primary);
}

.tx-cell__en {
  margin-left: 4px;
  font-size: 10px;
  font-weight: 400;
  color: var(--uvp-text-tertiary);
}

.tx-cell__stats {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin-top: 2px;
}

.tx-cell__count {
  font-size: 18px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  color: var(--uvp-text-primary);
}

.tx-cell__rate {
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  color: var(--uvp-brand-cyan);
}

.tx-cell__rate--warn {
  color: var(--uvp-warning);
}

.tx-cell__rate--bad {
  color: var(--uvp-danger);
}

.tx-cell__rate--idle {
  color: var(--uvp-text-tertiary);
}
</style>
