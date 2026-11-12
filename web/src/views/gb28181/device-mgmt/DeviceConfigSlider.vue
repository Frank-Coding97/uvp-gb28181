<script setup lang="ts">
/**
 * DeviceConfigSlider - 设备配置参数区的「滑杆 + 数字微调」控件
 *
 * 形态对齐专业 NVR 客户端（海康/大华）的属性面板：
 *   [−] [────●────] [+]  [  50  ] 秒
 *
 * ⛔ 四条别"顺手简化"的口径：
 *   1. ± 是**独立命中区**、固定走 `step`：粗调靠拖，精调靠点。少了它就没法把
 *      0~100000 的码率精确落在某一档上（拖动精度远不够）。
 *   2. 数值框允许直接键入，**失焦/回车时**才 clamp —— 打字中途改写会把
 *      "1080" 在敲到 "1" 时就夹成下界，用户根本打不完。
 *   3. 轨道**不做已填充色**：专业客户端就是细灰轨 + 白圆点。给码率这种
 *      连续量加填充会显示成一条几乎全满的实心条，比不画还难读。
 *   4. ⛔⛔ **± 必须显式写 `min-width`**（2026-11-12 踩）：全局按钮真源
 *      `styles/uvp-buttons.scss` 给 `.arco-btn` 设了 `min-width: var(--uvp-btn-min-w-md)`
 *      = **72px**。这里只写了 `width: 20px`，而 `min-width` 恒压过 `width`
 *      ⇒ 两个 ± 被撑成 72×20 的扁长条，把中间的滑轨和右边的数值框一起挤扁
 *      （截图表现：滑轨被拉成一条长线、数值框只剩 42px 看不出数字）。
 *      同理 `.cfg-slider-input` 也不能窄过 72px，否则「86400」+ 清除图标会互相压。
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
    <a-button
      html-type="button"
      type="outline"
      class="cfg-slider-step"
      :disabled="!canDec"
      :aria-label="label ? `减小${label}` : '减小'"
      @click="nudge(-1)"
    >
      <Minus :size="12" />
    </a-button>
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
    <a-button
      html-type="button"
      type="outline"
      class="cfg-slider-step"
      :disabled="!canInc"
      :aria-label="label ? `增大${label}` : '增大'"
      @click="nudge(1)"
    >
      <Plus :size="12" />
    </a-button>
    <span class="cfg-slider-value">
      <a-input
        class="cfg-slider-input"
        allow-clear
        inputmode="numeric"
        :model-value="String(modelValue ?? '')"
        :disabled="disabled"
        :placeholder="placeholder"
        :aria-label="label || undefined"
        @update:model-value="$emit('update:modelValue', $event)"
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

  /* ⛔ 不是 `1 1 auto`：滑杆**不该吃满整行**。上限 360px，窄容器里再自动收缩。 */
  flex: 1 1 220px;
  gap: 6px;
  align-items: center;
  min-width: 0;
  max-width: 360px;

  &.is-disabled {
    color: var(--uvp-text-disabled);

    .cfg-slider-step.arco-btn[type="button"] {
      color: var(--uvp-text-disabled);
      background: var(--uvp-shell-muted);
      border-color: var(--uvp-panel-border, #dbe4f0);
    }

    :deep(.cfg-slider-input.arco-input-disabled) {
      color: var(--uvp-text-disabled);
      background: var(--uvp-shell-muted);
      border-color: var(--uvp-panel-border, #dbe4f0);
    }

    .cfg-slider-track :deep(.arco-slider-btn::after) {
      background: var(--uvp-shell-muted);
      border-color: var(--uvp-text-disabled);
      box-shadow: none;
    }
  }
}

.cfg-slider-step.arco-btn[type="button"] {
  display: inline-flex;
  flex: none;
  align-items: center;
  justify-content: center;

  /* ⛔ min-width 必须与 width 同写：全局 `.arco-btn` 的 72px min-width 会压过 width。 */
  width: 24px;
  min-width: 24px;
  height: 24px;
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
    color: var(--uvp-text-disabled);
    cursor: not-allowed;
    background: var(--uvp-shell-muted);
    opacity: 1;
  }
}

.cfg-slider-track {
  flex: 1 1 auto;

  /* 44px 是"看得见轨道形状"的下限，但**必须允许继续收缩**：播放控制台侧栏只有
   * 360px 宽，扣掉标签列后控制区 ≈180px，± 与数值框已占 130 多，轨道只能拿剩下的。 */
  min-width: 24px;
  margin: 0 6px;
}

.cfg-slider-track :deep(.arco-slider-track) {
  height: 14px;
  background: transparent;
}

.cfg-slider-track :deep(.arco-slider-track::before) {
  height: 4px;
  background: var(--uvp-panel-border, #dfe7f3);
  box-shadow: inset 0 1px 2px rgb(15 23 42 / 12%);
}

.cfg-slider-track :deep(.arco-slider-bar) {
  height: 4px;
  background: transparent;
}

.cfg-slider-track :deep(.arco-slider-btn) {
  width: 16px;
  height: 16px;
}

.cfg-slider-track :deep(.arco-slider-btn::after) {
  width: 16px;
  height: 16px;
  background: var(--uvp-dialog-control-bg, #ffffff);
  border: 2px solid var(--uvp-brand);
  border-radius: 50%;
  box-shadow: 0 1px 3px rgb(15 23 42 / 22%);
}

.cfg-slider-track :deep(.arco-slider-btn:hover::after),
.cfg-slider-track :deep(.arco-slider-btn-active::after) {
  box-shadow: 0 1px 4px rgb(15 23 42 / 28%);
}

.cfg-slider-track :deep(.arco-slider-btn:focus-visible::after) {
  box-shadow: 0 0 0 3px var(--uvp-brand-soft, rgb(37 99 235 / 22%));
}

.cfg-slider-track :deep(.arco-slider-track-disabled .arco-slider-btn::after),
.cfg-slider-track :deep(.arco-slider-track-disabled .arco-slider-btn:hover::after),
.cfg-slider-track :deep(.arco-slider-track-disabled .arco-slider-btn-active::after) {
  box-shadow: none;
  transform: none;
}

.cfg-slider-value {
  display: inline-flex;
  flex: none;
  gap: 5px;
  align-items: center;

  em {
    font-size: 12px;
    font-style: normal;
    color: var(--uvp-text-tertiary);
  }
}

/* ⛔ 76px 是下限不是随意值：「86400」+ allow-clear 图标 + 左右内边距≈74px，
 *    再窄就必然互相遮挡（原 42px 时只剩半个字符）。 */
.cfg-slider-input {
  width: 76px;
  height: 24px;
  background: var(--uvp-dialog-control-bg, #ffffff);
  border: 1px solid var(--uvp-panel-border, #dbe4f0);
  border-radius: 4px;
}

.cfg-slider-input :deep(.arco-input-wrapper) {
  padding: 0 7px;
}

.cfg-slider-input :deep(.arco-input) {
  min-width: 0;
  padding: 0;
  font-size: 12.5px;
  font-variant-numeric: tabular-nums;
  color: var(--uvp-text-primary);
  text-align: right;
}

.cfg-slider-input :deep(.arco-input::placeholder) {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
  text-align: right;
}

.cfg-slider-input:focus-within {
  border-color: var(--uvp-brand);
}

.cfg-slider-input :deep(.arco-input:disabled) {
  color: var(--uvp-text-secondary);
}
</style>
