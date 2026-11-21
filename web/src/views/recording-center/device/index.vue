<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import dayjs from "dayjs";
import { ChevronLeft, ChevronRight, RefreshCw } from "@lucide/vue";
import { Message } from "@arco-design/web-vue";
import ChannelSourceTree from "../../gb28181/components/channel-tree/ChannelSourceTree.vue";
import { getRecordQueryOptions, type ChannelVO, type RecordQueryOptions } from "../../gb28181/device-mgmt/api";
import { readableHttpError, recordQueryTypeText } from "../../gb28181/device-mgmt/recordQueryState";
import RecordCacheProgressDialog from "../../gb28181/device-record-playback/RecordCacheProgressDialog.vue";
import { createRecordCacheTask } from "@/api/recordCache";
import { hasPermission } from "@/hooks/usePermission";
import { useDeviceStatusLabel } from "../../gb28181/useDeviceStatusDict";
import PlaybackStage from "./PlaybackStage.vue";
import PlaybackControls from "./PlaybackControls.vue";
import DayTimeline from "./DayTimeline.vue";
import SegmentDrawer from "./SegmentDrawer.vue";
import { useDayRecords } from "./useDayRecords";
import { usePlaybackSession } from "./usePlaybackSession";
import { DAY_FORMAT, dayRange, parseDateParam, parseTimeParam, resolveTimeOnDate, shiftDate } from "./urlState";

const route = useRoute();
const router = useRouter();
const deviceStatusLabel = useDeviceStatusLabel();

const canQuery = computed(() => hasPermission("gb28181:device-record:query"));
const canPlay = computed(() => hasPermission("gb28181:device-record:play"));
const canDownload = computed(() => hasPermission("gb28181:device-record:download"));

const channelId = ref(0);
const channel = ref<ChannelVO | null>(null);
const options = ref<RecordQueryOptions | null>(null);
const optionsLoading = ref(false);
const optionsError = ref("");
const date = ref(parseDateParam(route.query.date, dayjs().format(DAY_FORMAT)));
/**
 * URL 里显式带了日期才认它。
 *
 * 没带的时候不能直接用浏览器的「今天」—— 现场机器和服务器跨时区时，两边可能差一天，
 * 用户点进来会看到「今天没有录像」。正确做法是等查询配置回来，用后端 `serverNow`
 * 所在的那一天覆盖。
 */
const dateFromUrl = Boolean(String(route.query.date || "").trim());
const drawerOpen = ref(false);
const downloadPending = ref(false);
const cacheDialogVisible = ref(false);
const cacheTaskId = ref("");
const cacheTaskName = ref("");
const stageRef = ref<HTMLElement | null>(null);

const dayRecords = useDayRecords({ channelId, canQuery });
const records = dayRecords.records;
const playback = usePlaybackSession({ channelId, records, canPlay });

/**
 * 通道号的三种来源，优先级从高到低：
 * 1. 路径参数 —— 旧隐藏路由 /gb28181/device-record-playback/:channelId，设备列表点「录像」走的它；
 * 2. query.channel —— 侧边栏点「设备录像」后再选通道时写进去的，刷新页面靠它恢复；
 * 3. 都没有 —— 空态，让用户从左侧树里挑。
 *
 * ⛔ 菜单本身的 path 不能带 :channelId：侧边栏是拿菜单 path 原样 router.push 的，
 *    带参路径点下去会把 ":channelId" 当成真实通道号。
 */
const paramChannelId = computed(() => Number(String(route.params.channelId || "").trim()) || 0);
const queryChannelId = computed(() => Number(String(route.query.channel || "").trim()) || 0);
const initialChannelId = computed(() => paramChannelId.value || queryChannelId.value);

/** 深链：URL 里已经指定了通道（从设备列表点「录像」进来）。 */
const deepLinked = computed(() => initialChannelId.value > 0);

const deviceOnline = computed(() => options.value?.device.online !== false);
const queryLoading = computed(() => dayRecords.state.value === "querying");
const hasRecords = computed(() => records.value.length > 0);

const currentIndex = computed(() => records.value.findIndex(item => item.recordKey === playback.playback.value.recordKey));
const hasPrev = computed(() => currentIndex.value > 0);
const hasNext = computed(() => currentIndex.value >= 0 && currentIndex.value < records.value.length - 1);

