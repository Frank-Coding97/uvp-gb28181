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

const props = withDefaults(defineProps<{
    modelValue?: string | number | null;
    min?: number;
    max?: number;
    step?: number;
    unit?: string;
    disabled?: boolean;
    placeholder?: string;
    /** 供无障碍与测试定位；不影响渲染。 */
    label?: string;
}>(), {
    modelValue: "",
    min: 0,
    max: 100,
    step: 1,
    unit: "",
    disabled: false,
    placeholder: "",
    label: ""
});

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

function onTrackInput(event: Event) {
    emit("update:modelValue", (event.target as HTMLInputElement).value);
}

function onInputCommit(event: Event) {
    const el = event.target as HTMLInputElement;
    const raw = el.value.trim();
    if (!raw) {
        el.value = "";
        emit("update:modelValue", "");
        return;
    }
    const parsed = Number(raw);
    if (!Number.isFinite(parsed)) {
        el.value = String(props.modelValue ?? "");
        return;
    }
    const next = String(clamp(parsed));
    el.value = next;
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
        <input
            class="cfg-slider-track"
            type="range"
            :min="min"
            :max="max"
            :step="step"
            :value="numericValue"
            :disabled="disabled"
            :aria-label="label || placeholder || '数值调节'"
            @input="onTrackInput"
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
            <input
                class="cfg-slider-input"
                type="text"
                inputmode="numeric"
                :value="modelValue ?? ''"
                :disabled="disabled"
                :placeholder="placeholder"
                :aria-label="label || undefined"
                @change="onInputCommit"
                @keyup.enter="onInputCommit"
            />
            <em v-if="unit">{{ unit }}</em>
        </span>
    </div>
</template>

<style scoped lang="scss">
.cfg-slider {
    display: flex;
    flex: 1 1 auto;
    align-items: center;
    gap: 6px;
    width: 100%;
    max-width: 400px;
    min-width: 0;

    &.is-disabled {
        opacity: 0.55;
    }
}

.cfg-slider-step {
    flex: none;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
    padding: 0;
    color: var(--uvp-text-secondary);
    background: var(--uvp-dialog-control-bg, #f8fbff);
    border: 1px solid var(--uvp-panel-border, #dbe4f0);
    border-radius: 4px;
    cursor: pointer;
    transition: color 0.15s, border-color 0.15s;

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
    height: 4px;
    margin: 0;
    background: var(--uvp-panel-border, #dfe7f3);
    border-radius: 2px;
    box-shadow: inset 0 1px 2px rgb(15 23 42 / 12%);
    appearance: none;
    cursor: pointer;

    &:disabled {
        cursor: not-allowed;
    }

    &::-webkit-slider-thumb {
        width: 13px;
        height: 13px;
        background: #fff;
        border: 1.5px solid var(--uvp-brand);
        border-radius: 50%;
        box-shadow: 0 1px 3px rgb(15 23 42 / 22%);
        appearance: none;
        cursor: pointer;
    }

    &::-moz-range-thumb {
        width: 11px;
        height: 11px;
        background: #fff;
        border: 1.5px solid var(--uvp-brand);
        border-radius: 50%;
        cursor: pointer;
    }
}

.cfg-slider-value {
    flex: none;
    display: inline-flex;
    align-items: center;
    gap: 3px;

    em {
        color: var(--uvp-text-tertiary);
        font-size: 11px;
        font-style: normal;
    }
}

.cfg-slider-input {
    width: 58px;
    height: 20px;
    padding: 0 5px;
    color: var(--uvp-text-primary);
    font-size: 12px;
    text-align: right;
    background: var(--uvp-dialog-control-bg, #fff);
    border: 1px solid var(--uvp-panel-border, #dbe4f0);
    border-radius: 4px;

    &:focus {
        border-color: var(--uvp-brand);
        outline: none;
    }

    &:disabled {
        cursor: not-allowed;
    }
}
</style>
