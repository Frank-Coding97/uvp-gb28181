<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Message } from "@arco-design/web-vue";
import { Network, Radio, Server, ShieldAlert, Users } from "lucide-vue-next";
import { listZLMNodes, type ZLMNode } from "@/api/gb28181-zlm";
import {
  listZLMMediaViewers,
  listZLMNetworkSessions,
  type ZLMNetworkSessionPage,
  type ZLMStreamViewer,
  type ZLMStreamViewerPage
} from "@/api/gb28181-zlm-runtime";
import { useZLMContextStore } from "@/store/modules/zlm-context";
import { useUserStoreHook } from "@/store/modules/user";
import ZLMNodeContextBar from "./components/ZLMNodeContextBar.vue";
import ZLMSessionKickDialog from "./ZLMSessionKickDialog.vue";
import { zlmErrorPresentation, zlmFreshnessPresentation } from "./components/zlmFormatters";
import { useZLMRuntimePolling } from "./composables/useZLMRuntimePolling";
import {
  buildNetworkSessionQuery,
  buildViewerTarget,
  canKickViewer,
  type NetworkSessionFilter,
  type ViewerTargetForm
} from "./sessionManagementState";

type SessionTab = "network" | "viewers";
type PollPayload =
  | { kind: "network"; data: ZLMNetworkSessionPage }
  | { kind: "viewers"; data: ZLMStreamViewerPage }
  | { kind: "viewer-query-empty" };

const route = useRoute();
const router = useRouter();
const context = useZLMContextStore();
const userStore = useUserStoreHook();
const nodes = ref<ZLMNode[]>([]);
const nodesLoading = ref(false);
const nodesError = ref<unknown>(null);
const activeTab = ref<SessionTab>("network");
const networkFilter = reactive<NetworkSessionFilter>({ peerIp: "", localPort: "", page: 1, pageSize: 20 });
const viewerFilter = reactive<ViewerTargetForm>({
  schema: String(route.query.schema ?? ""),
  vhost: String(route.query.vhost ?? ""),
  app: String(route.query.app ?? ""),
  stream: String(route.query.stream ?? "")
});
const viewerPage = ref(1);
const viewerPageSize = ref(20);
const networkData = ref<ZLMNetworkSessionPage | null>(null);
const viewerData = ref<ZLMStreamViewerPage | null>(null);
const loading = ref(true);
const loadError = ref<unknown>(null);
const kickVisible = ref(false);
const kickViewer = ref<ZLMStreamViewer | null>(null);

const selectedNodeId = computed(() => context.selectedNodeId);
const selectedNodeName = computed(() => context.selectedNode?.name ?? `节点 #${selectedNodeId.value ?? "—"}`);
const contextNodes = computed(() => nodes.value.map(node => ({ id: node.id, name: node.name, state: node.state })));
const viewerTarget = computed(() => buildViewerTarget(viewerFilter));
const hasKickPermission = computed(() => userStore.account.permissions.includes("*:*:*") || userStore.account.permissions.includes("gb28181:zlm:session:kick"));
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value ?? nodesError.value));
const activeAsOf = computed(() => activeTab.value === "network" ? networkData.value?.asOf : viewerData.value?.asOf);
const freshness = computed(() => zlmFreshnessPresentation(activeAsOf.value));
const paused = computed(() => kickVisible.value);

async function loadNodes() {
  nodesLoading.value = true;
  try {
    const response = await listZLMNodes();
    if (response.code !== 0) throw new Error(response.message || "节点列表加载失败");
    nodes.value = response.data?.list ?? [];
    nodesError.value = null;
  } catch (error) {
    nodesError.value = error;
  } finally {
    nodesLoading.value = false;
  }
}