const osdTitle = computed(() => {
  const name = options.value?.channel.name || channel.value?.name || "";
  const record = playback.selectedRecord.value;
  return [name, record ? recordQueryTypeText(record.type) : ""].filter(Boolean).join(" · ");
});
const osdMeta = computed(() => {
  const scale = playback.playback.value.scale || 1;
  return `${scale}x · ${deviceStatusLabel(deviceOnline.value)}`;
});

/**
 * 空态由「为什么看不到画面」决定，顺序不能乱：
 * 没权限 → 没选通道 → 设备离线 → 查询失败 → 当天无录像 → 已选段待播。
 */
const emptyTitle = computed(() => {
  if (!channelId.value) return "从左侧选择一个通道";
  if (!deviceOnline.value) return "设备离线";
  if (dayRecords.state.value === "empty") return "这一天没有录像";
  if (["timeout", "offline", "error"].includes(dayRecords.state.value)) return "没有查到录像";
  if (hasRecords.value) return "选择一个时段开始播放";
  return "这一天没有录像";
});
const emptyDescription = computed(() => {
  if (!channelId.value) return "选好通道后会自动查当天的录像目录";
  if (!deviceOnline.value) return "设备离线时无法向它查询录像目录";
  const message = dayRecords.message.value;
  if (message) return message;
  if (dayRecords.state.value === "empty") return "检查设备的录像计划、存储卡或硬盘状态";
  return "";
});
const emptyAction = computed(() => (!channelId.value ? "" : !deviceOnline.value ? "去云端录像看看" : ""));

async function loadOptions(targetChannelId: number) {
  optionsError.value = "";
  optionsLoading.value = true;
  try {
    const response = await getRecordQueryOptions(targetChannelId);
    options.value = response.data;
    if (!dateFromUrl) {
      const serverDate = response.data.serverNow ? dayjs(response.data.serverNow).format(DAY_FORMAT) : date.value;
      if (serverDate !== date.value) date.value = serverDate;
    }
    await dayRecords.load(date.value, response.data);
    await autoStartFromUrl();
  } catch (error) {
    optionsError.value = readableHttpError(error, "无法加载该通道的录像查询配置");
    options.value = null;
  } finally {
    optionsLoading.value = false;
  }
}

/** 深链带时间点就从那一刻播；只带通道（从设备列表进来）就播当天最后一段。 */
async function autoStartFromUrl() {
  if (!canPlay.value || !hasRecords.value) return;
  const timeParam = parseTimeParam(route.query.t);
  if (timeParam) {
    await playback.seekTo(resolveTimeOnDate(date.value, timeParam));
    return;
  }
  if (!deepLinked.value) return;
  const last = records.value[records.value.length - 1];
  if (last?.startTime) await playback.seekTo(last.startTime);
}

async function selectChannel(next: ChannelVO) {
  if (!canQuery.value || next.id === channelId.value) return;
  await playback.stop(false, false).catch(() => undefined);
  dayRecords.clear();
  channel.value = next;
  channelId.value = next.id;
  options.value = null;
  await loadOptions(next.id);
  syncUrl();
}

async function reloadDay(force = false) {
  if (!options.value || !channelId.value) return;
  await dayRecords.load(date.value, options.value, { force });
}

async function changeDate(next: string) {
  if (next === date.value) return;
  await playback.stop(false, false).catch(() => undefined);
  date.value = next;
  syncUrl();
  await reloadDay();
}

function stepDate(days: number) {
  void changeDate(shiftDate(date.value, days));
}

function goToday() {
  const today = options.value?.serverNow ? dayjs(options.value.serverNow).format(DAY_FORMAT) : dayjs().format(DAY_FORMAT);
  void changeDate(today);
}

/**
 * 回写地址栏：日期 + 通道号。
 *
 * 走的若是带参路由（旧隐藏路由），通道号同时替换进路径，否则路径和 query 会互相打脸；
 * 不带参的路由只维护 ?channel=，这样同一条路由记录不换、组件不会被重建。
 */
function syncUrl() {
  const query = { ...route.query, date: date.value } as Record<string, string>;
  if (channelId.value) query.channel = String(channelId.value);
  if (paramChannelId.value && channelId.value) {
    void router.replace({ name: route.name as string, params: { channelId: String(channelId.value) }, query });
    return;
  }
  void router.replace({ path: route.path, query });
}

async function handleTimelineSeek(time: string) {
  const started = await playback.seekTo(time);
  if (!started) Message.info("这个时刻没有录像，请点时间轴上有颜色的时段");
}

