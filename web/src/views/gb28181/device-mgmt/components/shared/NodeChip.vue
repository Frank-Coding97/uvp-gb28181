<script setup lang="ts">
/**
 * NodeChip — 节点类型彩色 chip(目录树 / 列表 / 卡片 / 抽屉 通用)
 *
 * 5 色对应 spec §8.6:
 *   civil_code   淡紫
 *   biz_group    青色
 *   virtual_org  橙色(anomaly 同色)
 *   device       蓝色
 *   channel      绿色(在线)/ 灰色(离线)
 */
import { computed } from "vue";
import type { NodeType } from "../../api/catalog";

interface Props {
  type: NodeType;
  online?: boolean;
}

const props = withDefaults(defineProps<Props>(), { online: true });

const variantClass = computed(() => {
  if (props.type === "channel") {
    return props.online ? "t-channel-on" : "t-channel-off";
  }
  if (props.type === "civil_code") return "t-civil";
  if (props.type === "biz_group") return "t-biz";
  if (props.type === "virtual_org") return "t-virtual";
  return "t-device";
});
</script>

<template>
  <span :class="['node-chip', variantClass]">
    <slot>
      <!-- 默认 fallback 字符,实际用 SVG 替换 -->
      <span v-if="type === 'civil_code'">行</span>
      <span v-else-if="type === 'biz_group'">业</span>
      <span v-else-if="type === 'virtual_org'">虚</span>
      <span v-else-if="type === 'device'">设</span>
      <span v-else>通</span>
    </slot>
  </span>
</template>

<style lang="scss" scoped>
.node-chip {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  border-radius: var(--dm-radius-1);
  font-size: 10px;
  font-weight: 600;
  flex-shrink: 0;

  &.t-civil {
    background: rgba(167, 139, 250, 0.12);
    color: var(--node-civil);
  }
  &.t-biz {
    background: rgba(6, 182, 212, 0.12);
    color: var(--node-biz);
  }
  &.t-virtual {
    background: rgba(251, 146, 60, 0.12);
    color: var(--node-virtual);
  }
  &.t-device {
    background: var(--primary-fade-16);
    color: var(--node-device);
  }
  &.t-channel-on {
    background: var(--status-online-fade);
    color: var(--node-channel-on);
  }
  &.t-channel-off {
    background: var(--status-offline-fade);
    color: var(--node-channel-off);
  }
}
</style>
