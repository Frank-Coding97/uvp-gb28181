<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { AlertTriangle, CircleCheck, CircleX, Clock3, Loader2, Upload } from "@lucide/vue";
import { Message } from "@arco-design/web-vue";
import {
    listFirmwareUpgrades,
    upgradeDeviceFirmware,
    type DeviceVO,
    type FirmwareUpgradeRequest,
    type UpgradeOperation
} from "./api";

const props = withDefaults(defineProps<{
    visible: boolean;
    device: DeviceVO | null;
    canUpgrade: boolean;
    rebootBusy?: boolean;
}>(), {
    device: null,
    canUpgrade: false,
    rebootBusy: false
});

const emit = defineEmits<{
    busy: [value: boolean];
    firmwareUpdated: [firmware: string];
}>();

const PAGE_SIZE = 10;
const POLL_INTERVAL_MS = 2000;
const MAX_POLL_MS = 10 * 60_000;

const currentDevice = ref<DeviceVO | null>(props.device);
const loading = ref(false);
const historyLoading = ref(false);
const operations = ref<UpgradeOperation[]>([]);
const operationPage = ref(1);
const operationTotal = ref(0);
const confirmVisible = ref(false);
const submitPending = ref(false);
const submitError = ref("");
const activeOperation = ref<UpgradeOperation | null>(null);
const pollTimedOut = ref(false);
const uncertainSubmission = ref(false);
const pendingPayload = ref<Pick<FirmwareUpgradeRequest, "firmware" | "fileUrl" | "manufacturer"> | null>(null);
const form = reactive({ firmware: "", fileUrl: "", manufacturer: "" });

let requestVersion = 0;
let pollTimer: ReturnType<typeof setTimeout> | null = null;

const deviceName = computed(() => currentDevice.value?.alias?.trim() || currentDevice.value?.name?.trim() || "未命名设备");
const deviceOnline = computed(() => currentDevice.value?.online === true);
const protocol2022 = computed(() => currentDevice.value?.effectiveVersion === "2022");
const active = computed(() => activeOperation.value !== null && isActiveStatus(activeOperation.value.status) && !pollTimedOut.value);
const busy = computed(() => submitPending.value || active.value || uncertainSubmission.value);
const formReady = computed(() => Boolean(form.firmware.trim() && form.fileUrl.trim() && form.manufacturer.trim()));
const unavailableReason = computed(() => {
    if (!props.canUpgrade) return "当前账号没有设备升级权限。";
    if (!deviceOnline.value) return "设备离线，可查看记录，但不能发送升级请求。";
    if (!protocol2022.value) return "当前设备需使用 GB/T 28181-2022 配置后才能升级。";
    if (props.rebootBusy) return "设备重启请求处理中，暂不能发起升级。";
    if (uncertainSubmission.value) return "上一次升级请求结果未知，请先从记录确认，暂不要重复提交。";
    return "";
});
const submitDisabled = computed(() => Boolean(unavailableReason.value) || busy.value);

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

function isActiveStatus(status?: string | null) {
    return ["queued", "sent", "accepted"].includes(String(status || "").toLowerCase());
}

function statusKey(value?: string | null) {
    return String(value || "unknown").toLowerCase();
}

function statusText(value?: string | null) {
    return ({
        queued: "排队中",
        sent: "已发送",
        accepted: "已受理",
        succeeded: "升级成功",
        failed: "升级失败",
        rejected: "已拒绝",
        unknown: "结果未知"
    } as Record<string, string>)[statusKey(value)] || "结果未知";
}

function statusTone(value?: string | null) {
    const key = statusKey(value);
    if (key === "succeeded") return "success";
    if (key === "failed" || key === "rejected") return "danger";
    if (key === "unknown") return "warning";
    return "pending";
}

function formatDateTime(value?: string | null) {
    if (!value || value.startsWith("0001-01-01")) return "-";
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? "-" : date.toLocaleString("zh-CN", { hour12: false });
}

function failedReasonText(value?: string | null) {
    return ({
        "01": "软件下载超时",
        "02": "升级包损坏",
        "03": "系统异常",
        "99": "其他原因"
    } as Record<string, string>)[String(value || "")] || String(value || "");
}

