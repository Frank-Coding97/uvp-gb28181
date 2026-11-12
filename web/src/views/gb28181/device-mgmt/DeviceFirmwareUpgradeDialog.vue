<script setup lang="ts">
import { computed, ref } from "vue";
import { Upload } from "@lucide/vue";
import DeviceFirmwareUpgradePanel from "./DeviceFirmwareUpgradePanel.vue";
import type { DeviceVO, UpgradeOperation } from "./api";
import { useDeviceStatusLabel } from "../useDeviceStatusDict";

const props = withDefaults(
  defineProps<{
    visible: boolean;
    device: DeviceVO | null;
    canUpgrade: boolean;
    rebootBusy?: boolean;
    blockedReason?: string;
  }>(),
  {
    device: null,
    canUpgrade: false,
    rebootBusy: false,
    blockedReason: ""
  }
);

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
/** 在线状态文案（`device_status` 字典）。 */
const deviceStatusLabel = useDeviceStatusLabel();
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
  <a-modal
    :visible="visible"
    :width="'min(760px, calc(100vw - 32px))'"
    :closable="!closeBlocked"
    :mask-closable="!closeBlocked"
    :footer="false"
    modal-class="uvp-system-dialog firmware-upgrade-dialog"
    data-testid="firmware-upgrade-dialog"
    @cancel="close"
    @update:visible="updateVisible"
  >
    <template #title>
      <div class="upgrade-dialog-title">
        <span class="upgrade-dialog-icon"><Upload :size="17" /></span>
        <div>
          <strong>{{ title }}</strong>
          <small>当前版本 {{ currentFirmware }}</small>
        </div>
      </div>
    </template>
    <div class="upgrade-dialog-identity" data-testid="firmware-upgrade-device-identity">
      <div>
        <span>设备编码</span><strong class="mono">{{ device?.deviceId || "-" }}</strong>
      </div>
      <div>
        <span>在线状态</span><strong :class="deviceOnline ? 'online' : 'offline'">{{ deviceStatusLabel(deviceOnline) }}</strong>
      </div>
      <div>
        <span>厂商 / 型号</span><strong>{{ deviceVendor }}</strong>
      </div>
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
  </a-modal>
</template>

<style scoped>
:global(.firmware-upgrade-dialog .arco-modal-body) {
  max-height: min(760px, calc(100vh - 180px));
  overflow-y: auto;
}
.upgrade-dialog-title {
  display: flex;
  gap: 10px;
  align-items: center;
  min-width: 0;
}
.upgrade-dialog-icon {
  display: grid;
  flex: 0 0 auto;
  place-items: center;
  width: 32px;
  height: 32px;
  color: var(--uvp-brand);
  background: color-mix(in srgb, var(--uvp-brand) 12%, transparent);
  border-radius: 8px;
}
.upgrade-dialog-title > div {
  display: grid;
  gap: 2px;
  min-width: 0;
}
.upgrade-dialog-title strong {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 15px;
  color: var(--uvp-text-primary);
  white-space: nowrap;
}
.upgrade-dialog-title small {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}
.upgrade-dialog-identity {
  display: grid;
  grid-template-columns: 1.25fr 0.75fr 1fr;
  gap: 8px;
  padding: 10px 12px;
  margin-bottom: 12px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 9px;
}
.upgrade-dialog-identity > div {
  display: grid;
  gap: 3px;
  min-width: 0;
}
.upgrade-dialog-identity span {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.upgrade-dialog-identity strong {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 12px;
  color: var(--uvp-text-secondary);
  white-space: nowrap;
}
.upgrade-dialog-identity strong.online {
  color: var(--uvp-brand-cyan);
}
.upgrade-dialog-identity strong.offline {
  color: var(--uvp-warning);
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}

@media (width <= 640px) {
  .upgrade-dialog-identity {
    grid-template-columns: 1fr 1fr;
  }
  .upgrade-dialog-identity > div:first-child {
    grid-column: 1 / -1;
  }
  .upgrade-dialog-identity strong {
    overflow-wrap: anywhere;
    white-space: normal;
  }
}
</style>
