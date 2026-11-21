<script setup lang="ts">
import { ChevronDown, Download } from "@lucide/vue";
import type { RecordQueryItem } from "../../gb28181/device-mgmt/api";
import { recordQueryTypeText } from "../../gb28181/device-mgmt/recordQueryState";
import { durationText, formatClock } from "./urlState";

defineProps<{
  records: RecordQueryItem[];
  selectedRecordKey: string | null;
  open: boolean;
  canDownload: boolean;
  downloadPending?: boolean;
}>();

const emit = defineEmits<{
  "update:open": [value: boolean];
  select: [record: RecordQueryItem];
  download: [record: RecordQueryItem];
}>();

function fileSizeText(value: number | null) {
  if (value == null || value < 0) return "";
  if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(2)} GB`;
  if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(1)} MB`;
  if (value >= 1024) return `${Math.round(value / 1024)} KB`;
  return `${value} B`;
}
</script>

<template>
  <section class="segment-drawer" :class="{ 'is-open': open }">
    <button
      type="button"
      class="drawer-head"
      :aria-expanded="open"
      data-testid="segment-drawer-toggle"
      @click="emit('update:open', !open)"
    >
      <ChevronDown :size="14" class="drawer-caret" aria-hidden="true" />
      <span>录像段</span>
      <strong>{{ records.length }}</strong>
      <span v-if="!open && records.length" class="drawer-peek">
        {{ formatClock(records[0]?.startTime) }} 起 · 共
        {{ durationText(records[0]?.startTime, records[records.length - 1]?.endTime) }}
      </span>
    </button>

    <div v-if="open" class="drawer-body" data-testid="segment-drawer-body">
      <div v-if="!records.length" class="drawer-empty">这一天没有录像段</div>
      <button
        v-for="(record, index) in records"
        :key="record.recordKey"
        type="button"
        class="segment-row"
        :class="{ selected: record.recordKey === selectedRecordKey }"
        :data-testid="`segment-row-${index}`"
        @click="emit('select', record)"
      >
        <span class="segment-index">{{ String(index + 1).padStart(2, "0") }}</span>
        <span class="segment-time"> {{ formatClock(record.startTime) }}<i>-</i>{{ formatClock(record.endTime) }} </span>
        <span class="segment-duration">{{ durationText(record.startTime, record.endTime) }}</span>
        <span class="segment-type" :class="`type-${record.type || 'unknown'}`">{{ recordQueryTypeText(record.type) }}</span>
        <span class="segment-size">{{ fileSizeText(record.fileSize) }}</span>
        <a-button
          type="text"
          size="small"
          class="segment-archive"
          :disabled="!canDownload || downloadPending"
          aria-label="归档到服务器"
          title="把这一段存到平台（录像缓存）"
          @click.stop="emit('download', record)"
        >
          <template #icon><Download :size="14" /></template>
        </a-button>
      </button>
    </div>
  </section>
</template>

<style scoped>
.segment-drawer {
  background: var(--uvp-card, transparent);
  border-top: 1px solid var(--uvp-panel-border);
}
.drawer-head {
  display: flex;
  gap: 8px;
  align-items: center;
  width: 100%;
  height: 30px;
  padding: 0 14px;
  font: inherit;
  font-size: 12px;
  color: var(--uvp-text-secondary);
  text-align: left;
  cursor: pointer;
  background: transparent;
  border: 0;
}
.drawer-head:hover {
  background: var(--uvp-sidebar-active-bg);
}
.drawer-head strong {
  font-weight: 500;
  color: var(--uvp-text-primary);
}
.drawer-caret {
  transition: transform 0.15s ease;
}
.segment-drawer.is-open .drawer-caret {
  transform: rotate(180deg);
}
.drawer-peek {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}
.drawer-body {
  max-height: 168px;
  overflow-y: auto;
  border-top: 1px solid var(--uvp-panel-border);
}
.drawer-empty {
  padding: 16px;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
  text-align: center;
}
.segment-row {
  display: grid;
  grid-template-columns: 26px 1fr 76px 64px 76px 34px;
  gap: 8px;
  align-items: center;
  width: 100%;
  height: 30px;
  padding: 0 14px 0 8px;
  font: inherit;
  font-size: 12px;
  color: var(--uvp-text-secondary);
  text-align: left;
  cursor: pointer;
  background: transparent;
  border: 0;
}
.segment-row:hover {
  background: var(--uvp-sidebar-active-bg);
}
.segment-row.selected {
  background: var(--uvp-sidebar-active-bg);
  box-shadow: inset 2px 0 0 var(--uvp-brand);
}
.segment-index,
.segment-duration,
.segment-size {
  font-family: var(--uvp-font-mono, ui-monospace, monospace);
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.segment-time {
  font-family: var(--uvp-font-mono, ui-monospace, monospace);
  font-size: 11px;
  color: var(--uvp-text-primary);
}
.segment-time i {
  padding: 0 4px;
  font-style: normal;
  color: var(--uvp-text-tertiary);
}
.segment-type {
  font-size: 11px;
}
.segment-type.type-time {
  color: #378add;
}
.segment-type.type-alarm {
  color: #e24b4a;
}
.segment-type.type-manual {
  color: #b66b12;
}
.segment-type.type-unknown {
  color: var(--uvp-text-tertiary);
}
</style>
