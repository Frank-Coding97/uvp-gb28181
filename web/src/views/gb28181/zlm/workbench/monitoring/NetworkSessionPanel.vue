<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, toRef, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { Network, Radio, ShieldAlert, Users } from "lucide-vue-next";

import {
  listAllZLMMediaViewers,
  listAllZLMNetworkSessions,
  listZLMMediaViewers,
  listZLMNetworkSessions,
  type ZLMNetworkSessionPage,
  type ZLMStreamViewer,
  type ZLMStreamViewerPage
} from "@/api/gb28181-zlm-runtime";
import type { MediaScope } from "@/store/modules/media-workbench";
import { useUserStoreHook } from "@/store/modules/user";

import ZLMSessionKickDialog from "../../ZLMSessionKickDialog.vue";
import { zlmErrorPresentation } from "../../components/zlmFormatters";
import { useZLMRuntimePolling } from "../../composables/useZLMRuntimePolling";
import {
  createNetworkFilters,
  createViewerFilters,
  type MonitoringNetworkFilters,
  type MonitoringViewerFilters
} from "./monitoringState";
import { buildNetworkSessionQuery, buildViewerTarget, canKickViewer } from "../../sessionManagementState";
import { boundedPageRows } from "../boundedData";

type PollPayload =
  | { kind: "network"; data: ZLMNetworkSessionPage }
  | { kind: "viewers"; data: ZLMStreamViewerPage }
  | { kind: "viewer-query-empty" };

const props = withDefaults(
  defineProps<{
    active: boolean;
    view: "network" | "viewers";
    scope: MediaScope;
    nodeId: number | null;
    autoRefresh?: boolean;
    initialQuery?: Record<string, unknown>;
  }>(),
  {
    active: true,
    autoRefresh: true,
    initialQuery: undefined
  }
);

const userStore = useUserStoreHook();
const networkFilter = reactive<MonitoringNetworkFilters>(createNetworkFilters(props.initialQuery));
const viewerFilter = reactive<MonitoringViewerFilters>(createViewerFilters(props.initialQuery));
const viewerPage = ref(1);
const viewerPageSize = ref(10);
const networkData = ref<ZLMNetworkSessionPage | null>(null);
const viewerData = ref<ZLMStreamViewerPage | null>(null);
const loading = ref(false);
const loadError = ref<unknown>(null);
const kickVisible = ref(false);
const kickViewer = ref<ZLMStreamViewer | null>(null);
const kickNodeName = ref("");
const AUTO_REFRESH_SECONDS = 10;
const refreshCountdown = ref(0);
let refreshCountdownTimer: ReturnType<typeof setInterval> | null = null;
let disposed = false;

const scopeLabel = computed(() => (props.scope === "all" ? "全部节点" : `节点 #${props.nodeId ?? "—"}`));
const viewerTarget = computed(() => buildViewerTarget(viewerFilter));
const hasKickPermission = computed(
  () => userStore.account.permissions.includes("*:*:*") || userStore.account.permissions.includes("gb28181:zlm:session:kick")
);
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value));
const pollingPaused = computed(() => !props.autoRefresh || kickVisible.value);
// 观看者标签要先填完整媒体身份才会真正发请求，空查询时不该挂一个假倒计时。
const queryReady = computed(() => props.view === "network" || viewerTarget.value !== null);
const refreshButtonLabel = computed(() =>
  props.active && props.autoRefresh && refreshCountdown.value > 0 ? `刷新（${refreshCountdown.value}s）` : "刷新"
);
const requestNodeId = computed(() => (props.scope === "all" ? 1 : props.nodeId));
const networkRows = computed(() =>
  (networkData.value?.list ?? []).map(record => ({ ...record, rowKey: `${record.nodeId}:${record.id}` }))
);
const viewerRows = computed(() =>
  (viewerData.value?.list ?? []).map(record => ({ ...record, rowKey: `${record.nodeId}:${record.identifier}` }))
);

