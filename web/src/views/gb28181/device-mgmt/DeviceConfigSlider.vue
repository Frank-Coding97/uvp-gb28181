<script setup lang="ts">
/**
 * DeviceConfigSlider - 设备配置参数区的「滑杆 + 数字微调」控件
 *
 * 形态对齐专业 NVR 客户端（海康/大华）的属性面板：
 *   [−] [────●────] [+]  [  50  ] 秒
 *
 * ⛔ 三条别"顺手简化"的口径：
 *   1. ± 是**独立命中区**、固定走 `step`：粗调靠拖，精调靠点。少了它就没法把
 *      0~100000 的码率精确落在某一档上（拖动精度远不够）。
 *   2. 数值框允许直接键入，**失焦/回车时**才 clamp —— 打字中途改写会把
 *      "1080" 在敲到 "1" 时就夹成下界，用户根本打不完。
 *   3. 轨道**不做已填充色**：专业客户端就是细灰轨 + 白圆点。给码率这种
 *      连续量加填充会显示成一条几乎全满的实心条，比不画还难读。
 */
import { computed } from "vue";
import { Minus, Plus } from "@lucide/vue";

const props = withDefaults(
  defineProps<{
    modelValue?: string | number | null;
    min?: number;
    max?: number;
    step?: number;
    unit?: string;
    disabled?: boolean;
    placeholder?: string;
    /** 供无障碍与测试定位；不影响渲染。 */
    label?: string;
  }>(),
  {
    modelValue: "",
    min: 0,
    max: 100,
    step: 1,
    unit: "",
    disabled: false,
    placeholder: "",
    label: ""
  }
);

const emit = defineEmits<{ "update:modelValue": [value: string] }>();

function clamp(value: number): number {
  return Math.min(props.max, Math.max(props.min, value));
}

/** 空值/非法值一律回落到下界，保证滑块位置不因脏数据乱跳。 */
const numericValue = computed(() => {
  const parsed = Number(String(props.modelValue ?? "").trim());
  return Number.isFinite(parsed) ? clamp(parsed) : props.min;
});

const canDec = computed(() => !props.disabled && numericValue.value > props.min);
const canInc = computed(() => !props.disabled && numericValue.value < props.max);

function nudge(direction: -1 | 1) {
  if (props.disabled) return;
  emit("update:modelValue", String(clamp(numericValue.value + direction * props.step)));
}

function onTrackInput(value: number) {
  emit("update:modelValue", String(value));
}

function onInputCommit(valueOrEvent: string | Event) {
  const raw = (typeof valueOrEvent === "string" ? valueOrEvent : (valueOrEvent.target as HTMLInputElement).value).trim();
  if (!raw) {
    emit("update:modelValue", "");
    return;
  }
  const parsed = Number(raw);
  if (!Number.isFinite(parsed)) return;
  const next = String(clamp(parsed));
  if (next !== String(props.modelValue ?? "")) emit("update:modelValue", next);
}
</script>

<template>
  <div class="cfg-slider" :class="{ 'is-disabled': disabled }">
    <button
      type="button"
      class="cfg-slider-step"
      :disabled="!canDec"
      :aria-label="label ? `减小${label}` : '减小'"
      @click="nudge(-1)"
    >
      <Minus :size="11" />
    </button>
    <a-slider
      class="cfg-slider-track"
      :min="min"
      :max="max"
      :step="step"
      :model-value="numericValue"
      :disabled="disabled"
      :aria-label="label || placeholder || '数值调节'"
      :show-tooltip="false"
      @update:model-value="onTrackInput"
    />
    <button
      type="button"
      class="cfg-slider-step"
      :disabled="!canInc"
      :aria-label="label ? `增大${label}` : '增大'"
      @click="nudge(1)"
    >
      <Plus :size="11" />
    </button>
    <span class="cfg-slider-value">
      <a-input
        class="cfg-slider-input"
        inputmode="numeric"
        :model-value="modelValue ?? ''"
        :disabled="disabled"
        :placeholder="placeholder"
        :aria-label="label || undefined"
        @change="onInputCommit"
        @press-enter="onInputCommit"
      />
      <em v-if="unit">{{ unit }}</em>
    </span>
  </div>
</template>

<style scoped lang="scss">
.cfg-slider {
  display: flex;
  flex: 1 1 auto;
  gap: 6px;
  align-items: center;
  width: 100%;
  min-width: 0;
  max-width: 400px;

  &.is-disabled {
    opacity: 0.55;
  }
}

.cfg-slider-step {
  display: inline-flex;
  flex: none;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  padding: 0;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: var(--uvp-dialog-control-bg, #f8fbff);
  border: 1px solid var(--uvp-panel-border, #dbe4f0);
  border-radius: 4px;
  transition:
    color 0.15s,
    border-color 0.15s;

  &:hover:not(:disabled) {
    color: var(--uvp-brand);
    border-color: var(--uvp-brand);
  }

  &:disabled {
    color: var(--uvp-text-tertiary);
    cursor: not-allowed;
    opacity: 0.45;
  }
}

.cfg-slider-track {
  flex: 1 1 auto;
  min-width: 56px;
  margin: 0 8px;
}

.cfg-slider-track :deep(.arco-slider-track) {
  height: 14px;
  background: transparent;
}

.cfg-slider-track :deep(.arco-slider-track::before) {
  height: 3px;
  background: var(--uvp-panel-border, #dfe7f3);
  box-shadow: inset 0 1px 2px rgb(15 23 42 / 12%);
}

.cfg-slider-track :deep(.arco-slider-bar) {
  height: 3px;
  background: transparent;
}

.cfg-slider-track :deep(.arco-slider-btn) {
  width: 14px;
  height: 14px;
}

.cfg-slider-track :deep(.arco-slider-btn::after) {
  width: 14px;
  height: 14px;
  background: var(--uvp-dialog-control-bg, #ffffff);
  border: 2px solid var(--uvp-brand);
  border-radius: 50%;
  box-shadow: 0 1px 3px rgb(15 23 42 / 22%);
}

.cfg-slider-track :deep(.arco-slider-btn:hover::after),
.cfg-slider-track :deep(.arco-slider-btn-active::after) {
  box-shadow: 0 1px 4px rgb(15 23 42 / 28%);
}

.cfg-slider-value {
  display: inline-flex;
  flex: none;
  gap: 3px;
  align-items: center;

  em {
    font-size: 11px;
    font-style: normal;
    color: var(--uvp-text-tertiary);
  }
}

.cfg-slider-input {
  width: 58px;
  height: 20px;
  background: var(--uvp-dialog-control-bg, #ffffff);
  border: 1px solid var(--uvp-panel-border, #dbe4f0);
  border-radius: 4px;
}

.cfg-slider-input :deep(.arco-input-wrapper) {
  padding: 0 5px;
}

.cfg-slider-input :deep(.arco-input) {
  min-width: 0;
  padding: 0;
  font-size: 12px;
  color: var(--uvp-text-primary);
  text-align: right;
}

.cfg-slider-input:focus-within {
  border-color: var(--uvp-brand);
}
</style>
