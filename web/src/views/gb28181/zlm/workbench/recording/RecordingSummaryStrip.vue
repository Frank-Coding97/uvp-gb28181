<script setup lang="ts">
interface RecordingSummaryItem {
  key: string;
  label: string;
  value: number | null;
  note: string;
  tone?: "default" | "success" | "warning";
}

defineProps<{ items: readonly RecordingSummaryItem[] }>();
</script>

<template>
  <section class="recording-summary" aria-label="录制中心真实统计">
    <article v-for="item in items" :key="item.key" class="recording-summary__item" :data-tone="item.tone || 'default'">
      <span>{{ item.label }}</span>
      <strong>{{ item.value === null ? "—" : item.value }}</strong>
      <small>{{ item.value === null ? "未加载 / 无权限" : item.note }}</small>
    </article>
  </section>
</template>

<style scoped>
.recording-summary { display: grid; flex: none; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px; }
.recording-summary__item { position: relative; min-width: 0; padding: 12px 14px; overflow: hidden; background: linear-gradient(145deg, var(--zlm-card), var(--zlm-fill-1)); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); box-shadow: var(--uvp-panel-shadow); }
.recording-summary__item::before { position: absolute; top: 0; bottom: 0; left: 0; width: 3px; background: var(--zlm-brand-500); content: ""; }
.recording-summary__item[data-tone="success"]::before { background: var(--zlm-success-500); }
.recording-summary__item[data-tone="warning"]::before { background: var(--zlm-warn-500); }
.recording-summary__item span, .recording-summary__item small { display: block; overflow: hidden; color: var(--zlm-text-3); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.recording-summary__item strong { display: block; margin: 4px 0 2px; color: var(--zlm-text-1); font-family: var(--zlm-font-mono); font-size: 21px; line-height: 1.15; }
.recording-summary__item[data-tone="warning"] strong { color: var(--zlm-warn-600); }
@media (max-width: 900px) { .recording-summary { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 520px) { .recording-summary { grid-template-columns: 1fr; } }
</style>