const { refresh } = useZLMRuntimePolling<PollPayload>({
  nodeId: requestNodeId,
  active: toRef(props, "active"),
  paused: pollingPaused,
  intervalMs: 10_000,
  async load(nodeId, signal) {
    loading.value = true;
    const tab = props.view;
    if (tab === "network") {
      const response =
        props.scope === "all"
          ? await listAllZLMNetworkSessions(buildNetworkSessionQuery(networkFilter), signal)
          : await listZLMNetworkSessions(nodeId, buildNetworkSessionQuery(networkFilter), signal);
      if (response.code !== 0 || !response.data) throw new Error(response.message || "网络会话加载失败");
      return { kind: "network", data: response.data };
    }
    const target = viewerTarget.value;
    if (!target) return { kind: "viewer-query-empty" };
    const response =
      props.scope === "all"
        ? await listAllZLMMediaViewers(target, { page: viewerPage.value, pageSize: viewerPageSize.value }, signal)
        : await listZLMMediaViewers(nodeId, target, { page: viewerPage.value, pageSize: viewerPageSize.value }, signal);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "媒体观看者加载失败");
    return { kind: "viewers", data: response.data };
  },
  publish(value) {
    if (value.kind === "network" && props.view === "network") {
      networkData.value = { ...value.data, list: boundedPageRows(value.data.list, networkFilter.pageSize) };
    }
    if (value.kind === "viewers" && props.view === "viewers") {
      viewerData.value = { ...value.data, list: boundedPageRows(value.data.list, viewerPageSize.value) };
    }
    loadError.value = null;
    loading.value = false;
    scheduleRefreshCountdown();
  },
  onError(error) {
    loadError.value = error;
    loading.value = false;
    scheduleRefreshCountdown();
  }
});

watch([() => props.scope, () => props.nodeId], () => {
  kickVisible.value = false;
  kickViewer.value = null;
  kickNodeName.value = "";
  networkData.value = null;
  viewerData.value = null;
  loadError.value = null;
  loading.value = props.scope === "all" || props.nodeId !== null;
  if (props.active) requestRefresh();
});

watch(
  () => props.view,
  () => {
    loadError.value = null;
    if (props.active) requestRefresh();
  }
);

watch([() => props.active, () => props.autoRefresh, pollingPaused], () => {
  if (!props.active || !props.autoRefresh || pollingPaused.value) clearRefreshCountdown();
});

onBeforeUnmount(() => {
  disposed = true;
  clearRefreshCountdown();
});

/* 倒计时与实际轮询同源：轮询在请求结束后才排下一次定时器，
   所以「刷新（10s）」从本次落库开始倒数，到点正好是下一次自动刷新。 */
function clearRefreshCountdown() {
  if (refreshCountdownTimer) clearInterval(refreshCountdownTimer);
  refreshCountdownTimer = null;
  refreshCountdown.value = 0;
}

function tickRefreshCountdown() {
  if (refreshCountdown.value > 1) {
    refreshCountdown.value -= 1;
    return;
  }
  refreshCountdown.value = 0;
  clearRefreshCountdown();
}

function scheduleRefreshCountdown() {
  clearRefreshCountdown();
  if (disposed || !props.active || !props.autoRefresh || pollingPaused.value || !queryReady.value) return;
  refreshCountdown.value = AUTO_REFRESH_SECONDS;
  refreshCountdownTimer = setInterval(tickRefreshCountdown, 1_000);
}

function requestRefresh() {
  clearRefreshCountdown();
  refresh();
}

function queryNetwork() {
  networkFilter.page = 1;
  requestRefresh();
}

function resetNetwork() {
  networkFilter.peerIp = "";
  networkFilter.localPort = "";
  networkFilter.page = 1;
  requestRefresh();
}

function queryViewers() {
  viewerPage.value = 1;
  if (!viewerTarget.value) {
    viewerData.value = null;
    Message.warning("请填写完整的 Schema、VHost、App 和 Stream");
    return;
  }
  requestRefresh();
}

function resetViewers() {
  viewerFilter.schema = "";
  viewerFilter.vhost = "";
  viewerFilter.app = "";
  viewerFilter.stream = "";
  viewerPage.value = 1;
  viewerData.value = null;
  if (props.active) requestRefresh();
}

function changeNetworkPage(nextPage: number) {
  networkFilter.page = nextPage;
  requestRefresh();
}

function changeNetworkPageSize(nextSize: number) {
  networkFilter.pageSize = nextSize;
  networkFilter.page = 1;
  requestRefresh();
}

