<script setup lang="ts">
import { computed, reactive, ref, toRef, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { Network, Radio, ShieldAlert, Users } from "lucide-vue-next";

import {
  listZLMMediaViewers,
  listZLMNetworkSessions,
  type ZLMNetworkSessionPage,
  type ZLMStreamViewer,
  type ZLMStreamViewerPage
} from "@/api/gb28181-zlm-runtime";
import type { MediaScope } from "@/store/modules/media-workbench";
import { useUserStoreHook } from "@/store/modules/user";

import ZLMSessionKickDialog from "../../ZLMSessionKickDialog.vue";
import { zlmErrorPresentation, zlmFreshnessPresentation } from "../../components/zlmFormatters";
import { useZLMRuntimePolling } from "../../composables/useZLMRuntimePolling";
import {
  createNetworkFilters,
  createViewerFilters,
  type MonitoringNetworkFilters,
  type MonitoringViewerFilters
} from "./monitoringState";
import { buildNetworkSessionQuery, buildViewerTarget, canKickViewer } from "../../sessionManagementState";

type SessionTab = "network" | "viewers";
type PollPayload =
  | { kind: "network"; data: ZLMNetworkSessionPage }
  | { kind: "viewers"; data: ZLMStreamViewerPage }
  | { kind: "viewer-query-empty" };

const props = withDefaults(defineProps<{
  active: boolean;
  scope: MediaScope;
  nodeId: number | null;
  initialQuery?: Record<string, unknown>;
}>(), {
  active: true,
  initialQuery: undefined
});

const userStore = useUserStoreHook();
const activeTab = ref<SessionTab>("network");
const networkFilter = reactive<MonitoringNetworkFilters>(createNetworkFilters(props.initialQuery));
const viewerFilter = reactive<MonitoringViewerFilters>(createViewerFilters(props.initialQuery));
const viewerPage = ref(1);
const viewerPageSize = ref(20);
const networkData = ref<ZLMNetworkSessionPage | null>(null);
const viewerData = ref<ZLMStreamViewerPage | null>(null);
const loading = ref(false);
const loadError = ref<unknown>(null);
const kickVisible = ref(false);
const kickViewer = ref<ZLMStreamViewer | null>(null);

const scopeLabel = computed(() => props.scope === "all" ? "全部节点" : `节点 #${props.nodeId ?? "—"}`);
const viewerTarget = computed(() => buildViewerTarget(viewerFilter));
const hasKickPermission = computed(() => userStore.account.permissions.includes("*:*:*") || userStore.account.permissions.includes("gb28181:zlm:session:kick"));
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value));
const activeAsOf = computed(() => activeTab.value === "network" ? networkData.value?.asOf : viewerData.value?.asOf);
const freshness = computed(() => zlmFreshnessPresentation(activeAsOf.value));
const paused = computed(() => kickVisible.value);
const requestNodeId = computed(() => props.nodeId);

const { refresh } = useZLMRuntimePolling<PollPayload>({
  nodeId: requestNodeId,
  active: toRef(props, "active"),
  paused,
  intervalMs: 8_000,
  async load(nodeId, signal) {
    loading.value = true;
    const tab = activeTab.value;
    if (tab === "network") {
      const response = await listZLMNetworkSessions(nodeId, buildNetworkSessionQuery(networkFilter), signal);
      if (response.code !== 0 || !response.data) throw new Error(response.message || "网络会话加载失败");
      return { kind: "network", data: response.data };
    }
    const target = viewerTarget.value;
    if (!target) return { kind: "viewer-query-empty" };
    const response = await listZLMMediaViewers(nodeId, target, { page: viewerPage.value, pageSize: viewerPageSize.value }, signal);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "媒体观看者加载失败");
    return { kind: "viewers", data: response.data };
  },
  publish(value) {
    if (value.kind === "network" && activeTab.value === "network") networkData.value = value.data;
    if (value.kind === "viewers" && activeTab.value === "viewers") viewerData.value = value.data;
    loadError.value = null;
    loading.value = false;
  },
  onError(error) {
    loadError.value = error;
    loading.value = false;
  }
});