async function handleSegmentSelect(record: (typeof records.value)[number]) {
  const startTime = record.startTime;
  if (!startTime) return;
  await playback.seekTo(startTime);
}

async function handleDownload(record = playback.selectedRecord.value) {
  if (!canDownload.value || !record || !record.startTime || downloadPending.value) return;
  downloadPending.value = true;
  try {
    const response = await createRecordCacheTask({
      channelId: channelId.value,
      recordKey: record.recordKey,
      playFrom: record.startTime
    });
    const created = response?.data;
    if (created?.taskId) {
      cacheTaskId.value = created.taskId;
      cacheTaskName.value = record.name || "未命名录像";
      cacheDialogVisible.value = true;
      Message.success("已开始归档到服务器，可在「录像缓存」查看进度");
    }
  } catch (error) {
    Message.error(readableHttpError(error, "归档失败，请稍后重试"));
  } finally {
    downloadPending.value = false;
  }
}

function goRecordCache() {
  void router.push("/gb28181/record-cache");
}

function goCloudRecordings() {
  const query: Record<string, string> = { date: date.value };
  if (options.value?.channel.code) query.keyword = options.value.channel.code;
  void router.push({ path: "/gb28181/cloud-recordings", query });
}

async function toggleFullscreen() {
  if (!stageRef.value) return;
  if (document.fullscreenElement) await document.exitFullscreen();
  else await stageRef.value.requestFullscreen?.();
}

onMounted(async () => {
  if (!canQuery.value) return;
  const initial = initialChannelId.value;
  if (initial > 0) {
    channelId.value = initial;
    await loadOptions(initial);
  }
});
</script>

<template>
  <div class="snow-fill device-record-page">
    <div v-if="!canQuery" class="permission-state" data-testid="device-record-permission-denied">
      无权查询设备录像，请联系管理员分配「设备录像查询」权限。
    </div>

    <div v-else class="snow-fill-inner uvp-page-shell-flat device-record-shell">
      <aside class="channel-panel" aria-label="通道选择">
        <ChannelSourceTree
          :selected-channel-id="channelId || null"
          @select="selectChannel"
          @select-group="() => undefined"
          @favorite-saved="() => undefined"
        />
      </aside>

      <main class="workspace">
        <header class="workspace-bar" data-testid="device-record-bar">
          <div class="channel-context">
            <strong>{{ options?.channel.name || channel?.name || "未选择通道" }}</strong>
            <span v-if="options?.channel.code || channel?.channelId" class="channel-code">
              {{ options?.channel.code || channel?.channelId }}
            </span>
            <span v-if="options?.device" class="channel-device">{{ options.device.name }}</span>
            <i v-if="channelId" class="channel-status" :class="{ offline: !deviceOnline }">
              {{ deviceStatusLabel(deviceOnline) }}
            </i>
          </div>

          <div class="date-nav">
            <a-button type="secondary" :disabled="!channelId" aria-label="前一天" title="前一天" @click="stepDate(-1)">
              <template #icon><ChevronLeft :size="15" /></template>
            </a-button>
            <span class="date-value" data-testid="device-record-date">{{ date }}</span>
            <a-button type="secondary" :disabled="!channelId" aria-label="后一天" title="后一天" @click="stepDate(1)">
              <template #icon><ChevronRight :size="15" /></template>
            </a-button>
            <a-button type="secondary" :disabled="!channelId" @click="goToday">今天</a-button>
            <a-button
              type="secondary"
              :disabled="!channelId || queryLoading"
              class="uvp-refresh-btn"
              aria-label="重新查询"
              title="重新查询这一天"
              @click="reloadDay(true)"
            >
              <template #icon><RefreshCw :size="15" :class="{ spin: queryLoading }" /></template>
            </a-button>
          </div>
        </header>

        <div v-if="optionsError" class="workspace-error" role="alert">
          {{ optionsError }}
          <a-button type="text" size="small" @click="loadOptions(channelId)">重试</a-button>
        </div>

        <section ref="stageRef" class="stage-area">
          <PlaybackStage
            :url="playback.mediaUrl.value"
            :zlm-webrtc="playback.source.value?.zlmWebrtc"
            :has-audio="playback.hasAudio.value"
            :loading="playback.isLoading.value || optionsLoading"
            :status-text="playback.statusText.value"
            :failed="playback.playback.value.status === 'failed'"
            :osd-title="osdTitle"
            :osd-meta="osdMeta"
            :empty-title="emptyTitle"
            :empty-description="emptyDescription"
            :empty-action="emptyAction"
            @error="playback.handlePlayerError"
            @timeupdate="playback.handlePlayerTimeUpdate"
            @loading-change="playback.handlePlayerLoading"
            @empty-action="goCloudRecordings"
          />
        </section>

        <DayTimeline
          :range="dayRange(date, options?.serverNow)"
          :records="records"
          :current-time="playback.playback.value.currentTime"
          :selected-record-key="playback.playback.value.recordKey"
          :loading="queryLoading"
          :stale="dayRecords.stale.value"
          @seek="handleTimelineSeek"
          @select="
            key => {
              const record = records.find(item => item.recordKey === key);
              if (record) handleSegmentSelect(record);
            }
          "
        />

        <SegmentDrawer
          :records="records"
          :selected-record-key="playback.playback.value.recordKey"
          :open="drawerOpen"
          :can-download="canDownload"
          :download-pending="downloadPending"
          @update:open="drawerOpen = $event"
          @select="handleSegmentSelect"
          @download="handleDownload"
        />

        <PlaybackControls
          :playback="playback.playback.value"
          :selected-record="playback.selectedRecord.value"
          :can-play="canPlay"
          :can-download="canDownload"
          :control-pending="playback.controlPending.value"
          :download-pending="downloadPending"
          :has-prev="hasPrev"
          :has-next="hasNext"
          @toggle="playback.toggle"
          @stop="playback.stop()"
          @prev="playback.step(-1)"
          @next="playback.step(1)"
          @scale="playback.setScale"
          @download="handleDownload()"
          @fullscreen="toggleFullscreen"
        />
      </main>
    </div>

    <RecordCacheProgressDialog
      v-model:visible="cacheDialogVisible"
      :task-id="cacheTaskId"
      :task-name="cacheTaskName"
      @navigate="goRecordCache"
    />
  </div>
