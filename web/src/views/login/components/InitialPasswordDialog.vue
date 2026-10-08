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
        <a-input-password v-model="form.password" placeholder="请输入新密码" allow-clear />
      </a-form-item>
      <a-form-item label="确认新密码" required>
        <a-input-password v-model="form.confirmPassword" placeholder="请再次输入新密码" allow-clear />
      </a-form-item>
      <a-button type="primary" long html-type="submit" :loading="submitting">确认修改</a-button>
    </a-form>
  </a-modal>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { Message } from "@arco-design/web-vue";
import { changeInitialPasswordAPI } from "@/api/user";

const emit = defineEmits<{ success: [] }>();
const form = ref({ password: "", confirmPassword: "" });
const submitting = ref(false);

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
</style>
