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

type UpgradePayload = Pick<FirmwareUpgradeRequest, "firmware" | "fileUrl" | "manufacturer">;
type UpgradePhase = "prepare" | "confirm" | "tracking";
type FieldName = keyof UpgradePayload;

const props = withDefaults(defineProps<{
    visible: boolean;
    device: DeviceVO | null;
    canUpgrade: boolean;
    rebootBusy?: boolean;
    blockedReason?: string;
}>(), {
    device: null,
    canUpgrade: false,
    rebootBusy: false,
    blockedReason: ""
});

const emit = defineEmits<{
    busy: [value: boolean];
    firmwareUpdated: [firmware: string];
    viewRecords: [operationId?: string];
    operationUpdated: [operation: UpgradeOperation];
    submissionUncertain: [];
    closeBlocked: [value: boolean];
}>();

const POLL_INTERVAL_MS = 2000;
const MAX_POLL_MS = 10 * 60_000;

const currentDevice = ref<DeviceVO | null>(props.device);
const loading = ref(false);
const loadError = ref("");
const activeOperation = ref<UpgradeOperation | null>(null);
const pollTimedOut = ref(false);
const uncertainSubmission = ref(false);
const submitPending = ref(false);
const submitError = ref("");
const confirmVisible = ref(false);
const pendingPayload = ref<UpgradePayload | null>(null);
const submittedPayload = ref<UpgradePayload | null>(null);
const lastFirmwareEventOperationId = ref("");
const form = reactive<UpgradePayload>({ firmware: "", fileUrl: "", manufacturer: "" });
const fieldErrors = reactive<Record<FieldName, string>>({ firmware: "", fileUrl: "", manufacturer: "" });

let requestVersion = 0;
let pollTimer: ReturnType<typeof setTimeout> | null = null;
let initializedDeviceId: number | null = null;

const deviceName = computed(() => currentDevice.value?.alias?.trim() || currentDevice.value?.name?.trim() || "未命名设备");
const currentFirmware = computed(() => currentDevice.value?.firmware?.trim() || "未上报");
const deviceOnline = computed(() => currentDevice.value?.online === true);
const protocol2022 = computed(() => currentDevice.value?.effectiveVersion === "2022");
const active = computed(() => Boolean(activeOperation.value && isActiveStatus(activeOperation.value.status) && !pollTimedOut.value));
const busy = computed(() => submitPending.value || active.value || uncertainSubmission.value);
const phase = computed<UpgradePhase>(() => {
    if (submitPending.value || activeOperation.value || uncertainSubmission.value) return "tracking";
    return confirmVisible.value && pendingPayload.value ? "confirm" : "prepare";
});
const formReady = computed(() => Boolean(form.firmware.trim() && form.fileUrl.trim() && form.manufacturer.trim()));
const unavailableReason = computed(() => {
    if (props.blockedReason?.trim()) return props.blockedReason.trim();
    if (!props.canUpgrade) return "当前账号没有设备升级权限。";
    if (!deviceOnline.value) return "设备离线，可查看记录，但不能发送升级请求。";
    if (!protocol2022.value) return "当前设备需使用 GB/T 28181-2022 配置后才能升级。";
    if (props.rebootBusy) return "设备重启请求处理中，暂不能发起升级。";
    if (uncertainSubmission.value) return "上一次升级请求结果未知，请先从记录确认，暂不要重复提交。";
    if (loadError.value) return "暂时无法确认设备当前升级任务，请稍后重试。";
    return "";
});
const submitDisabled = computed(() => Boolean(unavailableReason.value) || busy.value || loading.value);
const taskOperation = computed(() => activeOperation.value);
const taskFirmware = computed(() => taskOperation.value?.firmware?.trim() || submittedPayload.value?.firmware?.trim() || "未确定");
const taskManufacturer = computed(() => taskOperation.value?.manufacturer?.trim() || submittedPayload.value?.manufacturer?.trim() || currentDevice.value?.manufacturer?.trim() || "未确定");
const taskStatus = computed(() => {
    if (submitPending.value) return "提交中";
    if (uncertainSubmission.value && !taskOperation.value) return "结果未知";
    return statusText(taskOperation.value?.status);
});
const taskTone = computed(() => {
    if (submitPending.value) return "pending";
    if (uncertainSubmission.value && !taskOperation.value) return "warning";
    return statusTone(taskOperation.value?.status);
});
const taskStage = computed(() => {
    if (submitPending.value) return "正在提交升级请求";
    const status = statusKey(taskOperation.value?.status);
    if (status === "queued") return "请求已创建，等待发送";
    if (status === "sent") return "平台已发送，等待设备受理";
    if (status === "accepted") return "设备已受理，等待升级完成";
    if (status === "succeeded") return "设备已完成升级";
    if (status === "failed" || status === "rejected") return "设备未完成升级";
    if (status === "unknown" || (uncertainSubmission.value && !taskOperation.value)) return "平台未能确认设备执行结果";
    return "等待任务状态";
});

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
    if (statusKey(operation.status) === "unknown") return "设备执行结果未知，请根据记录和设备状态确认";
    return "-";
}

