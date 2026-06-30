<script setup lang="ts">
/**
 * OnlineTimeline — 24h 在线时序条(E2)
 *
 * 48 个 30min slot,后端 GET /channel/:id/timeline 已返回。
 * Phase 1 后端只填当前状态,所有 slot 同色;Phase 2 真历史数据后此处不变。
 *
 * 颜色:online 绿 / offline 灰 / warning 琥珀
 * hover 显示时段
 */
import { computed } from "vue";
import type { TimelineSlot } from "../../api/device";

interface Props {
  slots: TimelineSlot[];
  phase1Simplified?: boolean;
}

const props = defineProps<Props>();

function fmtTime(s: string): string {
  try {
    const d = new Date(s);
    return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  } catch {
    return "—";
  }
}

const axisTicks = computed(() => {
  if (props.slots.length === 0) return [];
  const first = props.slots[0];
  const last = props.slots[props.slots.length - 1];
  return [
    { pos: 0, label: fmtTime(first.start) },
    { pos: 25, label: fmtTime(props.slots[Math.floor(props.slots.length / 4)].start) },
    { pos: 50, label: fmtTime(props.slots[Math.floor(props.slots.length / 2)].start) },
    { pos: 75, label: fmtTime(props.slots[Math.floor((3 * props.slots.length) / 4)].start) },
    { pos: 100, label: fmtTime(last.end) },
  ];
});
</script>

<template>
  <div class="online-timeline">
    <div class="bar">
      <span
        v-for="(s, i) in slots"
        :key="i"
        :class="['slot', s.status]"
        :title="`${fmtTime(s.start)} - ${fmtTime(s.end)} ${s.status}`"
      />
    </div>
    <div class="axis">
      <span
        v-for="t in axisTicks"
        :key="t.pos"
        class="tick"
        :style="{ left: `${t.pos}%` }"
      >
        {{ t.label }}
      </span>
    </div>
    <p v-if="phase1Simplified" class="hint">
      ⚠ Phase 1 简化:全部 slot 用当前状态填,真历史时序 Phase 2 接入
    </p>
  </div>
</template>

<style lang="scss" scoped>
.online-timeline {
  .bar {
    display: flex;
    gap: 1px;
    height: 18px;
    background: var(--color-bg-3);
    border-radius: var(--dm-radius-1);
    overflow: hidden;
  }

  .slot {
    flex: 1;
    min-width: 2px;

    &.online {
      background: var(--status-online);
    }
    &.offline {
      background: var(--status-offline);
    }
    &.warning {
      background: var(--status-warning);
    }
  }

  .axis {
    position: relative;
    height: 16px;
    margin-top: 4px;
    font-size: 10px;
    color: var(--color-text-4);
    font-family: var(--font-mono);

    .tick {
      position: absolute;
      top: 0;
      transform: translateX(-50%);

      &:first-child {
        transform: none;
      }

      &:last-child {
        transform: translateX(-100%);
      }
    }
  }

  .hint {
    margin-top: var(--space-2);
    font-size: 11px;
    color: var(--color-text-4);
  }
}
</style>
