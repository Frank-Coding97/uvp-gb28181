<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { AlertCircle, Bell, ChevronLeft, ChevronRight, Save } from "lucide-vue-next";
import type { SipNetworkInterfaces } from "@/api/gb28181";
import { loadStandaloneSetupStatus } from "@/api/standalone-setup";
import DeploymentStep from "@/views/gb28181/sip/steps/DeploymentStep.vue";
import NetworkStep from "@/views/gb28181/sip/steps/NetworkStep.vue";
import IdentityStep from "@/views/gb28181/sip/steps/IdentityStep.vue";
import ConfirmStep from "@/views/gb28181/sip/steps/ConfirmStep.vue";
import { useSipSetup } from "@/views/gb28181/sip/useSipSetup";
import { identityCanContinue, mediaHostsCanContinue, networkCanContinue, networkOptions, sipAddressAvailability } from "@/views/gb28181/sip/sipSetupRules";

const props = withDefaults(defineProps<{
    visible: boolean;
    // editing=true:从"编辑配置"入口进,标题/文案切换成编辑态,允许取消.
    // required=true:standalone pending_sip 首装引导,必须完成 SIP 配置.
    // required=false(默认):legacy 首装引导,保留"稍后再配".
    editing?: boolean;
    required?: boolean;
    standalone?: boolean;
}>(), { editing: false, required: false, standalone: false });
const emit = defineEmits<{
    close: [];
    saved: [];
}>();

const modalTitle = computed(() => (props.editing ? "编辑 SIP 配置" : "配置 SIP 服务"));
const canClose = computed(() => props.editing || !props.required);

const setup = useSipSetup();
const step = ref(1);
const detectedStandalone = ref(false);
const savedNetworkConfig = ref<{
    deploymentMode: "lan" | "public";
    listenIp: string;
    advertiseIp: string;
} | null>(null);
// reloadError 用于保存后热启动失败时,把后端的错误信息展示在 Modal 里,让用户改端口/IP 后重试.
const reloadError = ref("");
const standaloneMode = computed(() => props.standalone || detectedStandalone.value);

const savedAddressAvailability = computed(() => {
    if (!standaloneMode.value || !savedNetworkConfig.value) return "ok";
    return sipAddressAvailability(
        savedNetworkConfig.value.deploymentMode,
        savedNetworkConfig.value.listenIp,
        savedNetworkConfig.value.advertiseIp,
        setup.network.value
    );
});
const savedAddressChanged = computed(() => savedAddressAvailability.value === "missing");
const networkForStep = computed<SipNetworkInterfaces | null>(() => {
    const network = setup.network.value;
    const saved = savedNetworkConfig.value;
    if (!network || !standaloneMode.value || !saved) return network;
    return { ...network, items: networkOptions(network.items, saved.listenIp) };
});

const canNext = computed(() => {
    if (step.value === 1) return setup.form.deploymentMode !== "";
    if (step.value === 2) {
        return networkCanContinue(setup.form.deploymentMode, setup.form.listenIp, setup.form.advertiseIp) &&
            mediaHostsCanContinue(
                props.required && standaloneMode.value,
                setup.form.mediaReceiveHost,
                setup.form.mediaPlaybackHost
            );
    }
    if (step.value === 3) return identityCanContinue(
        setup.form.port,
        setup.form.serverId,
        setup.form.domain,
        setup.form.password,
        setup.hasExistingPassword.value
    );
    return true;
});

// 每次 Modal 打开都拉一次最新配置 + 网络接口,避免用先前会话残留.
watch(() => props.visible, async open => {
    if (!open) return;
    step.value = 1;
    reloadError.value = "";
    detectedStandalone.value = false;
    savedNetworkConfig.value = null;
    try {
        if (!props.standalone) {
            const standaloneProbe = await loadStandaloneSetupStatus();
            detectedStandalone.value = standaloneProbe.kind === "standalone";
        }
        await Promise.all([setup.loadStatus(), setup.loadNetwork()]);
        const config = setup.status.value?.config;
        if (config) {
            savedNetworkConfig.value = {
                deploymentMode: config.deploymentMode,
                listenIp: config.listenIp,
                advertiseIp: config.advertiseIp
            };
        }
    } catch {
        Message.warning(setup.error.value || "部分配置数据加载失败,可以先手动填写");
    }
});

