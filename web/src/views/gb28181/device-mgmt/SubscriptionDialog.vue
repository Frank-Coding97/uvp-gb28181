<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Bell, FolderTree, Loader2, MapPin, RefreshCcw } from "@lucide/vue";
import { Message } from "@arco-design/web-vue";
import {
    listDeviceSubscriptions,
    renewDeviceSubscription,
    updateDeviceSubscription,
    type DeviceSubscription,
    type SubscriptionKind
} from "./api";

const props = defineProps<{ visible: boolean; deviceId?: number; deviceName?: string }>();
const emit = defineEmits<{ "update:visible": [value: boolean]; changed: [] }>();

const loading = ref(false);
const rows = ref<DeviceSubscription[]>([]);
const pendingKind = ref<SubscriptionKind | null>(null);
const kinds: Array<{ kind: SubscriptionKind; label: string; icon: typeof FolderTree }> = [
    { kind: "catalog", label: "目录", icon: FolderTree },
    { kind: "mobile_position", label: "位置", icon: MapPin },
    { kind: "alarm", label: "报警", icon: Bell }
];

const byKind = computed(() => new Map(rows.value.map(row => [row.kind, row])));

function statusText(status?: DeviceSubscription["status"]) {
    return { disabled: "未启用", pending: "建立中", active: "已启用", degraded: "异常", expired: "已过期" }[status || "disabled"];
}

async function load() {
    if (!props.deviceId) return;
    loading.value = true;
    try {
        const res = await listDeviceSubscriptions(props.deviceId);
        if (res.code !== 0) throw new Error(res.message || "加载订阅失败");
        rows.value = res.data?.list || [];
    } catch (error: any) {
        Message.error(error?.message || "加载订阅失败");
    } finally {
        loading.value = false;
    }
}

async function update(kind: SubscriptionKind, enabled: boolean) {
    if (!props.deviceId || pendingKind.value) return;
    pendingKind.value = kind;
    try {
        const res = await updateDeviceSubscription(props.deviceId, kind, enabled);
        if (res.code !== 0) throw new Error(res.message || "更新订阅失败");
        rows.value = rows.value.map(row => row.kind === kind ? res.data : row);
        emit("changed");
    } catch (error: any) {
        Message.error(error?.message || "更新订阅失败");
    } finally {
        pendingKind.value = null;
    }
}

async function renew(kind: SubscriptionKind) {
    if (!props.deviceId || pendingKind.value) return;
    pendingKind.value = kind;
    try {
        const res = await renewDeviceSubscription(props.deviceId, kind);
        if (res.code !== 0) throw new Error(res.message || "续订失败");
        rows.value = rows.value.map(row => row.kind === kind ? res.data : row);
        emit("changed");
    } catch (error: any) {
        Message.error(error?.message || "续订失败");
    } finally {
        pendingKind.value = null;
    }
}

watch(() => props.visible, visible => { if (visible) load(); });
</script>

<template>
    <a-modal :visible="visible" :footer="false" :width="560" :mask-closable="!pendingKind" @update:visible="emit('update:visible', $event)">
        <template #title>订阅管理<span v-if="deviceName" class="dialog-device">{{ deviceName }}</span></template>
        <div class="subscription-list" :class="{ loading }">
            <div v-for="item in kinds" :key="item.kind" class="subscription-row">
                <component :is="item.icon" :size="18" class="subscription-icon" />
                <div class="subscription-main">
                    <strong>{{ item.label }}订阅</strong>
                    <span :class="`status-${byKind.get(item.kind)?.status || 'disabled'}`">{{ statusText(byKind.get(item.kind)?.status) }}</span>
                    <small>{{ byKind.get(item.kind)?.lastError || byKind.get(item.kind)?.lastNotifyAt || "暂无通知" }}</small>
                </div>
                <a-tooltip content="立即续订"><button class="icon-btn small framed" type="button" :disabled="pendingKind !== null || !byKind.get(item.kind)?.enabled" @click="renew(item.kind)"><Loader2 v-if="pendingKind === item.kind" :size="13" class="spin" /><RefreshCcw v-else :size="13" /></button></a-tooltip>
                <a-switch :model-value="byKind.get(item.kind)?.enabled || false" :loading="pendingKind === item.kind" :disabled="pendingKind !== null" @change="(value: boolean | string | number) => update(item.kind, Boolean(value))" />
            </div>
        </div>
    </a-modal>
</template>

<style scoped>
.dialog-device { margin-left: 8px; color: var(--uvp-text-tertiary); font-size: 13px; font-weight: 400; }
.subscription-list { display: grid; gap: 8px; }
.subscription-row { display: grid; grid-template-columns: 24px minmax(0, 1fr) 30px 42px; align-items: center; gap: 10px; min-height: 68px; padding: 10px; border: 1px solid var(--uvp-panel-border); border-radius: 6px; }
.subscription-icon { color: var(--uvp-brand); }
.subscription-main { display: grid; gap: 3px; min-width: 0; }
.subscription-main strong { font-size: 13px; }
.subscription-main span { font-size: 12px; }.subscription-main small { overflow: hidden; color: var(--uvp-text-tertiary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.status-active { color: #0f766e; }.status-pending { color: #b7791f; }.status-degraded { color: #d14343; }.status-expired { color: #b7791f; }.status-disabled { color: var(--uvp-text-tertiary); }
@media (max-width: 480px) { .subscription-row { grid-template-columns: 24px minmax(0, 1fr) 30px 42px; gap: 7px; padding: 9px; } }
</style>