watch([() => props.scope, () => props.nodeId], () => {
  kickVisible.value = false;
  kickViewer.value = null;
  networkData.value = null;
  viewerData.value = null;
  loadError.value = null;
  loading.value = props.nodeId !== null;
  if (props.active) refresh();
});

watch(activeTab, () => {
  loadError.value = null;
  if (props.active) refresh();
});

function queryNetwork() {
  networkFilter.page = 1;
  refresh();
}

function resetNetwork() {
  networkFilter.peerIp = "";
  networkFilter.localPort = "";
  networkFilter.page = 1;
  refresh();
}

function queryViewers() {
  viewerPage.value = 1;
  if (!viewerTarget.value) {
    viewerData.value = null;
    Message.warning("请填写完整的 Schema、VHost、App 和 Stream");
    return;
  }
  refresh();
}

function resetViewers() {
  viewerFilter.schema = "";
  viewerFilter.vhost = "";
  viewerFilter.app = "";
  viewerFilter.stream = "";
  viewerPage.value = 1;
  viewerData.value = null;
  if (props.active) refresh();
}

function changeNetworkPage(nextPage: number) {
  networkFilter.page = nextPage;
  refresh();
}

function changeNetworkPageSize(nextSize: number) {
  networkFilter.pageSize = nextSize;
  networkFilter.page = 1;
  refresh();
}

function changeViewerPage(nextPage: number) {
  viewerPage.value = nextPage;
  refresh();
}

function changeViewerPageSize(nextSize: number) {
  viewerPageSize.value = nextSize;
  viewerPage.value = 1;
  refresh();
}

function openKick(viewer: ZLMStreamViewer) {
  if (!canKickViewer(viewer, hasKickPermission.value)) return;
  kickViewer.value = { ...viewer, media: { ...viewer.media } };
  kickVisible.value = true;
}

function kickDone(result: { kicked: boolean; alreadyDisconnected: boolean; uncertain: boolean }) {
  if (result.uncertain) Message.warning("踢除结果未确认，请刷新回读");
  else if (result.alreadyDisconnected) Message.info("观看会话已断开");
  else Message.success("观看会话已踢除");
  refresh();
}

defineExpose({ refresh });
</script>

