<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { fetchSipNetworkInterfaces, fetchSipSetupStatus } from "@/api/gb28181";
import { loadStandaloneSetupStatus } from "@/api/standalone-setup";
import { useUserStoreHook } from "@/store/modules/user";
import { useSipSetupStore } from "@/store/modules/sip-setup";
import { hasSipStatusPermission, hasSipUpdatePermission } from "./sipSetupHostRules";
import { sipAddressAvailability } from "@/views/gb28181/sip/sipSetupRules";
import SipSetupModal from "./SipSetupModal.vue";

const props = defineProps<{
    // 允许 test 注入自定义 loader.测试用不动 store.
    statusLoader?: typeof fetchSipSetupStatus;
    userId?: number;
    permissions?: string[];
}>();

const store = useSipSetupStore();
let checkedSessionKey = "";
const account = useUserStoreHook().account;
const currentUserId = computed(() => props.userId ?? account.id);
const currentPermissions = computed(() => props.permissions ?? account.permissions);
const canViewStatus = computed(() => hasSipStatusPermission(currentPermissions.value));
const canUpdateConfig = computed(() => hasSipUpdatePermission(currentPermissions.value));
const standaloneSipRequired = ref(false);
const standaloneMode = ref(false);
let requestVersion = 0;

const permissionKey = (permissions: string[]) => permissions.slice().sort().join("|");

function refreshAfterSave() {
    if (!hasSipStatusPermission(currentPermissions.value)) {
        store.reset();
        return;
    }
    void store.refresh();
}

// 登录后拉一次 status,决定是否自动弹 Modal.
// 拉不到时:store.loadFailed=true → shouldAutoOpen 返 false → 不阻塞用户进入系统.
watch(
    () => [currentUserId.value, permissionKey(currentPermissions.value)] as const,
    async ([userId, permissionsKey]) => {
        const sessionKey = `${userId}:${permissionsKey}`;
        if (checkedSessionKey === sessionKey) return;
        checkedSessionKey = sessionKey;
        const version = ++requestVersion;
        const permissions = currentPermissions.value.slice();
        store.reset();
        standaloneSipRequired.value = false;
        standaloneMode.value = false;
        if (!userId || !hasSipStatusPermission(permissions)) {
            return;
        }
        const isCurrentSession = () =>
            version === requestVersion &&
            currentUserId.value === userId &&
            permissionKey(currentPermissions.value) === permissionsKey &&
            hasSipStatusPermission(currentPermissions.value);
        try {
            if (props.statusLoader) {
                const response = await props.statusLoader();
                if (!isCurrentSession()) return;
                if (response.code === 0) {
                    store.$patch({ status: response.data, loadFailed: false });
                } else {
                    store.$patch({ loadFailed: true });
                }
            } else {
                await store.refresh();
            }
            if (!isCurrentSession()) {
                if (!hasSipStatusPermission(currentPermissions.value)) store.reset();
                return;
            }
            const standaloneProbe = await loadStandaloneSetupStatus();
            if (!isCurrentSession()) return;
            standaloneMode.value = standaloneProbe.kind === "standalone";
            standaloneSipRequired.value =
                standaloneProbe.kind === "standalone" && standaloneProbe.status.phase === "pending_sip";
            let standaloneAddressChanged = false;
            if (standaloneProbe.kind === "standalone" && store.status?.config && hasSipUpdatePermission(permissions)) {
                try {
                    const networkResponse = await fetchSipNetworkInterfaces();
                    if (isCurrentSession() && networkResponse.code === 0) {
                        standaloneAddressChanged =
                            sipAddressAvailability(
                                store.status.config.deploymentMode,
                                store.status.config.listenIp,
                                store.status.config.advertiseIp,
                                networkResponse.data
                            ) === "missing";
                    }
                } catch {
                    // A failed interface scan cannot prove that a saved address disappeared.
                }
            }
            if (
                (standaloneAddressChanged || (standaloneSipRequired.value && store.needsAttention)) ||
                store.shouldAutoOpen(currentPermissions.value)
            ) {
                store.openModal();
            }
        } catch (error: any) {
            if (!isCurrentSession()) {
                if (!hasSipStatusPermission(currentPermissions.value)) store.reset();
                return;
            }
            Message.warning(error?.message || "SIP 配置状态暂时不可用");
        }
    },
    { immediate: true }
);
</script>

<template>
    <SipSetupModal
        v-if="canViewStatus"
        :visible="canUpdateConfig && store.modalOpen"
        :required="standaloneSipRequired"
        :standalone="standaloneMode"
        @close="store.closeModal({ suppressThisSession: true })"
        @saved="refreshAfterSave"
    />
</template>
