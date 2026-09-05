<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { AlertTriangle, CircleCheck, CircleX, Clock3, Loader2, RefreshCcw, RotateCcw } from "@lucide/vue";
import { Message } from "@arco-design/web-vue";
import {
    getDevice,
    listMaintenanceOperations,
    rebootDevice,
    type DeviceOperationResult,
    type DeviceVO,
    type MaintenanceOperation
} from "./api";
import DeviceFirmwareUpgradePanel from "./DeviceFirmwareUpgradePanel.vue";

const props = withDefaults(defineProps<{
    visible: boolean;
    device: DeviceVO | null;
    canReboot: boolean;
    canUpgrade: boolean;
}>(), {
    device: null,
    canReboot: false,
    canUpgrade: false
});

const emit = defineEmits<{
    "update:visible": [value: boolean];
    deviceUpdated: [device: DeviceVO];
}>();

const PAGE_SIZE = 10;
const POLL_INTERVAL_MS = 2000;
const MAX_POLL_MS = 60_000;

const currentDevice = ref<DeviceVO | null>(props.device);
const loading = ref(false);
const operationsLoading = ref(false);
const operations = ref<MaintenanceOperation[]>([]);
const operationPage = ref(1);
const operationTotal = ref(0);
const confirmVisible = ref(false);
const rebootPending = ref(false);
const rebootResult = ref<DeviceOperationResult | null>(null);
const rebootError = ref("");
const pollTimedOut = ref(false);
const firmwareUpgradeBusy = ref(false);

let requestVersion = 0;
let pollTimer: ReturnType<typeof setTimeout> | null = null;

const deviceName = computed(() => currentDevice.value?.alias?.trim() || currentDevice.value?.name?.trim() || "未命名设备");
const deviceVendor = computed(() => [currentDevice.value?.manufacturer, currentDevice.value?.model].filter(Boolean).join(" / ") || "未上报");
const deviceOnline = computed(() => currentDevice.value?.online === true);
const rebootOwnBusy = computed(() => rebootPending.value || (!pollTimedOut.value && isPendingStatus(rebootResult.value?.status, rebootResult.value?.responseRequired)));
const rebootDisabled = computed(() => rebootOwnBusy.value || firmwareUpgradeBusy.value);

function clearPollTimer() {
    if (pollTimer !== null) {
        clearTimeout(pollTimer);
        pollTimer = null;
    }
}

function invalidateRequests() {
    requestVersion += 1;
    clearPollTimer();
}

function isActive(version: number, deviceId: number) {
    return version === requestVersion && props.visible && props.device?.id === deviceId;
}

function formatDateTime(value?: string | null) {
    if (!value || value.startsWith("0001-01-01")) return "-";
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? "-" : date.toLocaleString("zh-CN", { hour12: false });
}

function statusKey(value?: string | null) {
    return String(value || "unknown").toLowerCase();
}

function statusText(value?: string | null) {
    return ({
        queued: "排队中",
        pending: "处理中",
        sent: "已发送",
        accepted: "已受理",
        rejected: "已拒绝",
        cancelled: "已取消",
        failed: "失败",
        timeout: "结果未知",
        unknown: "结果未知"
    } as Record<string, string>)[statusKey(value)] || String(value || "结果未知");
}

function statusTone(value?: string | null) {
    const key = statusKey(value);
    if (key === "failed" || key === "rejected" || key === "cancelled") return "danger";
    if (key === "timeout" || key === "unknown") return "warning";
    if (key === "sent") return "success";
    return "pending";
}

function isPendingStatus(status?: string | null, responseRequired?: boolean) {
    const key = statusKey(status);
    return key === "queued" || key === "pending" || key === "accepted" || (key === "sent" && responseRequired === true);
}

