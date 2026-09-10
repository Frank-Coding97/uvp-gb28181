<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { AlertTriangle, CircleX, Clock3, Loader2, RefreshCcw } from "@lucide/vue";
import { getDevice, rebootDevice, type DeviceOperationResult, type DeviceVO } from "./api";

const props = withDefaults(defineProps<{
    visible: boolean;
    device: DeviceVO | null;
    canReboot: boolean;
    blockedReason?: string;
    result?: DeviceOperationResult | null;
}>(), {
    device: null,
    canReboot: false,
    blockedReason: "",
    result: null
});

const emit = defineEmits<{
    "update:visible": [value: boolean];
    deviceUpdated: [device: DeviceVO];
    operationUpdated: [result: DeviceOperationResult];
    viewRecords: [operationId?: string];
    submissionUncertain: [];
}>();

const currentDevice = ref<DeviceVO | null>(props.device);
const loading = ref(false);
const deviceLoaded = ref(false);
const deviceLoadError = ref("");
const submitPending = ref(false);
const submissionError = ref("");
const localResult = ref<DeviceOperationResult | null>(props.result ?? null);
const hasSubmitted = ref(Boolean(props.result));

let requestVersion = 0;

const deviceName = computed(() => currentDevice.value?.alias?.trim() || currentDevice.value?.name?.trim() || "未命名设备");
const deviceOnline = computed(() => currentDevice.value?.online === true);
const channelCount = computed(() => currentDevice.value?.channelCount ?? 0);
const channelOnlineCount = computed(() => currentDevice.value?.channelOnlineCount ?? 0);
const displayResult = computed(() => props.result ?? localResult.value);
const resultVisible = computed(() => hasSubmitted.value || Boolean(displayResult.value));
const blockedMessage = computed(() => props.blockedReason?.trim() || "");

const unavailableReason = computed(() => {
    if (loading.value) return "正在读取设备最新状态，请稍候。";
    if (deviceLoadError.value) return "设备信息加载失败，无法执行重启。";
    if (!deviceLoaded.value || !currentDevice.value) return "设备信息未就绪，无法执行重启。";
    if (hasSubmitted.value || displayResult.value) return "本次重启请求已提交，请查看对应记录。";
    if (!props.canReboot) return "当前账号没有设备重启权限。";
    if (!deviceOnline.value) return "设备离线，可查看记录，但不能发送重启请求。";
    if (blockedMessage.value) return blockedMessage.value;
    return "";
});

const submitDisabled = computed(() => Boolean(unavailableReason.value) || submitPending.value);

function statusKey(value?: string | null) {
    return String(value || "unknown").toLowerCase();
}

function statusText(value?: string | null) {
    return ({
        queued: "请求已排队",
        pending: "请求处理中",
        sent: "请求已发送",
        accepted: "请求已受理",
        rejected: "请求已拒绝",
        cancelled: "请求已取消",
        failed: "请求失败",
        timeout: "结果未知",
        unknown: "结果未知"
    } as Record<string, string>)[statusKey(value)] || "结果未知";
}

function statusTone(value?: string | null) {
    const key = statusKey(value);
    if (key === "failed" || key === "rejected" || key === "cancelled") return "danger";
    if (key === "timeout" || key === "unknown") return "warning";
    if (key === "queued" || key === "pending") return "pending";
    return "neutral";
}

function resultDetail(result: DeviceOperationResult) {
    const key = statusKey(result.status);
    if (key === "sent") return "平台已发出重启请求，设备执行结果尚未回传。";
    if (key === "accepted") return "设备已受理重启请求，等待设备状态更新。";
    if (key === "queued") return "重启请求已进入发送队列，等待平台处理。";
    if (key === "pending") return "重启请求正在处理中，请等待设备状态更新。";
    if (key === "unknown" || key === "timeout") {
        return result.errorMessage?.trim() || "请求结果未知，请在维护记录中确认，暂不要重复提交。";
    }
    return result.errorMessage?.trim() || "请在维护记录中查看设备返回结果。";
}

function shouldPreserveLocalResult(result: DeviceOperationResult) {
    const key = statusKey(result.status);
    if (key === "unknown" || key === "timeout") return true;
    if (key === "queued" || key === "pending" || key === "accepted") return true;
    return key === "sent" && result.responseRequired === true;
}

