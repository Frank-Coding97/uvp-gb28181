<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Activity, Clock3, Radio, Server, Users } from "lucide-vue-next";
import { listZLMNodes, type ZLMNode } from "@/api/gb28181-zlm";
import { getZLMNodeRuntime, type ZLMNodeRuntime } from "@/api/gb28181-zlm-runtime";
import { useZLMContextStore } from "@/store/modules/zlm-context";
import ZLMNodeContextBar from "./components/ZLMNodeContextBar.vue";
import StatCard from "./components/StatCard.vue";
import Sparkline from "./components/Sparkline.vue";
import { formatZLMByteRate, zlmErrorPresentation, zlmFreshnessPresentation } from "./components/zlmFormatters";
import { useZLMRuntimePolling } from "./composables/useZLMRuntimePolling";
import { appendRuntimeSample, runtimeTrendPoint, type RuntimeTrendPoint } from "./runtimeOverviewState";

const route = useRoute();
const router = useRouter();
const context = useZLMContextStore();
const nodes = ref<ZLMNode[]>([]);
const nodesLoading = ref(false);
const nodesError = ref<unknown>(null);
const runtime = ref<ZLMNodeRuntime | null>(null);
const history = ref<RuntimeTrendPoint[]>([]);
const loading = ref(true);
const loadError = ref<unknown>(null);

const selectedNodeId = computed(() => context.selectedNodeId);
const contextNodes = computed(() => nodes.value.map(node => ({ id: node.id, name: node.name, state: node.state })));
const sample = computed(() => runtime.value ? runtimeTrendPoint(runtime.value) : null);
const mediaAvailable = computed(() => runtime.value?.mediaFreshness !== "unavailable" && runtime.value?.streams !== undefined);
const metricsAvailable = computed(() => runtime.value?.metricsComplete === true);
const freshness = computed(() => zlmFreshnessPresentation(runtime.value?.asOf));
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value ?? nodesError.value));
const streamTrend = computed(() => history.value.map(point => point.streamCount));
const viewerTrend = computed(() => history.value.map(point => point.viewerCount));
const throughputTrend = computed(() => history.value.map(point => point.throughput));
const sessionTrend = computed(() => history.value.map(point => point.sessionCount));
const selectedState = computed(() => context.selectedNode?.state);

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

const { refresh } = useZLMRuntimePolling<ZLMNodeRuntime>({
  nodeId: selectedNodeId,
  intervalMs: 5_000,
  async load(nodeId, signal) {
    loading.value = true;
    const response = await getZLMNodeRuntime(nodeId, signal);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "节点运行态加载失败");
    if (response.data.nodeId !== nodeId) throw new Error("后端返回了错误的节点运行态");
    return response.data;
  },
  publish(value) {
    runtime.value = value;
    if (value.metricsComplete && value.mediaFreshness !== "unavailable" && value.streams !== undefined) {
      history.value = appendRuntimeSample(history.value, value);
    }
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
  runtime.value = null;
  history.value = [];
  loadError.value = null;
  loading.value = nodeId !== null;
  if (nodeId && previous !== null && route.query.nodeId !== String(nodeId)) {
    void router.replace({ query: { ...route.query, nodeId: String(nodeId) } });
  }
});

watch(() => context.trendRevision, () => {
  history.value = [];
});

function gotoStreams() {
  if (!selectedNodeId.value) return;
  void router.push({ path: "/gb28181/zlm/streams", query: { nodeId: String(selectedNodeId.value) } });
}

function gotoSessions() {
  if (!selectedNodeId.value) return;
  void router.push({ path: "/gb28181/zlm/sessions", query: { nodeId: String(selectedNodeId.value) } });
}

onMounted(loadNodes);
</script>