function operationReason(operation: MaintenanceOperation) {
    if (operation.errorMessage?.trim()) return operation.errorMessage;
    if (operation.sipStatus === 0) return "无响应";
    if (operation.sipStatus != null && operation.sipStatus > 0) return `SIP ${operation.sipStatus}`;
    if (statusKey(operation.status) === "sent") return "平台已发送请求，设备执行结果未回传";
    if (statusKey(operation.status) === "timeout" || statusKey(operation.status) === "unknown") return "设备执行结果未知";
    return "-";
}

function actorText(actorId?: number | null) {
    return actorId && actorId > 0 ? String(actorId) : "未记录";
}

function operationStatus(operation: MaintenanceOperation) {
    if (pollTimedOut.value && operation.operationId === rebootResult.value?.operationId && isPendingStatus(operation.status, operation.responseRequired)) {
        return "结果未知";
    }
    return statusText(operation.status);
}

function operationTone(operation: MaintenanceOperation) {
    if (pollTimedOut.value && operation.operationId === rebootResult.value?.operationId && isPendingStatus(operation.status, operation.responseRequired)) {
        return "warning";
    }
    return statusTone(operation.status);
}

function replaceResultFromOperation(operation: MaintenanceOperation) {
    if (!rebootResult.value || rebootResult.value.operationId !== operation.operationId) return;
    rebootResult.value = {
        ...rebootResult.value,
        action: operation.action,
        status: operation.status,
        responseRequired: operation.responseRequired,
        targetCode: operation.targetCode,
        errorMessage: operation.errorMessage,
        completedAt: operation.completedAt
    };
    if (!isPendingStatus(operation.status, operation.responseRequired)) clearPollTimer();
}

async function refreshDevice(version: number, deviceId: number) {
    try {
        const result = await getDevice(deviceId);
        if (!isActive(version, deviceId) || result.code !== 0 || !result.data) return;
        currentDevice.value = result.data;
        emit("deviceUpdated", result.data);
    } catch {
        // The operation result remains visible; the next normal page refresh can re-read device state.
    }
}

function parseDeadline(deadlineAt?: string | null) {
    const timestamp = deadlineAt ? new Date(deadlineAt).getTime() : Number.NaN;
    return Number.isFinite(timestamp) ? timestamp : Date.now() + MAX_POLL_MS;
}

function schedulePoll(result: DeviceOperationResult, version: number, deviceId: number, deadlineOverride?: number) {
    clearPollTimer();
    if (!result.operationId || !isPendingStatus(result.status, result.responseRequired)) return;
    const deadline = deadlineOverride ?? parseDeadline(result.deadlineAt);
    if (Date.now() >= deadline) {
        pollTimedOut.value = true;
        rebootPending.value = false;
        return;
    }
    pollTimer = setTimeout(() => {
        pollTimer = null;
        void pollOperations(version, deviceId, result.operationId!, deadline);
    }, POLL_INTERVAL_MS);
}

async function pollOperations(version: number, deviceId: number, operationId: string, deadline: number) {
    if (!isActive(version, deviceId)) return;
    if (Date.now() >= deadline) {
        pollTimedOut.value = true;
        rebootPending.value = false;
        return;
    }
    try {
        const result = await listMaintenanceOperations(deviceId, { page: operationPage.value, pageSize: PAGE_SIZE });
        if (!isActive(version, deviceId)) return;
        if (result.code === 0) {
            operations.value = result.data?.list || [];
            operationTotal.value = result.data?.total || 0;
            const operation = operations.value.find(item => item.operationId === operationId);
            if (operation) {
                replaceResultFromOperation(operation);
                if (!isPendingStatus(operation.status, operation.responseRequired)) {
                    rebootPending.value = false;
                    return;
                }
            }
        }
    } catch {
        // A failed GET is not a failed reboot. Keep polling until the deadline, without retrying POST.
    }
    schedulePoll({
        ...(rebootResult.value || { action: "teleboot" }),
        operationId,
        status: rebootResult.value?.status || "pending",
        responseRequired: rebootResult.value?.responseRequired
    }, version, deviceId, deadline);
}

