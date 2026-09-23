<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { Camera, Images, Loader2 } from "@lucide/vue";
import { createDeviceSnapshotSession, getDeviceSnapshotSession, type DeviceSnapshotSession } from "@/api/gb28181";
import { SNAPSHOT_LIBRARY_PATH } from "../snapshot-library/snapshotLibraryState";
import FactChannelPicker from "./FactChannelPicker.vue";
import type { FactChannelOption } from "./deviceFactChannel";

/**
 * 图像抓拍配置（GB/T 28181 SnapshotConfig）—— 2026-09-20 从播放控制台侧栏「高级」搬来。
 *
 * ⛔ 它不是一句"控制命令"，是一个**会话**：平台下发"张数 + 间隔"，设备按张把 JPEG
 *    上传回来，所以要轮询会话到 completed / failed，再把图片列出来。
 *    这与「设备控制」页那五个动作（发一条命令、等设备结论）是两种机制，故单独成面板。
 *
 * ⛔ 接口是**通道级**的（`/channel/:id/device-snapshots`），设备级入口必须先落到具体通道。
 * ⛔ 下发前要求目标通道**在线**：设备离线时配置下不去，直接禁用比让用户点了没反应好。
 */
const props = defineProps<{
  /** 下发目标通道；null 表示还没解析出通道。 */
  channelId: number | null;
  channelOptions: FactChannelOption[];
  /** 是否具备抓拍配置权限（gb28181:device:snapshot）。 */
  canSnapshot: boolean;
  channelsLoading?: boolean;
}>();

const emit = defineEmits<{ (event: "update:channelId", value: number | null): void }>();

const router = useRouter();

const SNAPSHOT_POLL_INTERVAL_MS = 800;

const snapshotCount = ref(1);
const snapshotInterval = ref(1);
const pending = ref(false);
const session = ref<DeviceSnapshotSession | null>(null);
const error = ref("");
let pollTimer: number | null = null;
let generation = 0;

function clearPolling(invalidate = true) {
  if (pollTimer !== null) window.clearTimeout(pollTimer);
  pollTimer = null;
  if (invalidate) generation += 1;
}

function isStale(channelId: number, currentGeneration: number) {
  return currentGeneration !== generation || props.channelId !== channelId;
}

function statusText() {
  if (error.value) return "下发失败";
  const current = session.value;
  if (!current) return "配置后由设备上传 JPEG";
  if (current.state === "completed") return `已完成 ${current.receivedCount}/${current.snapNum}`;
  if (current.state === "failed") return current.error || "抓拍失败";
  return `接收中 ${current.receivedCount}/${current.snapNum}`;
}

async function pollSession(channelId: number, sessionId: string, currentGeneration: number) {
  if (!props.canSnapshot) return;
  clearPolling(false);
  try {
    const response = await getDeviceSnapshotSession(channelId, sessionId);
    if (isStale(channelId, currentGeneration) || response.code !== 0 || !response.data) return;
    session.value = response.data;
    if (response.data.state === "completed" || response.data.state === "failed") return;
  } catch (pollError: any) {
    if (isStale(channelId, currentGeneration)) return;
    if (session.value) session.value = { ...session.value, error: pollError?.message || "抓拍状态读取失败" };
  }
  if (isStale(channelId, currentGeneration)) return;
  pollTimer = window.setTimeout(() => void pollSession(channelId, sessionId, currentGeneration), SNAPSHOT_POLL_INTERVAL_MS);
}

async function run() {
  const channelId = props.channelId;
  if (!canSend.value || !channelId) return;
  const currentGeneration = generation;
  pending.value = true;
  session.value = null;
  error.value = "";
  try {
    const response = await createDeviceSnapshotSession(channelId, {
      snapNum: Number(snapshotCount.value),
      interval: Number(snapshotInterval.value)
    });
    if (isStale(channelId, currentGeneration)) return;
    if (response.code !== 0 || !response.data) throw new Error(response.message || "下发图像抓拍配置失败");
    session.value = response.data;
    void pollSession(channelId, response.data.sessionId, currentGeneration);
  } catch (submitError: any) {
    if (isStale(channelId, currentGeneration)) return;
    error.value = submitError?.message || "下发图像抓拍配置失败";
  } finally {
    if (!isStale(channelId, currentGeneration)) pending.value = false;
  }
}

watch([() => props.channelId, () => props.canSnapshot], () => {
  clearPolling();
  session.value = null;
  error.value = "";
  pending.value = false;
});

onBeforeUnmount(() => clearPolling());

const selectedChannel = computed(() => props.channelOptions.find(option => option.value === props.channelId) ?? null);
const formValid = computed(
  () => Number(snapshotCount.value) >= 1 && Number(snapshotCount.value) <= 10 && Number(snapshotInterval.value) >= 1
);
const canSend = computed(
  () => props.canSnapshot && props.channelId !== null && selectedChannel.value?.online === true && formValid.value
);
const blockedReason = computed(() => {
  if (!props.canSnapshot) return "当前账号没有图像抓拍权限";
  if (props.channelOptions.length === 0) return "该设备下暂无通道，无法下发抓拍配置";
  if (props.channelId === null) return "请先选择一个目标通道";
  if (selectedChannel.value?.online !== true) return "目标通道离线，配置无法下发";
  if (!formValid.value) return "张数需为 1-10，间隔需为 ≥1 秒的整数";
  return "";
});

