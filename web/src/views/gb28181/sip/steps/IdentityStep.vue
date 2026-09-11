<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Eye, EyeOff } from "lucide-vue-next";
import type { SipSetupForm } from "../useSipSetup";
import {
    deriveDomain,
    evaluatePasswordStrength,
    validatePort,
    validateServerId
} from "../sipSetupRules";

const props = defineProps<{ form: SipSetupForm; hasExistingPassword: boolean }>();
const emit = defineEmits<{ update: [patch: Partial<SipSetupForm>] }>();

// 引导页首次配置默认明文,方便用户核对.
const passwordVisible = ref(true);
// 用离焦触发的 "触碰过" 标记 —— 避免用户还没输就报"必填".
const touched = ref({ serverId: false, port: false, password: false });

function updateServerId(value: string) {
    // allow-clear 会传空串,也走同一路径同步 domain(会派生为空,与 UI 一致).
    emit("update", { serverId: value, domain: deriveDomain(value) });
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
        if (derived && derived !== props.form.domain) emit("update", { domain: derived });
    },
    { immediate: true }
);

const serverIdError = computed(() =>
    touched.value.serverId ? validateServerId(props.form.serverId) : ""
);
const portError = computed(() =>
    touched.value.port ? validatePort(props.form.port) : ""
);

// 密码强度实时计算 + 视觉指示
const passwordStrength = computed(() => evaluatePasswordStrength(props.form.password));
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
            <a-form-item
                label="SIP 端口"
                required
                :validate-status="portError ? 'error' : ''"
                :help="portError"
            >
                <!-- 用 a-input 而不是 a-input-number,避免自动 clamp 到 max 掩盖用户输错值 -->
                <a-input
                    :model-value="form.port ? String(form.port) : ''"
                    placeholder="1 - 65535"
                    allow-clear
                    @update:model-value="onPortInput"
                    @blur="touched.port = true"
                />
            </a-form-item>
            <a-form-item
                label="SIP 平台 ID"
                required
                :validate-status="serverIdError ? 'error' : ''"
                :help="serverIdError"
            >
                <a-input
                    :model-value="form.serverId"
                    :max-length="20"
                    placeholder="20 位数字编码"
                    allow-clear
                    @update:model-value="updateServerId"
                    @blur="touched.serverId = true"
                >
                    <template #suffix>
                        <span
                            class="input-counter"
                            :class="{ 'is-complete': form.serverId.length === 20 }"
                        >{{ form.serverId.length }}/20</span>
                    </template>
                </a-input>
            </a-form-item>
        </div>

        <a-form-item label="SIP 域">
            <a-input
                :model-value="form.domain"
                disabled
                placeholder="填入平台 ID 后自动生成"
            />
            <template #extra>按 GB/T 28181-2016,SIP 域取平台 ID 前 10 位(行政区划码),无需手动填写。</template>
        </a-form-item>

        <a-form-item
            label="SIP 密码"
            :required="!hasExistingPassword"
            :validate-status="passwordError ? 'error' : ''"
            :help="passwordError"
        >
            <a-input
                :model-value="form.password"
                :type="passwordVisible ? 'text' : 'password'"
                :placeholder="hasExistingPassword ? '留空保留原密码;设置新密码需 ≥ 12 位' : '至少 12 位,含大小写 / 数字 / 特殊字符 3 类'"
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

            <!-- 强度指示条 -->
            <div v-if="form.password" class="pw-strength" :class="`pw-strength--l${passwordStrength.level}`">
                <div class="pw-strength__bars">
                    <span :class="{ 'is-active': passwordStrength.level >= 1 }" />
                    <span :class="{ 'is-active': passwordStrength.level >= 2 }" />
                    <span :class="{ 'is-active': passwordStrength.level >= 3 }" />
                </div>
                <span class="pw-strength__label">强度: {{ passwordStrength.label }}</span>
            </div>

            <template #extra>
                <span v-if="!form.password && !hasExistingPassword">
                    国标 SIP 注册接口暴露在公网易被暴力破解,本平台强制用户设置强密码。
                </span>
                <span v-else-if="!form.password && hasExistingPassword">
                    留空保留原密码。如需修改,新密码需满足强度规则。
                </span>
                <span v-else class="pw-strength__hint">
                    要求:长度 ≥ 12,包含大写、小写、数字、特殊字符中的至少 3 类,不使用常见弱口令。
                </span>
            </template>
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

/* 密码强度指示 */
.pw-strength {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: 6px;
}

.pw-strength__bars {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 4px;
    flex: 1;
    max-width: 200px;
}

.pw-strength__bars span {
    height: 4px;
    background: var(--uvp-shell-muted, #eef4f8);
    border-radius: 2px;
    transition: background 200ms ease;
}

.pw-strength--l1 .pw-strength__bars span.is-active {
    background: var(--uvp-danger, #d14343);
}

.pw-strength--l2 .pw-strength__bars span.is-active {
    background: #d97706;
}

.pw-strength--l3 .pw-strength__bars span.is-active {
    background: #059669;
}

.pw-strength__label {
    font-size: 12px;
    font-weight: 600;
}

.pw-strength--l1 .pw-strength__label {
    color: var(--uvp-danger, #d14343);
}

.pw-strength--l2 .pw-strength__label {
    color: #d97706;
}

.pw-strength--l3 .pw-strength__label {
    color: #059669;
}

.pw-strength__hint {
    color: var(--uvp-text-tertiary);
    font-size: 12px;
}

.input-counter {
    color: var(--uvp-text-tertiary);
    font-size: 12px;
    font-family: "SFMono-Regular", Consolas, Menlo, monospace;
    letter-spacing: 0.5px;
    padding: 0 4px;
    transition: color 160ms ease;
}

.input-counter.is-complete {
    color: #059669;
    font-weight: 600;
}

@media (max-width: 640px) {
    .form-grid {
        grid-template-columns: 1fr;
        gap: 0;
    }
}
</style>
