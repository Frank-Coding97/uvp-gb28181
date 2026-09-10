<script setup lang="ts">
import { computed, ref } from "vue";
import { Upload } from "@lucide/vue";
import DeviceFirmwareUpgradePanel from "./DeviceFirmwareUpgradePanel.vue";
import type { DeviceVO, UpgradeOperation } from "./api";

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
    "update:visible": [value: boolean];
    busy: [value: boolean];
    firmwareUpdated: [firmware: string];
    viewRecords: [operationId?: string];
    operationUpdated: [operation: UpgradeOperation];
    submissionUncertain: [];
}>();

const closeBlocked = ref(false);
const deviceName = computed(() => props.device?.alias?.trim() || props.device?.name?.trim() || "未命名设备");
const currentFirmware = computed(() => props.device?.firmware?.trim() || "未上报");
const deviceVendor = computed(() => [props.device?.manufacturer, props.device?.model].filter(Boolean).join(" / ") || "未上报");
const deviceOnline = computed(() => props.device?.online === true);
const title = computed(() => `${deviceName.value} · 固件升级`);

function close() {
    if (closeBlocked.value) return;
    emit("update:visible", false);
}

function updateVisible(value: boolean) {
    if (!value && closeBlocked.value) return;
    emit("update:visible", value);
}

function setCloseBlocked(value: boolean) {
    closeBlocked.value = value;
}
</script>

<template>
    <a-drawer
        :visible="visible"
        :width="'min(680px, 100vw)'"
        :closable="!closeBlocked"
        :mask-closable="!closeBlocked"
        :footer="false"
        class="firmware-upgrade-drawer"
        data-testid="firmware-upgrade-drawer"
        @cancel="close"
        @update:visible="updateVisible"
    >
        <template #title>
            <div class="upgrade-drawer-title">
                <span class="upgrade-drawer-icon"><Upload :size="17" /></span>
                <div>
                    <strong>{{ title }}</strong>
                    <small>当前版本 {{ currentFirmware }}</small>
                </div>
            </div>
        </template>
        <div class="upgrade-drawer-identity" data-testid="firmware-upgrade-device-identity">
            <div><span>设备编码</span><strong class="mono">{{ device?.deviceId || "-" }}</strong></div>
            <div><span>在线状态</span><strong :class="deviceOnline ? 'online' : 'offline'">{{ deviceOnline ? "在线" : "离线" }}</strong></div>
            <div><span>厂商 / 型号</span><strong>{{ deviceVendor }}</strong></div>
        </div>
        <DeviceFirmwareUpgradePanel
            :visible="visible"
            :device="device"
            :can-upgrade="canUpgrade"
            :reboot-busy="rebootBusy"
            :blocked-reason="blockedReason"
            @busy="emit('busy', $event)"
            @firmware-updated="emit('firmwareUpdated', $event)"
            @view-records="emit('viewRecords', $event)"
            @operation-updated="emit('operationUpdated', $event)"
            @submission-uncertain="emit('submissionUncertain')"
            @close-blocked="setCloseBlocked"
        />
    </a-drawer>
</template>

<style scoped>
.firmware-upgrade-drawer :deep(.arco-drawer-header) { border-bottom-color: var(--uvp-panel-border); background: var(--uvp-panel-bg); }
.firmware-upgrade-drawer :deep(.arco-drawer-body) { padding: 16px; background: var(--uvp-page-bg); }
.upgrade-drawer-title { display: flex; align-items: center; gap: 10px; min-width: 0; }
.upgrade-drawer-icon { display: grid; width: 32px; height: 32px; flex: 0 0 auto; color: var(--uvp-brand); background: color-mix(in srgb, var(--uvp-brand) 12%, transparent); border-radius: 8px; place-items: center; }
.upgrade-drawer-title > div { display: grid; min-width: 0; gap: 2px; }
.upgrade-drawer-title strong { overflow: hidden; color: var(--uvp-text-primary); font-size: 15px; text-overflow: ellipsis; white-space: nowrap; }
.upgrade-drawer-title small { overflow: hidden; color: var(--uvp-text-tertiary); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.upgrade-drawer-identity { display: grid; grid-template-columns: 1.25fr .75fr 1fr; gap: 8px; padding: 10px 12px; margin-bottom: 12px; border: 1px solid var(--uvp-panel-border); border-radius: 9px; background: var(--uvp-list-toolbar-bg); }
.upgrade-drawer-identity > div { display: grid; gap: 3px; min-width: 0; }
.upgrade-drawer-identity span { color: var(--uvp-text-tertiary); font-size: 11px; }
.upgrade-drawer-identity strong { overflow: hidden; color: var(--uvp-text-secondary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.upgrade-drawer-identity strong.online { color: var(--uvp-brand-cyan); }
.upgrade-drawer-identity strong.offline { color: var(--uvp-warning); }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
@media (max-width: 640px) {
    .firmware-upgrade-drawer :deep(.arco-drawer-body) { padding: 10px; }
    .upgrade-drawer-identity { grid-template-columns: 1fr 1fr; }
    .upgrade-drawer-identity > div:first-child { grid-column: 1 / -1; }
    .upgrade-drawer-identity strong { white-space: normal; overflow-wrap: anywhere; }
}
</style>