async function save() {
    if (setup.saving.value) return;
    reloadError.value = "";
    try {
        const result = await setup.save({ includeMediaHosts: props.required && standaloneMode.value });
        if (result.reloadedOk) {
            Message.success("SIP 配置已保存并成功启动");
            emit("saved");
            emit("close");
        } else {
            // 保存到 DB 成功,但热启动失败(端口冲突/IP 不可达等).
            // Modal 保持打开,把错误显示在 banner 里,用户改一个字段就能再点保存.
            reloadError.value = result.reloadError || "SIP 服务启动失败,请检查配置";
            step.value = 4; // 停留在确认页,让用户看到当前 payload
        }
    } catch {
        Message.error(setup.error.value || "保存失败");
    }
}

function closeModal() {
    if (canClose.value) emit("close");
}
</script>

<template>
    <a-modal
        :visible="visible"
        :title="modalTitle"
        width="min(760px, calc(100vw - 24px))"
        :mask-closable="false"
        :esc-to-close="false"
        :closable="canClose"
        modal-class="uvp-system-dialog sip-setup-dialog"
        unmount-on-close
        @cancel="closeModal"
    >
        <div v-if="!editing" class="sip-modal-intro">
            <span class="sip-modal-intro-badge">
                <Bell :size="14" />
            </span>
            <div class="sip-modal-intro-text">
                <strong>首次接入国标设备前需要完成 SIP 参数配置</strong>
                <span v-if="required">请完成配置后再使用系统，保存并启动 SIP 后即可继续。</span>
                <span v-else>点稍后再配也可以先进入系统,右上角铃铛会一直提醒你完成配置</span>
            </div>
        </div>

        <div v-if="savedAddressChanged" class="sip-modal-address-warning">
            <span class="sip-modal-error-icon">
                <AlertCircle :size="18" />
            </span>
            <div class="sip-modal-error-body">
                <strong>SIP 接入地址已变化</strong>
                <span>已保存的本机 SIP 地址不在当前网卡列表中，请选择新的本机地址后再保存。</span>
                <em>当前已保存配置仍保留，未被自动修改。</em>
            </div>
        </div>

        <a-steps :current="step" size="small" class="sip-modal-steps">
            <a-step title="部署方式" description="局域网 / 公网" />
            <a-step title="网络地址" description="监听 & 宣告" />
            <a-step title="SIP 身份" description="ID / 域 / 密码" />
            <a-step title="确认" description="核对信息" />
        </a-steps>

        <div v-if="reloadError" class="sip-modal-error">
            <span class="sip-modal-error-icon">
                <AlertCircle :size="18" />
            </span>
            <div class="sip-modal-error-body">
                <strong>SIP 服务启动失败</strong>
                <span>{{ reloadError }}</span>
                <em>配置已保存,请修改后再试.常见原因:端口被占用、监听 IP 不可达。</em>
            </div>
        </div>

        <div class="sip-modal-body">
            <DeploymentStep v-if="step === 1" v-model="setup.form.deploymentMode" />
            <NetworkStep
                v-else-if="step === 2"
                :form="setup.form"
                :network="networkForStep"
                :media-required="required && standaloneMode"
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
                :media-required="required && standaloneMode"
            />
        </div>

        <template #footer>
            <a-button v-if="editing || !required" type="text" class="sip-modal-skip" @click="closeModal">
                {{ editing ? "取消" : "稍后再配" }}
            </a-button>
            <span class="sip-modal-spacer" />
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
                {{ editing ? "保存并应用" : "保存并启动" }}
            </a-button>
        </template>
    </a-modal>