async function loadOperations(version: number, deviceId: number, page = operationPage.value) {
    if (!isActive(version, deviceId)) return;
    operationsLoading.value = true;
    try {
        const result = await listMaintenanceOperations(deviceId, { page, pageSize: PAGE_SIZE });
        if (!isActive(version, deviceId)) return;
        if (result.code !== 0) throw new Error(result.message || "维护记录加载失败");
        operations.value = result.data?.list || [];
        operationTotal.value = result.data?.total || 0;
        operationPage.value = result.data?.page || page;
        const activeOperation = rebootResult.value?.operationId
            ? operations.value.find(item => item.operationId === rebootResult.value?.operationId)
            : undefined;
        if (activeOperation) replaceResultFromOperation(activeOperation);
    } catch (error: any) {
        if (isActive(version, deviceId)) Message.error(error?.message || "维护记录加载失败");
    } finally {
        if (isActive(version, deviceId)) operationsLoading.value = false;
    }
}

async function load(version: number, deviceId: number) {
    loading.value = true;
    operations.value = [];
    operationTotal.value = 0;
    operationPage.value = 1;
    try {
        const result = await getDevice(deviceId);
        if (isActive(version, deviceId) && result.code === 0 && result.data) {
            currentDevice.value = result.data;
            emit("deviceUpdated", result.data);
        }
    } catch (error: any) {
        if (isActive(version, deviceId)) Message.error(error?.message || "设备详情加载失败");
    } finally {
        if (isActive(version, deviceId)) loading.value = false;
    }
    await loadOperations(version, deviceId, 1);
}

