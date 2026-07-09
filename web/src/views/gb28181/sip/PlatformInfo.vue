<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import { fetchSipPlatformInfo, type SipPlatformInfo } from "@/api/gb28181";

const loading = ref(false);
const info = ref<SipPlatformInfo | null>(null);

const listenAddress = computed(() => {
    if (!info.value) return "";
    return `${info.value.sipIp || "-"}:${info.value.sipPort || "-"}`;
});

const transportText = computed(() => info.value?.transport?.join(" / ").toUpperCase() || "-");

const fields = computed(() => [
    { label: "平台国标编码", value: info.value?.serverId || "-", copyable: true },
    { label: "SIP 域", value: info.value?.domain || "-", copyable: true },
    { label: "SIP 监听地址", value: listenAddress.value || "-", copyable: true },
    { label: "传输协议", value: transportText.value },
    { label: "接入密码", value: info.value?.passwordMasked || "-", copyable: true },
    { label: "注册 URI", value: info.value?.registerUri || "-", copyable: true }
]);

async function refresh() {
    loading.value = true;
    try {
        const res = await fetchSipPlatformInfo();
        if (res.code === 0) {
            info.value = res.data;
        } else {
            Message.error(res.message || "加载失败");
        }
    } catch (e: any) {
        Message.error(e?.message || "加载失败");
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
                        <a-tag v-if="info?.enabled" color="green" bordered>已启用</a-tag>
                        <a-tag v-else color="gray" bordered>未启用</a-tag>
                    </div>
                    <a-button @click="refresh" :loading="loading">
                        <template #icon><icon-refresh /></template>
                        刷新
                    </a-button>
                </div>

                <a-spin :loading="loading" class="sip-platform-panel">
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
                                <span class="mono">{{ field.value }}</span>
                                <a-button
                                    v-if="field.copyable"
                                    size="mini"
                                    type="text"
                                    class="copy-btn"
                                    @click="copyText(field.value)"
                                >
                                    复制
                                </a-button>
                            </div>
                        </div>
                    </div>

                    <div class="notice-bar">
                        <span>配置来源: server/config/config.yml · gb28181.sip</span>
                        <span>如需修改接入参数,请调整服务端配置并重启 SIP 服务。</span>
                    </div>
                </a-spin>
            </div>
        </div>
    </div>
</template>

<style scoped>
.sip-platform-shell {
    padding: 0;
    overflow: hidden;
}

.sip-platform-page {
    height: 100%;
    min-width: 0;
    box-sizing: border-box;
    padding: 4px 8px;
    overflow: auto;
}

.sip-platform-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    min-width: 0;
    margin-bottom: 16px;
}

.status-line {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
}

.status-label {
    color: var(--uvp-text-tertiary);
    font-size: 12px;
}

.sip-platform-panel {
    display: block;
    width: 100%;
    min-width: 0;
    box-sizing: border-box;
    padding: 16px 18px 18px;
    overflow: hidden;
    background: var(--uvp-panel-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: var(--uvp-panel-radius);
    box-shadow: var(--uvp-panel-shadow);
}

.panel-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
    padding-bottom: 14px;
    margin-bottom: 4px;
    border-bottom: 1px solid var(--uvp-divider);
}

.panel-title {
    color: var(--uvp-text-primary);
    font-size: 15px;
    font-weight: 600;
    line-height: 22px;
}

.panel-subtitle {
    margin-top: 4px;
    color: var(--uvp-text-tertiary);
    font-size: 12px;
    line-height: 18px;
}

.info-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 0 28px;
    min-width: 0;
}

.info-item {
    display: flex;
    gap: 12px;
    min-width: 0;
    padding: 14px 0;
    border-bottom: 1px solid var(--uvp-divider);
}

.info-label {
    flex: 0 0 112px;
    color: var(--uvp-text-tertiary);
    font-size: 13px;
}

.info-value {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    color: var(--uvp-text-primary);
    font-size: 13px;
}

.mono {
    min-width: 0;
    overflow: hidden;
    font-family: var(--zlm-font-mono, ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace);
    text-overflow: ellipsis;
    white-space: nowrap;
}

.copy-btn {
    flex-shrink: 0;
}

.notice-bar {
    display: flex;
    flex-wrap: wrap;
    gap: 8px 16px;
    margin-top: 16px;
    padding: 12px 14px;
    color: var(--uvp-text-tertiary);
    font-size: 12px;
    background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 10px;
}

@media (max-width: 900px) {
    .info-grid {
        grid-template-columns: 1fr;
    }
}

@media (max-width: 768px) {
    .sip-platform-toolbar,
    .panel-head,
    .info-item {
        flex-direction: column;
        align-items: flex-start;
    }

    .info-label {
        flex-basis: auto;
    }

    .info-value {
        width: 100%;
    }
}
</style>
