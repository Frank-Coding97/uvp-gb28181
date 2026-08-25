<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import { BookOpen, Check, ChevronRight, Copy, Download, RefreshCw, Rocket, Server, Settings2, ShieldCheck } from "lucide-vue-next";
import {
    fetchSipPlatformInfo,
    fetchSipSetupStatus,
    type SipPlatformInfo,
    type SipSetupStatus
} from "@/api/gb28181";
import { useUserStoreHook } from "@/store/modules/user";
import SipSetupModal from "@/layout/components/SipSetupModal.vue";
import QrProvisionCard from "./QrProvisionCard.vue";
import { mayEditSipConfig, runtimeColor, runtimeLabel } from "./platformViewState";
import { mayGenerateQr } from "./qrProvisionState";
import { activeSipAddresses } from "./sipSetupRules";
import { writeTextToClipboard } from "@/utils/app";

const loading = ref(false);
const wizardVisible = ref(false);
const status = ref<SipSetupStatus | null>(null);
const platform = ref<SipPlatformInfo | null>(null);
const permissions = computed(() => useUserStoreHook().account.permissions);
const canEdit = computed(() => mayEditSipConfig(permissions.value));
const canGenerateQr = computed(() => mayGenerateQr(permissions.value));
const copiedKey = ref<string>("");

const config = computed(() => status.value?.config);
const deploymentLabel = computed(() =>
    config.value?.deploymentMode === "public" ? "公网部署" : "局域网部署"
);

const serverAddresses = computed(() => {
    const c = config.value;
    if (!c) return [];
    if (c.deploymentMode === "lan" && c.listenIp === "0.0.0.0") return platform.value?.sipIps || [];
    return activeSipAddresses(c.deploymentMode, c.listenIp, c.advertiseIp, []);
});

// 前 3 个字段走通用循环渲染;密码字段因为需要眼睛切换 + 遮罩显示,单独在模板里处理.
const infoFields = computed(() => {
    const c = config.value;
    if (!c) return [];
    return [
        ...(serverAddresses.value.length ? serverAddresses.value : [""]).map((value, index) => ({
            key: `server-${index}`,
            label: index === 0 ? "SIP 服务器地址" : "",
            value
        })),
        { key: "port", label: "SIP 端口", value: String(c.port) },
        { key: "serverId", label: "平台 ID", value: c.serverId },
        { key: "domain", label: "SIP 域", value: c.domain }
    ];
});

// 接入指南三步:对应用户在国标设备/客户端上要填的三块信息
const setupSteps = computed(() => [
    {
        n: 1,
        title: "填写服务器信息",
        desc: '在设备 "SIP 服务器 / 平台设置" 里填入平台的服务器地址和端口。',
        highlights: [
            ...serverAddresses.value.map((value, index) => ({
                label: index === 0 ? "SIP 服务器地址" : "可用地址",
                value
            })),
            { label: "SIP 端口", value: config.value ? String(config.value.port) : "" }
        ]
    },
    {
        n: 2,
        title: "填写平台身份",
        desc: '把平台的 GBID (SIP 服务器 ID) 和 SIP 域填到对应字段。',
        highlights: [
            { label: "平台 ID / GBID", value: config.value?.serverId || "" },
            { label: "SIP 域", value: config.value?.domain || "" }
        ]
    },
    {
        n: 3,
        title: "填写接入密码",
        desc: '在 "注册密码 / 认证密码" 字段填入本平台设置的 SIP 密码,然后启用注册。',
        highlights: []
    }
]);

async function refresh() {
    loading.value = true;
    try {
        const [statusResponse, platformResponse] = await Promise.all([
            fetchSipSetupStatus(),
            fetchSipPlatformInfo()
        ]);
        if (statusResponse.code !== 0) throw new Error(statusResponse.message || "加载失败");
        status.value = statusResponse.data;
        if (platformResponse.code === 0) platform.value = platformResponse.data;
    } catch (error: any) {
        Message.error(error?.message || "加载失败");
    } finally {
        loading.value = false;
    }
}