function changeViewerPage(nextPage: number) {
  viewerPage.value = nextPage;
  requestRefresh();
}

function changeViewerPageSize(nextSize: number) {
  viewerPageSize.value = nextSize;
  viewerPage.value = 1;
  requestRefresh();
}

function openKick(viewer: ZLMStreamViewer) {
  if (!canKickViewer(viewer, hasKickPermission.value)) return;
  kickViewer.value = { ...viewer, media: { ...viewer.media } };
  kickNodeName.value = viewer.nodeName || `节点 #${viewer.nodeId}`;
  kickVisible.value = true;
}

function kickDone(result: { kicked: boolean; alreadyDisconnected: boolean; uncertain: boolean }) {
  if (result.uncertain) Message.warning("踢除结果未确认，请刷新回读");
  else if (result.alreadyDisconnected) Message.info("观看会话已断开");
  else Message.success("观看会话已踢除");
  requestRefresh();
}

defineExpose({ refresh: requestRefresh });
</script>

<template>
  <div class="monitoring-panel session-panel">
    <div v-if="loadError && (networkData || viewerData)" class="monitoring-banner monitoring-banner--warning" role="status">
      本次刷新失败：{{ errorPresentation.label }}；保留当前 Tab 的筛选、分页和上一次数据。
    </div>
    <div
      v-if="(props.view === 'network' && networkData?.partial) || (props.view === 'viewers' && viewerData?.partial)"
      class="monitoring-banner monitoring-banner--warning"
      role="status"
    >
      部分节点读取失败，当前结果只包含成功返回的节点；请刷新重试。
    </div>
    <div v-if="!hasKickPermission" class="monitoring-banner" role="status">
      当前账号可查看会话，但没有 `gb28181:zlm:session:kick` 权限；踢除按钮不会显示。
    </div>

    <template v-if="props.view === 'network'">
      <s-layout-search class="session-search">
        <template #fields
          ><slot name="scope" /><a-input-search
            v-model="networkFilter.peerIp"
            allow-clear
            placeholder="远端 IP"
            class="peer-filter"
            @search="queryNetwork" /><a-input
            v-model="networkFilter.localPort"
            allow-clear
            placeholder="本地端口"
            class="port-filter"
            @press-enter="queryNetwork"
        /></template>
        <template #actions
          ><a-button type="primary" @click="queryNetwork">查询</a-button><a-button @click="resetNetwork">重置</a-button
          ><a-button
            class="uvp-page-action-btn uvp-refresh-btn session-refresh-btn"
            aria-label="刷新网络会话"
            :loading="loading"
            @click="requestRefresh"
            ><template #icon><icon-refresh /></template>{{ refreshButtonLabel }}</a-button
          ></template
        >
        <template #extra
          ><span class="scope-note">当前范围：{{ scopeLabel }}</span></template
        >
      </s-layout-search>

      <div v-if="loading && !networkData" class="monitoring-state" role="status"><a-spin />正在加载网络会话…</div>
      <div v-else-if="loadError && !networkData" class="monitoring-state monitoring-state--error" role="alert">
        <ShieldAlert :size="36" /><strong>{{ errorPresentation.label }}</strong
        ><a-button v-if="errorPresentation.retryable" @click="requestRefresh" class="uvp-refresh-btn">重新加载</a-button>
      </div>
      <template v-else>
        <section class="session-table-panel">
          <a-table
            :data="networkRows"
            :loading="loading"
            row-key="rowKey"
            :pagination="false"
            :scroll="{ y: '100%' }"
            class="uvp-data-table"
            ><template #columns
              ><a-table-column title="节点" :width="140"
                ><template #cell="{ record }">{{ record.nodeName || `节点 #${record.nodeId}` }}</template></a-table-column
              ><a-table-column title="会话 ID" data-index="id" :width="220" /><a-table-column title="远端"
                ><template #cell="{ record }">{{ record.peerIp }}:{{ record.peerPort }}</template></a-table-column
              ><a-table-column title="本地"
                ><template #cell="{ record }">{{ record.localIp }}:{{ record.localPort }}</template></a-table-column
              ><a-table-column title="类型" data-index="type" /><a-table-column
                title="类型 ID"
                data-index="typeId"
              /><a-table-column title="可执行操作" :width="150"
                ><template #cell><span class="muted">仅观测</span></template></a-table-column
              ></template
            ><template #empty
              ><div class="session-empty"><Network :size="38" /><strong>没有符合筛选的网络会话</strong></div></template
            ></a-table
          >
        </section>
        <div class="session-pagination uvp-pagination-bar">
          <span>共 {{ networkData?.total ?? 0 }} 个网络会话<span v-if="networkData?.truncated">（后端已截断）</span></span
          ><a-pagination
            :current="networkFilter.page"
            :page-size="networkFilter.pageSize"
            :total="networkData?.total ?? 0"
            show-page-size
            :page-size-options="[10, 20, 50, 100]"
            @change="changeNetworkPage"
            @page-size-change="changeNetworkPageSize"
          />
        </div>
      </template>
    </template>

    <template v-else>
      <s-layout-search class="session-search">
        <template #fields
          ><slot name="scope" /><a-input
            v-model="viewerFilter.schema"
            allow-clear
            placeholder="Schema"
            class="media-filter-short" /><a-input
            v-model="viewerFilter.vhost"
            allow-clear
            placeholder="VHost"
            class="media-filter-vhost" /><a-input
            v-model="viewerFilter.app"
            allow-clear
            placeholder="App"
            class="media-filter-app" /><a-input-search
            v-model="viewerFilter.stream"
            allow-clear
            placeholder="Stream"
            class="media-filter-stream"
            @search="queryViewers"
        /></template>
        <template #actions
          ><a-button type="primary" :disabled="!viewerTarget" @click="queryViewers">查询</a-button
          ><a-button @click="resetViewers">重置</a-button
          ><a-button
            class="uvp-page-action-btn uvp-refresh-btn session-refresh-btn"
            aria-label="刷新媒体观看者"
            :loading="loading"
            :disabled="!viewerTarget"
            @click="requestRefresh"
            ><template #icon><icon-refresh /></template>{{ refreshButtonLabel }}</a-button
          ></template
        >
        <template #extra><span class="scope-note">需完整 MediaIdentity，踢除还需 kickable=true</span></template>
      </s-layout-search>

      <div v-if="!viewerTarget" class="monitoring-state" role="status">
        <Radio :size="36" /><strong>请输入完整媒体身份</strong
        ><span>Schema、VHost、App、Stream 缺一不可，页面不会猜默认值。</span>
      </div>
      <div v-else-if="loading && !viewerData" class="monitoring-state" role="status"><a-spin />正在加载媒体观看者…</div>
      <div v-else-if="loadError && !viewerData" class="monitoring-state monitoring-state--error" role="alert">
        <ShieldAlert :size="36" /><strong>{{ errorPresentation.label }}</strong>
      </div>
      <template v-else>
        <section class="session-table-panel">
          <a-table
            :data="viewerRows"
            :loading="loading"
            row-key="rowKey"
            :pagination="false"
            :scroll="{ y: '100%' }"
            class="uvp-data-table"
            ><template #columns
              ><a-table-column title="节点" :width="140"
                ><template #cell="{ record }">{{ record.nodeName || `节点 #${record.nodeId}` }}</template></a-table-column
              ><a-table-column title="观看标识" data-index="identifier" :width="240" /><a-table-column title="远端"
                ><template #cell="{ record }">{{ record.peerIp }}:{{ record.peerPort }}</template></a-table-column
              ><a-table-column title="本地"
                ><template #cell="{ record }">{{ record.localIp }}:{{ record.localPort }}</template></a-table-column
              ><a-table-column title="类型" data-index="typeId" /><a-table-column title="操作" :width="150"
                ><template #cell="{ record }"
                  ><a-button
                    v-if="record.kickable"
                    size="small"
                    status="danger"
                    :disabled="!hasKickPermission"
                    @click="openKick(record)"
                    >踢除</a-button
                  ><span v-else class="muted">不可踢除</span></template
                ></a-table-column
              ></template
            ><template #empty
              ><div class="session-empty"><Users :size="38" /><strong>该媒体流当前没有观看者</strong></div></template
            ></a-table
          >
        </section>
        <div class="session-pagination uvp-pagination-bar">
          <span>共 {{ viewerData?.total ?? 0 }} 个观看者<span v-if="viewerData?.truncated">（后端已截断）</span></span
          ><a-pagination
            :current="viewerPage"
            :page-size="viewerPageSize"
            :total="viewerData?.total ?? 0"
            show-page-size
            :page-size-options="[10, 20, 50, 100]"
            @change="changeViewerPage"
            @page-size-change="changeViewerPageSize"
          />
        </div>
      </template>
    </template>

    <ZLMSessionKickDialog
      v-model:visible="kickVisible"
      :node-name="kickNodeName || scopeLabel"
      :viewer="kickViewer"
      @done="kickDone"
    />
  </div>
