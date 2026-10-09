<template>
  <a-modal
    :visible="true"
    title="修改初始密码"
    modal-class="uvp-system-dialog"
    :closable="false"
    :mask-closable="false"
    :esc-to-close="false"
    :footer="false"
  >
    <p class="initial-password-dialog__hint">首次登录需要先修改初始密码，完成后请使用新密码重新登录。</p>
    <p class="initial-password-dialog__rule">{{ passwordRuleHint }}</p>
    <a-form :model="form" layout="vertical" @submit="onSubmit">
      <a-form-item label="新密码" required>
        <div class="initial-password-dialog__password-field">
          <a-input-password v-model="form.password" placeholder="请输入新密码" allow-clear />
          <div class="password-strength" aria-live="polite">
            <span
              v-for="segment in 3"
              :key="segment"
              class="password-strength__segment"
              :class="{ active: passwordStrength >= segment, valid: passwordStrength === 3 }"
            />
            <span class="password-strength__label">{{ passwordStrengthLabel }}</span>
          </div>
        </div>
      </a-form-item>
      <a-form-item label="确认新密码" required>
        <a-input-password v-model="form.confirmPassword" placeholder="请再次输入新密码" allow-clear />
      </a-form-item>
      <a-button type="primary" long html-type="submit" :loading="submitting">确认修改</a-button>
    </a-form>
  </a-modal>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import { changeInitialPasswordAPI } from "@/api/user";
import { storeToRefs } from "pinia";
import { useSysConfigStore } from "@/store/modules/sys-config";

const emit = defineEmits<{ success: [] }>();
const form = ref({ password: "", confirmPassword: "" });
const submitting = ref(false);
const sysConfigStore = useSysConfigStore();
const { safeConfig } = storeToRefs(sysConfigStore);
const passwordRuleHint = computed(() => {
  const min = safeConfig.value.minPasswordLength;
  const special = safeConfig.value.requireSpecialChar ? "，且必须包含特殊字符 !@#$%" : "";
  return `密码至少 ${min || 1} 位${special}`;
});
const passwordStrength = computed(() => {
  if (!form.value.password) return 0;
  const minLength = safeConfig.value.minPasswordLength;
  const lengthValid = !minLength || form.value.password.length >= minLength;
  const specialValid = !safeConfig.value.requireSpecialChar || /[!@#$%]/.test(form.value.password);
  if (lengthValid && specialValid) return 3;
  if (lengthValid) return 2;
  return 1;
});
const passwordStrengthLabel = computed(() => {
  if (!form.value.password) return "请输入密码";
  return passwordStrength.value === 3 ? "符合要求" : "继续完善";
});

const onSubmit = async ({ errors }: { errors?: Record<string, unknown> }) => {
  if (errors || submitting.value) return;
  if (!form.value.password || !form.value.confirmPassword) {
    Message.warning("请输入新密码并再次确认");
    return;
  }
  if (form.value.password !== form.value.confirmPassword) {
    Message.warning("两次输入的密码不一致");
    return;
  }
  const minLength = safeConfig.value.minPasswordLength;
  if (minLength > 0 && form.value.password.length < minLength) {
    Message.warning(`密码长度不能少于${minLength}位`);
    return;
  }
  if (safeConfig.value.requireSpecialChar && !/[!@#$%]/.test(form.value.password)) {
    Message.warning("密码必须包含特殊字符 !@#$%");
    return;
  }

  submitting.value = true;
  try {
    await changeInitialPasswordAPI(form.value);
    Message.success("密码修改成功，请使用新密码重新登录");
    emit("success");
  } finally {
    submitting.value = false;
  }
};
</script>

<style lang="scss" scoped>
.initial-password-dialog__hint {
  margin: 0 0 20px;
  line-height: 1.6;
  color: var(--color-text-2);
}

.initial-password-dialog__rule {
  margin: -12px 0 20px;
  font-size: 13px;
  color: var(--color-text-3);
}

.password-strength {
  display: flex;
  gap: 6px;
  align-items: center;
  margin-top: 8px;
}

.initial-password-dialog__password-field {
  width: 100%;
  min-width: 0;
}

.password-strength__segment {
  flex: 1;
  height: 4px;
  background: var(--color-fill-3);
  border-radius: 999px;
  transition: background-color 0.18s ease;
}

.password-strength__segment.active {
  background: #f59e0b;
}

.password-strength__segment.valid {
  background: #16a34a;
}

.password-strength__label {
  flex-shrink: 0;
  min-width: 52px;
  font-size: 12px;
  color: var(--color-text-3);
  text-align: right;
  white-space: nowrap;
}
</style>
