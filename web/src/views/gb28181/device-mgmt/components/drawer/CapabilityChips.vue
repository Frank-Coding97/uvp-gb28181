<script setup lang="ts">
/**
 * CapabilityChips — 通道能力 chips(E2)
 *
 * 从 channel.capabilities JSON 渲染 5 个 chip:
 *   audio / h265 / night_vision / alarm_io / recording
 *
 * 数据形态:capabilities 是后端 JSON 列(null 或 '{"audio":true,...}'),
 * 父级传 raw string,本组件做 safe parse + 渲染。
 *
 * 三态:has → 蓝色 / missing → 灰底 / null(无上报) → 整体显 "未上报能力"
 */
import { computed } from "vue";

interface Props {
  capabilities: string | null;
  ptzType?: number;
}

const props = defineProps<Props>();

interface CapMap {
  audio?: boolean;
  h265?: boolean;
  night_vision?: boolean;
  alarm_io?: boolean;
  recording?: boolean;
}

const parsed = computed<CapMap | null>(() => {
  if (!props.capabilities) return null;
  try {
    return JSON.parse(props.capabilities) as CapMap;
  } catch {
    return null;
  }
});

const items = computed(() => {
  const m = parsed.value;
  return [
    { key: "ptz", label: "云台", enabled: (props.ptzType ?? 0) > 0 },
    { key: "audio", label: "音频", enabled: !!m?.audio },
    { key: "h265", label: "H.265", enabled: !!m?.h265 },
    { key: "night_vision", label: "夜视", enabled: !!m?.night_vision },
    { key: "alarm_io", label: "报警 IO", enabled: !!m?.alarm_io },
    { key: "recording", label: "录像", enabled: !!m?.recording },
  ];
});

const hasReported = computed(() => parsed.value !== null || (props.ptzType ?? 0) > 0);
</script>

<template>
  <div class="capability-chips">
    <div v-if="hasReported" class="chip-row">
      <span
        v-for="it in items"
        :key="it.key"
        :class="['cap-chip', { enabled: it.enabled }]"
      >
        <span class="dot" />
        {{ it.label }}
      </span>
    </div>
    <p v-else class="empty">设备未上报能力字段(capabilities = null)。</p>
  </div>
</template>

<style lang="scss" scoped>
.capability-chips {
  .chip-row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  .empty {
    color: var(--color-text-4);
    font-size: var(--font-12);
  }
}

.cap-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 22px;
  padding: 0 8px;
  border-radius: var(--dm-radius-pill);
  background: var(--color-bg-3);
  color: var(--color-text-4);
  font-size: 11px;
  border: 1px solid var(--color-border-2);

  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--color-text-4);
  }

  &.enabled {
    background: var(--primary-fade-08);
    color: var(--primary-5);
    border-color: var(--primary-fade-24);

    .dot {
      background: var(--primary-5);
    }
  }
}
</style>