</template>

<style scoped>
.permission-state {
  padding: 48px 24px;
  font-size: 13px;
  color: var(--uvp-text-secondary);
  text-align: center;
}
.device-record-shell {
  display: flex;
  min-height: 0;
  overflow: hidden;
}
.channel-panel {
  display: flex;
  flex: 0 0 240px;
  flex-direction: column;
  min-height: 0;
  background: var(--uvp-card, transparent);
  border-right: 1px solid var(--uvp-panel-border);
}
.workspace {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
}
.workspace-bar {
  display: flex;
  gap: 12px;
  align-items: center;
  min-height: 46px;
  padding: 6px 14px;
  border-bottom: 1px solid var(--uvp-panel-border);
}
.channel-context {
  display: flex;
  gap: 8px;
  align-items: center;
  min-width: 0;
}
.channel-context strong {
  font-size: 13px;
  font-weight: 500;
  color: var(--uvp-text-primary);
  white-space: nowrap;
}
.channel-code {
  font-family: var(--uvp-font-mono, ui-monospace, monospace);
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.channel-device {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 11px;
  color: var(--uvp-text-secondary);
  white-space: nowrap;
}
.channel-status {
  padding: 1px 7px;
  font-size: 10px;
  font-style: normal;
  color: var(--uvp-success);
  background: color-mix(in srgb, var(--uvp-success) 12%, transparent);
  border-radius: 8px;
}
.channel-status.offline {
  color: var(--uvp-danger);
  background: color-mix(in srgb, var(--uvp-danger) 12%, transparent);
}
.date-nav {
  display: flex;
  gap: 6px;
  align-items: center;
  margin-left: auto;
}
.date-value {
  min-width: 96px;
  font-family: var(--uvp-font-mono, ui-monospace, monospace);
  font-size: 12px;
  color: var(--uvp-text-primary);
  text-align: center;
}
.workspace-error {
  display: flex;
  gap: 8px;
  align-items: center;
  padding: 6px 14px;
  font-size: 12px;
  color: var(--uvp-danger);
  background: color-mix(in srgb, var(--uvp-danger) 8%, transparent);
}
.stage-area {
  display: flex;
  flex: 1;
  min-height: 0;
  background: #1f1f1e;
}
.spin {
  animation: device-record-spin 0.9s linear infinite;
}

@keyframes device-record-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
