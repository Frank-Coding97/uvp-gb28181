<script setup lang="ts">
// 定长字段位数指示模板(硬规则 3):输入框 #suffix 内展示 n/total,达标变绿加粗。
// 用法:<template #suffix><s-counter-suffix :value="form.upstreamServerId.length" :total="20" /></template>
defineProps<{ value: number; total: number }>();
</script>

<template>
  <span class="s-counter-suffix" :class="{ 'is-complete': total > 0 && value >= total }">{{ value }}/{{ total }}</span>
</template>

<style scoped>
.s-counter-suffix {
  padding: 0 4px;
  color: var(--color-text-3, #86909c);
  font-size: 12px;
  font-family: "SFMono-Regular", Consolas, Menlo, monospace;
  letter-spacing: 0.5px;
  transition: color 160ms ease;
}

.s-counter-suffix.is-complete {
  color: #059669;
  font-weight: 600;
}
</style>