function createIdempotencyKey() {
    if (typeof globalThis.crypto?.randomUUID === "function") return globalThis.crypto.randomUUID();
    return `device-reboot-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

function notifyRebootResult(result: DeviceOperationResult) {
    const key = statusKey(result.status);
    if (result.deduplicated) {
        Message.info("已有相同重启请求，已返回原操作");
        return;
    }
    if (key === "sent") {
        Message.success("重启请求已发送，设备执行结果未回传");
        return;
    }
    if (key === "queued" || key === "pending") {
        Message.info("重启请求已排队，等待发送");
        return;
    }
    if (key === "accepted") {
        Message.info("重启请求已受理，设备执行结果未回传");
        return;
    }
    if (key === "unknown" || key === "timeout") {
        Message.warning("重启结果未知，请根据设备状态确认");
        return;
    }
    if (key === "rejected" || key === "failed" || key === "cancelled") {
        Message.error(result.errorMessage || "重启请求未被设备接受");
        return;
    }
    Message.info(`重启请求状态：${statusText(result.status)}`);
}

function requestReboot() {
    if (!currentDevice.value || !props.canReboot || !deviceOnline.value || rebootDisabled.value) return;
    rebootError.value = "";
    confirmVisible.value = true;
}

function cancelReboot() {
    if (rebootPending.value) return;
    confirmVisible.value = false;
}

async function confirmReboot() {
    const device = currentDevice.value;
    const version = requestVersion;
    if (!device || !props.visible || !props.canReboot || !device.online || rebootDisabled.value) return;
    confirmVisible.value = false;
    rebootPending.value = true;
    rebootError.value = "";
    pollTimedOut.value = false;
    try {
        const result = await rebootDevice(device.id, { confirmed: true, idempotencyKey: createIdempotencyKey() });
        if (!isActive(version, device.id)) return;
        if (result.code !== 0 || !result.data) throw new Error(result.message || "重启请求发送失败");
        rebootResult.value = result.data;
        notifyRebootResult(result.data);
        await refreshDevice(version, device.id);
        await loadOperations(version, device.id, 1);
        schedulePoll(result.data, version, device.id);
    } catch (error: any) {
        if (isActive(version, device.id)) {
            rebootError.value = error?.message || "重启请求发送失败";
            Message.error(rebootError.value);
        }
    } finally {
        if (isActive(version, device.id)) rebootPending.value = false;
    }
}

function changePage(page: number) {
    const deviceId = currentDevice.value?.id;
    if (!deviceId || !props.visible || operationsLoading.value) return;
    operationPage.value = page;
    void loadOperations(requestVersion, deviceId, page);
}

function onFirmwareUpgradeBusy(value: boolean) {
    firmwareUpgradeBusy.value = value;
}

function onFirmwareUpdated(firmware: string) {
    if (!currentDevice.value || !firmware.trim()) return;
    currentDevice.value = { ...currentDevice.value, firmware };
    emit("deviceUpdated", currentDevice.value);
}

function closeDialog() {
    confirmVisible.value = false;
    emit("update:visible", false);
}

watch([() => props.visible, () => props.device?.id], ([visible, deviceId]) => {
    invalidateRequests();
    currentDevice.value = props.device;
    confirmVisible.value = false;
    rebootPending.value = false;
    rebootResult.value = null;
    rebootError.value = "";
    pollTimedOut.value = false;
    firmwareUpgradeBusy.value = false;
    operations.value = [];
    operationTotal.value = 0;
    operationPage.value = 1;
    if (visible && typeof deviceId === "number") void load(requestVersion, deviceId);
}, { immediate: true });

watch(() => props.device, (device) => {
    if (!device || device.id === currentDevice.value?.id) currentDevice.value = device;
}, { deep: true });

onBeforeUnmount(() => invalidateRequests());
</script>

<template>
    <a-modal
        :visible="visible"
        modal-class="uvp-system-dialog device-maintenance-dialog"
        :width="720"
        :footer="false"
        :mask-closable="!rebootPending"
        unmount-on-close
        @update:visible="emit('update:visible', $event)"
        @cancel="closeDialog"
    >
        <template #title>
            <span class="maintenance-title"><RotateCcw :size="17" />设备维护</span>
            <span class="maintenance-title-device">{{ deviceName }}</span>
        </template>

        <a-spin :loading="loading" class="maintenance-spin">
            <div v-if="currentDevice" class="maintenance-body">
                <section class="maintenance-overview" data-testid="maintenance-overview">
                    <div class="maintenance-device-heading">
                        <div class="maintenance-device-icon"><RotateCcw :size="20" /></div>
                        <div class="maintenance-device-identity">
                            <strong>{{ deviceName }}</strong>
                            <span class="mono">{{ currentDevice.deviceId }}</span>
                        </div>
                        <span class="maintenance-status" :class="{ online: deviceOnline }">
                            <span class="maintenance-status-dot"></span>{{ deviceOnline ? '在线' : '离线' }}
                        </span>
                    </div>
                    <div class="maintenance-facts">
                        <div><span>厂商 / 型号</span><strong>{{ deviceVendor }}</strong></div>
                        <div><span>固件版本</span><strong class="mono">{{ currentDevice.firmware || '未上报' }}</strong></div>
                        <div><span>通道数</span><strong>{{ currentDevice.channelOnlineCount }} / {{ currentDevice.channelCount }}</strong></div>
                    </div>
                </section>

                <section class="maintenance-actions" data-testid="maintenance-actions">
                    <div class="maintenance-section-heading">
                        <div><span class="maintenance-eyebrow">设备级操作</span><strong>整机维护</strong></div>
                        <span class="maintenance-scope">影响整台设备及所属通道</span>
                    </div>
                    <div class="maintenance-action-row">
                        <div class="maintenance-action-icon"><RefreshCcw :size="17" /></div>
                        <div class="maintenance-action-copy">
                            <strong>重启设备</strong>
                            <span>向设备发送整机重启请求，设备可能短暂离线。</span>
                        </div>
                        <a-button
                            data-testid="maintenance-reboot"
                            type="primary"
                            :disabled="!canReboot || !deviceOnline || rebootDisabled"
                            :loading="rebootPending"
                            :title="!canReboot ? '暂无设备重启权限' : (firmwareUpgradeBusy ? '设备升级处理中，暂不能重启' : (!deviceOnline ? '设备离线，无法发送重启请求' : ''))"
                            @click="requestReboot"
                        >
                            <template #icon><Loader2 v-if="rebootPending" :size="14" class="spin" /><RefreshCcw v-else :size="14" /></template>
                            {{ rebootOwnBusy ? '请求处理中' : '重启设备' }}
                        </a-button>
                    </div>
                    <p v-if="!canReboot" class="maintenance-action-hint warning"><CircleX :size="14" />当前账号没有设备重启权限。</p>
                    <p v-else-if="firmwareUpgradeBusy" class="maintenance-action-hint warning"><CircleX :size="14" />升级任务尚未确认完成，暂不能重启设备。</p>
                    <p v-else-if="!deviceOnline" class="maintenance-action-hint warning"><CircleX :size="14" />设备离线，可查看记录，但不能发送重启请求。</p>
                    <p v-else class="maintenance-action-hint"><AlertTriangle :size="14" />“已发送”仅表示平台已发出请求，不代表设备已经完成重启。</p>
                    <div v-if="rebootError" class="maintenance-error" role="alert"><CircleX :size="14" />{{ rebootError }}</div>
                    <div v-if="rebootResult" class="maintenance-result" :class="`tone-${statusTone(rebootResult.status)}`" role="status">
                        <CircleCheck v-if="statusTone(rebootResult.status) === 'success'" :size="15" />
                        <Clock3 v-else-if="statusTone(rebootResult.status) === 'pending'" :size="15" />
                        <CircleX v-else :size="15" />
                        <span>最近请求：{{ pollTimedOut ? '结果未知' : statusText(rebootResult.status) }}<template v-if="rebootResult.operationId"> · {{ rebootResult.operationId }}</template></span>
                    </div>
                    <div v-if="confirmVisible" class="maintenance-confirm" data-testid="maintenance-confirm" role="alertdialog" aria-label="确认重启设备">
                        <div class="maintenance-confirm-icon"><AlertTriangle :size="19" /></div>
                        <div class="maintenance-confirm-copy">
                            <strong>确认重启整台设备？</strong>
                            <span>{{ deviceName }}（{{ currentDevice.deviceId }}）及其所有通道可能短暂离线，当前播放会中断。</span>
                        </div>
                        <div class="maintenance-confirm-actions">
                            <a-button data-testid="maintenance-reboot-cancel" :disabled="rebootPending" @click="cancelReboot">取消</a-button>
                            <a-button data-testid="maintenance-reboot-confirm" type="primary" :loading="rebootPending" @click="confirmReboot">确认重启</a-button>
                        </div>
                    </div>
                </section>

                <DeviceFirmwareUpgradePanel
                    :visible="visible"
                    :device="currentDevice"
                    :can-upgrade="canUpgrade"
                    :reboot-busy="rebootOwnBusy"
                    @busy="onFirmwareUpgradeBusy"
                    @firmwareUpdated="onFirmwareUpdated"
                />

                <section class="maintenance-history" data-testid="maintenance-history">
                    <div class="maintenance-section-heading">
                        <div><span class="maintenance-eyebrow">审计记录</span><strong>最近维护操作</strong></div>
                        <span class="maintenance-scope">共 {{ operationTotal }} 条</span>
                    </div>
                    <div v-if="operationsLoading && !operations.length" class="maintenance-history-state"><Loader2 :size="17" class="spin" />正在加载维护记录</div>
                    <div v-else-if="!operations.length" class="maintenance-history-state">暂无维护记录</div>
                    <div v-else class="maintenance-operation-list">
                        <article v-for="operation in operations" :key="operation.operationId" class="maintenance-operation" data-testid="maintenance-operation">
                            <div class="maintenance-operation-main">
                                <strong>{{ operation.action === 'teleboot' ? '重启设备' : operation.action }}</strong>
                                <span class="mono" :title="operation.operationId">{{ operation.operationId }}</span>
                            </div>
                            <dl class="maintenance-operation-facts">
                                <div><dt>状态</dt><dd><span class="operation-status" :class="`tone-${operationTone(operation)}`">{{ operationStatus(operation) }}</span></dd></div>
                                <div><dt>操作人</dt><dd>userId {{ actorText(operation.actorId) }}</dd></div>
                                <div><dt>创建时间</dt><dd class="mono" :title="formatDateTime(operation.createdAt)">{{ formatDateTime(operation.createdAt) }}</dd></div>
                                <div><dt>原因</dt><dd :title="operationReason(operation)">{{ operationReason(operation) }}</dd></div>
                            </dl>
                        </article>
                    </div>
                    <footer v-if="operationTotal > PAGE_SIZE" class="maintenance-pagination">
                        <a-pagination :current="operationPage" :page-size="PAGE_SIZE" :total="operationTotal" :loading="operationsLoading" show-jumper @change="changePage" />
                    </footer>
                </section>

            </div>
        </a-spin>
    </a-modal>
</template>

<style scoped>
.maintenance-body { display: grid; gap: 12px; color: var(--uvp-text-primary); }
.maintenance-title { display: inline-flex; align-items: center; gap: 7px; color: var(--uvp-text-primary); font-weight: 650; }
.maintenance-title-device { margin-left: 10px; color: var(--uvp-text-tertiary); font-size: 12px; font-weight: 400; }
.maintenance-overview, .maintenance-actions, .maintenance-history { padding: 14px; border: 1px solid var(--uvp-panel-border); border-radius: 12px; background: var(--uvp-panel-bg); }
.maintenance-device-heading, .maintenance-section-heading, .maintenance-action-row { display: flex; align-items: center; gap: 10px; }
.maintenance-device-heading { min-width: 0; }
.maintenance-device-icon, .maintenance-action-icon, .maintenance-confirm-icon { display: grid; place-items: center; flex: 0 0 auto; width: 34px; height: 34px; color: var(--uvp-brand); border-radius: 9px; background: var(--uvp-brand-soft); }
.maintenance-device-identity { display: grid; min-width: 0; gap: 2px; }
.maintenance-device-identity strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.maintenance-device-identity span { color: var(--uvp-text-tertiary); font-size: 12px; }
.maintenance-status { display: inline-flex; align-items: center; gap: 5px; padding: 4px 9px; margin-left: auto; color: var(--uvp-danger); background: var(--uvp-danger-soft); border: 1px solid var(--uvp-danger-border); border-radius: 999px; font-size: 12px; white-space: nowrap; }
.maintenance-status.online { color: var(--uvp-brand-cyan); background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent); border-color: color-mix(in srgb, var(--uvp-brand-cyan) 26%, transparent); }
.maintenance-status-dot { width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
.maintenance-facts { display: grid; grid-template-columns: 1.4fr 1fr 0.7fr; gap: 10px; padding-top: 13px; margin-top: 13px; border-top: 1px solid var(--uvp-panel-border); }
.maintenance-facts div { display: grid; min-width: 0; gap: 4px; }
.maintenance-facts span, .maintenance-scope, .maintenance-eyebrow, .maintenance-operation-facts dt { color: var(--uvp-text-tertiary); font-size: 11px; }
.maintenance-facts strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; }
.maintenance-section-heading { justify-content: space-between; margin-bottom: 12px; }
.maintenance-section-heading > div { display: grid; gap: 2px; }
.maintenance-section-heading strong { font-size: 14px; }
.maintenance-eyebrow { color: var(--uvp-brand); font-weight: 650; letter-spacing: .04em; }
.maintenance-action-row { padding: 11px 0; border-top: 1px solid var(--uvp-panel-border); }
.maintenance-action-copy { display: grid; flex: 1; min-width: 0; gap: 3px; }
.maintenance-action-copy span, .maintenance-action-hint { color: var(--uvp-text-tertiary); font-size: 12px; }
.maintenance-action-hint { display: flex; align-items: center; gap: 6px; margin: 9px 0 0 44px; line-height: 1.45; }
.maintenance-action-hint.warning { color: var(--uvp-warning); }
.maintenance-error, .maintenance-result { display: flex; align-items: center; gap: 6px; padding: 8px 10px; margin-top: 9px; border-radius: 8px; font-size: 12px; }
.maintenance-error { color: var(--uvp-danger); background: var(--uvp-danger-soft); border: 1px solid var(--uvp-danger-border); }
.maintenance-result { color: var(--uvp-warning); background: var(--uvp-warning-soft); border: 1px solid var(--uvp-warning-border); }
.maintenance-result.tone-success { color: var(--uvp-brand-cyan); background: color-mix(in srgb, var(--uvp-brand-cyan) 9%, transparent); border-color: color-mix(in srgb, var(--uvp-brand-cyan) 24%, transparent); }
.maintenance-result.tone-danger { color: var(--uvp-danger); background: var(--uvp-danger-soft); border-color: var(--uvp-danger-border); }
.maintenance-history-state { display: flex; min-height: 72px; align-items: center; justify-content: center; gap: 6px; color: var(--uvp-text-tertiary); font-size: 12px; }
.maintenance-operation-list { display: grid; gap: 8px; }
.maintenance-operation { display: grid; grid-template-columns: minmax(120px, .8fr) minmax(0, 2.2fr); gap: 12px; padding: 10px; border: 1px solid var(--uvp-panel-border); border-radius: 9px; background: var(--uvp-list-toolbar-bg); }
.maintenance-operation-main { display: grid; align-content: start; gap: 4px; min-width: 0; }
.maintenance-operation-main strong { font-size: 13px; }
.maintenance-operation-main span { overflow: hidden; color: var(--uvp-text-tertiary); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.maintenance-operation-facts { display: grid; grid-template-columns: .7fr .8fr 1.4fr 1.4fr; gap: 8px; margin: 0; }
.maintenance-operation-facts div { display: grid; align-content: start; gap: 3px; min-width: 0; }
.maintenance-operation-facts dd { overflow: hidden; margin: 0; color: var(--uvp-text-secondary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.operation-status { display: inline-flex; width: fit-content; padding: 2px 7px; border-radius: 999px; color: var(--uvp-warning); background: var(--uvp-warning-soft); font-size: 11px; }
.operation-status.tone-success { color: var(--uvp-brand-cyan); background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent); }
.operation-status.tone-danger { color: var(--uvp-danger); background: var(--uvp-danger-soft); }
.maintenance-pagination { display: flex; justify-content: flex-end; padding-top: 10px; }
.maintenance-confirm { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 10px; padding: 11px; margin-top: 2px; color: var(--uvp-warning); background: var(--uvp-warning-soft); border: 1px solid var(--uvp-warning-border); border-radius: 10px; }
.maintenance-confirm-icon { color: var(--uvp-warning); background: transparent; }
.maintenance-confirm-copy { display: grid; gap: 3px; min-width: 0; }
.maintenance-confirm-copy strong { color: var(--uvp-text-primary); font-size: 13px; }
.maintenance-confirm-copy span { color: var(--uvp-text-secondary); font-size: 12px; line-height: 1.45; }
.maintenance-confirm-actions { display: flex; gap: 6px; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.spin { animation: spin 0.8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 640px) {
    .maintenance-facts { grid-template-columns: 1fr 1fr; }
    .maintenance-facts div:last-child { grid-column: 1 / -1; }
    .maintenance-operation { grid-template-columns: 1fr; gap: 8px; }
    .maintenance-operation-facts { grid-template-columns: 1fr 1fr; }
    .maintenance-confirm { grid-template-columns: auto minmax(0, 1fr); }
    .maintenance-confirm-actions { grid-column: 1 / -1; justify-content: flex-end; }
    .maintenance-action-row { align-items: flex-start; flex-wrap: wrap; }
    .maintenance-action-row .arco-btn { margin-left: 44px; }
}
</style>
