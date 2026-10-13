<script setup lang="ts">
/**
 * 生命周期状态圆点(8px 实心圆 + 文字)
 *
 * 用法:<LifecycleDot :state="record.state" />
 *
 * ⛔ 文案走 `media_node_state` 字典（默认 在线 / 维护中 / 离线），圆点色走 CSS 变量。
 */
import { computed } from "vue";
import { useDictLabel } from "@/hooks/useDictOptions";
import { DICT_CODE_MEDIA_NODE_STATE, MEDIA_NODE_STATE_LABEL_FALLBACK } from "../../mediaNodeState";

const props = withDefaults(
  defineProps<{
    state: "active" | "maintenance" | "offline" | string;
    showText?: boolean; // 默认 true
  }>(),
  { showText: true }
);

const stateLabel = useDictLabel(DICT_CODE_MEDIA_NODE_STATE, MEDIA_NODE_STATE_LABEL_FALLBACK);

const color = computed(() => {
  switch (props.state) {
    case "active":
      return "var(--zlm-state-active)";
    case "maintenance":
      return "var(--zlm-state-maintenance)";
    case "offline":
      return "var(--zlm-state-offline)";
    default:
      return "var(--zlm-text-3)";
  }
});

const showText = computed(() => props.showText !== false);
</script>

<template>
  <span class="lifecycle-dot" :aria-label="`生命周期：${stateLabel(state)}`">
    <span class="dot" :style="{ background: color }" />
    <span v-if="showText" class="text">{{ stateLabel(state) }}</span>
  </span>
</template>

<style scoped>
.lifecycle-dot {
  display: inline-flex;
  gap: var(--zlm-space-2);
  align-items: center;
  font-family: var(--zlm-font-body);
  font-size: var(--zlm-fs-body);
  color: var(--zlm-text-2);
}

.dot {
  display: inline-block;
  flex-shrink: 0;
  width: 8px;
  height: 8px;
  border-radius: var(--zlm-radius-full);
}

.text {
  line-height: var(--zlm-lh-tight);
}
</style>
