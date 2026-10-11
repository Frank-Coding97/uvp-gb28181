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
    <a-form :model="form" layout="vertical" @submit="onSubmit">
      <a-form-item label="新密码" required>
        <div class="initial-password-dialog__password-field">
          <a-input-password v-model="form.password" placeholder="请输入新密码" allow-clear />
          <!-- 强度条与下方清单一一对应：一条规则亮一段 -->
          <div class="password-strength" aria-live="polite">
            <span
              v-for="rule in passwordRules"
              :key="rule.key"
              class="password-strength__segment"
              :class="{ 'is-satisfied': rule.satisfied }"
            />
            <span class="password-strength__label">{{ passwordStrengthLabel }}</span>
          </div>
          <ul class="password-rules" data-testid="password-rule-list">
            <li
              v-for="rule in passwordRules"
              :key="rule.key"
              class="password-rules__item"
              :class="{ 'is-satisfied': rule.satisfied }"
              :data-rule="rule.key"
            >
              <span class="password-rules__mark" aria-hidden="true">
                <Check v-if="rule.satisfied" :size="11" :stroke-width="3.5" />
              </span>
              <span>{{ rule.label }}</span>
            </li>
          </ul>
        </div>
      </a-form-item>
      <a-form-item label="确认新密码" required>
        <!-- ⛔ 必须包一层容器：a-form-item 的内容区是 flex row，
             输入框和下方的清单直接并列会被排成左右两块（踩过）。 -->
        <div class="initial-password-dialog__password-field">
          <a-input-password v-model="form.confirmPassword" placeholder="请再次输入新密码" allow-clear />
          <!-- 与密码框下的三条规则同一套视觉：满足=绿底对号，未满足=空心圈 -->
          <ul class="password-rules" data-testid="confirm-rule-list">
            <li class="password-rules__item" :class="{ 'is-satisfied': confirmMatched }" data-rule="confirm-match">
              <span class="password-rules__mark" aria-hidden="true">
                <Check v-if="confirmMatched" :size="11" :stroke-width="3.5" />
              </span>
              <span>{{ confirmHint }}</span>
            </li>
          </ul>
        </div>
      </a-form-item>
      <a-button type="primary" long html-type="submit" :loading="submitting">确认修改</a-button>
    </a-form>
  </a-modal>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import { Check } from "lucide-vue-next";
import { Message } from "@arco-design/web-vue";
import { changeInitialPasswordAPI } from "@/api/user";
import { storeToRefs } from "pinia";
import { useSysConfigStore } from "@/store/modules/sys-config";
import { buildPasswordPolicyRules, firstPasswordPolicyError } from "@/utils/password-policy";

const emit = defineEmits<{ success: [] }>();
const form = ref({ password: "", confirmPassword: "" });
const submitting = ref(false);
const sysConfigStore = useSysConfigStore();
const { safeConfig } = storeToRefs(sysConfigStore);

const passwordPolicyOptions = computed(() => ({
  minLength: safeConfig.value.minPasswordLength,
  requireSpecialChar: safeConfig.value.requireSpecialChar
}));

// 规则清单与强度条共用同一份数据源，避免"显示一套、判一套"。
const passwordRules = computed(() => buildPasswordPolicyRules(form.value.password, passwordPolicyOptions.value));
const passwordStrength = computed(() => passwordRules.value.filter(rule => rule.satisfied).length);
const passwordAllSatisfied = computed(() => passwordStrength.value === passwordRules.value.length);
const passwordStrengthLabel = computed(() => {
  if (!form.value.password) return "请输入密码";
  return passwordAllSatisfied.value ? "符合要求" : "继续完善";
});

// 确认框的对号：只有确认框非空且与密码完全一致才点亮，避免"两边都空"被误判为一致。
const confirmMatched = computed(() => Boolean(form.value.confirmPassword) && form.value.confirmPassword === form.value.password);
const confirmHint = computed(() => {
  if (confirmMatched.value) return "两次输入的密码一致";
  return form.value.confirmPassword ? "两次输入的密码不一致" : "两次输入的密码需一致";
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
  const policyError = firstPasswordPolicyError(form.value.password, passwordPolicyOptions.value);
  if (policyError) {
    Message.warning(policyError);
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

.password-strength__segment.is-satisfied {
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

.password-rules {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 0;
  margin: 10px 0 0;
  list-style: none;
}

.password-rules__item {
  display: flex;
  gap: 8px;
  align-items: center;
  font-size: 12px;
  line-height: 18px;
  color: var(--color-text-3);
  transition: color 0.18s ease;
}

.password-rules__item.is-satisfied {
  color: var(--color-text-2);
}

.password-rules__mark {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  width: 14px;
  height: 14px;
  color: #ffffff;
  border: 1px solid var(--color-fill-4);
  border-radius: 50%;
  transition:
    background-color 0.18s ease,
    border-color 0.18s ease;
}

.password-rules__item.is-satisfied .password-rules__mark {
  background: #16a34a;
  border-color: #16a34a;
}
</style>
