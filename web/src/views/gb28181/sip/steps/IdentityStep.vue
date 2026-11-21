<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Eye, EyeOff } from "lucide-vue-next";
import SCounterSuffix from "@/components/s-counter-suffix/index.vue";
import SPasswordRules from "@/components/s-password-rules/index.vue";
import type { SipSetupForm } from "../useSipSetup";
import {
  buildSipPasswordRules,
  deriveDomain,
  evaluatePasswordStrength,
  validateDomain,
  validatePort,
  validateServerId
} from "../sipSetupRules";

const props = defineProps<{ form: SipSetupForm; hasExistingPassword: boolean }>();
const emit = defineEmits<{ update: [patch: Partial<SipSetupForm>] }>();

// 引导页首次配置默认明文,方便用户核对.
const passwordVisible = ref(true);
// 用离焦触发的 "触碰过" 标记 —— 避免用户还没输就报"必填".
const touched = ref({ serverId: false, domain: false, port: false, password: false });

function updateServerId(value: string) {
  emit("update", { serverId: value });
}

function onPortInput(value: string) {
  // 允许用户输任意数字,校验交给 portError.
  // 非数字字符被 parseInt 抛掉;空字符串保留 0(触发 portError 提示"必填/范围").
  const digits = value.replace(/[^0-9]/g, "");
  emit("update", { port: digits ? Number(digits) : 0 });
}

watch(
  () => props.form.serverId,
  serverId => {
    const derived = deriveDomain(serverId);
    if (!props.form.domain && derived) emit("update", { domain: derived });
  },
  { immediate: true }
);

const serverIdError = computed(() => (touched.value.serverId ? validateServerId(props.form.serverId) : ""));
const domainError = computed(() => (touched.value.domain ? validateDomain(props.form.domain) : ""));
const portError = computed(() => (touched.value.port ? validatePort(props.form.port) : ""));

// 密码强度实时计算 + 视觉指示
const passwordStrength = computed(() => evaluatePasswordStrength(props.form.password));
// 格式提示清单与上面的强度分级**同源**(sipSetupRules 内部共用一份 facts),不会出现
// "清单全打勾、下一步却点不动"。
const sipPasswordRules = computed(() => buildSipPasswordRules(props.form.password));
const passwordError = computed(() => {
  if (!touched.value.password) return "";
  if (!props.form.password) {
    // 编辑态且沿用旧密码时不需要输入
    return props.hasExistingPassword ? "" : "请设置 SIP 接入密码";
  }
  return passwordStrength.value.reason;
});
</script>

<template>
  <a-form layout="vertical" class="identity-form">
    <div class="form-grid">
      <a-form-item label="SIP 端口" required :validate-status="portError ? 'error' : ''" :help="portError">
        <!-- 用 a-input 而不是 a-input-number,避免自动 clamp 到 max 掩盖用户输错值 -->
        <a-input
          :model-value="form.port ? String(form.port) : ''"
          placeholder="1 - 65535"
          allow-clear
          @update:model-value="onPortInput"
          @blur="touched.port = true"
        />
      </a-form-item>
      <a-form-item label="SIP 平台 ID" required :validate-status="serverIdError ? 'error' : ''" :help="serverIdError">
        <a-input
          :model-value="form.serverId"
          :max-length="20"
          placeholder="20 位数字编码"
          allow-clear
          @update:model-value="updateServerId"
          @blur="touched.serverId = true"
        >
          <template #suffix>
            <s-counter-suffix :value="form.serverId.length" :total="20" />
          </template>
        </a-input>
      </a-form-item>
    </div>

    <a-form-item label="SIP 域" :validate-status="domainError ? 'error' : ''" :help="domainError">
      <a-input
        :model-value="form.domain"
        :max-length="10"
        allow-clear
        placeholder="10 位数字编码,默认取平台 ID 前 10 位"
        @update:model-value="emit('update', { domain: $event })"
        @blur="touched.domain = true"
      >
        <template #suffix>
          <s-counter-suffix :value="form.domain.length" :total="10" />
        </template>
      </a-input>
      <template #extra>默认取平台 ID 前 10 位,可手动修改为 10 位数字。</template>
    </a-form-item>

    <a-form-item
      label="SIP 密码"
      :required="!hasExistingPassword"
      :validate-status="passwordError ? 'error' : ''"
      :help="passwordError"
    >
      <!-- ⛔ 必须包一层 width:100% 容器:a-form-item 的内容区是 flex row,
           输入框与下方的「提示 + 格式清单」直接并列会被排成左右两块(InitialPasswordDialog 踩过)。 -->
      <div class="identity-form__password-field">
        <a-input
          :model-value="form.password"
          :type="passwordVisible ? 'text' : 'password'"
          :placeholder="hasExistingPassword ? '留空保留原密码;设置新密码需 ≥ 8 位' : '至少 8 位,含大小写 / 数字 / 特殊字符 3 类'"
          allow-clear
          @update:model-value="emit('update', { password: $event })"
          @blur="touched.password = true"
        >
          <template #suffix>
            <a-button
              type="text"
              shape="circle"
              :title="passwordVisible ? '隐藏密码' : '显示密码'"
              @click="passwordVisible = !passwordVisible"
            >
              <EyeOff v-if="passwordVisible" :size="16" />
              <Eye v-else :size="16" />
            </a-button>
          </template>
        </a-input>

        <span v-if="!form.password && !hasExistingPassword" class="identity-form__password-note">
          国标 SIP 注册接口暴露在公网易被暴力破解,本平台强制用户设置强密码。
        </span>
        <span v-else-if="!form.password && hasExistingPassword" class="identity-form__password-note">
          留空保留原密码。如需修改,新密码需满足强度规则。
        </span>

        <!-- 格式提示:强度条 + 逐条规则打对号,与「修改初始密码」弹窗同一套观感(共享组件)。 -->
        <s-password-rules :rules="sipPasswordRules" :filled="Boolean(form.password)" testid="sip-password-rule-list" />
      </div>
    </a-form-item>
  </a-form>
</template>

<style scoped>
.identity-form {
  max-width: 560px;
  margin: 0 auto;
}

.form-grid {
  display: grid;
  grid-template-columns: minmax(140px, 0.6fr) minmax(260px, 1.4fr);
  gap: 14px;
}

/* 密码强度条 / 规则清单的样式在共享组件 s-password-rules 内,此处不再重复定义。 */

.identity-form__password-field {
  width: 100%;
  min-width: 0;
}

.identity-form__password-note {
  display: block;
  margin-top: 6px;
  font-size: 12px;
  line-height: 18px;
  color: var(--uvp-text-tertiary);
}

@media (width <= 640px) {
  .form-grid {
    grid-template-columns: 1fr;
    gap: 0;
  }
}
</style>