<template>
  <div class="monitoring-panel session-panel">
    <header class="monitoring-panel__header">
      <div>
        <div class="monitoring-panel__eyebrow"><Network :size="14" />连接与观看</div>
        <h2>会话管理</h2>
        <p>网络连接与媒体观看者是两套独立口径；踢除仅对后端标记为可操作且当前账号有权限的观看会话开放。</p>
      </div>
      <span v-if="activeAsOf" :class="`freshness freshness--${freshness.tone}`" :title="freshness.description">{{ freshness.label }} · {{ activeAsOf }}</span>
    </header>

    <div v-if="props.scope === 'all'" class="monitoring-banner" role="status">会话接口按节点提供。请选择具体节点后查看网络会话与媒体观看者，避免把跨节点连接误合并。</div>
    <div v-if="loadError && (networkData || viewerData)" class="monitoring-banner monitoring-banner--warning" role="status">本次刷新失败：{{ errorPresentation.label }}；保留当前 Tab 的筛选、分页和上一次数据。</div>
    <div v-if="!hasKickPermission" class="monitoring-banner" role="status">当前账号可查看会话，但没有 `gb28181:zlm:session:kick` 权限；踢除按钮不会显示。</div>

    <a-tabs v-model:active-key="activeTab" class="session-tabs">
      <a-tab-pane key="network" title="网络会话">
        <s-layout-search class="session-search">
          <template #fields><a-input-search v-model="networkFilter.peerIp" allow-clear placeholder="远端 IP" class="peer-filter" @search="queryNetwork" /><a-input v-model="networkFilter.localPort" allow-clear placeholder="本地端口" class="port-filter" @press-enter="queryNetwork" /></template>
          <template #actions><a-button type="primary" :disabled="props.scope === 'all'" @click="queryNetwork">查询</a-button><a-button @click="resetNetwork">重置</a-button><a-button class="uvp-refresh-btn" :loading="loading" :disabled="props.scope === 'all'" @click="refresh"><template #icon><icon-refresh /></template>刷新</a-button></template>
          <template #extra><span class="scope-note">{{ props.scope === 'all' ? '请先选择节点' : `当前范围：${scopeLabel}` }}</span></template>
        </s-layout-search>

        <div v-if="props.scope === 'all'" class="monitoring-state" role="status"><Network :size="36" /><strong>请选择具体节点</strong><span>网络会话是节点级数据，当前不做未经后端证明的跨节点聚合。</span></div>
        <div v-else-if="loading && !networkData" class="monitoring-state" role="status"><a-spin />正在加载网络会话…</div>
        <div v-else-if="loadError && !networkData" class="monitoring-state monitoring-state--error" role="alert"><ShieldAlert :size="36" /><strong>{{ errorPresentation.label }}</strong><a-button v-if="errorPresentation.retryable" @click="refresh">重新加载</a-button></div>
        <section v-else class="session-table-panel">
          <a-table :data="networkData?.list || []" :loading="loading" row-key="id" :pagination="false" class="uvp-data-table"><template #columns><a-table-column title="会话 ID" data-index="id" :width="220" /><a-table-column title="远端"><template #cell="{ record }">{{ record.peerIp }}:{{ record.peerPort }}</template></a-table-column><a-table-column title="本地"><template #cell="{ record }">{{ record.localIp }}:{{ record.localPort }}</template></a-table-column><a-table-column title="类型" data-index="type" /><a-table-column title="类型 ID" data-index="typeId" /><a-table-column title="可执行操作" :width="150"><template #cell><span class="muted">仅观测</span></template></a-table-column></template><template #empty><div class="session-empty"><Network :size="38" /><strong>没有符合筛选的网络会话</strong></div></template></a-table>
          <div class="session-pagination"><span>共 {{ networkData?.total ?? 0 }} 个网络会话<span v-if="networkData?.truncated">（后端已截断）</span></span><a-pagination :current="networkFilter.page" :page-size="networkFilter.pageSize" :total="networkData?.total ?? 0" show-page-size :page-size-options="[20, 50, 100]" @change="changeNetworkPage" @page-size-change="changeNetworkPageSize" /></div>
        </section>
      </a-tab-pane>

      <a-tab-pane key="viewers" title="媒体观看者">
        <s-layout-search class="session-search">
          <template #fields><a-input v-model="viewerFilter.schema" allow-clear placeholder="Schema" class="media-filter-short" /><a-input v-model="viewerFilter.vhost" allow-clear placeholder="VHost" class="media-filter-vhost" /><a-input v-model="viewerFilter.app" allow-clear placeholder="App" class="media-filter-app" /><a-input-search v-model="viewerFilter.stream" allow-clear placeholder="Stream" class="media-filter-stream" @search="queryViewers" /></template>
          <template #actions><a-button type="primary" :disabled="props.scope === 'all'" @click="queryViewers">查询</a-button><a-button @click="resetViewers">重置</a-button><a-button class="uvp-refresh-btn" :loading="loading" :disabled="props.scope === 'all' || !viewerTarget" @click="refresh"><template #icon><icon-refresh /></template>刷新</a-button></template>
          <template #extra><span class="scope-note">{{ props.scope === 'all' ? '请先选择节点' : '需完整 MediaIdentity，踢除还需 kickable=true' }}</span></template>
        </s-layout-search>

        <div v-if="props.scope === 'all'" class="monitoring-state" role="status"><Users :size="36" /><strong>请选择具体节点</strong><span>观看者接口需要 nodeId 与完整 MediaIdentity。</span></div>
        <div v-else-if="!viewerTarget" class="monitoring-state" role="status"><Radio :size="36" /><strong>请输入完整媒体身份</strong><span>Schema、VHost、App、Stream 缺一不可，页面不会猜默认值。</span></div>
        <div v-else-if="loading && !viewerData" class="monitoring-state" role="status"><a-spin />正在加载媒体观看者…</div>
        <div v-else-if="loadError && !viewerData" class="monitoring-state monitoring-state--error" role="alert"><ShieldAlert :size="36" /><strong>{{ errorPresentation.label }}</strong></div>
        <section v-else class="session-table-panel">
          <a-table :data="viewerData?.list || []" :loading="loading" row-key="identifier" :pagination="false" class="uvp-data-table"><template #columns><a-table-column title="观看标识" data-index="identifier" :width="240" /><a-table-column title="远端"><template #cell="{ record }">{{ record.peerIp }}:{{ record.peerPort }}</template></a-table-column><a-table-column title="本地"><template #cell="{ record }">{{ record.localIp }}:{{ record.localPort }}</template></a-table-column><a-table-column title="类型" data-index="typeId" /><a-table-column title="操作" :width="150"><template #cell="{ record }"><a-button v-if="canKickViewer(record, hasKickPermission)" size="small" status="danger" @click="openKick(record)">踢除</a-button><span v-else class="muted">{{ record.kickable ? '无权限' : '不可踢除' }}</span></template></a-table-column></template><template #empty><div class="session-empty"><Users :size="38" /><strong>该媒体流当前没有观看者</strong></div></template></a-table>
          <div class="session-pagination"><span>共 {{ viewerData?.total ?? 0 }} 个观看者<span v-if="viewerData?.truncated">（后端已截断）</span></span><a-pagination :current="viewerPage" :page-size="viewerPageSize" :total="viewerData?.total ?? 0" show-page-size :page-size-options="[20, 50, 100]" @change="changeViewerPage" @page-size-change="changeViewerPageSize" /></div>
        </section>
      </a-tab-pane>
    </a-tabs>

    <ZLMSessionKickDialog v-model:visible="kickVisible" :node-name="scopeLabel" :viewer="kickViewer" @done="kickDone" />
  </div>