async function copyValue(key: string, value: string) {
    if (!value || value === "-" || value.startsWith("(")) return;
    if (await writeTextToClipboard(value)) {
        copiedKey.value = key;
        Message.success("已复制到剪贴板");
        setTimeout(() => {
            if (copiedKey.value === key) copiedKey.value = "";
        }, 1500);
        return;
    }
    Message.warning("复制失败,请手动选中");
}

async function copyAll() {
    if (!config.value) return;
    const c = config.value;
    const text = [
        ...(serverAddresses.value.length
            ? serverAddresses.value.map(value => `SIP 服务器地址: ${value}`)
            : ["SIP 服务器地址: (当前无可用地址)"]),
        `SIP 端口: ${c.port}`,
        `平台 ID: ${c.serverId}`,
        `SIP 域: ${c.domain}`,
        `SIP 密码: ${c.password || "(未设置)"}`
    ].join("\n");
    if (await writeTextToClipboard(text)) {
        Message.success("已复制 SIP 接入信息");
        return;
    }
    Message.warning("复制失败,请手动选中");
}

onMounted(refresh);
</script>

<template>
    <div class="snow-fill">
        <div class="snow-fill-inner uvp-page-shell-flat sip-platform-shell">
            <div class="sip-platform-page">
                <!-- 顶部工具栏 -->
                <div class="sip-platform-toolbar">
                    <div class="status-line">
                        <span class="status-label">GB28181 SIP 服务</span>
                        <a-tag v-if="status" :color="runtimeColor(status.runtime.state)" bordered>
                            {{ runtimeLabel(status.runtime.state) }}
                        </a-tag>
                    </div>
                    <div class="toolbar-actions">
                        <a-button v-if="config" @click="copyAll">
                            <template #icon><Copy :size="15" /></template>
                            复制 SIP 接入信息
                        </a-button>
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

                <!-- 失败态告警 -->
                <a-alert v-if="status?.runtime.state === 'failed'" type="error" :show-icon="true">
                    {{ status.runtime.errorSummary || "SIP 服务启动失败,请点击编辑配置修正参数" }}
                </a-alert>

                <a-spin :loading="loading" class="sip-platform-wrapper">
                    <!-- 未配置态 -->
                    <a-empty
                        v-if="status && status.configStatus === 'unconfigured'"
                        description="尚未配置 SIP 服务"
                        class="sip-empty"
                    >
                        <a-button v-if="canEdit" type="primary" @click="wizardVisible = true">开始配置</a-button>
                    </a-empty>

                    <!-- 已配置:双栏布局 -->
                    <div v-else-if="config" class="sip-grid">
                        <!-- 左栏:主内容 -->
                        <div class="sip-grid__main">
                            <!-- 部署方式 -->
                            <section class="sip-card sip-card--intro">
                                <div class="sip-card__icon">
                                    <Rocket :size="18" />
                                </div>
                                <div class="sip-card__intro-body">
                                    <span class="sip-card__intro-label">部署方式</span>
                                    <span class="sip-card__intro-value">{{ deploymentLabel }}</span>
                                </div>
                                <a-tag color="arcoblue" class="sip-card__intro-tag">
                                    {{ config.deploymentMode === "public" ? "跨公网 NAT" : "同局域网" }}
                                </a-tag>
                            </section>

                            <!-- 设备接入信息 -->
                            <section class="sip-card">
                                <header class="sip-card__header">
                                    <span class="sip-card__icon sip-card__icon--soft">
                                        <Server :size="16" />
                                    </span>
                                    <h4>设备接入信息</h4>
                                    <span class="sip-card__header-hint">设备端 SIP 配置填以下参数</span>
                                </header>
                                <div class="sip-card__rows">
                                    <div v-for="field in infoFields" :key="field.key" class="sip-row">
                                        <span class="sip-row__label">{{ field.label }}</span>
                                        <div class="sip-row__body">
                                            <code class="sip-value">{{ field.value || "-" }}</code>
                                            <button
                                                v-if="field.value && field.value !== '-'"
                                                type="button"
                                                class="sip-copy"
                                                :class="{ 'is-copied': copiedKey === field.key }"
                                                :title="copiedKey === field.key ? '已复制' : `复制${field.label}`"
                                                @click="copyValue(field.key, field.value)"
                                            >
                                                <Check v-if="copiedKey === field.key" :size="14" />
                                                <Copy v-else :size="14" />
                                            </button>
                                        </div>
                                    </div>

                                    <!-- 密码单独一行:直接展示明文 + 复制 -->
                                    <div class="sip-row sip-row--full">
                                        <span class="sip-row__label">SIP 密码</span>
                                        <div class="sip-row__body">
                                            <code class="sip-value sip-value--mono">{{ config?.password || "-" }}</code>
                                            <button
                                                v-if="config?.password"
                                                type="button"
                                                class="sip-copy"
                                                :class="{ 'is-copied': copiedKey === 'password' }"
                                                :title="copiedKey === 'password' ? '已复制' : '复制密码'"
                                                @click="copyValue('password', config.password)"
                                            >
                                                <Check v-if="copiedKey === 'password'" :size="14" />
                                                <Copy v-else :size="14" />
                                            </button>
                                        </div>
                                    </div>
                                </div>
                            </section>

                            <!-- 接入指南卡 -->
                            <section class="sip-card sip-card--guide">
                                <header class="sip-card__header">
                                    <span class="sip-card__icon sip-card__icon--soft">
                                        <BookOpen :size="16" />
                                    </span>
                                    <h4>设备接入指南</h4>
                                </header>
                                <ol class="guide-steps">
                                    <li v-for="step in setupSteps" :key="step.n" class="guide-step">
                                        <span class="guide-step__num">{{ step.n }}</span>
                                        <div class="guide-step__body">
                                            <div class="guide-step__title">{{ step.title }}</div>
                                            <p class="guide-step__desc">{{ step.desc }}</p>
                                            <div
                                                v-if="step.highlights.length"
                                                class="guide-step__highlights"
                                            >
                                                <div
                                                    v-for="hi in step.highlights"
                                                    :key="`${hi.label}-${hi.value}`"
                                                    class="guide-step__highlight"
                                                >
                                                    <span class="guide-step__hl-label">{{ hi.label }}</span>
                                                    <code>{{ hi.value || "-" }}</code>
                                                </div>
                                            </div>
                                        </div>
                                    </li>
                                </ol>

                                <a
                                    class="guide-download"
                                    href="https://download.uvplatform.cn/"
                                    target="_blank"
                                    rel="noopener noreferrer"
                                >
                                    <Download :size="15" />
                                    <span>
                                        <strong>国标模拟器下载</strong>
                                        <small>Android 移动端模拟器,可模拟设备接入与联调</small>
                                    </span>
                                    <ChevronRight :size="15" />
                                </a>
                            </section>
                        </div>

                        <!-- 右栏:辅助面板 -->
                        <aside class="sip-grid__side">
                            <!-- 服务状态卡 -->
                            <section class="sip-card sip-card--status">
                                <header class="sip-card__header">
                                    <span class="sip-card__icon sip-card__icon--soft">
                                        <ShieldCheck :size="16" />
                                    </span>
                                    <h4>服务状态</h4>
                                </header>
                                <div class="status-detail">
                                    <div class="status-detail__row">
                                        <span>运行状态</span>
                                        <a-tag :color="runtimeColor(status!.runtime.state)" size="small">
                                            {{ runtimeLabel(status!.runtime.state) }}
                                        </a-tag>
                                    </div>
                                    <div class="status-detail__row">
                                        <span>传输协议</span>
                                        <code>{{ platform?.transport?.join(" / ").toUpperCase() || "UDP / TCP" }}</code>
                                    </div>
                                    <div class="status-detail__row">
                                        <span>监听地址</span>
                                        <code>{{ config.listenIp }}:{{ config.port }}</code>
                                    </div>
                                    <div v-if="status?.runtime.updatedAt" class="status-detail__row">
                                        <span>最近更新</span>
                                        <code class="status-detail__ts">{{
                                            new Date(status.runtime.updatedAt).toLocaleString("zh-CN", { hour12: false })
                                        }}</code>
                                    </div>
                                </div>
                            </section>

                            <!-- 扫码接入卡 -->
                            <QrProvisionCard v-if="canGenerateQr && config" />
                        </aside>
                    </div>
                </a-spin>
            </div>
        </div>
        <SipSetupModal
            :visible="wizardVisible"
            editing
            @close="wizardVisible = false"
            @saved="refresh"
        />
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

