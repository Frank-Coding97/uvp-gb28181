<script setup lang="ts">
import { Message } from "@arco-design/web-vue";
import { onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  createStandaloneAdmin,
  forgetBootstrapToken,
  isBcryptPasswordLengthValid,
  isStandaloneAdminPasswordValid,
  loadStandaloneSetupStatus,
  readBootstrapTokenOnce,
  type StandaloneSetupPhase
} from "@/api/standalone-setup";

const route = useRoute();
const router = useRouter();
const setupToken = ref(readBootstrapTokenOnce(route.query as Record<string, unknown>));
const phase = ref<StandaloneSetupPhase | null>(null);
const statusLoading = ref(true);
const statusError = ref("");
const submitError = ref("");
const submitting = ref(false);
const username = ref("");
const password = ref("");
const passwordConfirmation = ref("");
const fieldErrors = ref<Record<string, string>>({});

function validateForm(): boolean {
  const errors: Record<string, string> = {};
  const normalizedUsername = username.value.trim();
  if (!normalizedUsername) errors.username = "请输入管理员用户名";
  if (!password.value) errors.password = "请输入管理员密码";
  else if (!isBcryptPasswordLengthValid(password.value)) errors.password = "密码的 UTF-8 长度不能超过 72 字节";
  else if (!isStandaloneAdminPasswordValid(password.value)) {
    errors.password = "密码至少 6 个字符，且至少包含 !@#$% 中的一个特殊字符";
  }
  if (!passwordConfirmation.value) errors.passwordConfirmation = "请再次输入管理员密码";
  else if (password.value !== passwordConfirmation.value) errors.passwordConfirmation = "两次输入的密码不一致";
  fieldErrors.value = errors;
  return Object.keys(errors).length === 0;
}

async function submit() {
  submitError.value = "";
  if (!setupToken.value || phase.value !== "pending_admin" || !validateForm()) return;

  submitting.value = true;
  try {
    const result = await createStandaloneAdmin({ username: username.value.trim(), password: password.value }, setupToken.value);
    if (result.outcome === "created") {
      forgetBootstrapToken();
      setupToken.value = null;
      Message.success("管理员已创建，请登录并配置 SIP");
      await router.replace("/login");
      return;
    }
    if (result.outcome === "conflict") {
      forgetBootstrapToken();
      setupToken.value = null;
      Message.info("管理员已创建，请直接登录");
      await router.replace("/login");
      return;
    }
    if (result.outcome === "unauthorized") {
      submitError.value =
        result.status === 403 ? "初始化链接已失效，请从本机启动器重新打开" : "管理员创建失败，请检查初始化凭据后重试";
      return;
    }
    if (result.outcome === "missing-token") {
      submitError.value = "管理员创建失败，请检查初始化凭据后重试";
      return;
    }
    submitError.value = "管理员创建失败，请稍后重试";
  } catch {
    submitError.value = "暂时无法连接本机服务，请稍后重试";
  } finally {
    submitting.value = false;
  }
}

async function loadStatus() {
  statusLoading.value = true;
  statusError.value = "";
  try {
    const probe = await loadStandaloneSetupStatus();
    if (probe.kind === "standalone") {
      phase.value = probe.status.phase;
      if (probe.status.phase !== "pending_admin") await router.replace("/login");
    } else if (probe.kind === "legacy") {
      await router.replace("/login");
    } else {
      statusError.value = "暂时无法确认本机初始化状态，请稍后刷新页面";
    }
  } finally {
    statusLoading.value = false;
  }
}

onMounted(loadStatus);
</script>

<template>
  <main class="standalone-setup-page">
    <section class="standalone-setup-card" aria-labelledby="standalone-setup-title">
      <div class="standalone-setup-brand" aria-hidden="true">
        <div class="brand-mark">UVP</div>
        <div class="brand-label">统一视频接入平台</div>
        <div class="brand-rule"></div>
        <p>本机首次启动向导</p>
      </div>

      <div class="standalone-setup-content">
        <header class="standalone-setup-header">
          <span class="eyebrow">LOCAL INSTALLATION</span>
          <h1 id="standalone-setup-title">创建首个管理员</h1>
          <p>请设置用于登录控制台的管理员账号，完成后继续配置 SIP。</p>
        </header>

        <p v-if="statusLoading" class="setup-status" role="status">正在检查本机初始化状态…</p>
        <p v-else-if="statusError" class="setup-message setup-message-error" role="alert">{{ statusError }}</p>
        <p v-else-if="!setupToken" class="setup-message setup-message-warning" role="alert">
          请从本机启动器重新打开首装页面。初始化凭据只在启动器打开的页面中有效。
        </p>

        <form
          v-if="!statusLoading && !statusError && phase === 'pending_admin'"
          class="standalone-setup-form"
          novalidate
          @submit.prevent="submit"
        >
          <label class="form-field">
            <span>管理员用户名</span>
            <input
              v-model="username"
              name="username"
              type="text"
              autocomplete="username"
              placeholder="请输入管理员用户名"
              :aria-invalid="Boolean(fieldErrors.username)"
              @input="fieldErrors.username = ''"
            />
            <small v-if="fieldErrors.username" class="field-error">{{ fieldErrors.username }}</small>
          </label>

          <label class="form-field">
            <span>管理员密码</span>
            <input
              v-model="password"
              name="password"
              type="password"
              autocomplete="new-password"
              placeholder="请输入管理员密码"
              :aria-invalid="Boolean(fieldErrors.password)"
              @input="fieldErrors.password = ''"
            />
            <small class="form-hint">至少 6 个字符，包含 !@#$% 中至少一个，UTF-8 不超过 72 字节</small>
            <small v-if="fieldErrors.password" class="field-error">{{ fieldErrors.password }}</small>
          </label>

          <label class="form-field">
            <span>确认密码</span>
            <input
              v-model="passwordConfirmation"
              name="passwordConfirmation"
              type="password"
              autocomplete="new-password"
              placeholder="请再次输入管理员密码"
              :aria-invalid="Boolean(fieldErrors.passwordConfirmation)"
              @input="fieldErrors.passwordConfirmation = ''"
            />
            <small v-if="fieldErrors.passwordConfirmation" class="field-error">{{ fieldErrors.passwordConfirmation }}</small>
          </label>

          <p v-if="submitError" class="setup-message setup-message-error" role="alert">{{ submitError }}</p>
          <button class="arco-btn arco-btn-primary setup-submit" type="submit" :disabled="submitting || !setupToken">
            {{ submitting ? "正在创建…" : "创建管理员并继续" }}
          </button>
        </form>
      </div>
    </section>
  </main>
