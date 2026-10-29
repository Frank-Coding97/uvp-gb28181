<script setup lang="ts">
import type { FactChannelOption } from "./deviceFactChannel";

/**
 * 设备详情抽屉里四个面板共用的目标通道选择器：
 * 「设备状态」「存储卡」（**查询** DeviceStatus / StorageCard）与
 * 「设备控制」「图像抓拍配置」（**下发** DeviceControl / SnapshotConfig）。
 *
 * ⛔ 它们走的是同一族**通道级**接口（`/channel/:id/...`），所以必须绑**同一份**通道选择 ——
 *    各持一份就会出现"在状态页选了 2 号通道、切到控制页却下发给了 1 号"。
 *
 * ⛔ 只在**选项多于一个**时才出现：单通道设备上再摆一个只能选它自己的下拉，
 *    是纯噪声；一个通道都没有时也不摆（面板自己会说明"暂无通道"）。
 */
const props = defineProps<{
  options: FactChannelOption[];
  modelValue: number | null;
  loading?: boolean;
}>();

const emit = defineEmits<{ (event: "update:modelValue", value: number | null): void }>();

function handleChange(value: unknown) {
  emit("update:modelValue", typeof value === "number" ? value : null);
}

function optionLabel(option: FactChannelOption) {
  return option.online ? option.label : `${option.label}（离线）`;
}
</script>

<template>
  <div v-if="props.options.length > 1" class="fact-channel-picker">
    <!-- ⛔ 措辞必须中性：「设备状态」「存储卡」是**查询**，「设备控制 / 图像抓拍配置」是**下发**。
         写成"查询通道"会让后两页的操作员以为这里的通道只影响查询结果。 -->
    <span class="fact-channel-picker-label">目标通道</span>
    <a-select
      class="fact-channel-picker-select"
      size="small"
      :model-value="props.modelValue ?? undefined"
      :loading="props.loading"
      placeholder="选择通道"
      @change="handleChange"
    >
      <a-option v-for="option in props.options" :key="option.value" :value="option.value">
        {{ optionLabel(option) }}
      </a-option>
    </a-select>
    <span class="fact-channel-picker-hint">查询与下发都按通道登记</span>
  </div>
</template>

<style scoped>
.fact-channel-picker {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.fact-channel-picker-label {
  flex: none;
}
.fact-channel-picker-select {
  min-width: 220px;
  max-width: 320px;
}
.fact-channel-picker-hint {
  flex: none;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
</style>
