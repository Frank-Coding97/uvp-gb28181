<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { ChevronLeft, ChevronRight, Save } from "lucide-vue-next";
import DeploymentStep from "./steps/DeploymentStep.vue";
import NetworkStep from "./steps/NetworkStep.vue";
import IdentityStep from "./steps/IdentityStep.vue";
import ConfirmStep from "./steps/ConfirmStep.vue";
import { useSipSetup } from "./useSipSetup";
import { identityCanContinue, networkCanContinue } from "./sipSetupRules";

const props = withDefaults(defineProps<{
    modelValue: boolean;
    editing?: boolean;
}>(), { editing: false });
const emit = defineEmits<{
    "update:modelValue": [value: boolean];
    saved: [];
    skipped: [];
}>();

const setup = useSipSetup();
const step = ref(1);

const title = computed(() => props.editing ? "编辑 SIP 配置" : "配置 SIP 服务");
const canNext = computed(() => {
    if (step.value === 1) return setup.form.deploymentMode !== "";
    if (step.value === 2) return networkCanContinue(setup.form.deploymentMode, setup.form.listenIp, setup.form.advertiseIp);
    if (step.value === 3) return identityCanContinue(
        setup.form.port,
        setup.form.serverId,
        setup.form.domain,
        setup.form.password,
        setup.hasExistingPassword.value
    );
    return true;
});

watch(() => props.modelValue, async visible => {
    if (!visible) return;
    step.value = 1;
    try {
        await Promise.all([setup.loadStatus(), setup.loadNetwork()]);
    } catch {
        Message.warning(setup.error.value || "部分配置数据加载失败");
    }
});

function close() {
    emit("update:modelValue", false);
}

async function save() {
    if (setup.saving.value) return;
    try {
        await setup.save();
        Message.success("SIP 配置已保存，重启服务后生效");
        emit("saved");
        close();
    } catch {
        Message.error(setup.error.value || "保存失败");
    }
}

async function skip() {
    try {
        await setup.skip();
        emit("skipped");
        close();
    } catch (error: any) {
        Message.error(error?.message || "操作失败");
    }
}
</script>

<template>
    <a-modal
        :visible="modelValue"
        :title="title"
        width="min(720px, calc(100vw - 24px))"
        :mask-closable="false"
        :esc-to-close="false"
        :footer="false"
        modal-class="sip-setup-modal"
        @cancel="close"
    >
        <div class="wizard-shell">
            <a-steps :current="step" size="small" class="wizard-steps">
                <a-step title="部署方式" />
                <a-step title="网络地址" />
                <a-step title="SIP 身份" />
                <a-step title="确认" />
            </a-steps>

            <div class="wizard-body">
                <DeploymentStep v-if="step === 1" v-model="setup.form.deploymentMode" />
                <NetworkStep
                    v-else-if="step === 2"
                    :form="setup.form"
                    :network="setup.network.value"
                    @update="Object.assign(setup.form, $event)"
                />
                <IdentityStep
                    v-else-if="step === 3"
                    :form="setup.form"
                    :has-existing-password="setup.hasExistingPassword.value"
                    @update="Object.assign(setup.form, $event)"
                />
                <ConfirmStep
                    v-else
                    :form="setup.form"
                    :has-password="setup.hasExistingPassword.value"
                    :network="setup.network.value"
                />
            </div>

            <div class="wizard-footer">
                <a-button v-if="!editing && step === 1" type="text" @click="skip">稍后配置</a-button>
                <span class="footer-spacer" />
                <a-button v-if="step > 1" @click="step--">
                    <template #icon><ChevronLeft :size="16" /></template>
                    上一步
                </a-button>
                <a-button v-if="step < 4" type="primary" :disabled="!canNext" @click="step++">
                    下一步
                    <template #icon><ChevronRight :size="16" /></template>
                </a-button>
                <a-button v-else type="primary" :loading="setup.saving.value" @click="save">
                    <template #icon><Save :size="16" /></template>
                    保存配置
                </a-button>
            </div>
        </div>
    </a-modal>
</template>

<style scoped>
.wizard-shell {
    display: grid;
    grid-template-rows: auto minmax(300px, 1fr) auto;
    min-height: 430px;
    min-width: 0;
}

.wizard-steps {
    min-width: 0;
    padding: 2px 8px 18px;
    border-bottom: 1px solid var(--uvp-divider);
}

.wizard-body {
    min-width: 0;
    padding: 22px 4px;
}

.step-placeholder {
    display: grid;
    min-height: 280px;
    color: var(--uvp-text-tertiary);
    place-items: center;
}

.wizard-footer {
    display: flex;
    align-items: center;
    gap: 10px;
    padding-top: 14px;
    border-top: 1px solid var(--uvp-divider);
}

.footer-spacer {
    flex: 1;
}

@media (max-width: 640px) {
    .wizard-shell {
        height: min(620px, calc(100vh - 140px));
        min-height: 0;
    }

    .wizard-body {
        min-width: 0;
        padding-top: 16px;
        overflow-y: auto;
    }

    .wizard-steps :deep(.arco-steps-item) {
        min-width: 0;
    }

    .wizard-steps :deep(.arco-steps-item-content) {
        display: none;
    }

    .wizard-steps :deep(.arco-steps-item-active .arco-steps-item-content) {
        display: block;
    }
}
</style>