</template>

<style scoped>
/* ⛔ 竖向滚动条必须落在表体上，不能落在外层面板：这一层是 flex 列，
   `min-height: 0` 缺一个，下面的表格就撑高整页 ⇒ 表头跟着页面滚走。
   高度用 `flex: 1` 而不是 `height: 100%` —— 上方还有搜索栏/提示条时，
   百分比会按容器全高算，把底部分页顶出可视区。 */
.monitoring-panel {
  box-sizing: border-box;
  display: flex;
  flex: 1;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  color: var(--zlm-text-2);
}
.monitoring-banner {
  flex: none;
  padding: 9px 12px;
  margin: 10px 0;
  font-size: var(--zlm-fs-caption);
  color: var(--zlm-text-2);
  background: var(--zlm-info-50);
  border: 1px solid var(--zlm-info-500);
  border-radius: var(--zlm-radius-md);
}
.monitoring-banner--warning {
  color: var(--zlm-warn-600);
  background: var(--zlm-warn-50);
  border-color: var(--zlm-warn-500);
}
.session-search {
  flex: none;
  margin-bottom: 12px;
}

/* 倒计时会让按钮文案在「刷新」和「刷新（10s）」之间变化，固定最小宽度防止布局抖动。 */
.session-refresh-btn {
  min-width: 128px;
}
.peer-filter {
  width: 220px;
}
.port-filter {
  width: 130px;
}
.media-filter-short {
  width: 100px;
}
.media-filter-vhost {
  width: 170px;
}
.media-filter-app {
  width: 130px;
}
.media-filter-stream {
  width: 190px;
}
.scope-note {
  font-size: var(--zlm-fs-caption);
  color: var(--zlm-text-3);
}
.monitoring-state {
  display: flex;
  flex-direction: column;
  gap: 10px;
  align-items: center;
  justify-content: center;
  min-height: 280px;
  color: var(--zlm-text-3);
  text-align: center;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: var(--uvp-panel-radius);
}
.monitoring-state strong {
  color: var(--zlm-text-1);
}
.monitoring-state--error {
  color: var(--zlm-danger-600);
  background: var(--zlm-danger-50);
  border-color: var(--zlm-danger-500);
}
.session-table-panel {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: var(--uvp-panel-radius);
  box-shadow: var(--uvp-panel-shadow);
}

