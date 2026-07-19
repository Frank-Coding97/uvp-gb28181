<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import { Bell, FolderTree, MapPin, RefreshCcw, Save } from "@lucide/vue";
import { Message } from "@arco-design/web-vue";
import {
    listDeviceSubscriptions,
    renewDeviceSubscription,
    updateDeviceSubscription,
    type DeviceSubscription,
    type SubscriptionUpdate,
    type SubscriptionKind
} from "./api";

const props = defineProps<{ visible: boolean; deviceId?: number; deviceName?: string }>();
const emit = defineEmits<{ "update:visible": [value: boolean]; changed: [] }>();

const loading = ref(false);
const rows = ref<DeviceSubscription[]>([]);
const pendingKind = ref<SubscriptionKind | null>(null);
const drafts = reactive<Record<SubscriptionKind, { expiresSeconds: number; intervalSeconds: number }>>({
    catalog: { expiresSeconds: 3600, intervalSeconds: 0 },
    mobile_position: { expiresSeconds: 3600, intervalSeconds: 30 },
    alarm: { expiresSeconds: 3600, intervalSeconds: 0 }
});
const kinds: Array<{ kind: SubscriptionKind; label: string; icon: typeof FolderTree }> = [
    { kind: "catalog", label: "目录", icon: FolderTree },
    { kind: "mobile_position", label: "位置", icon: MapPin },
    { kind: "alarm", label: "报警", icon: Bell }
];

const byKind = computed(() => new Map(rows.value.map(row => [row.kind, row])));

function updateDraft(row: DeviceSubscription) {
    drafts[row.kind] = { expiresSeconds: row.expiresSeconds, intervalSeconds: row.intervalSeconds };
}

function replaceRow(row: DeviceSubscription) {
    rows.value = rows.value.map(item => item.kind === row.kind ? row : item);
    updateDraft(row);
}

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
        rows.value.forEach(updateDraft);
    } catch (error: any) {
        Message.error(error?.message || "加载订阅失败");
    } finally {
        loading.value = false;
    }
}

async function update(kind: SubscriptionKind, data: SubscriptionUpdate, successMessage?: string) {
    if (!props.deviceId || pendingKind.value) return;
    pendingKind.value = kind;
    try {
        const res = await updateDeviceSubscription(props.deviceId, kind, data);
        if (res.code !== 0) throw new Error(res.message || "更新订阅失败");
        replaceRow(res.data);
        emit("changed");
        if (successMessage) Message.success(successMessage);
    } catch (error: any) {
        Message.error(error?.message || "更新订阅失败");
    } finally {
        pendingKind.value = null;
    }
}

async function saveSettings(kind: SubscriptionKind) {
    const draft = drafts[kind];
    if (!Number.isInteger(draft.expiresSeconds) || draft.expiresSeconds < 60 || draft.expiresSeconds > 604800) {
        Message.warning("订阅有效期需在 60-604800 秒之间");
        return;
    }
    if (kind === "mobile_position" && (!Number.isInteger(draft.intervalSeconds) || draft.intervalSeconds < 1 || draft.intervalSeconds > 86400)) {
        Message.warning("位置上报间隔需在 1-86400 秒之间");
        return;
    }
    await update(kind, {
        expiresSeconds: draft.expiresSeconds,
        ...(kind === "mobile_position" ? { intervalSeconds: draft.intervalSeconds } : {})
    }, byKind.value.get(kind)?.enabled ? "配置已保存并应用" : "配置已保存");
}

async function renew(kind: SubscriptionKind) {
    if (!props.deviceId || pendingKind.value) return;
    pendingKind.value = kind;
    try {
        const res = await renewDeviceSubscription(props.deviceId, kind);
        if (res.code !== 0) throw new Error(res.message || "续订失败");
        replaceRow(res.data);
        emit("changed");
        Message.success("订阅已续订");
    } catch (error: any) {
        Message.error(error?.message || "续订失败");
    } finally {
        pendingKind.value = null;
    }
}

watch(() => props.visible, visible => { if (visible) load(); });
</script>