</template>

<style scoped>
.monitoring-panel { box-sizing: border-box; min-width: 0; color: var(--zlm-text-2); }.monitoring-panel__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 12px; }.monitoring-panel__eyebrow { display: inline-flex; align-items: center; gap: 7px; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.monitoring-panel h2 { margin: 4px 0 0; color: var(--zlm-text-1); font-size: 19px; }.monitoring-panel p { margin: 5px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); line-height: 1.55; }.freshness { color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.freshness--warning { color: var(--zlm-warn-600); }.freshness--danger { color: var(--zlm-danger-600); }.monitoring-banner { margin: 10px 0; padding: 9px 12px; color: var(--zlm-text-2); background: var(--zlm-info-50); border: 1px solid var(--zlm-info-500); border-radius: var(--zlm-radius-md); font-size: var(--zlm-fs-caption); }.monitoring-banner--warning { color: var(--zlm-warn-600); background: var(--zlm-warn-50); border-color: var(--zlm-warn-500); }
.session-tabs { margin-top: 10px; }.session-search { margin-bottom: 12px; }.peer-filter { width: 220px; }.port-filter { width: 130px; }.media-filter-short { width: 100px; }.media-filter-vhost { width: 170px; }.media-filter-app { width: 130px; }.media-filter-stream { width: 190px; }.scope-note { color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.monitoring-state { display: flex; min-height: 280px; flex-direction: column; align-items: center; justify-content: center; gap: 10px; color: var(--zlm-text-3); text-align: center; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); }.monitoring-state strong { color: var(--zlm-text-1); }.monitoring-state--error { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border-color: var(--zlm-danger-500); }.session-table-panel { overflow: hidden; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); box-shadow: var(--uvp-panel-shadow); }.session-empty { display: flex; flex-direction: column; align-items: center; gap: 8px; padding: 48px 16px; color: var(--zlm-text-3); }.session-empty strong { color: var(--zlm-text-1); }.session-pagination { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 12px 16px; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); border-top: 1px solid var(--zlm-border); }.muted { color: var(--zlm-text-4); }
@media (max-width: 800px) { .monitoring-panel__header { flex-direction: column; }.peer-filter, .port-filter, .media-filter-short, .media-filter-vhost, .media-filter-app, .media-filter-stream { width: 100%; }.session-pagination { align-items: flex-start; flex-direction: column; } }
</style>
