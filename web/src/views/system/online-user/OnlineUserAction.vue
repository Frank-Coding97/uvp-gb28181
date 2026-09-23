<script setup lang="ts">
import { ref } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { LogOut } from "lucide-vue-next";
import { forceLogoutOnlineSessionAPI, type OnlineUserSession } from "@/api/online-user";

const props = defineProps<{
  session: OnlineUserSession;
  currentSid: string;
  canForce: boolean;
}>();
const emit = defineEmits<{ success: [sid: string] }>();

const loading = ref(false);
const error = ref("");

function errorMessage(cause: unknown) {
  if (cause instanceof Error) return cause.message;
  if (typeof cause === "string") return cause;
  if (cause && typeof cause === "object" && "message" in cause) return String(cause.message);
  return "强制下线失败";
}

function confirmForceLogout() {
  if (props.session.sid === props.currentSid || loading.value) return;
  error.value = "";
  Modal.confirm({
    title: "强制下线",
    content: `确认强制下线 ${props.session.username} 在 ${props.session.clientIp} 的会话？`,
    okText: "强退",
    cancelText: "取消",
    okButtonProps: { status: "danger" },
    async onOk() {
      loading.value = true;
      try {
        const result = await forceLogoutOnlineSessionAPI(props.session.sid);
        if (result.code !== 0) throw new Error(result.message || "强制下线失败");
        Message.success("强制下线成功");
        emit("success", props.session.sid);
      } catch (cause: unknown) {
        error.value = errorMessage(cause);
        Message.error(error.value);
        throw cause;
      } finally {
        loading.value = false;
      }
    }
  });
}
</script>

<template>
  <div v-if="canForce" class="online-user-action">
    <a-tooltip :content="session.sid === currentSid ? '当前会话请使用退出登录' : '强制下线此会话'" position="top">
      <a-button
        type="text"
        status="danger"
        :disabled="session.sid === currentSid"
        :loading="loading"
        :aria-label="session.sid === currentSid ? '当前会话不可强退' : `强制下线 ${session.username}`"
        @click="confirmForceLogout"
      >
        <template #icon><LogOut :size="15" /></template>
        强退
      </a-button>
    </a-tooltip>
    <span v-if="error" class="online-user-action__error" role="alert">{{ error }}</span>
  </div>
</template>

<style scoped>
.online-user-action {
  display: inline-flex;
  min-width: 0;
  flex-direction: column;
  align-items: center;
}

.online-user-action :deep(.arco-btn) {
  min-width: 72px;
  min-height: 44px;
}

.online-user-action :deep(.arco-btn:focus-visible) {
  outline: 2px solid rgb(var(--primary-6));
  outline-offset: 2px;
}

.online-user-action__error {
  max-width: 128px;
  color: rgb(var(--danger-6));
  font-size: 12px;
  line-height: 1.4;
  overflow-wrap: anywhere;
}

@media (prefers-reduced-motion: reduce) {
  .online-user-action,
  .online-user-action * {
    animation-duration: 0.01ms !important;
    transition-duration: 0.01ms !important;
  }
}
</style>