function createIdempotencyKey() {
    if (typeof globalThis.crypto?.randomUUID === "function") return globalThis.crypto.randomUUID();
    return `device-reboot-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

function clearSession() {
    requestVersion += 1;
    loading.value = false;
    deviceLoaded.value = false;
    deviceLoadError.value = "";
    submitPending.value = false;
    submissionError.value = "";
}

function isActive(version: number, deviceId: number) {
    return version === requestVersion && props.visible && props.device?.id === deviceId;
}

async function loadDevice(version: number, deviceId: number) {
    loading.value = true;
    deviceLoaded.value = false;
    deviceLoadError.value = "";
    try {
        const response = await getDevice(deviceId);
        if (!isActive(version, deviceId)) return;
        if (response.code !== 0 || !response.data) {
            deviceLoadError.value = response.message ? `设备信息加载失败：${response.message}` : "设备信息加载失败";
            return;
        }
        currentDevice.value = response.data;
        deviceLoaded.value = true;
        emit("deviceUpdated", response.data);
    } catch (error: any) {
        if (isActive(version, deviceId)) {
            deviceLoadError.value = error?.message ? `设备信息加载失败：${error.message}` : "设备信息加载失败";
        }
    } finally {
        if (isActive(version, deviceId)) loading.value = false;
    }
}

function closeDialog() {
    if (submitPending.value) return;
    emit("update:visible", false);
}

function handleModalVisible(nextVisible: boolean) {
    if (nextVisible) {
        emit("update:visible", true);
        return;
    }
    closeDialog();
}

function viewRecords() {
    emit("viewRecords", displayResult.value?.operationId);
}

async function confirmReboot() {
    const device = currentDevice.value;
    const version = requestVersion;
    if (!device || submitDisabled.value || !deviceLoaded.value || !props.visible) return;

    submitPending.value = true;
    submissionError.value = "";
    let responseReturned = false;
    try {
        const response = await rebootDevice(device.id, {
            confirmed: true,
            idempotencyKey: createIdempotencyKey()
        });
        if (!isActive(version, device.id)) return;
        responseReturned = true;
        if (response.code !== 0 || !response.data) {
            submissionError.value = response.message || "重启请求发送失败";
            return;
        }
        localResult.value = response.data;
        hasSubmitted.value = true;
        emit("operationUpdated", response.data);
    } catch (error: any) {
        if (!isActive(version, device.id)) return;
        if (!responseReturned) {
            const unknownResult: DeviceOperationResult = {
                action: "teleboot",
                status: "unknown",
                targetScope: "device",
                targetCode: device.deviceId,
                errorMessage: "请求结果未知，请在维护记录中确认，暂不要重复提交。"
            };
            localResult.value = unknownResult;
            hasSubmitted.value = true;
            emit("submissionUncertain");
        } else {
            submissionError.value = error?.message || "重启请求发送失败";
        }
    } finally {
        if (isActive(version, device.id)) submitPending.value = false;
    }
}

watch([() => props.visible, () => props.device?.id], ([visible, deviceId], previous) => {
    const previousDeviceId = currentDevice.value?.id;
    const nextDeviceId = typeof deviceId === "number" ? deviceId : undefined;
    const deviceChanged = previousDeviceId !== nextDeviceId;
    const justOpened = visible && previous?.[0] === false;

    clearSession();
    currentDevice.value = props.device;
    if (deviceChanged) {
        localResult.value = props.result ?? null;
        hasSubmitted.value = Boolean(props.result);
    } else if (props.result) {
        localResult.value = props.result;
        hasSubmitted.value = true;
    } else if (justOpened && localResult.value && !shouldPreserveLocalResult(localResult.value)) {
        localResult.value = null;
        hasSubmitted.value = false;
    }

    if (visible && nextDeviceId !== undefined) void loadDevice(requestVersion, nextDeviceId);
}, { immediate: true });

watch(() => props.device, device => {
    if (device?.id === currentDevice.value?.id) currentDevice.value = device;
}, { deep: true });

watch(() => props.result, result => {
    if (!result) return;
    localResult.value = result;
    hasSubmitted.value = true;
});

onBeforeUnmount(() => clearSession());
</script>

<template>
    <a-modal
        :visible="visible"
        modal-class="uvp-system-dialog device-reboot-dialog"
        width="min(480px, calc(100vw - 32px))"
        :footer="false"
        :mask-closable="!submitPending"
        :closable="!submitPending"
        unmount-on-close
        @update:visible="handleModalVisible"
        @cancel="closeDialog"
    >
        <template #title>
            <span class="reboot-title"><RefreshCcw :size="17" />重启设备</span>
        </template>

        <a-spin :loading="loading" class="reboot-spin">
            <div class="reboot-body" data-testid="device-reboot-body">
                <section class="reboot-device-card" data-testid="device-reboot-device">
                    <div class="reboot-device-heading">
                        <div class="reboot-device-icon"><RefreshCcw :size="20" /></div>
                        <div class="reboot-device-identity">
                            <strong>{{ deviceName }}</strong>
                            <span class="mono">{{ currentDevice?.deviceId || props.device?.deviceId || '-' }}</span>
                        </div>
                        <span class="reboot-status" :class="{ online: deviceOnline }">
                            <span class="reboot-status-dot"></span>{{ deviceOnline ? '设备在线' : '设备离线' }}
                        </span>
                    </div>
                    <div class="reboot-device-facts">
                        <div><span>厂商 / 型号</span><strong>{{ [currentDevice?.manufacturer, currentDevice?.model].filter(Boolean).join(' / ') || '未上报' }}</strong></div>
                        <div><span>通道影响</span><strong>{{ channelOnlineCount }} / {{ channelCount }} 在线</strong></div>
                    </div>
                </section>

                <section v-if="resultVisible && displayResult" class="reboot-result-card" data-testid="device-reboot-result" role="status">
                    <div class="reboot-result-icon" :class="`tone-${statusTone(displayResult.status)}`">
                        <RefreshCcw v-if="statusTone(displayResult.status) === 'neutral'" :size="19" />
                        <Clock3 v-else-if="statusTone(displayResult.status) === 'pending'" :size="19" />
                        <CircleX v-else :size="19" />
                    </div>
                    <div class="reboot-result-copy">
                        <strong>{{ statusText(displayResult.status) }}</strong>
                        <span>{{ resultDetail(displayResult) }}</span>
                        <span v-if="displayResult.operationId" class="mono">操作编号：{{ displayResult.operationId }}</span>
                    </div>
                    <p v-if="displayResult.deduplicated" class="reboot-deduplicated">已有相同重启请求，已返回原操作。</p>
                    <div class="reboot-result-actions">
                        <a-button data-testid="device-reboot-view-records" @click="viewRecords">
                            查看本次记录
                        </a-button>
                        <a-button data-testid="device-reboot-result-close" :disabled="submitPending" @click="closeDialog">关闭</a-button>
                    </div>
                </section>

                <section v-else class="reboot-confirm-card" data-testid="device-reboot-confirmation" role="alertdialog" aria-label="确认重启设备">
                    <div class="reboot-confirm-heading">
                        <div class="reboot-confirm-icon"><AlertTriangle :size="20" /></div>
                        <div><strong>确认重启设备？</strong><span>这是一项整机级操作</span></div>
                    </div>
                    <p class="reboot-impact">整台设备及所属通道都会受影响（当前 {{ channelCount }} 个通道），当前播放可能中断，设备也会短暂离线。</p>
                    <p v-if="deviceLoadError" class="reboot-hint warning" role="alert"><CircleX :size="14" />{{ deviceLoadError }}</p>
                    <p v-else-if="unavailableReason" class="reboot-hint warning"><CircleX :size="14" />{{ unavailableReason }}</p>
                    <p v-else class="reboot-hint"><AlertTriangle :size="14" />“请求已发送”只代表平台发出请求，不代表设备已经完成重启。</p>
                    <p v-if="submissionError" class="reboot-error" role="alert"><CircleX :size="14" />{{ submissionError }}</p>
                    <div class="reboot-confirm-actions">
                        <a-button data-testid="device-reboot-cancel" :disabled="submitPending" @click="closeDialog">取消</a-button>
                        <a-button data-testid="device-reboot-confirm" type="primary" :disabled="submitDisabled" :loading="submitPending" @click="confirmReboot">
                            <template #icon><Loader2 v-if="submitPending" :size="14" class="spin" /><RefreshCcw v-else :size="14" /></template>
                            {{ submitPending ? '正在发送' : '确认重启' }}
                        </a-button>
                    </div>
                </section>
            </div>
        </a-spin>
    </a-modal>
</template>

<style scoped>
.reboot-body { display: grid; gap: 12px; color: var(--uvp-text-primary); }
.reboot-title { display: inline-flex; align-items: center; gap: 7px; color: var(--uvp-text-primary); font-weight: 650; }
.reboot-device-card, .reboot-confirm-card, .reboot-result-card { padding: 15px; border: 1px solid var(--uvp-panel-border); border-radius: 12px; background: var(--uvp-panel-bg); }
.reboot-device-heading, .reboot-confirm-heading { display: flex; align-items: center; gap: 10px; }
.reboot-device-heading { min-width: 0; }
.reboot-device-icon, .reboot-confirm-icon, .reboot-result-icon { display: grid; place-items: center; flex: 0 0 auto; width: 36px; height: 36px; border-radius: 10px; color: var(--uvp-brand); background: var(--uvp-brand-soft); }
.reboot-device-identity, .reboot-confirm-heading > div:last-child, .reboot-result-copy { display: grid; min-width: 0; gap: 3px; }
.reboot-device-identity strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.reboot-device-identity span, .reboot-confirm-heading span, .reboot-impact, .reboot-hint, .reboot-result-copy span { color: var(--uvp-text-tertiary); font-size: 12px; }
.reboot-status { display: inline-flex; align-items: center; gap: 5px; padding: 4px 9px; margin-left: auto; color: var(--uvp-danger); background: var(--uvp-danger-soft); border: 1px solid var(--uvp-danger-border); border-radius: 999px; font-size: 12px; white-space: nowrap; }
.reboot-status.online { color: var(--uvp-brand-cyan); background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent); border-color: color-mix(in srgb, var(--uvp-brand-cyan) 26%, transparent); }
.reboot-status-dot { width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
.reboot-device-facts { display: grid; grid-template-columns: 1.2fr .8fr; gap: 12px; padding-top: 13px; margin-top: 13px; border-top: 1px solid var(--uvp-panel-border); }
.reboot-device-facts div { display: grid; min-width: 0; gap: 4px; }
.reboot-device-facts span { color: var(--uvp-text-tertiary); font-size: 11px; }
.reboot-device-facts strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; }
.reboot-confirm-card, .reboot-result-card { background: var(--uvp-list-toolbar-bg); }
.reboot-confirm-heading { color: var(--uvp-warning); }
.reboot-confirm-heading strong, .reboot-result-copy strong { color: var(--uvp-text-primary); font-size: 15px; }
.reboot-confirm-heading span { display: block; margin-top: 2px; }
.reboot-impact { padding: 11px 12px; margin: 14px 0 0; color: var(--uvp-text-secondary); line-height: 1.55; background: var(--uvp-warning-soft); border: 1px solid var(--uvp-warning-border); border-radius: 9px; }
.reboot-hint, .reboot-error { display: flex; align-items: center; gap: 6px; margin: 10px 0 0; line-height: 1.45; }
.reboot-hint.warning { color: var(--uvp-warning); }
.reboot-error { padding: 8px 10px; color: var(--uvp-danger); background: var(--uvp-danger-soft); border: 1px solid var(--uvp-danger-border); border-radius: 8px; }
.reboot-confirm-actions, .reboot-result-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 17px; }
.reboot-confirm-actions :deep(.arco-btn[disabled]), .reboot-confirm-actions :deep(.arco-btn-disabled), .reboot-result-actions :deep(.arco-btn[disabled]), .reboot-result-actions :deep(.arco-btn-disabled) { color: var(--uvp-text-tertiary) !important; background: var(--uvp-panel-bg) !important; border-color: var(--uvp-panel-border) !important; box-shadow: none !important; cursor: not-allowed; opacity: .58; }
.reboot-result-card { display: grid; grid-template-columns: auto minmax(0, 1fr); align-items: start; gap: 10px; }
.reboot-result-icon.tone-neutral { color: var(--uvp-brand-cyan); background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent); }
.reboot-result-icon.tone-pending { color: var(--uvp-warning); background: var(--uvp-warning-soft); }
.reboot-result-icon.tone-warning { color: var(--uvp-warning); background: var(--uvp-warning-soft); }
.reboot-result-icon.tone-danger { color: var(--uvp-danger); background: var(--uvp-danger-soft); }
.reboot-result-copy span { line-height: 1.45; }
.reboot-result-copy .mono { color: var(--uvp-text-tertiary); font-size: 11px; }
.reboot-deduplicated { grid-column: 1 / -1; padding: 8px 10px; margin: 0; color: var(--uvp-brand-cyan); background: color-mix(in srgb, var(--uvp-brand-cyan) 9%, transparent); border-radius: 8px; font-size: 12px; }
.reboot-result-actions { grid-column: 1 / -1; margin-top: 5px; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.spin { animation: spin .8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 480px) {
    .reboot-device-facts { grid-template-columns: 1fr; }
    .reboot-status { align-self: flex-start; }
    .reboot-device-heading { align-items: flex-start; flex-wrap: wrap; }
    .reboot-status { margin-left: 46px; }
    .reboot-confirm-actions, .reboot-result-actions { flex-wrap: wrap; }
}
</style>