function operationReason(operation: UpgradeOperation) {
    if (operation.failedReason?.trim()) {
        const reason = failedReasonText(operation.failedReason);
        return operation.errorMessage?.trim() ? `${reason}：${operation.errorMessage}` : reason;
    }
    if (operation.errorMessage?.trim()) return operation.errorMessage;
    if (statusKey(operation.status) === "sent") return "请求已发送，等待设备回报升级结果";
    if (statusKey(operation.status) === "accepted") return "设备已受理，等待升级完成";
    if (statusKey(operation.status) === "unknown") return "设备执行结果未知，请根据设备状态确认";
    return "-";
}

function actorText(actorId?: number | null) {
    return actorId && actorId > 0 ? String(actorId) : "未记录";
}

function displayedStatus(operation: UpgradeOperation) {
    return pollTimedOut.value && activeOperation.value?.operationId === operation.operationId ? "结果未知" : statusText(operation.status);
}

function displayedTone(operation: UpgradeOperation) {
    return pollTimedOut.value && activeOperation.value?.operationId === operation.operationId ? "warning" : statusTone(operation.status);
}

function parseDeadline(deadlineAt?: string | null) {
    const timestamp = deadlineAt ? new Date(deadlineAt).getTime() : Number.NaN;
    return Number.isFinite(timestamp) ? timestamp : Date.now() + MAX_POLL_MS;
}

function markUnknown(operation: UpgradeOperation) {
    activeOperation.value = operation;
    pollTimedOut.value = true;
    uncertainSubmission.value = true;
}

function emitCompletedFirmware(operation: UpgradeOperation) {
    const firmware = operation.currentFirmware?.trim();
    if (statusKey(operation.status) === "succeeded" && firmware) emit("firmwareUpdated", firmware);
}

function recoverActiveOperation() {
    const unresolved = operations.value.find(item => statusKey(item.status) === "unknown");
    if (unresolved) {
        markUnknown(unresolved);
        return;
    }
    const operation = operations.value.find(item => isActiveStatus(item.status));
    if (!operation) return;
    activeOperation.value = operation;
    if (Date.now() >= parseDeadline(operation.deadlineAt)) {
        markUnknown(operation);
        return;
    }
    schedulePoll(operation, requestVersion, operation.deviceId);
}

function schedulePoll(operation: UpgradeOperation, version: number, deviceId: number, deadlineOverride?: number) {
    clearPollTimer();
    if (!isActiveStatus(operation.status) || !isActive(version, deviceId)) return;
    const deadline = deadlineOverride ?? parseDeadline(operation.deadlineAt);
    if (Date.now() >= deadline) {
        markUnknown(operation);
        return;
    }
    pollTimer = setTimeout(() => {
        pollTimer = null;
        void pollOperation(version, deviceId, operation.operationId, deadline);
    }, POLL_INTERVAL_MS);
}

async function pollOperation(version: number, deviceId: number, operationId: string, deadline: number) {
    if (!isActive(version, deviceId)) return;
    if (Date.now() >= deadline) {
        if (activeOperation.value?.operationId === operationId) markUnknown(activeOperation.value);
        return;
    }
    try {
        const result = await listFirmwareUpgrades(deviceId, { page: operationPage.value, pageSize: PAGE_SIZE });
        if (!isActive(version, deviceId)) return;
        if (result.code === 0) {
            operations.value = result.data?.list || [];
            operationTotal.value = result.data?.total || 0;
            const operation = operations.value.find(item => item.operationId === operationId);
            if (operation) {
                activeOperation.value = operation;
                if (!isActiveStatus(operation.status)) {
                    uncertainSubmission.value = statusKey(operation.status) === "unknown";
                    submitPending.value = false;
                    clearPollTimer();
                    emitCompletedFirmware(operation);
                    return;
                }
                schedulePoll(operation, version, deviceId, deadline);
                return;
            }
        }
    } catch {
        // 查询失败不等于升级失败，继续查询直到截止时间，不重复发送升级请求。
    }
    schedulePoll({ operationId, deviceId, status: "sent" } as UpgradeOperation, version, deviceId, deadline);
}

