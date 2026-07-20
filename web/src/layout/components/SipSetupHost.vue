<script setup lang="ts">
import { computed, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { fetchSipSetupStatus } from "@/api/gb28181";
import { useUserStoreHook } from "@/store/modules/user";
import { useSipSetupStore } from "@/store/modules/sip-setup";
import SipSetupModal from "./SipSetupModal.vue";

const props = defineProps<{
    // 允许 test 注入自定义 loader.测试用不动 store.
    statusLoader?: typeof fetchSipSetupStatus;
    userId?: number;
    permissions?: string[];
}>();

const store = useSipSetupStore();
const checkedSessionKeys = new Set<string>();
const account = useUserStoreHook().account;
const currentUserId = computed(() => props.userId ?? account.id);
const currentPermissions = computed(() => props.permissions ?? account.permissions);

// 登录后拉一次 status,决定是否自动弹 Modal.
// 拉不到时:store.loadFailed=true → shouldAutoOpen 返 false → 不阻塞用户进入系统.
watch(
    () => [currentUserId.value, currentPermissions.value.slice().sort().join("|")] as const,
    async ([userId, permissionKey]) => {
        if (!userId) return;
        const sessionKey = `${userId}:${permissionKey}`;
        if (checkedSessionKeys.has(sessionKey)) return;
        checkedSessionKeys.add(sessionKey);
        try {
            if (props.statusLoader) {
                const response = await props.statusLoader();
                if (response.code === 0) {
                    store.$patch({ status: response.data, loadFailed: false });
                } else {
                    store.$patch({ loadFailed: true });
                }
            } else {
                await store.refresh();
            }
            if (store.shouldAutoOpen(currentPermissions.value)) {
                store.openModal();
            }
        } catch (error: any) {
            Message.warning(error?.message || "SIP 配置状态暂时不可用");
        }
    },
    { immediate: true }
);
</script>

<template>
    <SipSetupModal
        :visible="store.modalOpen"
        @close="store.closeModal({ suppressThisSession: true })"
        @saved="() => store.refresh()"
    />
</template>