</template>

<style scoped>
.standalone-setup-page {
  box-sizing: border-box;
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 32px 20px;
  color: var(--color-text-1, #1d2129);
  background: radial-gradient(circle at 20% 10%, rgba(var(--primary-1), 0.8), transparent 42%), var(--color-fill-1, #f2f3f5);
}

.standalone-setup-card {
  width: min(880px, 100%);
  min-height: 500px;
  display: grid;
  grid-template-columns: 0.82fr 1.18fr;
  overflow: hidden;
  border: 1px solid var(--color-border-2, #e5e6eb);
  border-radius: 12px;
  background: var(--color-bg-2, #fff);
  box-shadow: 0 18px 50px rgb(29 33 41 / 12%);
}

.standalone-setup-brand {
  position: relative;
  display: flex;
  flex-direction: column;
  padding: 52px 40px;
  color: #fff;
  background: linear-gradient(145deg, #0f2b52, #1d4f86);
}

.brand-mark {
  font-size: 44px;
  font-weight: 800;
  letter-spacing: 0.12em;
}

.brand-label {
  margin-top: 8px;
  color: rgb(255 255 255 / 78%);
  font-size: 14px;
  letter-spacing: 0.16em;
}

.brand-rule {
  width: 44px;
  height: 3px;
  margin-top: auto;
  background: rgb(var(--primary-6));
}

.standalone-setup-brand p {
  margin: 16px 0 0;
  color: rgb(255 255 255 / 72%);
  font-size: 13px;
}

.standalone-setup-content {
  padding: 56px clamp(28px, 7vw, 72px);
}

.standalone-setup-header .eyebrow {
  color: rgb(var(--primary-6));
  font-size: 11px;
  letter-spacing: 0.18em;
}

.standalone-setup-header h1 {
  margin: 12px 0 8px;
  color: var(--color-text-1, #1d2129);
  font-size: 28px;
  line-height: 1.3;
}

.standalone-setup-header p {
  margin: 0;
  color: var(--color-text-3, #86909c);
  font-size: 14px;
  line-height: 1.6;
}

.setup-status,
.setup-message {
  margin: 24px 0 0;
  font-size: 13px;
  line-height: 1.6;
}

.setup-message {
  padding: 10px 12px;
  border-radius: 4px;
}

.setup-message-warning {
  color: rgb(var(--warning-7));
  background: rgb(var(--warning-1));
}

.setup-message-error {
  color: rgb(var(--danger-7));
  background: rgb(var(--danger-1));
}

.standalone-setup-form {
  display: grid;
  gap: 18px;
  margin-top: 28px;
}

.form-field {
  display: grid;
  gap: 7px;
  color: var(--color-text-2, #4e5969);
  font-size: 13px;
}

.form-field input {
  box-sizing: border-box;
  width: 100%;
  height: 38px;
  padding: 0 12px;
  color: var(--color-text-1, #1d2129);
  font: inherit;
  border: 1px solid var(--color-border-3, #c9cdd4);
  border-radius: 4px;
  outline: none;
  background: var(--color-bg-1, #fff);
  transition:
    border-color 0.15s,
    box-shadow 0.15s;
}

.form-field input:hover,
.form-field input:focus {
  border-color: rgb(var(--primary-6));
}

.form-field input:focus {
  box-shadow: 0 0 0 2px rgba(var(--primary-6), 0.15);
}

.form-field input[aria-invalid="true"] {
  border-color: rgb(var(--danger-6));
}

.field-error {
  color: rgb(var(--danger-6));
  font-size: 12px;
}

.setup-submit {
  width: 100%;
  min-height: 40px;
  margin-top: 4px;
  border: 0;
  border-radius: 4px;
  color: #fff;
  font: inherit;
  cursor: pointer;
  background: rgb(var(--primary-6));
}

.setup-submit:hover:not(:disabled) {
  background: rgb(var(--primary-5));
}

.setup-submit:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

@media (max-width: 680px) {
  .standalone-setup-card {
    grid-template-columns: 1fr;
  }

  .standalone-setup-brand {
    min-height: 132px;
    padding: 28px 32px;
  }

  .brand-mark {
    font-size: 32px;
  }

  .brand-rule {
    margin-top: 24px;
  }

  .standalone-setup-content {
    padding: 36px 32px 44px;
  }
}
</style>