async function loadHistory(version: number, deviceId: number, page = operationPage.value) {
    if (!isActive(version, deviceId)) return;
    historyLoading.value = true;
    try {
        const result = await listFirmwareUpgrades(deviceId, { page, pageSize: PAGE_SIZE });
        if (!isActive(version, deviceId)) return;
        if (result.code !== 0) throw new Error(result.message || "升级记录加载失败");
        operations.value = result.data?.list || [];
        operationTotal.value = result.data?.total || 0;
        operationPage.value = result.data?.page || page;
        if (!activeOperation.value) recoverActiveOperation();
    } catch (error: any) {
        if (isActive(version, deviceId)) Message.error(error?.message || "升级记录加载失败");
    } finally {
        if (isActive(version, deviceId)) historyLoading.value = false;
    }
}

function resetForm(device: DeviceVO | null) {
    form.firmware = "";
    form.fileUrl = "";
    form.manufacturer = device?.manufacturer?.trim() || "";
}

async function load(version: number, deviceId: number) {
    resetForm(currentDevice.value);
    operations.value = [];
    operationTotal.value = 0;
    operationPage.value = 1;
    activeOperation.value = null;
    pollTimedOut.value = false;
    uncertainSubmission.value = false;
    submitError.value = "";
    await loadHistory(version, deviceId, 1);
}