<template>
  <div class="snow-fill">
    <div class="snow-fill-inner uvp-page-shell-flat runtime-overview">
      <header class="runtime-overview__header">
        <div>
          <h1>运行总览</h1>
          <p>按节点查看媒体、会话、吞吐和线程指标；每个趋势最多保留当前节点最近 60 个采样点。</p>
        </div>
        <span v-if="runtime" class="sample-time"><Clock3 :size="14" />采样 {{ runtime.asOf }}</span>
      </header>

      <ZLMNodeContextBar
        :nodes="contextNodes"
        :query-node-id="route.query.nodeId"
        :loading="nodesLoading || loading"
        title="运行态节点"
        @refresh="loadNodes(); refresh()"
      />

      <div v-if="loadError && runtime" class="runtime-banner runtime-banner--warning" role="status">
        本次刷新失败：{{ errorPresentation.label }}。页面保留 {{ runtime.asOf }} 的上一次成功采样。
      </div>
      <div v-else-if="runtime?.status === 'partial'" class="runtime-banner runtime-banner--warning" role="status">
        节点只返回了部分运行态；缺失指标显示为破折号，不按 0 处理。
      </div>
      <div v-else-if="runtime?.status === 'maintenance'" class="runtime-banner runtime-banner--warning" role="status">
        当前节点处于维护状态，不承接新会话；历史数据仅供排查。
      </div>
      <div v-else-if="runtime?.status === 'unavailable'" class="runtime-banner runtime-banner--danger" role="alert">
        当前节点运行态不可用，请检查连接、节点生命周期和能力探测结果。
      </div>

      <div v-if="nodesLoading && !nodes.length" class="runtime-state" role="status" aria-label="正在加载媒体节点">
        <a-spin /><span>正在加载媒体节点…</span>
      </div>
      <div v-else-if="nodesError && !nodes.length" class="runtime-state runtime-state--error" role="alert">
        <Server :size="36" /><strong>{{ errorPresentation.label }}</strong><a-button @click="loadNodes">重新加载</a-button>
      </div>
      <div v-else-if="!nodes.length" class="runtime-state" role="status" aria-label="尚未配置媒体节点">
        <Server :size="36" /><strong>还没有可见的媒体节点</strong><span>先完成节点配置与权限分配。</span>
      </div>
      <div v-else-if="!selectedNodeId" class="runtime-state" role="status" aria-label="请选择媒体节点">
        <Server :size="36" /><strong>请选择一个媒体节点</strong>
      </div>
      <div v-else-if="loading && !runtime" class="runtime-state" role="status" aria-label="正在加载节点运行态">
        <a-spin /><span>正在采集节点运行态…</span>
      </div>
      <div v-else-if="loadError && !runtime" class="runtime-state runtime-state--error" role="alert">
        <Server :size="36" /><strong>{{ errorPresentation.label }}</strong>
        <span>{{ selectedState === 'offline' ? '所选节点已离线，保留选择用于排查。' : '可以刷新重试，其他节点不受影响。' }}</span>
        <a-button v-if="errorPresentation.retryable" @click="refresh">重新加载</a-button>
      </div>

      <template v-if="runtime && sample">
        <section class="runtime-kpis" aria-label="节点运行关键指标">
          <StatCard title="在线媒体流" :value="mediaAvailable ? sample.streamCount : undefined" unit="路" accent="brand" trend="点击查看完整流列表" @click="gotoStreams">
            <template #spark><Sparkline :data="streamTrend" color="brand" fill /></template>
          </StatCard>
          <StatCard title="媒体观看者" :value="mediaAvailable ? sample.viewerCount : undefined" unit="个" accent="accent" trend="各流 readerCount 合计">
            <template #spark><Sparkline :data="viewerTrend" color="accent" fill /></template>
          </StatCard>
          <StatCard title="媒体吞吐" :value-text="mediaAvailable ? formatZLMByteRate(sample.throughput) : '—'" trend="所有在线流 bytesSpeed 合计">
            <template #spark><Sparkline :data="throughputTrend" color="info" fill /></template>
          </StatCard>
          <StatCard title="网络会话" :value="metricsAvailable ? sample.sessionCount : undefined" unit="个" trend="点击查看网络会话" @click="gotoSessions">
            <template #spark><Sparkline :data="sessionTrend" color="warning" fill /></template>
          </StatCard>
          <StatCard title="NetThread 负载" :value="metricsAvailable ? sample.netThreadLoad : undefined" is-percent unit="%" trend="网络线程平均负载" :accent="sample.netThreadLoad >= .8 ? 'danger' : 'default'" />
          <StatCard title="WorkThread 负载" :value="metricsAvailable ? sample.workThreadLoad : undefined" is-percent unit="%" trend="工作线程平均负载" :accent="sample.workThreadLoad >= .8 ? 'danger' : 'default'" />
          <StatCard title="文件描述符 / Socket" :value="metricsAvailable ? sample.fdCount : undefined" unit="个" trend="ZLM socketCount 口径" />
          <StatCard title="正在录制" :value="mediaAvailable ? sample.recordingCount : undefined" unit="路" trend="MP4 或 HLS 录制标记" />
        </section>

        <section class="runtime-panel" aria-labelledby="runtime-stream-title">
          <div class="runtime-panel__heading">
            <div><h2 id="runtime-stream-title">当前媒体采样</h2><p>完整媒体身份和来源均来自后端运行态，不拼接 ZLM 地址或端口。</p></div>
            <a-button size="small" @click="gotoStreams">查看全部流</a-button>
          </div>
          <div v-if="!mediaAvailable" class="runtime-empty" role="status">媒体采样不可用，当前不能判断在线流是否为 0。</div>
          <div v-else-if="runtime.streams?.length" class="runtime-streams">
            <button
              v-for="stream in runtime.streams.slice(0, 8)"
              :key="`${stream.media.schema}/${stream.media.vhost}/${stream.media.app}/${stream.media.stream}`"
              type="button"
              class="runtime-stream"
              :aria-label="`查看流 ${stream.media.app}/${stream.media.stream}`"
              @click="router.push({ path: '/gb28181/zlm/streams', query: { nodeId: String(runtime.nodeId), ...stream.media } })"
            >
              <Radio :size="15" />
              <span><strong>{{ stream.media.app }}/{{ stream.media.stream }}</strong><small>{{ stream.media.schema }} · {{ stream.media.vhost }}</small></span>
              <span class="runtime-stream__right"><Users :size="13" />{{ stream.readerCount }}<Activity :size="13" />{{ formatZLMByteRate(stream.bytesSpeed) }}</span>
            </button>
          </div>
          <div v-else class="runtime-empty" role="status">后端确认当前节点没有在线媒体流。</div>
        </section>

        <footer class="runtime-footnote" :data-tone="freshness.tone" :title="freshness.description">
          {{ freshness.label }} · 已记录 {{ history.length }} / 60 个当前节点采样点
        </footer>
      </template>
    </div>
  </div>