const { refresh } = useZLMRuntimePolling<PollPayload>({
  nodeId: selectedNodeId,
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

watch(selectedNodeId, (nodeId, previous) => {
  if (nodeId === previous) return;
  kickVisible.value = false;
  kickViewer.value = null;
  networkData.value = null;
  viewerData.value = null;
  networkFilter.page = 1;
  viewerPage.value = 1;
  loadError.value = null;
  loading.value = nodeId !== null;
  if (nodeId) void router.replace({ query: { ...route.query, nodeId: String(nodeId) } });
});

watch(activeTab, () => {
  loadError.value = null;
  refresh();
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
  const target = viewerTarget.value;
  void router.replace({ query: { nodeId: String(selectedNodeId.value ?? ""), ...target } });
  refresh();
}

function resetViewers() {
  viewerFilter.schema = "";
  viewerFilter.vhost = "";
  viewerFilter.app = "";
  viewerFilter.stream = "";
  viewerPage.value = 1;
  viewerData.value = null;
  void router.replace({ query: selectedNodeId.value ? { nodeId: String(selectedNodeId.value) } : {} });
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
  context.selectNode(viewer.nodeId);
  kickViewer.value = { ...viewer, media: { ...viewer.media } };
  kickVisible.value = true;
}

function kickDone(result: { kicked: boolean; alreadyDisconnected: boolean; uncertain: boolean }) {
  if (result.uncertain) Message.warning("踢除结果未确认，请刷新回读");
  else if (result.alreadyDisconnected) Message.info("观看会话已断开");
  else Message.success("观看会话已踢除");
  refresh();
}

onMounted(loadNodes);
</script>

<template>
  <div class="snow-fill">
    <div class="snow-fill-inner uvp-page-shell-flat session-management">
      <header class="session-header"><div><h1>会话管理</h1><p>网络连接与媒体观看者是两套独立口径；网络会话不推断媒体身份，也不提供误导性的踢除按钮。</p></div><span v-if="activeAsOf" :class="`freshness freshness--${freshness.tone}`" :title="freshness.description">{{ freshness.label }} · {{ activeAsOf }}</span></header>

      <ZLMNodeContextBar :nodes="contextNodes" :query-node-id="route.query.nodeId" :loading="nodesLoading || loading" title="会话所在节点" @refresh="loadNodes(); refresh()" />

      <div v-if="loadError && (networkData || viewerData)" class="session-banner session-banner--warning" role="status">本次刷新失败：{{ errorPresentation.label }}。保留当前 Tab 的筛选、分页和上一次数据。</div>
      <div v-if="!hasKickPermission" class="session-banner" role="status">当前账号可查看会话，但没有 `gb28181:zlm:session:kick` 权限；踢除按钮不会显示。</div>

      <a-tabs v-model:active-key="activeTab" class="session-tabs">
        <a-tab-pane key="network" title="网络会话">
          <s-layout-search class="session-search">
            <template #fields><a-input-search v-model="networkFilter.peerIp" allow-clear placeholder="远端 IP" class="peer-filter" @search="queryNetwork" /><a-input v-model="networkFilter.localPort" allow-clear placeholder="本地端口" class="port-filter" @press-enter="queryNetwork" /></template>
            <template #actions><a-button type="primary" @click="queryNetwork">查询</a-button><a-button @click="resetNetwork">重置</a-button><a-button class="uvp-refresh-btn" :loading="loading" @click="refresh"><template #icon><icon-refresh /></template>刷新</a-button></template>
            <template #extra><span class="scope-note">只读网络连接口径，不猜测对应媒体流</span></template>
          </s-layout-search>

          <div v-if="loading && !networkData" class="session-state" role="status"><a-spin />正在加载网络会话…</div>
          <div v-else-if="loadError && !networkData" class="session-state session-state--error" role="alert"><ShieldAlert :size="36" /><strong>{{ errorPresentation.label }}</strong><a-button v-if="errorPresentation.retryable" @click="refresh">重新加载</a-button></div>
          <section v-else class="session-table-panel">
            <a-table :data="networkData?.list || []" :loading="loading" row-key="id" :pagination="false" class="uvp-data-table"><template #columns><a-table-column title="会话 ID" data-index="id" :width="220" /><a-table-column title="远端"><template #cell="{ record }">{{ record.peerIp }}:{{ record.peerPort }}</template></a-table-column><a-table-column title="本地"><template #cell="{ record }">{{ record.localIp }}:{{ record.localPort }}</template></a-table-column><a-table-column title="类型" data-index="type" /><a-table-column title="类型 ID" data-index="typeId" /><a-table-column title="可执行操作" :width="150"><template #cell><span class="muted">仅观测</span></template></a-table-column></template><template #empty><div class="session-empty"><Network :size="38" /><strong>没有符合筛选的网络会话</strong></div></template></a-table>
            <div class="session-pagination"><span>共 {{ networkData?.total ?? 0 }} 个网络会话<span v-if="networkData?.truncated">（后端已截断）</span></span><a-pagination :current="networkFilter.page" :page-size="networkFilter.pageSize" :total="networkData?.total ?? 0" show-page-size :page-size-options="[20, 50, 100]" @change="changeNetworkPage" @page-size-change="changeNetworkPageSize" /></div>
          </section>
        </a-tab-pane>

        <a-tab-pane key="viewers" title="媒体观看者">
          <s-layout-search class="session-search">
            <template #fields><a-input v-model="viewerFilter.schema" allow-clear placeholder="Schema" class="media-filter-short" /><a-input v-model="viewerFilter.vhost" allow-clear placeholder="VHost" class="media-filter-vhost" /><a-input v-model="viewerFilter.app" allow-clear placeholder="App" class="media-filter-app" /><a-input-search v-model="viewerFilter.stream" allow-clear placeholder="Stream" class="media-filter-stream" @search="queryViewers" /></template>
            <template #actions><a-button type="primary" @click="queryViewers">查询</a-button><a-button @click="resetViewers">重置</a-button><a-button class="uvp-refresh-btn" :loading="loading" :disabled="!viewerTarget" @click="refresh"><template #icon><icon-refresh /></template>刷新</a-button></template>
            <template #extra><span class="scope-note">需完整 MediaIdentity，踢除还需 kickable=true</span></template>
          </s-layout-search>

          <div v-if="!viewerTarget" class="session-state" role="status"><Radio :size="36" /><strong>请输入完整媒体身份</strong><span>Schema、VHost、App、Stream 缺一不可，页面不会猜默认值。</span></div>
          <div v-else-if="loading && !viewerData" class="session-state" role="status"><a-spin />正在加载媒体观看者…</div>
          <div v-else-if="loadError && !viewerData" class="session-state session-state--error" role="alert"><ShieldAlert :size="36" /><strong>{{ errorPresentation.label }}</strong></div>
          <section v-else class="session-table-panel">
            <a-table :data="viewerData?.list || []" :loading="loading" row-key="identifier" :pagination="false" class="uvp-data-table"><template #columns><a-table-column title="观看标识" data-index="identifier" :width="240" /><a-table-column title="远端"><template #cell="{ record }">{{ record.peerIp }}:{{ record.peerPort }}</template></a-table-column><a-table-column title="本地"><template #cell="{ record }">{{ record.localIp }}:{{ record.localPort }}</template></a-table-column><a-table-column title="类型" data-index="typeId" /><a-table-column title="操作" :width="150"><template #cell="{ record }"><a-button v-if="canKickViewer(record, hasKickPermission)" size="small" status="danger" @click="openKick(record)">踢除</a-button><span v-else class="muted">{{ record.kickable ? '无权限' : '不可踢除' }}</span></template></a-table-column></template><template #empty><div class="session-empty"><Users :size="38" /><strong>该媒体流当前没有观看者</strong></div></template></a-table>
            <div class="session-pagination"><span>共 {{ viewerData?.total ?? 0 }} 个观看者<span v-if="viewerData?.truncated">（后端已截断）</span></span><a-pagination :current="viewerPage" :page-size="viewerPageSize" :total="viewerData?.total ?? 0" show-page-size :page-size-options="[20, 50, 100]" @change="changeViewerPage" @page-size-change="changeViewerPageSize" /></div>
          </section>
        </a-tab-pane>
      </a-tabs>

      <div v-if="nodesLoading && !nodes.length" class="session-overlay" role="status"><a-spin />正在加载媒体节点…</div>
      <div v-else-if="nodesError && !nodes.length" class="session-overlay session-overlay--error" role="alert"><Server :size="36" /><strong>{{ errorPresentation.label }}</strong><a-button @click="loadNodes">重新加载</a-button></div>
      <div v-else-if="!nodes.length" class="session-overlay" role="status"><Server :size="36" /><strong>还没有可见媒体节点</strong></div>

      <ZLMSessionKickDialog v-model:visible="kickVisible" :node-name="selectedNodeName" :viewer="kickViewer" @done="kickDone" />
    </div>
  </div>
</template>

<style scoped>
.session-management { position: relative; box-sizing: border-box; height: 100%; padding: 4px 8px 24px; overflow: auto; color: var(--zlm-text-2); }.session-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 16px; }.session-header h1 { margin: 0; color: var(--zlm-text-1); font-size: 20px; }.session-header p { margin: 5px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.freshness { color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.freshness--warning { color: var(--zlm-warn-600); }.freshness--danger { color: var(--zlm-danger-600); }
.session-banner { margin-top: 12px; padding: 10px 14px; color: var(--zlm-text-2); background: var(--zlm-info-50); border: 1px solid var(--zlm-info-500); border-radius: var(--zlm-radius-md); font-size: var(--zlm-fs-caption); }.session-banner--warning { color: var(--zlm-warn-600); background: var(--zlm-warn-50); border-color: var(--zlm-warn-500); }.session-tabs { margin-top: 12px; }.session-search { margin-bottom: 14px; }.peer-filter { width: 220px; }.port-filter { width: 130px; }.media-filter-short { width: 100px; }.media-filter-vhost { width: 170px; }.media-filter-app { width: 130px; }.media-filter-stream { width: 190px; }.scope-note { color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }
.session-state, .session-overlay { display: flex; min-height: 280px; flex-direction: column; align-items: center; justify-content: center; gap: 10px; text-align: center; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); }.session-state strong, .session-overlay strong { color: var(--zlm-text-1); }.session-state--error, .session-overlay--error { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border-color: var(--zlm-danger-500); }.session-overlay { position: absolute; inset: 86px 8px 24px; z-index: 3; min-height: 320px; }
.session-table-panel { overflow: hidden; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); box-shadow: var(--uvp-panel-shadow); }.session-empty { display: flex; flex-direction: column; align-items: center; gap: 8px; padding: 48px 16px; color: var(--zlm-text-3); }.session-empty strong { color: var(--zlm-text-1); }.session-pagination { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 12px 16px; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); border-top: 1px solid var(--zlm-border); }.muted { color: var(--zlm-text-4); }
@media (max-width: 800px) { .session-header { flex-direction: column; }.peer-filter, .port-filter, .media-filter-short, .media-filter-vhost, .media-filter-app, .media-filter-stream { width: 100%; }.session-pagination { align-items: flex-start; flex-direction: column; } }
</style>