/* ⛔⛔ Arco 只给 a-table **根节点**写行内 `height:100%`（scroll.y 是字符串时），
   下层的 `.arco-table-container` 仍是 `height:auto` ⇒ 它只按「表头 + 表体内容」收边，
   表格盒子提前收尾、下面空一大片，表体也拿不到剩余高度（实测：面板 283px、
   容器只有 165px）。所以必须把容器这一层也钉成 100%，表体的 `max-height:100%`
   才解得开、行数多时才会在表体里滚。 */
.session-table-panel :deep(.arco-table-container) {
  height: 100%;
  min-height: 0;
}
.session-empty {
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: center;
  padding: 48px 16px;
  color: var(--zlm-text-3);
}
.session-empty strong {
  color: var(--zlm-text-1);
}
.session-pagination {
  display: flex;
  flex: none;
  gap: 16px;
  align-items: center;
  justify-content: space-between;
  margin-top: 12px;
  font-size: var(--zlm-fs-caption);
  color: var(--zlm-text-3);
}
.muted {
  color: var(--zlm-text-4);
}

@media (width <= 800px) {
  .peer-filter,
  .port-filter,
  .media-filter-short,
  .media-filter-vhost,
  .media-filter-app,
  .media-filter-stream {
    width: 100%;
  }
  .session-pagination {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>
