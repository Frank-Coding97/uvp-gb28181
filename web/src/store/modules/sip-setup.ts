// SIP 引导状态全局 store.
// Header 铃铛 + Modal 需要共享"是否未配置""是否手动打开引导"两个信号,不适合分散在组件 local state.
import { defineStore } from "pinia";
import { ref, computed } from "vue";
import { fetchSipSetupStatus, type SipSetupStatus, type SipRuntimeState } from "@/api/gb28181";
import { hasSipUpdatePermission } from "@/layout/components/sipSetupHostRules";

export const useSipSetupStore = defineStore("sip-setup", () => {
    const status = ref<SipSetupStatus | null>(null);
    const loadFailed = ref(false);
    // Modal 显隐由外部主动控制:自动弹出(未配置 + 权限) OR 用户点铃铛手动打开.
    // 用户点"稍后再配"会关掉 Modal,但 status 里的 unconfigured 依然让 header 红点持续显示.
    const modalOpen = ref(false);
    const suppressedThisSession = ref(false);

    async function refresh(): Promise<void> {
        try {
            const response = await fetchSipSetupStatus();
            if (response.code === 0) {
                status.value = response.data;
                loadFailed.value = false;
            } else {
                loadFailed.value = true;
            }
        } catch {
            loadFailed.value = true;
        }
    }

    // 是否需要在 Header 显示红点 —— 只要状态告诉我们 SIP 未配置就显示,不管用户是否 skip 过.
    // 未配置 = runtime.state === "unconfigured";已配置但启动失败(failed) 也算需要提醒,让用户能点开修.
    const needsAttention = computed(() => {
        if (loadFailed.value || !status.value) return false;
        const state: SipRuntimeState = status.value.runtime?.state;
        return state === "unconfigured" || state === "failed";
    });

    // 登录后是否需要自动弹 Modal —— 未配置 + 有权限 + 本 session 未主动关闭过.
    // API 拉不到 status 时,不阻塞用户进入系统(loadFailed=true 时返回 false).
    function shouldAutoOpen(permissions: string[]): boolean {
        if (loadFailed.value || !status.value) return false;
        if (!hasSipUpdatePermission(permissions)) return false;
        if (suppressedThisSession.value) return false;
        return needsAttention.value;
    }

    function openModal() {
        modalOpen.value = true;
    }

    function closeModal(options: { suppressThisSession?: boolean } = {}) {
        modalOpen.value = false;
        if (options.suppressThisSession) {
            suppressedThisSession.value = true;
        }
    }

    // 保存成功后:更新 status,Modal 关闭(不再需要提醒).
    function applySaved(next: Partial<SipSetupStatus>) {
        if (status.value) {
            status.value = { ...status.value, ...next };
        }
        modalOpen.value = false;
    }

    // 用于退出登录时重置 —— 避免下个用户复用上个用户的 suppressed 标记.
    function reset() {
        status.value = null;
        loadFailed.value = false;
        modalOpen.value = false;
        suppressedThisSession.value = false;
    }

    return {
        status,
        loadFailed,
        modalOpen,
        needsAttention,
        shouldAutoOpen,
        refresh,
        openModal,
        closeModal,
        applySaved,
        reset
    };
});
