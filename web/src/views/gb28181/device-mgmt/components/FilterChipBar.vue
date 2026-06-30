<script setup lang="ts">
/**
 * FilterChipBar — 当前筛选条件 chip 条(C4)
 *
 * 从 URL query 解析所有 filter 参数,渲染为 chip;
 * chip × 关闭 → 移除该 filter;全部清除 → 清空所有 filter
 */
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";

const route = useRoute();
const router = useRouter();

interface FilterChip {
  key: string;
  label: string;
  value: string;
}

const FILTER_KEYS: Record<string, string> = {
  status: "状态",
  vendor: "厂商",
  q: "关键字",
  ptz: "云台",
  nodeId: "节点",
};

const chips = computed<FilterChip[]>(() => {
  const out: FilterChip[] = [];
  for (const [key, label] of Object.entries(FILTER_KEYS)) {
    const v = route.query[key];
    if (v && typeof v === "string" && v.trim()) {
      let display = v;
      if (key === "status") display = v === "online" ? "在线" : "离线";
      if (key === "ptz") display = v === "1" ? "有云台" : "无云台";
      out.push({ key, label, value: display });
    }
  }
  return out;
});

const hasFilters = computed(() => chips.value.length > 0);

function removeChip(key: string) {
  const next = { ...route.query };
  delete next[key];
  next.page = "1";
  router.replace({ query: next });
}

function clearAll() {
  const next = { ...route.query };
  for (const k of Object.keys(FILTER_KEYS)) {
    delete next[k];
  }
  next.page = "1";
  router.replace({ query: next });
}
</script>

<template>
  <div v-if="hasFilters" class="filter-chip-bar">
    <span
      v-for="chip in chips"
      :key="chip.key"
      class="chip"
    >
      <span class="chip-label">{{ chip.label }}:</span>
      <span class="chip-value">{{ chip.value }}</span>
      <button class="chip-close" type="button" @click="removeChip(chip.key)" title="移除">
        <svg viewBox="0 0 24 24" width="10" height="10" fill="none" stroke="currentColor" stroke-width="3">
          <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
        </svg>
      </button>
    </span>
    <button class="clear-all" type="button" @click="clearAll">清除筛选</button>
  </div>
</template>

<style lang="scss" scoped>
.filter-chip-bar {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) 0;
  flex-wrap: wrap;
}

.chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 24px;
  padding: 0 8px;
  background: var(--primary-fade-08);
  border: 1px solid var(--primary-fade-24);
  border-radius: var(--dm-radius-pill);
  font-size: 11px;
  color: var(--primary-5);

  .chip-label {
    color: var(--color-text-3);
  }

  .chip-value {
    font-weight: 500;
  }

  .chip-close {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 14px;
    height: 14px;
    background: transparent;
    border: 0;
    border-radius: 50%;
    color: var(--primary-5);
    cursor: pointer;
    margin-left: 2px;
    transition: background var(--duration-fast) var(--ease-out);

    &:hover {
      background: var(--primary-fade-16);
    }
  }
}

.clear-all {
  appearance: none;
  border: 0;
  background: transparent;
  color: var(--color-text-4);
  font-size: 11px;
  cursor: pointer;
  padding: 2px 6px;
  border-radius: var(--dm-radius-1);
  transition: color var(--duration-fast) var(--ease-out);

  &:hover {
    color: var(--color-text-2);
  }
}
</style>