.status-line,
.toolbar-actions {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
}
.toolbar-actions :deep(.arco-btn) { box-sizing: border-box; height: 44px; min-height: 44px; border-radius: 10px; }

.status-label {
    color: var(--uvp-text-tertiary);
    font-size: 12px;
}

.sip-platform-page > :deep(.arco-alert) {
    margin-bottom: 14px;
}

.sip-platform-wrapper {
    display: block;
    width: 100%;
    min-height: 260px;
}

.sip-empty {
    padding: 60px 0;
}

/* --- 双栏 grid --- */
.sip-grid {
    display: grid;
    grid-template-columns: minmax(0, 1.7fr) minmax(300px, 1fr);
    gap: 18px;
    align-items: start;
}

.sip-grid__main,
.sip-grid__side {
    display: grid;
    gap: 16px;
    min-width: 0;
}

/* --- 卡片通用 --- */
.sip-card {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 12px 14px;
    background: var(--uvp-panel-bg, #ffffff);
    border: 1px solid var(--uvp-panel-border, #e8edf5);
    border-radius: 12px;
}

.sip-card__header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding-bottom: 8px;
    border-bottom: 1px dashed var(--uvp-panel-border, #e8edf5);
}

.sip-card__header h4 {
    margin: 0;
    font-size: 13px;
    font-weight: 600;
    color: var(--uvp-text-primary, #1f2937);
}

.sip-card__header-hint {
    margin-left: auto;
    color: var(--uvp-text-tertiary, #6b7280);
    font-size: 11.5px;
}

.sip-card__icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 30px;
    height: 30px;
    color: #ffffff;
    background: var(--uvp-brand, #2563eb);
    border-radius: 8px;
    flex-shrink: 0;
}

.sip-card__icon--soft {
    width: 26px;
    height: 26px;
    color: var(--uvp-brand, #2563eb);
    background: var(--uvp-brand-soft, #e8f2ff);
    border-radius: 7px;
}

/* --- 部署方式卡 --- */
.sip-card--intro {
    flex-direction: row;
    align-items: center;
    gap: 12px;
    padding: 12px 16px;
    background: linear-gradient(120deg, var(--uvp-brand-soft, #e8f2ff) 0%, var(--uvp-panel-bg, #ffffff) 62%);
    border-color: rgb(37 99 235 / 18%);
}

.sip-card__intro-body {
    display: flex;
    flex-direction: column;
    gap: 2px;
    flex: 1;
    min-width: 0;
}

.sip-card__intro-label {
    font-size: 11.5px;
    color: var(--uvp-text-tertiary, #6b7280);
    letter-spacing: 0.4px;
}

.sip-card__intro-value {
    font-size: 15px;
    font-weight: 620;
    color: var(--uvp-text-primary, #1f2937);
}

.sip-card__intro-tag {
    flex-shrink: 0;
}

/* --- 主字段行 --- */
.sip-card__rows {
    display: grid;
    gap: 12px;
}

.sip-row {
    display: grid;
    grid-template-columns: 140px minmax(0, 1fr);
    gap: 14px;
    align-items: center;
    padding: 6px 2px;
}

.sip-row__label {
    color: var(--uvp-text-tertiary, #6b7280);
    font-size: 13px;
    line-height: 1.5;
}

.sip-row__body {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
}

.sip-value {
    min-width: 0;
    overflow-wrap: anywhere;
    color: var(--uvp-brand-strong, #1d4ed8);
    font-family: "SFMono-Regular", Consolas, Menlo, monospace;
    font-size: 14.5px;
    font-weight: 600;
    letter-spacing: 0;
}

.sip-value--mono {
    letter-spacing: 0.5px;
}

.sip-copy {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    padding: 0;
    background: transparent;
    border: 1px solid transparent;
    border-radius: 6px;
    color: var(--uvp-text-tertiary, #6b7280);
    cursor: pointer;
    transition: all 160ms ease;
    flex-shrink: 0;
}

.sip-copy:hover {
    background: var(--uvp-brand-soft, #e8f2ff);
    color: var(--uvp-brand, #2563eb);
    border-color: rgb(37 99 235 / 22%);
}

.sip-copy.is-copied {
    background: var(--uvp-success-soft, #ecfdf5);
    color: #059669;
    border-color: rgb(5 150 105 / 30%);
}

/* --- 服务状态卡 --- */
.status-detail {
    display: grid;
    gap: 6px;
    padding-top: 0;
}

.status-detail__row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 2px;
    font-size: 12.5px;
}

.status-detail__row > span {
    color: var(--uvp-text-tertiary, #6b7280);
}

.status-detail__row code {
    color: var(--uvp-text-primary, #1f2937);
    font-family: "SFMono-Regular", Consolas, Menlo, monospace;
    font-size: 12.5px;
}

.status-detail__ts {
    font-size: 11.5px !important;
    color: var(--uvp-text-secondary, #4b5563) !important;
}

/* --- 接入指南卡 --- */
.guide-steps {
    list-style: none;
    padding: 0;
    margin: 0;
    display: grid;
    gap: 18px;
}

.guide-step {
    display: grid;
    grid-template-columns: 24px minmax(0, 1fr);
    gap: 10px;
    align-items: flex-start;
}

.guide-step__num {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    background: var(--uvp-brand-soft, #e8f2ff);
    color: var(--uvp-brand-strong, #1d4ed8);
    border-radius: 50%;
    font-size: 12px;
    font-weight: 700;
    margin-top: 1px;
}

.guide-step__body {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
}

.guide-step__title {
    font-size: 13px;
    font-weight: 600;
    color: var(--uvp-text-primary, #1f2937);
    line-height: 1.4;
}

.guide-step__desc {
    margin: 0;
    color: var(--uvp-text-tertiary, #6b7280);
    font-size: 12px;
    line-height: 1.55;
}

.guide-step__highlights {
    display: grid;
    gap: 4px;
    margin-top: 4px;
    padding: 8px 10px;
    background: var(--uvp-shell-muted, #eef4f8);
    border-radius: 8px;
}

.guide-step__highlight {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    min-width: 0;
    font-size: 12px;
}

.guide-step__hl-label {
    color: var(--uvp-text-tertiary, #6b7280);
    flex-shrink: 0;
}

.guide-step__highlight code {
    min-width: 0;
    overflow-wrap: anywhere;
    color: var(--uvp-brand-strong, #1d4ed8);
    font-family: "SFMono-Regular", Consolas, Menlo, monospace;
    font-weight: 600;
    text-align: right;
}

.guide-download {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: 6px;
    padding: 12px 14px;
    color: var(--uvp-brand-strong, #1d4ed8);
    background: var(--uvp-brand-soft, #e8f2ff);
    border: 1px solid rgb(37 99 235 / 18%);
    border-radius: 8px;
    text-decoration: none;
    transition: background-color 160ms ease, border-color 160ms ease;
}

.guide-download:hover {
    background: color-mix(in srgb, var(--uvp-brand, #2563eb) 18%, var(--uvp-panel-bg, #ffffff));
    border-color: rgb(37 99 235 / 32%);
}

.guide-download > span {
    display: flex;
    flex: 1;
    min-width: 0;
    flex-direction: column;
    gap: 2px;
}

.guide-download strong {
    color: var(--uvp-text-primary, #1f2937);
    font-size: 12.5px;
}

.guide-download small {
    color: var(--uvp-text-tertiary, #6b7280);
    font-size: 11.5px;
    line-height: 1.45;
}

.guide-download > svg:last-child {
    flex-shrink: 0;
    color: var(--uvp-text-tertiary, #6b7280);
}

@media (max-width: 1080px) {
    .sip-grid {
        grid-template-columns: 1fr;
    }
}

/* 桌面端内容尽量一屏放下:字段两列 */
@media (min-width: 1081px) {
    .sip-card__rows {
        grid-template-columns: repeat(2, minmax(0, 1fr));
        column-gap: 22px;
    }

    .sip-row--full {
        grid-column: 1 / -1;
    }
}

@media (max-width: 640px) {
    .sip-platform-toolbar {
        flex-direction: column;
        align-items: flex-start;
    }

    .toolbar-actions {
        width: 100%;
        justify-content: flex-end;
    }

    .sip-row {
        grid-template-columns: 1fr;
        gap: 4px;
    }
}
</style>
