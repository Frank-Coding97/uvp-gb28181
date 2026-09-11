<script setup lang="ts">
// 数值输入模板(硬规则 2):a-input 实现的数字字段。
// - 不做静默 clamp:超范围/非法输入保留原文并显示错误,绝不悄悄改成边界值。
// - 只有合法值才回写 v-model(number),非法期间保留最后一次合法值。
// - 校验触发时机为 blur(硬规则 4);保存前可通过模板 ref 读取 error 兜底。
// 用法:<s-number-field ref="portField" v-model="form.port" :min="1" :max="65535" required />
import { computed, ref, watch } from "vue";
import { parseNumberText, validateNumberText } from "./number-text";

const props = withDefaults(
  defineProps<{
    modelValue?: number | null;
    min?: number;
    max?: number;
    integer?: boolean;
    required?: boolean;
    placeholder?: string;
    disabled?: boolean;
    allowClear?: boolean;
  }>(),
  { modelValue: null, min: 1, max: Number.MAX_SAFE_INTEGER, integer: true, required: false, placeholder: "", disabled: false, allowClear: true }
);

const emit = defineEmits<{ (e: "update:modelValue", value: number | null): void; (e: "blur"): void }>();

const text = ref(props.modelValue === null || props.modelValue === undefined ? "" : String(props.modelValue));
const touched = ref(false);

watch(
  () => props.modelValue,
  value => {
    const current = parseNumberText(text.value, props.integer);
    if (current !== (value ?? null)) text.value = value === null || value === undefined ? "" : String(value);
  }
);

const inRange = (parsed: number) => parsed >= props.min && parsed <= props.max;

const error = computed(() => validateNumberText(text.value, { required: props.required, integer: props.integer, min: props.min, max: props.max }));
const shownError = computed(() => (touched.value ? error.value : ""));

function handleInput(value: string) {
  text.value = value;
  const parsed = parseNumberText(value, props.integer);
  if (parsed !== null && inRange(parsed)) emit("update:modelValue", parsed);
}

function handleBlur() {
  touched.value = true;
  const parsed = parseNumberText(text.value, props.integer);
  if (parsed !== null) {
    text.value = String(parsed);
    if (inRange(parsed)) emit("update:modelValue", parsed);
  }
  emit("blur");
}

defineExpose({ error });
</script>

<template>
  <div class="s-number-field" :class="{ 'is-error': shownError }">
    <a-input
      :model-value="text"
      :placeholder="placeholder"
      :disabled="disabled"
      :allow-clear="allowClear"
      inputmode="numeric"
      @update:model-value="handleInput"
      @blur="handleBlur"
    />
    <div v-if="shownError" class="s-number-field__error">{{ shownError }}</div>
  </div>
</template>

<style scoped>
.s-number-field {
  width: 100%;
}

.s-number-field :deep(.arco-input-wrapper) {
  width: 100%;
}

.s-number-field.is-error :deep(.arco-input-wrapper) {
  background: #fff7f7;
  box-shadow: inset 0 0 0 1px rgb(209 67 67 / 55%);
}

.s-number-field__error {
  margin-top: 4px;
  color: #d14343;
  font-size: 12px;
  line-height: 18px;
}
</style>