<template>
    <a-modal :visible="visible" modal-class="uvp-system-dialog" :footer="false" :width="620" :mask-closable="!pendingKind" @update:visible="emit('update:visible', $event)">
        <template #title>订阅管理<span v-if="deviceName" class="dialog-device">{{ deviceName }}</span></template>
        <div class="subscription-list" :class="{ loading }">
            <div v-for="item in kinds" :key="item.kind" class="subscription-row">
                <component :is="item.icon" :size="18" class="subscription-icon" />
                <div class="subscription-main">
                    <strong>{{ item.label }}订阅</strong>
                    <span :class="`status-${byKind.get(item.kind)?.status || 'disabled'}`">{{ statusText(byKind.get(item.kind)?.status) }}</span>
                    <small>{{ byKind.get(item.kind)?.lastError || byKind.get(item.kind)?.lastNotifyAt || "暂无通知" }}</small>
                    <div class="subscription-settings">
                        <label>
                            <span>有效期</span>
                            <a-input-number v-model="drafts[item.kind].expiresSeconds" :min="60" :max="604800" :precision="0" :disabled="pendingKind !== null" hide-button />
                            <em>秒</em>
                        </label>
                        <label v-if="item.kind === 'mobile_position'">
                            <span>上报间隔</span>
                            <a-input-number v-model="drafts[item.kind].intervalSeconds" :min="1" :max="86400" :precision="0" :disabled="pendingKind !== null" hide-button />
                            <em>秒</em>
                        </label>
                    </div>
                </div>
                <div class="subscription-actions">
                    <a-button type="primary" size="mini" @click="saveSettings(item.kind)" :loading="pendingKind === item.kind" :disabled="pendingKind !== null">
                        <template #icon><Save :size="13" /></template>
                        保存设置
                    </a-button>
                    <a-button class="subscription-renew" size="mini" @click="renew(item.kind)" :loading="pendingKind === item.kind" :disabled="pendingKind !== null || !byKind.get(item.kind)?.enabled">
                        <template #icon><RefreshCcw :size="13" /></template>
                        续订
                    </a-button>
                </div>
                <a-switch :model-value="byKind.get(item.kind)?.enabled || false" :loading="pendingKind === item.kind" :disabled="pendingKind !== null" @change="(value: boolean | string | number) => update(item.kind, { enabled: Boolean(value) }, Boolean(value) ? '订阅已启用' : '订阅已关闭')" />
            </div>
        </div>
    </a-modal>
</template>

<style scoped>
.dialog-device { margin-left: 8px; color: var(--uvp-text-tertiary); font-size: 13px; font-weight: 400; }
.subscription-list { display: grid; gap: 8px; }
.subscription-row { display: grid; grid-template-columns: 24px minmax(0, 1fr) auto 42px; align-items: center; gap: 12px; min-height: 104px; padding: 12px; border: 1px solid var(--uvp-panel-border); border-radius: 6px; }
.subscription-icon { color: var(--uvp-brand); }
.subscription-main { display: grid; gap: 3px; min-width: 0; }
.subscription-main strong { font-size: 13px; }
.subscription-main span { font-size: 12px; }.subscription-main small { overflow: hidden; color: var(--uvp-text-tertiary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.subscription-settings { display: flex; flex-wrap: wrap; gap: 8px 14px; margin-top: 5px; }
.subscription-settings label { display: inline-flex; align-items: center; gap: 5px; color: var(--uvp-text-secondary); font-size: 12px; white-space: nowrap; }
.subscription-settings :deep(.arco-input-number) { width: 82px; }
.subscription-settings em { color: var(--uvp-text-tertiary); font-size: 12px; font-style: normal; }
.subscription-actions { display: grid; gap: 6px; }
.subscription-actions :deep(.subscription-renew) { color: var(--uvp-brand-cyan) !important; background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, var(--uvp-panel-bg)) !important; border-color: color-mix(in srgb, var(--uvp-brand-cyan) 30%, var(--uvp-panel-border)) !important; }
.subscription-actions :deep(.subscription-renew:hover:not(:disabled)) { background: color-mix(in srgb, var(--uvp-brand-cyan) 16%, var(--uvp-panel-bg)) !important; border-color: color-mix(in srgb, var(--uvp-brand-cyan) 48%, var(--uvp-panel-border)) !important; }
.status-active { color: #0f766e; }.status-pending { color: #b7791f; }.status-degraded { color: #d14343; }.status-expired { color: #b7791f; }.status-disabled { color: var(--uvp-text-tertiary); }
@media (max-width: 640px) { .subscription-row { grid-template-columns: 24px minmax(0, 1fr) 42px; gap: 9px; } .subscription-actions { grid-column: 2 / 4; grid-template-columns: repeat(2, minmax(0, 1fr)); } }
</style>
