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
        @click="emit('cell-click', cell.kind)"
      >
        <div class="tx-cell__head">
          <div class="tx-cell__icon">{{ cell.iconAbbr }}</div>
          <div class="tx-cell__trend" :class="trendClass(cell.trendPct)">
            {{ trendText(cell.trendPct) }}
          </div>
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
  (e: "cell-click", kind: string): void;
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

function trendText(pct: number): string {
  if (Math.abs(pct) < 0.5) return "— 0%";
  if (pct > 0) return `↑ ${pct.toFixed(1)}%`;
  return `↓ ${Math.abs(pct).toFixed(1)}%`;
}

function trendClass(pct: number): string {
  if (Math.abs(pct) < 0.5) return "tx-cell__trend--neutral";
  return pct > 0 ? "tx-cell__trend--up" : "tx-cell__trend--down";
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
  gap: 10px;
}

.tx-section__title {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
  letter-spacing: 0.5px;
}

.tx-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 10px;
}

.tx-cell {
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 10px;
  padding: 10px 12px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  font: inherit;
  color: inherit;
  cursor: pointer;
  text-align: left;
  appearance: none;
  transition: all 0.15s ease;
  position: relative;
  overflow: hidden;
  min-height: 76px;
}

.tx-cell:hover:not(.tx-cell--placeholder) {
  border-color: var(--uvp-brand);
  background: var(--uvp-panel-bg);
  box-shadow: 0 8px 18px rgb(37 99 235 / 10%);
  transform: translateY(-1px);
}

.tx-cell:focus-visible {
  outline: none;
  border-color: var(--uvp-brand);
  box-shadow: 0 0 0 3px rgb(37 99 235 / 12%);
}

.tx-cell--alert {
  border-color: var(--uvp-danger-border);
  background: var(--uvp-danger-soft);
}

.tx-cell--alert::before {
  content: "";
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 2px;
  background: var(--uvp-danger);
}

.tx-cell--placeholder {
  cursor: default;
  background: var(--uvp-list-toolbar-bg);
  border-style: dashed;
  border-color: var(--uvp-panel-border);
}

.tx-cell__head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.tx-cell__icon {
  width: 22px;
  height: 22px;
  border-radius: 4px;
  background: var(--uvp-brand-soft);
  color: var(--uvp-brand);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.3px;
}

.tx-cell--alert .tx-cell__icon {
  background: var(--uvp-danger-soft);
  color: var(--uvp-danger);
}

.tx-cell__trend {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}

.tx-cell__trend--up {
  color: var(--uvp-brand-cyan);
}

.tx-cell__trend--down {
  color: var(--uvp-danger);
}

.tx-cell__trend--neutral {
  color: var(--uvp-text-tertiary);
}

.tx-cell__name {
  font-size: 13px;
  color: var(--uvp-text-primary);
  font-weight: 500;
}

.tx-cell__en {
  color: var(--uvp-text-tertiary);
  font-size: 10px;
  font-weight: 400;
  margin-left: 4px;
}

.tx-cell__stats {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
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
  color: var(--uvp-brand-cyan);
  font-variant-numeric: tabular-nums;
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
