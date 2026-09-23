<script setup lang="ts">
import { computed } from "vue";
import { Bell } from "lucide-vue-next";
import { useSipSetupStore } from "@/store/modules/sip-setup";
import { useUserStoreHook } from "@/store/modules/user";
import { hasSipUpdatePermission } from "@/layout/components/sipSetupHostRules";

// SIP 未配置/启动失败时,在 Header 显示铃铛 + 红点.
// 点击 → 打开 SetupHost 的 Modal(通过 store 唤起).
// 没权限的用户不显示 —— 他们看到也无法处理.
const store = useSipSetupStore();
const permissions = computed(() => useUserStoreHook().account.permissions);

const visible = computed(() => hasSipUpdatePermission(permissions.value) && store.needsAttention);

const tooltip = computed(() => {
    const state = store.status?.runtime?.state;
    if (state === "failed") return "SIP 服务启动失败,点击查看和修改配置";
    return "SIP 尚未配置,点击进入引导";
});

function openSetup() {
    store.openModal();
}
</script>

<template>
    <a-tooltip v-if="visible" :content="tooltip" position="bottom">
        <a-button
            size="mini"
            type="text"
            class="icon_btn sip-setup-bell"
            aria-label="SIP 配置提醒"
            @click="openSetup"
        >
            <template #icon>
                <Bell :size="18" />
            </template>
        </a-button>
    </a-tooltip>
</template>

<style scoped>
.sip-setup-bell {
    position: relative;
    color: var(--uvp-danger, #d14343);
}

.sip-setup-bell::before {
    position: absolute;
    top: -2px;
    right: -2px;
    width: 8px;
    height: 8px;
    content: "";
    background: var(--uvp-danger, #d14343);
    border: 2px solid var(--uvp-workspace-bg, #ffffff);
    border-radius: 50%;
    animation: sip-bell-pulse 2s infinite;
}

@keyframes sip-bell-pulse {
    0%,
    100% {
        transform: scale(1);
        opacity: 1;
    }

    50% {
        transform: scale(1.15);
        opacity: 0.75;
    }
}

.sip-setup-bell:hover {
    color: var(--uvp-danger, #d14343);
    background: var(--uvp-danger-soft, #fff2f2);
}
</style>
