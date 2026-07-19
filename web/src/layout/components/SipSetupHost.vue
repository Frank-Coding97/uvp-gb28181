<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { fetchSipSetupStatus } from "@/api/gb28181";
import { useUserStoreHook } from "@/store/modules/user";
import SipSetupWizard from "@/views/gb28181/sip/SipSetupWizard.vue";
import { hasSipUpdatePermission, shouldOpenSipSetup } from "./sipSetupHostRules";

const props = defineProps<{
    statusLoader?: typeof fetchSipSetupStatus;
    userId?: number;
    permissions?: string[];
}>();

const checkedSessionKeys = new Set<string>();
const visible = ref(false);
const account = useUserStoreHook().account;
const currentUserId = computed(() => props.userId ?? account.id);
const currentPermissions = computed(() => props.permissions ?? account.permissions);

watch(
    () => [currentUserId.value, currentPermissions.value.slice().sort().join("|")] as const,
    async ([userId, permissionKey]) => {
        if (!userId || !hasSipUpdatePermission(currentPermissions.value)) return;
        const sessionKey = `${userId}:${permissionKey}`;
        if (checkedSessionKeys.has(sessionKey)) return;
        checkedSessionKeys.add(sessionKey);
        try {
            const response = await (props.statusLoader || fetchSipSetupStatus)();
            if (response.code !== 0) throw new Error(response.message || "读取 SIP 配置状态失败");
            visible.value = shouldOpenSipSetup(response.data, currentPermissions.value);
        } catch (error: any) {
            Message.warning(error?.message || "SIP 配置状态暂时不可用");
        }
    },
    { immediate: true }
);
</script>

<template>
    <SipSetupWizard v-model="visible" @saved="visible = false" @skipped="visible = false" />
</template>
