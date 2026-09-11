<script setup lang="ts">
// 密码输入模板(硬规则 1/5):allow-clear + 明文/遮罩眼睛切换 + 三段强度指示条。
// 强度规则默认复用 sipSetupRules.evaluatePasswordStrength(弱红/合格橙/强绿),
// 可通过 evaluate prop 覆盖(如后端规则同步后的自定义实现)。
import { computed, ref } from "vue";
import { Eye, EyeOff } from "lucide-vue-next";
import { evaluatePasswordStrength } from "@/views/gb28181/sip/sipSetupRules";

const props = withDefaults(
  defineProps<{
    modelValue: string;
    placeholder?: string;
    disabled?: boolean;
    evaluate?: (password: string) => { level: number; label: string };
  }>(),
  { placeholder: "", disabled: false, evaluate: undefined }
);

const emit = defineEmits<{ (e: "update:modelValue", value: string): void; (e: "blur"): void }>();

const visible = ref(false);
const strength = computed(() => (props.evaluate ?? evaluatePasswordStrength)(props.modelValue));
</script>

<template>
  <div class="s-password-field">
    <a-input
      :model-value="modelValue"
      :type="visible ? 'text' : 'password'"
      :placeholder="placeholder"
      :disabled="disabled"
      allow-clear
      autocomplete="new-password"
      @update:model-value="emit('update:modelValue', $event)"
      @blur="emit('blur')"
    >
      <template #suffix>
        <a-button type="text" shape="circle" :title="visible ? '隐藏密码' : '显示密码'" @click="visible = !visible">
          <template #icon><EyeOff v-if="visible" :size="15" /><Eye v-else :size="15" /></template>
        </a-button>
      </template>
    </a-input>
    <div v-if="modelValue" class="s-password-strength" :class="`s-password-strength--l${strength.level}`">
      <div class="s-password-strength__bars">
        <span :class="{ 'is-active': strength.level >= 1 }" />
        <span :class="{ 'is-active': strength.level >= 2 }" />
        <span :class="{ 'is-active': strength.level >= 3 }" />
      </div>
      <span class="s-password-strength__label">强度：{{ strength.label }}</span>
    </div>
  </div>
</template>

<style scoped>
.s-password-field {
  width: 100%;
}

.s-password-field :deep(.arco-input-wrapper) {
  width: 100%;
}

.s-password-strength {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 6px;
}

.s-password-strength__bars {
  display: grid;
  flex: 1;
  max-width: 200px;
  grid-template-columns: repeat(3, 1fr);
  gap: 4px;
}

.s-password-strength__bars span {
  height: 4px;
  background: var(--uvp-shell-muted, #eef4f8);
  border-radius: 2px;
  transition: background 200ms ease;
}

.s-password-strength--l1 .s-password-strength__bars span.is-active {
  background: #d14343;
}

.s-password-strength--l2 .s-password-strength__bars span.is-active {
  background: #d97706;
}

.s-password-strength--l3 .s-password-strength__bars span.is-active {
  background: #059669;
}

.s-password-strength__label {
  color: var(--color-text-2, #4e5969);
  font-size: 12px;
  font-weight: 600;
}

.s-password-strength--l1 .s-password-strength__label {
  color: #d14343;
}

.s-password-strength--l2 .s-password-strength__label {
  color: #d97706;
}

.s-password-strength--l3 .s-password-strength__label {
  color: #059669;
}
</style>
