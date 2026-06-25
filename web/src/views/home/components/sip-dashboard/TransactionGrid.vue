<template>
  <div class="tx-section">
    <div class="tx-section__title">协议事务 · 今日</div>
    <div class="tx-grid">
      <div
        v-for="cell in cells"
        :key="cell.kind"
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
      </div>
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
  gap: 8px;
}

.tx-section__title {
  font-size: 11px;
  color: #5a6478;
  letter-spacing: 1px;
  text-transform: uppercase;
}

.tx-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 8px;
}

.tx-cell {
  background: #1a2235;
  border: 1px solid #1f2a40;
  border-radius: 8px;
  padding: 10px 12px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  cursor: pointer;
  transition: transform 0.1s, border-color 0.1s;
  position: relative;
  overflow: hidden;
  min-height: 76px;
}

.tx-cell:hover:not(.tx-cell--placeholder) {
  border-color: #4d8eff;
  transform: translateY(-1px);
}

.tx-cell--alert {
  border-color: rgba(255, 90, 95, 0.4);
  background: rgba(255, 90, 95, 0.08);
}

.tx-cell--alert::before {
  content: "";
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 2px;
  background: #ff5a5f;
}

.tx-cell--placeholder {
  cursor: default;
  background: rgba(26, 34, 53, 0.4);
}

.tx-cell__head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.tx-cell__icon {
  width: 20px;
  height: 20px;
  border-radius: 4px;
  background: rgba(77, 142, 255, 0.15);
  color: #4d8eff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  font-weight: 600;
}

.tx-cell--alert .tx-cell__icon {
  background: rgba(255, 90, 95, 0.15);
  color: #ff5a5f;
}

.tx-cell__trend {
  font-size: 10px;
  color: #5a6478;
}

.tx-cell__trend--up {
  color: #3ddc84;
}

.tx-cell__trend--down {
  color: #ff5a5f;
}

.tx-cell__trend--neutral {
  color: #5a6478;
}

.tx-cell__name {
  font-size: 12px;
  color: #e6edf7;
  font-weight: 500;
}

.tx-cell__en {
  color: #5a6478;
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
  font-size: 16px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  color: #e6edf7;
}

.tx-cell__rate {
  font-size: 11px;
  color: #3ddc84;
  font-variant-numeric: tabular-nums;
}

.tx-cell__rate--warn {
  color: #ffb547;
}

.tx-cell__rate--bad {
  color: #ff5a5f;
}

.tx-cell__rate--idle {
  color: #5a6478;
}
</style>
