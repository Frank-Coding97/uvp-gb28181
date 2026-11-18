<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { Share2 } from "@lucide/vue";
import {
  acquireChannelVideoShareLink,
  getChannelVideoShare,
  setChannelVideoShare,
  type ChannelVideoShareState
} from "@/api/gb28181";

const props = defineProps<{
  channelId: number;
  channelName: string;
  initialState?: ChannelVideoShareState;
  mode: "switch" | "action";
}>();
const emit = defineEmits<{ change: [state: ChannelVideoShareState] }>();
const state = ref<ChannelVideoShareState>({ enabled: false, pending: false, revision: 0 });
const busy = ref(true);
const blockedPageURL = ref("");
const expiresText = computed(() => {
  if (state.value.expired) return "链接已到期，已有观看可继续；关闭后重新开启可生成新链接";
  if (!state.value.expiresAt) return state.value.pending ? "取得分享页时开始计时" : "开启通道视频分享";
  return `链接有效期至 ${new Date(state.value.expiresAt).toLocaleString()}`;
});
let expiryTimer: ReturnType<typeof setTimeout> | undefined;
watch(state, current => {
  clearTimeout(expiryTimer);
  if (props.mode !== "switch" || !current.enabled || current.expired || !current.expiresAt) return;
  const now = current.serverTime ? Date.parse(current.serverTime) : Date.now();
  const delay = Math.max(0, Date.parse(current.expiresAt) - now);
  expiryTimer = setTimeout(() => void load(), Math.min(delay + 50, 2_147_483_647));
});
watch(
  () => props.initialState,
  value => {
    if (value) {
      applyState(value);
      busy.value = false;
    }
  },
  { immediate: true }
);

function applyState(value: ChannelVideoShareState) {
  if (value.revision !== state.value.revision || !value.enabled || value.expired) blockedPageURL.value = "";
  state.value = value;
  emit("change", value);
}

async function load() {
  try {
    const response = await getChannelVideoShare(props.channelId);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "读取分享状态失败");
    applyState(response.data);
  } catch (error: any) {
    Message.error(error?.message || "读取分享状态失败");
  } finally {
    busy.value = false;
  }
}

async function changeEnabled(enabled: boolean) {
  if (busy.value) return;
  const confirmed = await new Promise<boolean>(resolve => {
    Modal.confirm({
      title: enabled ? "开启视频分享" : "关闭视频分享",
      content: enabled
        ? "开启后立即开始计时，有效期采用统一全局配置，通道不可单独调整。"
        : "关闭后，已发出的链接、二维码和嵌入页面将失效，正在观看的会话也会停止。",
      okText: enabled ? "开启分享" : "关闭分享",
      cancelText: "取消",
      onOk: () => resolve(true),
      onCancel: () => resolve(false)
    });
  });
  if (!confirmed) return;
  busy.value = true;
  try {
    const response = await setChannelVideoShare(props.channelId, enabled, state.value.revision);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "分享状态保存失败");
    applyState(response.data);
    Message.success(enabled ? "已开启分享" : "已关闭分享");
  } catch (error: any) {
    Message.error(error?.message || "分享状态保存失败");
    await load();
  } finally {
    busy.value = false;
  }
}

async function openShare() {
  if (busy.value || !state.value.enabled) return;
  if (state.value.expired) {
    Message.warning("分享链接已到期，请关闭后重新开启分享以生成新链接；当前观看不受链接到期影响。");
    return;
  }
  const page = window.open("", "_blank");
  if (page) page.opener = null;
  busy.value = true;
  try {
    const response = await acquireChannelVideoShareLink(props.channelId, state.value.revision);
    if (response.code === 409 || response.code === 410) throw { response: { status: response.code } };
    const link = response.data;
    if (response.code !== 0 || !link?.publicId || !link.signature) throw new Error(response.message || "取得分享链接失败");
    const fragment = new URLSearchParams({ id: link.publicId, sig: link.signature });
    const sharePagePath = `${import.meta.env.BASE_URL.replace(/\/$/, "")}/share.html`;
    const url = new URL(`${sharePagePath}#${fragment.toString()}`, window.location.origin).toString();
    applyState(link);
    if (page) page.location.replace(url);
    else {
      blockedPageURL.value = url;
      Message.warning("浏览器拦截了新窗口，请点击旁边的“打开分享页”。");
    }
  } catch (error: any) {
    page?.close();
    if (error?.response?.status === 410) Message.warning("分享链接已到期，请关闭后重新开启分享以生成新链接。");
    else Message.error(error?.message || "取得分享链接失败");
    if (error?.response?.status === 409 || error?.response?.status === 410) {
      blockedPageURL.value = "";
      await load();
    }
  } finally {
    busy.value = false;
  }
}

function refreshWhenVisible() {
  if (props.mode === "switch" && document.visibilityState === "visible" && !busy.value) void load();
}
onMounted(() => {
  if (!props.initialState) void load();
  document.addEventListener("visibilitychange", refreshWhenVisible);
});
onUnmounted(() => {
  clearTimeout(expiryTimer);
  document.removeEventListener("visibilitychange", refreshWhenVisible);
});
</script>

<template>
  <div class="channel-video-share-control" :data-share-mode="mode" @click.stop @dblclick.stop>
    <a-tooltip v-if="mode === 'switch'" :content="expiresText" position="top">
      <a-switch
        :model-value="state.enabled"
        :loading="busy"
        size="small"
        aria-label="开启通道视频分享"
        @change="(value: boolean) => changeEnabled(value)"
      />
    </a-tooltip>
    <template v-else-if="state.enabled">
      <a-tooltip :content="state.expired ? expiresText : '分享页'" position="top">
        <button class="share-page-button" type="button" aria-label="打开通道分享页" :disabled="busy" @click="openShare">
          <Share2 :size="14" />
        </button>
      </a-tooltip>
      <a v-if="blockedPageURL" :href="blockedPageURL" target="_blank" rel="noopener noreferrer" aria-label="手动打开分享页"
        >打开分享页</a
      >
    </template>
  </div>
</template>

<style scoped>
.channel-video-share-control {
  display: inline-flex;
  gap: 6px;
  align-items: center;
}
.channel-video-share-control a {
  font-size: 12px;
  color: var(--uvp-brand);
}
.share-page-button {
  display: inline-grid;
  place-items: center;
  width: 26px;
  height: 26px;
  padding: 0;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: transparent;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 5px;
}
.share-page-button:hover:not(:disabled) {
  color: var(--uvp-brand);
  border-color: var(--uvp-brand);
}
.share-page-button:disabled {
  cursor: wait;
  opacity: 0.55;
}
</style>