</template>

<style lang="scss">
/* uvp-system-dialog 大部分样式已由全局 arco-overrides / uvp-ui-language 提供;
   这里只补 SIP 引导页专有的排版. */
.sip-setup-dialog .arco-modal-body {
    display: flex;
    flex-direction: column;
    gap: 16px;
}

.sip-setup-dialog .sip-modal-intro {
    display: flex;
    gap: 12px;
    align-items: flex-start;
    padding: 14px 16px;
    background: var(--uvp-brand-soft);
    border: 1px solid rgb(37 99 235 / 12%);
    border-radius: 10px;
}

.sip-setup-dialog .sip-modal-intro-badge {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: 28px;
    height: 28px;
    background: var(--uvp-brand);
    color: #ffffff;
    border-radius: 8px;
}

.sip-setup-dialog .sip-modal-intro-text {
    display: flex;
    flex-direction: column;
    gap: 3px;
    font-size: 12.5px;
    color: var(--uvp-text-secondary);
    line-height: 1.55;
}

.sip-setup-dialog .sip-modal-intro-text strong {
    font-size: 13px;
    font-weight: 620;
    color: var(--uvp-text-primary);
}

.sip-setup-dialog .sip-modal-address-warning {
    display: flex;
    gap: 12px;
    align-items: flex-start;
    padding: 12px 14px;
    background: var(--uvp-warning-soft);
    border: 1px solid var(--uvp-warning-border);
    border-radius: 10px;
}

.sip-setup-dialog .sip-modal-address-warning .sip-modal-error-icon {
    color: var(--uvp-warning);
}

.sip-setup-dialog .sip-modal-address-warning .sip-modal-error-body {
    color: var(--uvp-warning);
}

.sip-setup-dialog .sip-modal-steps {
    padding: 6px 4px 4px;
}

.sip-setup-dialog .sip-modal-steps .arco-steps-item-title {
    font-size: 13px;
    font-weight: 600;
    color: var(--uvp-text-primary);
}

.sip-setup-dialog .sip-modal-steps .arco-steps-item-description {
    margin-top: 2px;
    color: var(--uvp-text-tertiary);
    font-size: 11.5px;
}

.sip-setup-dialog .sip-modal-error {
    display: flex;
    gap: 12px;
    align-items: flex-start;
    padding: 12px 14px;
    background: var(--uvp-danger-soft);
    border: 1px solid var(--uvp-danger-border);
    border-radius: 10px;
}

.sip-setup-dialog .sip-modal-error-icon {
    display: inline-flex;
    color: var(--uvp-danger);
    padding-top: 1px;
}

.sip-setup-dialog .sip-modal-error-body {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 12.5px;
    color: var(--uvp-danger);
    line-height: 1.55;
}

.sip-setup-dialog .sip-modal-error-body strong {
    font-size: 13px;
    font-weight: 620;
}

.sip-setup-dialog .sip-modal-error-body em {
    font-style: normal;
    font-size: 12px;
    color: var(--uvp-text-tertiary);
}

.sip-setup-dialog .sip-modal-body {
    padding-top: 4px;
}

/* footer 通过 uvp-system-dialog 的 flex 布局排列.
   稍后再配按钮左对齐,占位符把主按钮推到右边 */
.sip-setup-dialog .arco-modal-footer {
    justify-content: flex-start !important;
}

.sip-setup-dialog .arco-modal-footer .sip-modal-skip {
    color: var(--uvp-text-tertiary);
}

.sip-setup-dialog .arco-modal-footer .sip-modal-skip:hover {
    color: var(--uvp-text-secondary);
    background: var(--uvp-shell-muted);
}

.sip-setup-dialog .arco-modal-footer .sip-modal-spacer {
    flex: 1;
}

@media (max-width: 640px) {
    .sip-setup-dialog .sip-modal-steps .arco-steps-item-description {
        display: none;
    }
}
</style>
