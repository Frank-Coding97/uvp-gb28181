<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import { Copy, RefreshCw, Settings2 } from "lucide-vue-next";
import {
    fetchSipPlatformInfo,
    fetchSipSetupStatus,
    type SipPlatformInfo,
    type SipSetupStatus
} from "@/api/gb28181";
import { useUserStoreHook } from "@/store/modules/user";
import SipSetupWizard from "./SipSetupWizard.vue";
import { mayEditSipConfig, runtimeColor, runtimeLabel } from "./platformViewState";

const loading = ref(false);
const wizardVisible = ref(false);
const status = ref<SipSetupStatus | null>(null);
const platform = ref<SipPlatformInfo | null>(null);
const permissions = computed(() => useUserStoreHook().account.permissions);
const canEdit = computed(() => mayEditSipConfig(Boolean(status.value?.canConfigure), permissions.value));

const fields = computed(() => {
    const config = status.value?.config;
    if (!config) return [];
    return [
        { label: "平台国标编码", value: config.serverId, copyable: true },
        { label: "SIP 域", value: config.domain, copyable: true },
        { label: "SIP 监听地址", value: `${config.listenIp}:${config.port}`, copyable: true },
        { label: "设备接入地址", value: `${config.advertiseIp}:${config.port}`, copyable: true },
        { label: "传输协议", value: platform.value?.transport?.join(" / ").toUpperCase() || "UDP / TCP" },
        { label: "接入密码", value: config.hasPassword ? "******" : "-" },
        { label: "注册 URI", value: platform.value?.registerUri || `sip:${config.serverId}@${config.advertiseIp}:${config.port}`, copyable: true }
    ];
});

async function refresh() {
    loading.value = true;
    try {
        const [statusResponse, platformResponse] = await Promise.all([fetchSipSetupStatus(), fetchSipPlatformInfo()]);
        if (statusResponse.code !== 0) throw new Error(statusResponse.message || "加载失败");
        status.value = statusResponse.data;
        if (platformResponse.code === 0) platform.value = platformResponse.data;
    } catch (error: any) {
        Message.error(error?.message || "加载失败");
    } finally {
        loading.value = false;
    }
}

async function copyText(value: string) {
    if (!value || value === "-") return;
    try {
        await navigator.clipboard.writeText(value);
        Message.success("已复制");
    } catch {
        Message.warning("当前浏览器不支持自动复制");
    }
}

onMounted(refresh);
</script>

<template>
    <div class="snow-fill">
        <div class="snow-fill-inner uvp-page-shell-flat sip-platform-shell">
            <div class="sip-platform-page">
                <div class="sip-platform-toolbar">
                    <div class="status-line">
                        <span class="status-label">GB28181 SIP 服务</span>
                        <a-tag
                            v-if="status"
                            :color="runtimeColor(status.runtime.state)"
                            bordered
                        >
                            {{ runtimeLabel(status.runtime.state) }}
                        </a-tag>
                    </div>
                    <div class="toolbar-actions">
                        <a-tooltip content="刷新状态">
                            <a-button shape="circle" :loading="loading" @click="refresh">
                                <RefreshCw :size="16" />
                            </a-button>
                        </a-tooltip>
                        <a-button v-if="canEdit && status?.config" type="primary" @click="wizardVisible = true">
                            <template #icon><Settings2 :size="16" /></template>
                            编辑配置
                        </a-button>
                    </div>
                </div>

                <a-alert v-if="status?.runtime.state === 'restart_required'" type="warning" :show-icon="true">
                    SIP 配置已保存，重启服务后生效。
                </a-alert>
                <a-alert v-else-if="status?.runtime.state === 'failed'" type="error" :show-icon="true">
                    {{ status.runtime.errorSummary || "SIP 服务启动失败" }}
                </a-alert>

                <a-spin :loading="loading" class="sip-platform-panel">
                    <a-empty v-if="status && status.configStatus === 'unconfigured'" description="尚未配置 SIP 服务">
                        <a-button v-if="canEdit" type="primary" @click="wizardVisible = true">开始配置</a-button>
                    </a-empty>

                    <template v-else>
                        <div class="panel-head">
                            <div>
                                <div class="panel-title">本级平台接入信息</div>
                                <div class="panel-subtitle">下级平台或国标设备接入本平台时使用</div>
                            </div>
                        </div>
                        <div class="info-grid">
                            <div v-for="field in fields" :key="field.label" class="info-item">
                                <div class="info-label">{{ field.label }}</div>
                                <div class="info-value">
                                    <code>{{ field.value || "-" }}</code>
                                    <a-tooltip v-if="field.copyable" content="复制">
                                        <a-button shape="circle" size="mini" type="text" @click="copyText(field.value)">
                                            <Copy :size="14" />
                                        </a-button>
                                    </a-tooltip>
                                </div>
                            </div>
                        </div>
                    </template>
                </a-spin>
            </div>
        </div>
        <SipSetupWizard v-model="wizardVisible" editing @saved="refresh" />
    </div>
</template>

<style scoped>
.sip-platform-shell { padding: 0; overflow: hidden; }
.sip-platform-page { height: 100%; min-width: 0; box-sizing: border-box; padding: 4px 8px; overflow: auto; }
.sip-platform-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 16px; min-width: 0; margin-bottom: 16px; }
.status-line, .toolbar-actions { display: flex; align-items: center; gap: 10px; min-width: 0; }
.status-label, .panel-subtitle, .info-label { color: var(--uvp-text-tertiary); font-size: 12px; }
.sip-platform-page > :deep(.arco-alert) { margin-bottom: 14px; }
.sip-platform-panel { display: block; width: 100%; min-height: 260px; box-sizing: border-box; padding: 16px 18px 18px; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); box-shadow: var(--uvp-panel-shadow); }
.panel-head { padding-bottom: 14px; border-bottom: 1px solid var(--uvp-divider); }
.panel-title { color: var(--uvp-text-primary); font-size: 15px; font-weight: 600; line-height: 22px; }
.panel-subtitle { margin-top: 4px; line-height: 18px; }
.info-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 28px; min-width: 0; }
.info-item { display: flex; gap: 12px; min-width: 0; padding: 14px 0; border-bottom: 1px solid var(--uvp-divider); }
.info-label { flex: 0 0 112px; }
.info-value { display: flex; align-items: center; gap: 8px; min-width: 0; }
.info-value code { min-width: 0; overflow: hidden; color: var(--uvp-text-primary); font-size: 13px; text-overflow: ellipsis; white-space: nowrap; letter-spacing: 0; }
@media (max-width: 900px) { .info-grid { grid-template-columns: 1fr; } }
@media (max-width: 640px) {
    .sip-platform-toolbar, .info-item { align-items: flex-start; }
    .sip-platform-toolbar { flex-direction: column; }
    .toolbar-actions { width: 100%; justify-content: flex-end; }
    .info-item { flex-direction: column; gap: 5px; }
    .info-label { flex-basis: auto; }
    .info-value { width: 100%; }
}
</style>