</template>

<style scoped>
.runtime-overview { box-sizing: border-box; height: 100%; padding: 4px 8px 24px; overflow: auto; color: var(--zlm-text-2); }
.runtime-overview__header, .runtime-panel__heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.runtime-overview__header { margin-bottom: 16px; }
.runtime-overview h1, .runtime-overview h2 { margin: 0; color: var(--zlm-text-1); }
.runtime-overview h1 { font-size: 20px; }.runtime-overview h2 { font-size: 15px; }
.runtime-overview p { margin: 5px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); line-height: 1.6; }
.sample-time { display: inline-flex; align-items: center; gap: 6px; color: var(--zlm-text-3); font-family: var(--zlm-font-mono); font-size: var(--zlm-fs-caption); }
.runtime-banner { margin: 12px 0 0; padding: 10px 14px; border: 1px solid; border-radius: var(--zlm-radius-md); font-size: var(--zlm-fs-caption); }
.runtime-banner--warning { color: var(--zlm-warn-600); background: var(--zlm-warn-50); border-color: var(--zlm-warn-500); }
.runtime-banner--danger, .runtime-state--error { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border-color: var(--zlm-danger-500); }
.runtime-state { display: flex; min-height: 300px; flex-direction: column; align-items: center; justify-content: center; gap: 10px; margin-top: 16px; text-align: center; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); }
.runtime-state strong { color: var(--zlm-text-1); }
.runtime-kpis { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 16px; margin-top: 16px; }
.runtime-kpis :deep(.stat-card) { cursor: default; }
.runtime-kpis :deep(.stat-card:has(.trend)) { cursor: pointer; }
.runtime-panel { margin-top: 16px; padding: 16px; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); box-shadow: var(--uvp-panel-shadow); }
.runtime-panel__heading { margin-bottom: 12px; }
.runtime-streams { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
.runtime-stream { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 10px; min-width: 0; padding: 10px 12px; color: var(--zlm-text-2); text-align: left; background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-md); cursor: pointer; }
.runtime-stream:hover { border-color: var(--zlm-brand-500); }.runtime-stream:focus-visible { outline: 2px solid var(--zlm-brand-500); outline-offset: 2px; }
.runtime-stream > span { min-width: 0; }.runtime-stream strong, .runtime-stream small { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.runtime-stream strong { color: var(--zlm-text-1); }.runtime-stream small { margin-top: 3px; color: var(--zlm-text-4); font-family: var(--zlm-font-mono); }
.runtime-stream__right { display: inline-flex; align-items: center; gap: 5px; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }
.runtime-empty { padding: 36px 12px; color: var(--zlm-text-3); text-align: center; }
.runtime-footnote { margin-top: 12px; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); text-align: right; }
.runtime-footnote[data-tone="warning"] { color: var(--zlm-warn-600); }.runtime-footnote[data-tone="danger"] { color: var(--zlm-danger-600); }
@media (max-width: 1180px) { .runtime-kpis { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 760px) { .runtime-overview__header, .runtime-panel__heading { flex-direction: column; } .runtime-kpis, .runtime-streams { grid-template-columns: 1fr; } .runtime-stream__right { display: none; } }
</style>