function createIdempotencyKey() {
    if (typeof globalThis.crypto?.randomUUID === "function") return globalThis.crypto.randomUUID();
    return `device-upgrade-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

function isHttpUrl(value: string) {
    try {
        const url = new URL(value);
        return url.protocol === "http:" || url.protocol === "https:";
    } catch {
        return false;
    }
}

function requestUpgrade() {
    if (submitDisabled.value) return;
    submitError.value = "";
    if (!form.firmware.trim()) {
        Message.warning("请输入目标固件版本");
        return;
    }
    if (!form.manufacturer.trim()) {
        Message.warning("请输入设备厂商");
        return;
    }
    if (!form.fileUrl.trim() || !isHttpUrl(form.fileUrl.trim())) {
        Message.warning("请输入可由设备访问的 HTTP 或 HTTPS 升级文件地址");
        return;
    }
    pendingPayload.value = {
        firmware: form.firmware.trim(),
        fileUrl: form.fileUrl.trim(),
        manufacturer: form.manufacturer.trim()
    };
    confirmVisible.value = true;
}

function cancelUpgrade() {
    if (submitPending.value) return;
    confirmVisible.value = false;
    pendingPayload.value = null;
}

function notifyResult(operation: UpgradeOperation) {
    if (operation.deduplicated) {
        Message.info("已有相同升级请求，已返回原操作");
        return;
    }
    if (statusKey(operation.status) === "succeeded") {
        Message.success("设备升级已完成");
    } else if (statusKey(operation.status) === "accepted") {
        Message.info("设备已受理升级请求，等待完成结果");
    } else if (statusKey(operation.status) === "sent") {
        Message.success("升级请求已发送，等待设备回报结果");
    } else if (statusKey(operation.status) === "unknown") {
        Message.warning("升级结果未知，请根据记录和设备状态确认");
    } else if (statusKey(operation.status) === "failed" || statusKey(operation.status) === "rejected") {
        Message.error(operation.errorMessage || operationReason(operation));
    } else {
        Message.info(`升级请求状态：${statusText(operation.status)}`);
    }
}

async function confirmUpgrade() {
    const device = currentDevice.value;
    const payload = pendingPayload.value;
    const version = requestVersion;
    if (!device || !payload || !isActive(version, device.id) || submitDisabled.value) return;
    confirmVisible.value = false;
    pendingPayload.value = null;
    submitPending.value = true;
    submitError.value = "";
    pollTimedOut.value = false;
    try {
        const result = await upgradeDeviceFirmware(device.id, {
            confirmed: true,
            idempotencyKey: createIdempotencyKey(),
            ...payload
        });
        if (!isActive(version, device.id)) return;
        if (result.code !== 0 || !result.data) throw new Error(result.message || "升级请求发送失败");
        activeOperation.value = result.data;
        uncertainSubmission.value = false;
        notifyResult(result.data);
        await loadHistory(version, device.id, 1);
        if (!isActiveStatus(result.data.status)) {
            submitPending.value = false;
            emitCompletedFirmware(result.data);
            return;
        }
        schedulePoll(result.data, version, device.id);
    } catch (error: any) {
        if (isActive(version, device.id)) {
            submitError.value = error?.message || "升级请求结果未知，请先从记录确认，暂不要重复提交。";
            uncertainSubmission.value = true;
            submitPending.value = false;
            Message.warning(submitError.value);
        }
    } finally {
        if (isActive(version, device.id) && !active.value) submitPending.value = false;
    }
}

function changePage(page: number) {
    const deviceId = currentDevice.value?.id;
    if (!deviceId || !props.visible || historyLoading.value) return;
    operationPage.value = page;
    void loadHistory(requestVersion, deviceId, page);
}

watch(() => busy.value, value => emit("busy", value), { immediate: true });

watch([() => props.visible, () => props.device?.id], ([visible, deviceId]) => {
    invalidateRequests();
    currentDevice.value = props.device;
    confirmVisible.value = false;
    pendingPayload.value = null;
    submitPending.value = false;
    activeOperation.value = null;
    pollTimedOut.value = false;
    uncertainSubmission.value = false;
    submitError.value = "";
    if (visible && typeof deviceId === "number") void load(requestVersion, deviceId);
}, { immediate: true });

watch(() => props.device, device => {
    if (!device || device.id === currentDevice.value?.id) currentDevice.value = device;
}, { deep: true });

onBeforeUnmount(() => {
    invalidateRequests();
    emit("busy", false);
});
</script>

<template>
    <section class="firmware-upgrade-panel" data-testid="firmware-upgrade-panel" :aria-busy="loading ? 'true' : 'false'">
        <div class="upgrade-section-heading">
            <div><span class="upgrade-eyebrow">设备级操作</span><strong>设备固件升级</strong></div>
            <span class="upgrade-scope" :title="deviceName">设备根据地址获取升级文件</span>
        </div>

        <div class="upgrade-form" data-testid="firmware-upgrade-form">
            <label>
                <span>目标固件版本</span>
                <a-input v-model="form.firmware" placeholder="例如 V5.9.0" :disabled="submitDisabled || confirmVisible" allow-clear />
            </label>
            <label>
                <span>设备厂商</span>
                <a-input v-model="form.manufacturer" placeholder="请输入升级包对应厂商" :disabled="submitDisabled || confirmVisible" allow-clear />
            </label>
            <label class="upgrade-url-field">
                <span>升级文件地址</span>
                <a-input v-model="form.fileUrl" placeholder="设备可访问的 HTTP 或 HTTPS 地址" :disabled="submitDisabled || confirmVisible" allow-clear />
            </label>
            <div class="upgrade-submit-wrap">
                <a-button
                    data-testid="firmware-upgrade-submit"
                    type="primary"
                    :disabled="submitDisabled || !formReady"
                    :loading="submitPending"
                    :title="unavailableReason || (!formReady ? '请填写完整升级信息' : '')"
                    @click="requestUpgrade"
                >
                    <template #icon><Loader2 v-if="submitPending" :size="14" class="spin" /><Upload v-else :size="14" /></template>
                    {{ uncertainSubmission ? '等待结果确认' : busy ? '升级处理中' : '发起升级' }}
                </a-button>
            </div>
        </div>

        <p v-if="unavailableReason" class="upgrade-hint warning"><CircleX :size="14" />{{ unavailableReason }}</p>
        <p v-else class="upgrade-hint"><AlertTriangle :size="14" />请确认设备能访问此地址。升级结果及当前版本以设备最终回报为准。</p>
        <div v-if="submitError" class="upgrade-error" role="alert"><CircleX :size="14" />{{ submitError }}</div>

        <div v-if="confirmVisible && pendingPayload" class="upgrade-confirm" data-testid="firmware-upgrade-confirmation" role="alertdialog" aria-label="确认设备固件升级">
            <div class="upgrade-confirm-icon"><AlertTriangle :size="19" /></div>
            <div class="upgrade-confirm-copy">
                <strong>确认向设备发起升级？</strong>
                <span>目标版本 {{ pendingPayload.firmware }}，厂商 {{ pendingPayload.manufacturer }}。设备会从指定地址获取文件，升级期间可能短暂离线。</span>
                <span class="upgrade-confirm-url" :title="pendingPayload.fileUrl">文件地址：{{ pendingPayload.fileUrl }}</span>
            </div>
            <div class="upgrade-confirm-actions">
                <a-button data-testid="firmware-upgrade-cancel" @click="cancelUpgrade">取消</a-button>
                <a-button data-testid="firmware-upgrade-confirm" type="primary" :loading="submitPending" @click="confirmUpgrade">确认升级</a-button>
            </div>
        </div>

        <div class="upgrade-history" data-testid="firmware-upgrade-history">
            <div class="upgrade-history-heading"><strong>升级记录</strong><span>共 {{ operationTotal }} 条</span></div>
            <div v-if="historyLoading && !operations.length" class="upgrade-history-state"><Loader2 :size="17" class="spin" />正在加载升级记录</div>
            <div v-else-if="!operations.length" class="upgrade-history-state">暂无升级记录</div>
            <div v-else class="upgrade-operation-list">
                <article v-for="operation in operations" :key="operation.operationId" class="upgrade-operation" data-testid="firmware-upgrade-operation">
                    <div class="upgrade-operation-head">
                        <div><strong>设备固件升级</strong><span class="mono" :title="operation.operationId">{{ operation.operationId }}</span></div>
                        <span class="upgrade-status" :class="`tone-${displayedTone(operation)}`">
                            <CircleCheck v-if="displayedTone(operation) === 'success'" :size="13" />
                            <Clock3 v-else-if="displayedTone(operation) === 'pending'" :size="13" />
                            <CircleX v-else :size="13" />
                            {{ displayedStatus(operation) }}
                        </span>
                    </div>
                    <dl class="upgrade-operation-facts">
                        <div><dt>目标版本</dt><dd class="mono">{{ operation.firmware || '-' }}</dd></div>
                        <div><dt>设备回报版本</dt><dd class="mono">{{ operation.currentFirmware || '-' }}</dd></div>
                        <div><dt>厂商</dt><dd>{{ operation.manufacturer || '-' }}</dd></div>
                        <div><dt>操作人</dt><dd>userId {{ actorText(operation.actorId) }}</dd></div>
                        <div><dt>创建时间</dt><dd class="mono" :title="formatDateTime(operation.createdAt)">{{ formatDateTime(operation.createdAt) }}</dd></div>
                        <div><dt>结果说明</dt><dd :title="operationReason(operation)">{{ operationReason(operation) }}</dd></div>
                    </dl>
                    <details v-if="operation.sessionId" class="upgrade-session">
                        <summary>查看操作记录</summary>
                        <code>{{ operation.sessionId }}</code>
                    </details>
                </article>
            </div>
            <footer v-if="operationTotal > PAGE_SIZE" class="upgrade-pagination">
                <a-pagination :current="operationPage" :page-size="PAGE_SIZE" :total="operationTotal" :loading="historyLoading" show-jumper @change="changePage" />
            </footer>
        </div>
    </section>
</template>

<style scoped>
.firmware-upgrade-panel { padding: 14px; border: 1px solid var(--uvp-panel-border); border-radius: 12px; background: var(--uvp-panel-bg); }
.upgrade-section-heading, .upgrade-history-heading { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-bottom: 12px; }
.upgrade-section-heading > div { display: grid; gap: 2px; }
.upgrade-section-heading strong { font-size: 14px; }
.upgrade-eyebrow { color: var(--uvp-brand); font-size: 11px; font-weight: 650; letter-spacing: .04em; }
.upgrade-scope, .upgrade-history-heading > span, .upgrade-hint { color: var(--uvp-text-tertiary); font-size: 11px; }
.upgrade-form { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; padding-top: 11px; border-top: 1px solid var(--uvp-panel-border); }
.upgrade-form label { display: grid; gap: 5px; min-width: 0; }
.upgrade-form label > span { color: var(--uvp-text-secondary); font-size: 12px; }
.upgrade-url-field { grid-column: 1 / -1; }
.upgrade-submit-wrap { display: flex; align-items: end; justify-content: flex-end; }
.upgrade-form :deep(.arco-input-wrapper) { color: var(--uvp-text-primary); background: var(--uvp-dialog-control-bg) !important; border-color: var(--uvp-dialog-border) !important; }
.upgrade-form :deep(.arco-input) { color: var(--uvp-text-primary) !important; }
.upgrade-form :deep(.arco-input::placeholder) { color: var(--uvp-text-tertiary) !important; }
.upgrade-hint, .upgrade-error { display: flex; align-items: flex-start; gap: 6px; margin: 10px 0 0; line-height: 1.45; }
.upgrade-hint.warning { color: var(--uvp-warning); }
.upgrade-error { padding: 8px 10px; color: var(--uvp-danger); background: var(--uvp-danger-soft); border: 1px solid var(--uvp-danger-border); border-radius: 8px; font-size: 12px; }
.upgrade-confirm { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 10px; padding: 11px; margin-top: 10px; color: var(--uvp-warning); background: var(--uvp-warning-soft); border: 1px solid var(--uvp-warning-border); border-radius: 10px; }
.upgrade-confirm-icon { display: grid; place-items: center; }
.upgrade-confirm-copy { display: grid; gap: 3px; min-width: 0; }
.upgrade-confirm-copy strong { color: var(--uvp-text-primary); font-size: 13px; }
.upgrade-confirm-copy span { color: var(--uvp-text-secondary); font-size: 12px; line-height: 1.45; }
.upgrade-confirm-url { overflow: hidden; color: var(--uvp-text-tertiary) !important; text-overflow: ellipsis; white-space: nowrap; }
.upgrade-confirm-actions { display: flex; gap: 6px; }
.upgrade-history { padding-top: 14px; margin-top: 14px; border-top: 1px solid var(--uvp-panel-border); }
.upgrade-history-heading { margin-bottom: 9px; }
.upgrade-history-heading strong { font-size: 13px; }
.upgrade-history-state { display: flex; min-height: 64px; align-items: center; justify-content: center; gap: 6px; color: var(--uvp-text-tertiary); font-size: 12px; }
.upgrade-operation-list { display: grid; gap: 8px; }
.upgrade-operation { display: grid; gap: 9px; padding: 10px; border: 1px solid var(--uvp-panel-border); border-radius: 9px; background: var(--uvp-list-toolbar-bg); }
.upgrade-operation-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; min-width: 0; }
.upgrade-operation-head > div { display: grid; gap: 3px; min-width: 0; }
.upgrade-operation-head strong { font-size: 13px; }
.upgrade-operation-head .mono { overflow: hidden; color: var(--uvp-text-tertiary); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.upgrade-status { display: inline-flex; align-items: center; gap: 4px; flex: 0 0 auto; padding: 2px 7px; color: var(--uvp-warning); background: var(--uvp-warning-soft); border-radius: 999px; font-size: 11px; }
.upgrade-status.tone-success { color: var(--uvp-brand-cyan); background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent); }
.upgrade-status.tone-danger { color: var(--uvp-danger); background: var(--uvp-danger-soft); }
.upgrade-operation-facts { display: grid; grid-template-columns: .8fr .9fr .8fr .9fr 1.2fr 1.5fr; gap: 8px; margin: 0; }
.upgrade-operation-facts div { display: grid; align-content: start; gap: 3px; min-width: 0; }
.upgrade-operation-facts dt { color: var(--uvp-text-tertiary); font-size: 11px; }
.upgrade-operation-facts dd { overflow: hidden; margin: 0; color: var(--uvp-text-secondary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.upgrade-session { color: var(--uvp-text-tertiary); font-size: 11px; }
.upgrade-session summary { cursor: pointer; width: fit-content; }
.upgrade-session code { display: block; overflow: hidden; padding-top: 5px; text-overflow: ellipsis; white-space: nowrap; }
.upgrade-pagination { display: flex; justify-content: flex-end; padding-top: 10px; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.spin { animation: spin .8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 640px) {
    .upgrade-form { grid-template-columns: 1fr; }
    .upgrade-url-field { grid-column: auto; }
    .upgrade-submit-wrap { justify-content: stretch; }
    .upgrade-submit-wrap .arco-btn { width: 100%; }
    .upgrade-confirm { grid-template-columns: auto minmax(0, 1fr); }
    .upgrade-confirm-actions { grid-column: 1 / -1; justify-content: flex-end; }
    .upgrade-operation-head { align-items: flex-start; flex-direction: column; }
    .upgrade-operation-facts { grid-template-columns: 1fr 1fr; }
}
</style>
