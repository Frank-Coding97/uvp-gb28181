<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { Copy, ExternalLink, Share2 } from "@lucide/vue";
import SQRCodeDraw from "@/components/s-qrcode-draw/index.vue";
import { escapeHTMLAttribute } from "@/share/shareSession";
import {
  acquireChannelVideoShareLink,
  getChannelVideoShare,
  regenerateChannelVideoShare,
  setChannelVideoShare,
  type ChannelVideoShareLink,
  type ChannelVideoShareState
} from "@/api/gb28181";

const props = defineProps<{ channelId: number; channelName: string; initialState?: ChannelVideoShareState }>();
const emit = defineEmits<{ change: [state: ChannelVideoShareState] }>();
const state = ref<ChannelVideoShareState>({ enabled: false, pending: false, revision: 0 });
const busy = ref(true);
const dialogVisible = ref(false);
const shareUrl = ref("");
const iframeCode = computed(() => {
  if (!shareUrl.value) return "";
  const url = new URL(shareUrl.value);
  const fragment = new URLSearchParams(url.hash.slice(1));
  fragment.set("embed", "1");
  url.hash = fragment.toString();
  return `<iframe src="${escapeHTMLAttribute(url.toString())}" title="${escapeHTMLAttribute(props.channelName)}" allow="autoplay; fullscreen" allowfullscreen></iframe>`;
});
const expiresText = computed(() => {
  if (!state.value.expiresAt) return state.value.pending ? "取得分享页时开始计时" : "未开启";
  return `有效期至 ${new Date(state.value.expiresAt).toLocaleString()}`;
});
let expiryTimer: ReturnType<typeof setTimeout> | undefined;
watch(state, current => {
  clearTimeout(expiryTimer);
  if (!current.enabled || !current.expiresAt) return;
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
  if (value.revision !== state.value.revision || !value.enabled) {
    shareUrl.value = "";
    dialogVisible.value = false;
  }
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
  if (enabled || state.value.enabled) {
    const confirmed = await new Promise<boolean>(resolve => {
      Modal.confirm({
        title: enabled ? "开启视频分享" : "关闭视频分享",
        content: enabled
          ? "开启后立即开始计时，有效期采用统一全局配置，通道不可单独调整。"
          : "关闭后，已发出的链接、二维码和嵌入页面将失效。",
        okText: enabled ? "开启分享" : "关闭分享",
        cancelText: "取消",
        onOk: () => resolve(true),
        onCancel: () => resolve(false)
      });
    });
    if (!confirmed) return;
  }
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
  busy.value = true;
  try {
    const response = await acquireChannelVideoShareLink(props.channelId, state.value.revision);
    if (response.code === 409) throw { response: { status: 409 } };
    const link = response.data;
    if (response.code !== 0 || !link?.publicId || !link.signature) throw new Error(response.message || "取得分享链接失败");
    updateShareMaterial(link);
    dialogVisible.value = true;
  } catch (error: any) {
    Message.error(error?.message || "取得分享链接失败");
    if (error?.response?.status === 409) {
      shareUrl.value = "";
      dialogVisible.value = false;
      await load();
    }
  } finally {
    busy.value = false;
  }
}

function updateShareMaterial(link: ChannelVideoShareLink) {
  if (!link.publicId || !link.signature) throw new Error("取得分享链接失败");
  const fragment = new URLSearchParams({ id: link.publicId, sig: link.signature });
  const sharePagePath = `${import.meta.env.BASE_URL.replace(/\/$/, "")}/share.html`;
  const url = new URL(`${sharePagePath}#${fragment.toString()}`, window.location.origin).toString();
  applyState(link);
  shareUrl.value = url;
  dialogVisible.value = true;
}

async function regenerateShare() {
  if (busy.value) return;
  const confirmed = await new Promise<boolean>(resolve => {
    Modal.confirm({
      title: "重新生成分享链接",
      content: "重新生成后，旧链接、二维码和 iframe 嵌入页面将永久失效。新链接按全局配置重新计时。",
      okText: "重新生成",
      cancelText: "取消",
      onOk: () => resolve(true),
      onCancel: () => resolve(false)
    });
  });
  if (!confirmed) return;
  busy.value = true;
  try {
    const response = await regenerateChannelVideoShare(props.channelId, state.value.revision);
    if (response.code === 409) throw { response: { status: 409 } };
    if (response.code !== 0 || !response.data) throw new Error(response.message || "重新生成分享链接失败");
    updateShareMaterial(response.data);
    Message.success("已重新生成分享链接");
  } catch (error: any) {
    if (error?.response?.status === 409) {
      dialogVisible.value = false;
      shareUrl.value = "";
      Message.error("分享状态已更新，请重新打开分享窗口");
      await load();
    } else {
      Message.error(error?.message || "重新生成分享链接失败");
    }
  } finally {
    busy.value = false;
  }
}

async function copy(value: string) {
  try {
    await navigator.clipboard.writeText(value);
    Message.success("已复制");
  } catch {
    Message.error("复制失败，请检查浏览器剪贴板权限");
  }
}

function openPage() {
  if (shareUrl.value) window.open(shareUrl.value, "_blank", "noopener,noreferrer");
}

function refreshWhenVisible() {
  if (document.visibilityState === "visible" && !busy.value) void load();
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
  <div class="channel-video-share-control" @click.stop>
    <a-tooltip content="开启通道视频分享" position="top">
      <a-switch
        :model-value="state.enabled"
        :loading="busy"
        size="small"
        aria-label="开启通道视频分享"
        @change="(value: boolean) => changeEnabled(value)"
      />
    </a-tooltip>
    <a-tooltip v-if="state.enabled" content="分享页" position="top">
      <button class="share-page-button" type="button" aria-label="打开通道分享页" :disabled="busy" @click="openShare">
        <Share2 :size="14" />
      </button>
    </a-tooltip>
    <a-modal v-model:visible="dialogVisible" title="通道分享" :footer="false" :width="520">
      <div class="share-dialog">
        <div class="share-dialog__heading">
          <strong>{{ channelName }}</strong>
          <span>{{ expiresText }}</span>
        </div>
        <div class="share-dialog__url">
          <code>{{ shareUrl }}</code>
          <a-button size="small" aria-label="复制分享链接" @click="copy(shareUrl)"><Copy :size="14" /></a-button>
        </div>
        <div class="share-dialog__embed">
          <div class="share-dialog__label">iframe 嵌入代码</div>
          <pre>{{ iframeCode }}</pre>
          <a-button size="small" @click="copy(iframeCode)"
            ><template #icon><Copy :size="14" /></template>复制代码</a-button
          >
        </div>
        <div v-if="dialogVisible" class="share-dialog__qr">
          <SQRCodeDraw :text="shareUrl" :options="{ width: 168, margin: 1 }" />
          <span>扫码观看实时画面</span>
        </div>
        <a-button type="primary" @click="openPage">
          <template #icon><ExternalLink :size="14" /></template>打开分享页
        </a-button>
        <a-button aria-label="重新生成分享链接" :loading="busy" :disabled="busy" @click="regenerateShare">重新生成</a-button>
      </div>
    </a-modal>
  </div>
</template>

<style scoped>
.channel-video-share-control {
  display: inline-flex;
  gap: 6px;
  align-items: center;
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
.share-dialog {
  display: grid;
  gap: 16px;
}
.share-dialog__heading,
.share-dialog__qr {
  display: grid;
  gap: 5px;
  justify-items: center;
}
.share-dialog__heading span,
.share-dialog__qr span {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.share-dialog__url {
  display: flex;
  gap: 8px;
  align-items: center;
  min-width: 0;
}
.share-dialog__url code {
  flex: 1;
  min-width: 0;
  padding: 8px;
  overflow-wrap: anywhere;
  background: var(--uvp-page-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 5px;
}
.share-dialog__embed pre {
  max-height: 80px;
  padding: 8px;
  overflow: auto;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
  background: var(--uvp-page-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 5px;
}
.share-dialog__label {
  margin-bottom: 6px;
  font-size: 12px;
  font-weight: 600;
}
</style>