function parseDeadline(deadlineAt?: string | null) {
    const timestamp = deadlineAt ? new Date(deadlineAt).getTime() : Number.NaN;
    return Number.isFinite(timestamp) ? timestamp : Date.now() + MAX_POLL_MS;
}

function publishOperation(operation: UpgradeOperation) {
    activeOperation.value = operation;
    emit("operationUpdated", operation);
}

function markUnknown(operation?: UpgradeOperation) {
    pollTimedOut.value = true;
    uncertainSubmission.value = true;
    submitPending.value = false;
    if (operation) {
        publishOperation({
            ...operation,
            status: "unknown",
            errorMessage: operation.errorMessage || "升级结果未知，请根据记录和设备状态确认"
        });
    }
}

function emitCompletedFirmware(operation: UpgradeOperation) {
    const firmware = operation.currentFirmware?.trim();
    if (statusKey(operation.status) === "succeeded" && firmware && lastFirmwareEventOperationId.value !== operation.operationId) {
        lastFirmwareEventOperationId.value = operation.operationId;
        emit("firmwareUpdated", firmware);
    }
}

function recoverOperation(operations: UpgradeOperation[]) {
    const unresolved = operations.find(item => statusKey(item.status) === "unknown");
    const running = operations.find(item => isActiveStatus(item.status));
    const recovered = unresolved || running;
    if (recovered) {
        pollTimedOut.value = statusKey(recovered.status) === "unknown";
        uncertainSubmission.value = pollTimedOut.value;
        publishOperation(recovered);
        if (isActiveStatus(recovered.status)) {
            if (Date.now() >= parseDeadline(recovered.deadlineAt)) markUnknown(recovered);
            else schedulePoll(recovered, requestVersion, recovered.deviceId);
        }
    }
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
        const result = await listFirmwareUpgrades(deviceId, { page: 1, pageSize: 10 });
        if (!isActive(version, deviceId)) return;
        if (result.code === 0) {
            const operation = result.data?.list?.find(item => item.operationId === operationId);
            if (operation) {
                publishOperation(operation);
                pollTimedOut.value = false;
                uncertainSubmission.value = statusKey(operation.status) === "unknown";
                if (!isActiveStatus(operation.status)) {
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
    schedulePoll({ ...(activeOperation.value || {}), operationId, deviceId, status: "sent" } as UpgradeOperation, version, deviceId, deadline);
}

async function loadCurrentOperation(version: number, deviceId: number) {
    if (!isActive(version, deviceId)) return;
    loading.value = true;
    loadError.value = "";
    try {
        const result = await listFirmwareUpgrades(deviceId, { page: 1, pageSize: 10 });
        if (!isActive(version, deviceId)) return;
        if (result.code !== 0) throw new Error(result.message || "升级任务状态加载失败");
        const operations = result.data?.list || [];
        const localOperationId = activeOperation.value?.operationId;
        const localOperation = localOperationId ? operations.find(item => item.operationId === localOperationId) : undefined;
        if (localOperation) {
            publishOperation(localOperation);
            pollTimedOut.value = statusKey(localOperation.status) === "unknown";
            uncertainSubmission.value = pollTimedOut.value;
            if (isActiveStatus(localOperation.status)) schedulePoll(localOperation, version, deviceId);
        } else if (!activeOperation.value || isActiveStatus(activeOperation.value.status) || uncertainSubmission.value) {
            if (!activeOperation.value || !isActiveStatus(activeOperation.value.status)) recoverOperation(operations);
            else if (operations.some(item => item.operationId === activeOperation.value?.operationId)) recoverOperation(operations);
        }
    } catch (error: any) {
        if (isActive(version, deviceId)) {
            loadError.value = error?.message || "升级任务状态加载失败";
            Message.error(loadError.value);
        }
    } finally {
        if (isActive(version, deviceId)) loading.value = false;
    }
}

function resetForDevice(device: DeviceVO | null) {
    form.firmware = "";
    form.fileUrl = "";
    form.manufacturer = device?.manufacturer?.trim() || "";
    clearFieldErrors();
    activeOperation.value = null;
    pollTimedOut.value = false;
    uncertainSubmission.value = false;
    submitPending.value = false;
    submitError.value = "";
    confirmVisible.value = false;
    pendingPayload.value = null;
    submittedPayload.value = null;
    loadError.value = "";
    lastFirmwareEventOperationId.value = "";
}

function clearFieldErrors() {
    fieldErrors.firmware = "";
    fieldErrors.fileUrl = "";
    fieldErrors.manufacturer = "";
}

function validateField(field: FieldName) {
    const value = form[field].trim();
    if (field === "firmware") fieldErrors.firmware = value ? "" : "请输入目标固件版本";
    if (field === "manufacturer") fieldErrors.manufacturer = value ? "" : "请输入设备厂商";
    if (field === "fileUrl") {
        if (!value) fieldErrors.fileUrl = "请输入升级文件地址";
        else if (!isHttpUrl(value)) fieldErrors.fileUrl = "请输入 HTTP 或 HTTPS 地址";
        else fieldErrors.fileUrl = "";
    }
}

function validateForm() {
    validateField("firmware");
    validateField("manufacturer");
    validateField("fileUrl");
    return !fieldErrors.firmware && !fieldErrors.manufacturer && !fieldErrors.fileUrl;
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
    if (submitDisabled.value || !validateForm()) return;
    submitError.value = "";
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

function createIdempotencyKey() {
    if (typeof globalThis.crypto?.randomUUID === "function") return globalThis.crypto.randomUUID();
    return `device-upgrade-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

function notifyResult(operation: UpgradeOperation) {
    if (operation.deduplicated) {
        Message.info("已有相同升级请求，已返回原操作");
        return;
    }
    if (statusKey(operation.status) === "succeeded") Message.success("设备升级已完成");
    else if (statusKey(operation.status) === "accepted") Message.info("设备已受理升级请求，等待完成结果");
    else if (statusKey(operation.status) === "sent") Message.info("升级请求已发送，等待设备回报结果");
    else if (statusKey(operation.status) === "unknown") Message.warning("升级结果未知，请根据记录和设备状态确认");
    else if (statusKey(operation.status) === "failed" || statusKey(operation.status) === "rejected") Message.error(operation.errorMessage || operationReason(operation));
    else Message.info(`升级请求状态：${statusText(operation.status)}`);
}

async function confirmUpgrade() {
    const device = currentDevice.value;
    const payload = pendingPayload.value;
    const version = requestVersion;
    if (!device || !payload || !isActive(version, device.id) || submitDisabled.value) return;
    confirmVisible.value = false;
    pendingPayload.value = null;
    submittedPayload.value = payload;
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
        publishOperation(result.data);
        uncertainSubmission.value = false;
        notifyResult(result.data);
        if (!isActiveStatus(result.data.status)) {
            submitPending.value = false;
            emitCompletedFirmware(result.data);
            return;
        }
        submitPending.value = false;
        schedulePoll(result.data, version, device.id);
    } catch (error: any) {
        if (isActive(version, device.id)) {
            submitError.value = error?.message || "升级请求结果未知，请先从记录确认，暂不要重复提交。";
            uncertainSubmission.value = true;
            submitPending.value = false;
            emit("submissionUncertain");
            Message.warning(submitError.value);
        }
    }
}

function viewRecords() {
    emit("viewRecords", activeOperation.value?.operationId);
}

function startNewUpgrade() {
    clearPollTimer();
    activeOperation.value = null;
    pollTimedOut.value = false;
    uncertainSubmission.value = false;
    submitError.value = "";
    submittedPayload.value = null;
    confirmVisible.value = false;
    pendingPayload.value = null;
    form.firmware = "";
    form.fileUrl = "";
    form.manufacturer = currentDevice.value?.manufacturer?.trim() || "";
    clearFieldErrors();
}

watch(() => busy.value, value => emit("busy", value), { immediate: true });
watch(() => submitPending.value, value => emit("closeBlocked", value), { immediate: true });

watch([() => props.visible, () => props.device?.id], ([visible, deviceId]) => {
    invalidateRequests();
    currentDevice.value = props.device;
    if (typeof deviceId !== "number") return;
    if (initializedDeviceId !== deviceId) {
        initializedDeviceId = deviceId;
        resetForDevice(props.device);
    }
    if (visible) void loadCurrentOperation(requestVersion, deviceId);
}, { immediate: true });

watch(() => props.device, device => {
    currentDevice.value = device;
    if (device && initializedDeviceId === device.id && !form.manufacturer.trim()) form.manufacturer = device.manufacturer?.trim() || "";
}, { deep: true });

onBeforeUnmount(() => {
    invalidateRequests();
    // Do not emit busy=false here: a parent may keep the operation lock after a drawer is unmounted.
});
</script>

<template>
    <section class="firmware-upgrade-panel" data-testid="firmware-upgrade-panel" :aria-busy="loading || submitPending ? 'true' : 'false'">
        <div class="upgrade-section-heading">
            <div><span class="upgrade-eyebrow">设备级操作</span><strong>设备固件升级</strong></div>
            <span class="upgrade-scope" :title="deviceName">当前版本 {{ currentFirmware }}</span>
        </div>

        <div v-if="phase === 'prepare'" class="upgrade-form" data-testid="firmware-upgrade-form">
            <label data-testid="firmware-field">
                <span>目标固件版本</span>
                <a-input v-model="form.firmware" placeholder="例如 V5.9.0" :disabled="submitDisabled" allow-clear @blur="validateField('firmware')" />
                <small aria-live="polite" class="field-error" data-testid="firmware-error">{{ fieldErrors.firmware }}</small>
            </label>
            <label data-testid="manufacturer-field">
                <span>设备厂商</span>
                <a-input v-model="form.manufacturer" placeholder="请输入升级包对应厂商" :disabled="submitDisabled" allow-clear @blur="validateField('manufacturer')" />
                <small aria-live="polite" class="field-error" data-testid="manufacturer-error">{{ fieldErrors.manufacturer }}</small>
            </label>
            <label class="upgrade-url-field" data-testid="file-url-field">
                <span>升级文件地址</span>
                <a-input v-model="form.fileUrl" placeholder="设备可访问的 HTTP 或 HTTPS 地址" :disabled="submitDisabled" allow-clear @blur="validateField('fileUrl')" />
                <small aria-live="polite" class="field-error" data-testid="file-url-error">{{ fieldErrors.fileUrl }}</small>
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
                    下一步：核对信息
                </a-button>
            </div>
        </div>

        <div v-else-if="phase === 'confirm' && pendingPayload" class="upgrade-confirm" data-testid="firmware-upgrade-confirmation" role="alertdialog" aria-label="确认设备固件升级">
            <div class="upgrade-step-label">2 / 3 · 确认升级</div>
            <div class="upgrade-confirm-icon"><AlertTriangle :size="19" /></div>
            <div class="upgrade-confirm-copy">
                <strong>确认向设备发起升级？</strong>
                <dl class="upgrade-confirm-facts">
                    <div><dt>设备</dt><dd>{{ deviceName }}</dd></div>
                    <div><dt>版本</dt><dd class="mono">{{ currentFirmware }} → {{ pendingPayload.firmware }}</dd></div>
                    <div><dt>厂商</dt><dd>{{ pendingPayload.manufacturer }}</dd></div>
                    <div><dt>影响</dt><dd>升级期间设备及其通道可能短暂离线</dd></div>
                </dl>
                <span class="upgrade-confirm-url" :title="pendingPayload.fileUrl">文件地址：{{ pendingPayload.fileUrl }}</span>
            </div>
            <p v-if="unavailableReason" class="upgrade-hint warning" role="alert">{{ unavailableReason }}</p>
            <div class="upgrade-confirm-actions">
                <a-button data-testid="firmware-upgrade-cancel" @click="cancelUpgrade">返回修改</a-button>
                <a-button data-testid="firmware-upgrade-confirm" type="primary" :disabled="submitDisabled" :loading="submitPending" @click="confirmUpgrade">确认升级</a-button>
            </div>
        </div>

        <div v-else class="upgrade-tracking" data-testid="firmware-upgrade-tracking">
            <div class="upgrade-tracking-head">
                <div>
                    <span class="upgrade-step-label">3 / 3 · 本次任务</span>
                    <strong>{{ taskStage }}</strong>
                </div>
                <span class="upgrade-status" :class="`tone-${taskTone}`">
                    <CircleCheck v-if="taskTone === 'success'" :size="14" />
                    <Clock3 v-else-if="taskTone === 'pending'" :size="14" />
                    <CircleX v-else :size="14" />
                    {{ taskStatus }}
                </span>
            </div>
            <ol class="upgrade-stages" data-testid="firmware-upgrade-stages">
                <li :class="{ current: submitPending || !taskOperation, complete: Boolean(taskOperation) }">准备请求</li>
                <li :class="{ current: taskOperation && ['queued', 'sent'].includes(taskOperation.status), complete: taskOperation && ['accepted', 'succeeded'].includes(statusKey(taskOperation.status)) }">设备受理</li>
                <li :class="{ current: taskOperation && ['accepted'].includes(statusKey(taskOperation.status)), complete: taskOperation && statusKey(taskOperation.status) === 'succeeded' }">设备完成</li>
            </ol>
            <p v-if="submitError" class="upgrade-error" role="alert"><CircleX :size="14" />{{ submitError }}</p>
            <p v-if="taskOperation && (statusKey(taskOperation.status) === 'sent' || statusKey(taskOperation.status) === 'accepted')" class="upgrade-hint"><AlertTriangle :size="14" />平台状态会持续更新，关闭抽屉不会取消升级任务。</p>
            <p v-if="uncertainSubmission" class="upgrade-hint warning"><AlertTriangle :size="14" />请先从升级记录确认本次请求，暂不要重复提交。</p>
            <p v-if="taskOperation && ['failed', 'rejected', 'unknown'].includes(taskOperation.status)" class="upgrade-hint warning" role="status">{{ operationReason(taskOperation) }}</p>
            <dl class="upgrade-task-facts">
                <div><dt>设备</dt><dd>{{ deviceName }}</dd></div>
                <div><dt>当前版本</dt><dd class="mono">{{ currentFirmware }}</dd></div>
                <div><dt>目标版本</dt><dd class="mono">{{ taskFirmware }}</dd></div>
                <div><dt>厂商</dt><dd>{{ taskManufacturer }}</dd></div>
                <div v-if="taskOperation?.createdAt"><dt>发起时间</dt><dd class="mono">{{ formatDateTime(taskOperation.createdAt) }}</dd></div>
                <div v-if="taskOperation?.completedAt"><dt>完成时间</dt><dd class="mono">{{ formatDateTime(taskOperation.completedAt) }}</dd></div>
            </dl>
            <details v-if="taskOperation" class="upgrade-technical-details">
                <summary>技术详情</summary>
                <dl>
                    <div><dt>操作 ID</dt><dd class="mono">{{ taskOperation.operationId || '-' }}</dd></div>
                    <div><dt>Session ID</dt><dd class="mono">{{ taskOperation.sessionId || '-' }}</dd></div>
                    <div><dt>SIP 状态</dt><dd class="mono">{{ taskOperation.sipStatus ?? '-' }}</dd></div>
                    <div><dt>结果说明</dt><dd>{{ operationReason(taskOperation) }}</dd></div>
                </dl>
            </details>
            <div class="upgrade-tracking-actions">
                <a-button data-testid="firmware-upgrade-view-records" @click="viewRecords">查看升级记录</a-button>
                <a-button v-if="taskOperation && !isActiveStatus(taskOperation.status) && !uncertainSubmission" data-testid="firmware-upgrade-new" type="primary" @click="startNewUpgrade">准备新升级</a-button>
            </div>
        </div>

        <p v-if="phase === 'prepare' && unavailableReason" class="upgrade-hint warning"><CircleX :size="14" />{{ unavailableReason }}</p>
        <p v-else-if="phase === 'prepare' && !loadError" class="upgrade-hint"><AlertTriangle :size="14" />请确认设备能访问此地址。升级结果及当前版本以设备最终回报为准。</p>
        <p v-if="loadError" class="upgrade-error" role="alert"><CircleX :size="14" />{{ loadError }}</p>
    </section>
</template>

<style scoped>
.firmware-upgrade-panel { padding: 14px; border: 1px solid var(--uvp-panel-border); border-radius: 12px; background: var(--uvp-panel-bg); }
.upgrade-section-heading { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-bottom: 12px; }
.upgrade-section-heading > div { display: grid; gap: 2px; }
.upgrade-section-heading strong { font-size: 14px; }
.upgrade-eyebrow, .upgrade-step-label { color: var(--uvp-brand); font-size: 11px; font-weight: 650; letter-spacing: .04em; }
.upgrade-scope, .upgrade-hint { color: var(--uvp-text-tertiary); font-size: 11px; }
.upgrade-form { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; padding-top: 11px; border-top: 1px solid var(--uvp-panel-border); }
.upgrade-form label { display: grid; gap: 5px; min-width: 0; }
.upgrade-form label > span { color: var(--uvp-text-secondary); font-size: 12px; }
.upgrade-url-field { grid-column: 1 / -1; }
.upgrade-submit-wrap { display: flex; align-items: end; justify-content: flex-end; }
.upgrade-form :deep(.arco-input-wrapper) { color: var(--uvp-text-primary); background: var(--uvp-dialog-control-bg) !important; border-color: var(--uvp-dialog-border) !important; }
.upgrade-form :deep(.arco-input) { color: var(--uvp-text-primary) !important; }
.upgrade-form :deep(.arco-input::placeholder) { color: var(--uvp-text-tertiary) !important; }
.field-error { display: block; min-height: 15px; color: var(--uvp-danger); font-size: 11px; line-height: 1.35; }
.upgrade-hint, .upgrade-error { display: flex; align-items: flex-start; gap: 6px; margin: 10px 0 0; line-height: 1.45; }
.upgrade-hint.warning { color: var(--uvp-warning); }
.upgrade-error { padding: 8px 10px; color: var(--uvp-danger); background: var(--uvp-danger-soft); border: 1px solid var(--uvp-danger-border); border-radius: 8px; font-size: 12px; }
.upgrade-confirm, .upgrade-tracking { display: grid; gap: 12px; padding-top: 11px; border-top: 1px solid var(--uvp-panel-border); }
.upgrade-confirm { grid-template-columns: auto minmax(0, 1fr); }
.upgrade-confirm > .upgrade-hint { grid-column: 1 / -1; }
.upgrade-confirm > .upgrade-step-label { grid-column: 1 / -1; }
.upgrade-confirm-icon { display: grid; width: 34px; height: 34px; color: var(--uvp-warning); background: var(--uvp-warning-soft); border-radius: 50%; place-items: center; }
.upgrade-confirm-copy { display: grid; gap: 8px; min-width: 0; }
.upgrade-confirm-copy strong, .upgrade-tracking-head strong { color: var(--uvp-text-primary); font-size: 14px; }
.upgrade-confirm-facts, .upgrade-task-facts, .upgrade-technical-details dl { display: grid; grid-template-columns: 1fr 1fr; gap: 8px 12px; margin: 0; }
.upgrade-confirm-facts div, .upgrade-task-facts div, .upgrade-technical-details dl div { display: grid; gap: 3px; min-width: 0; }
.upgrade-confirm-facts dt, .upgrade-task-facts dt, .upgrade-technical-details dt { color: var(--uvp-text-tertiary); font-size: 11px; }
.upgrade-confirm-facts dd, .upgrade-task-facts dd, .upgrade-technical-details dd { overflow: hidden; margin: 0; color: var(--uvp-text-secondary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.upgrade-confirm-url { overflow: hidden; color: var(--uvp-text-tertiary); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.upgrade-confirm-actions, .upgrade-tracking-actions { display: flex; justify-content: flex-end; gap: 7px; }
.upgrade-confirm-actions { position: sticky; bottom: 0; z-index: 1; grid-column: 1 / -1; padding-top: 10px; background: var(--uvp-panel-bg); }
.upgrade-tracking-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; }
.upgrade-tracking-head > div { display: grid; gap: 3px; }
.upgrade-status { display: inline-flex; align-items: center; gap: 4px; flex: 0 0 auto; padding: 3px 8px; color: var(--uvp-warning); background: var(--uvp-warning-soft); border-radius: 999px; font-size: 11px; }
.upgrade-status.tone-success { color: var(--uvp-brand-cyan); background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent); }
.upgrade-status.tone-danger { color: var(--uvp-danger); background: var(--uvp-danger-soft); }
.upgrade-stages { display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; padding: 0; margin: 0; list-style: none; counter-reset: stage; }
.upgrade-stages li { position: relative; padding: 8px 8px 8px 28px; color: var(--uvp-text-tertiary); background: var(--uvp-list-toolbar-bg); border: 1px solid var(--uvp-panel-border); border-radius: 8px; font-size: 11px; }
.upgrade-stages li::before { position: absolute; top: 7px; left: 9px; color: var(--uvp-text-tertiary); content: counter(stage); counter-increment: stage; font-weight: 700; }
.upgrade-stages li.current { color: var(--uvp-brand); border-color: var(--uvp-brand); }
.upgrade-stages li.complete { color: var(--uvp-brand-cyan); }
.upgrade-technical-details { color: var(--uvp-text-tertiary); font-size: 11px; }
.upgrade-technical-details summary { cursor: pointer; width: fit-content; }
.upgrade-technical-details dl { padding-top: 8px; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.spin { animation: spin .8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 640px) {
    .upgrade-form { grid-template-columns: 1fr; }
    .upgrade-url-field { grid-column: auto; }
    .upgrade-submit-wrap { justify-content: stretch; }
    .upgrade-submit-wrap .arco-btn { width: 100%; }
    .upgrade-tracking-head { flex-direction: column; }
    .upgrade-stages { grid-template-columns: 1fr; }
    .upgrade-confirm-facts, .upgrade-task-facts, .upgrade-technical-details dl { grid-template-columns: 1fr; }
}
</style>