// 图像库菜单行是迁移写进 sys_menu 的，路由由菜单树动态生成 —— 后端没重启应用迁移之前，
// `/gb28181/snapshot-library` 这个路由**根本不存在**。
// ⛔ 那种情况下 push 会落到 404（白屏），比按钮置灰更让人费解，所以先探测再决定可用性。
const libraryReady = computed(() => router.resolve({ path: SNAPSHOT_LIBRARY_PATH }).matched.length > 0);

/**
 * 会话面板只装在内存里，页面一刷新就没了，而图片已经落库 ——
 * 图像库的 `sessionId` 筛选是事后把**同一次抓拍**重新聚起来的唯一线索，所以带进 URL。
 */
function openInLibrary() {
  const current = session.value;
  if (!current?.sessionId || !libraryReady.value) return;
  void router.push({
    path: SNAPSHOT_LIBRARY_PATH,
    query: {
      sessionId: current.sessionId,
      ...(current.channelCode ? { channelCode: current.channelCode } : {})
    }
  });
}
</script>

<template>
  <div class="snapshot-panel">
    <FactChannelPicker
      :options="props.channelOptions"
      :model-value="props.channelId"
      :loading="props.channelsLoading"
      @update:model-value="value => emit('update:channelId', value)"
    />

    <section class="fact-group">
      <header class="fact-group-head">
        <span class="fact-group-title"><Camera :size="14" />图像抓拍配置</span>
        <span class="fact-group-status">GBT28181 上报</span>
      </header>

      <div class="snapshot-fields">
        <label>
          张数
          <input v-model.number="snapshotCount" data-testid="snapshot-count" type="number" min="1" max="10" />
        </label>
        <label>
          间隔（秒）
          <input v-model.number="snapshotInterval" data-testid="snapshot-interval" type="number" min="1" max="3600" />
        </label>
      </div>

      <div class="snapshot-actions">
        <a-button size="small" type="primary" :loading="pending" :disabled="!canSend" data-testid="snapshot-submit" @click="run">
          <template #icon>
            <Loader2 v-if="pending" :size="13" class="spin" />
            <Camera v-else :size="13" />
          </template>
          下发抓拍配置
        </a-button>
        <span v-if="blockedReason" class="snapshot-hint" data-testid="snapshot-blocked">{{ blockedReason }}</span>
      </div>

      <p class="snapshot-status" aria-live="polite" data-testid="snapshot-status">{{ statusText() }}</p>
      <p v-if="error" class="snapshot-error" data-testid="snapshot-error">{{ error }}</p>

      <!-- 这一批图在库里的入口：会话本身刷新即丢，图片不会 —— 跳过去按会话看全量。 -->
      <div v-if="session?.sessionId" class="snapshot-library-link">
        <a-button size="mini" :disabled="!libraryReady" data-testid="snapshot-library-open" @click="openInLibrary">
          <template #icon><Images :size="12" /></template>
          在图像库中查看本次抓拍
        </a-button>
        <span v-if="!libraryReady" class="snapshot-hint" data-testid="snapshot-library-blocked">
          图像库菜单尚未启用（后端重启应用迁移后可用）
        </span>
      </div>

      <div v-if="session?.files.length" class="snapshot-results" data-testid="snapshot-results">
        <a v-for="file in session.files" :key="file.name" :href="file.url" target="_blank" rel="noopener noreferrer">
          <img :src="file.url" :alt="file.name" /><span>{{ file.name }}</span>
        </a>
      </div>
    </section>
  </div>
</template>

<style scoped>
.snapshot-panel {
  display: grid;
  gap: 12px;
}
.fact-group {
  display: grid;
  gap: 10px;
  padding: 12px 14px 14px;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 12px;
}
.fact-group-head {
  display: flex;
  gap: 10px;
  align-items: center;
  justify-content: space-between;
}
.fact-group-title {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  font-size: 12px;
  font-weight: 620;
  color: var(--uvp-text-secondary);
}
.fact-group-status {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.snapshot-fields {
  display: flex;
  flex-wrap: wrap;
  gap: 14px;
}
.snapshot-fields label {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  font-size: 12px;
  color: var(--uvp-text-secondary);
}
.snapshot-fields input {
  width: 84px;
  padding: 3px 6px;
  font-size: 12px;
  color: var(--uvp-text-primary);
  background: var(--uvp-shell-muted);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 6px;
}
.snapshot-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
}
.snapshot-hint {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.snapshot-status {
  margin: 0;
  font-size: 12px;
  color: var(--uvp-text-secondary);
}
.snapshot-error {
  margin: 0;
  font-size: 11.5px;
  line-height: 1.5;
  color: var(--uvp-danger);
  overflow-wrap: anywhere;
}
.snapshot-library-link {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
}
.snapshot-results {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.snapshot-results a {
  display: inline-flex;
  flex-direction: column;
  gap: 4px;
  width: 96px;
  font-size: 10.5px;
  color: var(--uvp-text-tertiary);
  overflow-wrap: anywhere;
  text-decoration: none;
}
.snapshot-results img {
  width: 96px;
  height: 64px;
  object-fit: cover;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 6px;
}
.spin {
  animation: snapshot-spin 0.9s linear infinite;
}

@keyframes snapshot-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
