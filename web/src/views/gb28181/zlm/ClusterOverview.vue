<script setup lang="ts">
import { computed, ref } from "vue";
import { useRouter } from "vue-router";
import { Activity, ArrowRight, Radio, Server, Users } from "lucide-vue-next";
import { getZLMOverview, type ZLMOverview, type ZLMNodeRuntime, type ZLMRuntimeMedia } from "@/api/gb28181-zlm-runtime";
import { useZLMRuntimePolling } from "./composables/useZLMRuntimePolling";
import { formatZLMByteRate, formatZLMProtocol, zlmErrorPresentation, zlmFreshnessPresentation } from "./components/zlmFormatters";
import StatCard from "./components/StatCard.vue";
import { nodeOverviewLocation, overviewHealthSummary, streamOverviewLocation } from "./clusterOverviewState";

const router = useRouter();
const overview = ref<ZLMOverview | null>(null);
const loading = ref(true);
const loadError = ref<unknown>(null);
const singletonScope = ref<number | null>(1);

const health = computed(() => overview.value ? overviewHealthSummary(overview.value) : null);
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value));
const freshness = computed(() => zlmFreshnessPresentation(overview.value?.asOf));
const visibleStreams = computed(() => overview.value?.streams.slice(0, 12) ?? []);

const { refresh } = useZLMRuntimePolling<ZLMOverview>({
  nodeId: singletonScope,
  intervalMs: 10_000,
  async load(_scope, signal) {
    loading.value = true;
    const response = await getZLMOverview(signal);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "集群总览加载失败");
    return response.data;
  },
  publish(value) {
    overview.value = value;
    loadError.value = null;
    loading.value = false;
  },
  onError(error) {
    loadError.value = error;
    loading.value = false;
  }
});

function nodeStateText(node: ZLMNodeRuntime) {
  if (node.state === "maintenance") return "维护";
  if (node.state === "offline") return "离线";
  if (node.status === "unavailable") return "采集失败";
  if (node.status === "partial") return "部分数据";
  return "在线";
}

function nodeStateTone(node: ZLMNodeRuntime) {
  if (node.state === "offline" || node.status === "unavailable") return "danger";
  if (node.state === "maintenance" || node.status === "partial") return "warning";
  return "success";
}

function gotoNode(node: ZLMNodeRuntime) {
  void router.push(nodeOverviewLocation(node.nodeId));
}

function gotoStream(stream: ZLMRuntimeMedia) {
  void router.push(streamOverviewLocation(stream));
}

function streamRowKey(stream: ZLMRuntimeMedia) {
  return [stream.nodeId, stream.media.schema, stream.media.vhost, stream.media.app, stream.media.stream].join("\u001f");
}
</script>

<template>
  <div class="snow-fill">
    <div class="snow-fill-inner uvp-page-shell-flat cluster-overview">
      <header class="cluster-overview__header">
        <div>
          <h1>集群总览</h1>
          <p>聚合所有可见 ZLM 节点的运行态；单节点失败不会清空其他节点数据。</p>
        </div>
        <div class="cluster-overview__refresh">
          <span v-if="overview" :class="`freshness freshness--${freshness.tone}`" :title="freshness.description">
            {{ freshness.label }} · {{ overview.asOf }}
          </span>
          <a-button class="uvp-refresh-btn" :loading="loading" aria-label="刷新集群总览" @click="refresh">
            <template #icon><icon-refresh /></template>
            刷新
          </a-button>
        </div>
      </header>

      <div v-if="health?.kind === 'partial'" class="cluster-status cluster-status--warning" role="status" :aria-label="health.accessibleLabel">
        <strong>部分节点不可用</strong>
        <span>成功 {{ health.successfulCount }} 个，失败 {{ health.failedCount }} 个；当前仍展示已采集数据。</span>
      </div>
      <div v-else-if="health?.kind === 'unavailable'" class="cluster-status cluster-status--danger" role="alert" :aria-label="health.accessibleLabel">
        <strong>集群运行态暂不可用</strong>
        <span>{{ health.failedCount }} 个节点采集失败，请检查节点连接和权限。</span>
      </div>
      <div v-else-if="health?.kind === 'inactive'" class="cluster-status cluster-status--warning" role="status" :aria-label="health.accessibleLabel">
        <strong>没有活跃采样节点</strong>
        <span>节点均处于维护或离线状态；运行指标以破折号和状态文字展示，不按 0 处理。</span>
      </div>
      <div v-else-if="loadError && overview" class="cluster-status cluster-status--warning" role="status">
        <strong>本次刷新失败</strong>
        <span>{{ errorPresentation.label }}；保留上一次成功数据。</span>
      </div>

      <section v-if="overview" class="overview-kpis" aria-label="集群关键指标">
        <StatCard title="媒体节点" :value="overview.nodes.length" :trend="`${overview.metrics.sampledNodeCount} 个完成指标采样`" accent="brand" />
        <StatCard title="在线媒体流" :value="overview.metrics.streamCount" :trend="`${overview.mediaSampledNodeIds.length} 个节点完成媒体采样`" accent="accent" />
        <StatCard title="网络会话" :value="overview.metrics.networkSessionCount" trend="运行态网络会话总数" />
        <StatCard title="平均线程负载" :value="overview.metrics.netThreadLoadAvg" is-percent unit="%" trend="NetThread 平均值" :accent="overview.metrics.netThreadLoadAvg >= 0.8 ? 'danger' : 'default'" />
      </section>

      <div v-if="loading && !overview" class="overview-state" role="status" aria-label="正在加载集群总览">
        <a-spin />
        <span>正在采集集群运行态…</span>
      </div>
      <div v-else-if="loadError && !overview" class="overview-state overview-state--error" role="alert">
        <Server :size="34" aria-hidden="true" />
        <strong>{{ errorPresentation.label }}</strong>
        <span>{{ errorPresentation.retryable ? "可刷新重试，其他管理页面不会受影响。" : "请确认账号权限或重新登录。" }}</span>
        <a-button v-if="errorPresentation.retryable" @click="refresh">重新加载</a-button>
      </div>
      <div v-else-if="health?.kind === 'empty'" class="overview-state" role="status" aria-label="尚未配置媒体节点">
        <Server :size="34" aria-hidden="true" />
        <strong>还没有媒体节点</strong>
        <span>添加节点并完成探测后，集群指标会出现在这里。</span>
        <a-button type="primary" @click="router.push('/gb28181/zlm/nodes')">前往节点管理</a-button>
      </div>

      <template v-if="overview && overview.nodes.length">
        <section class="overview-section" aria-labelledby="cluster-nodes-title">
          <div class="section-heading">
            <div>
              <h2 id="cluster-nodes-title">节点运行态</h2>
              <p>节点状态、采样口径和运行指标分别展示，离线或维护不会被伪装为 0。</p>
            </div>
            <a-link @click="router.push('/gb28181/zlm/nodes')">全部节点 <ArrowRight :size="14" /></a-link>
          </div>
          <div class="node-grid">
            <button
              v-for="node in overview.nodes"
              :key="node.nodeId"
              type="button"
              class="node-card"
              :data-tone="nodeStateTone(node)"
              :aria-label="`查看节点 ${node.name}，状态 ${nodeStateText(node)}`"
              @click="gotoNode(node)"
            >
              <div class="node-card__header">
                <span class="node-card__name">{{ node.name }}</span>
                <span class="node-card__state"><i />{{ nodeStateText(node) }}</span>
              </div>
              <div class="node-card__meta">#{{ node.nodeId }} · {{ node.freshness }} · {{ node.asOf || "暂无采样" }}</div>
              <dl class="node-card__metrics">
                <div><dt>流</dt><dd>{{ node.mediaFreshness === 'unavailable' ? '—' : node.streams?.length ?? 0 }}</dd></div>
                <div><dt>会话</dt><dd>{{ node.metricsComplete ? node.metrics.networkSessionCount : '—' }}</dd></div>
                <div><dt>Net</dt><dd>{{ node.metricsComplete ? `${Math.round(node.metrics.netThreadLoad * 100)}%` : '—' }}</dd></div>
              </dl>
              <p v-if="node.error" class="node-card__error">{{ node.error.message }}</p>
            </button>
          </div>
        </section>

        <section class="overview-section" aria-labelledby="cluster-streams-title">
          <div class="section-heading">
            <div>
              <h2 id="cluster-streams-title">流分布</h2>
              <p>展示最近采样到的媒体身份；点击后携带完整 nodeId/schema/vhost/app/stream 进入流管理。</p>
            </div>
            <span>{{ visibleStreams.length }} / {{ overview.streams.length }}</span>
          </div>
          <div v-if="visibleStreams.length" class="stream-table">
            <a-table :data="visibleStreams" :pagination="false" :row-key="streamRowKey" size="small" class="uvp-data-table">
              <template #columns>
                <a-table-column title="媒体身份">
                  <template #cell="{ record }">
                    <button type="button" class="stream-link" :aria-label="`查看流 ${record.media.app}/${record.media.stream}`" @click="gotoStream(record)">
                      <Radio :size="14" aria-hidden="true" />
                      <span>{{ record.media.app }}/{{ record.media.stream }}</span>
                    </button>
                    <div class="stream-sub">{{ record.media.vhost }}</div>
                  </template>
                </a-table-column>
                <a-table-column title="节点" :width="110"><template #cell="{ record }">#{{ record.nodeId }}</template></a-table-column>
                <a-table-column title="协议" :width="110"><template #cell="{ record }">{{ formatZLMProtocol(record.media.schema) }}</template></a-table-column>
                <a-table-column title="读者" :width="100"><template #cell="{ record }"><Users :size="13" /> {{ record.readerCount }}</template></a-table-column>
                <a-table-column title="速率" :width="130"><template #cell="{ record }"><Activity :size="13" /> {{ formatZLMByteRate(record.bytesSpeed) }}</template></a-table-column>
              </template>
            </a-table>
          </div>
          <div v-else class="stream-empty">当前采样范围内没有在线媒体流。</div>
        </section>
      </template>
    </div>
  </div>
</template>

<style scoped>
.cluster-overview { height: 100%; overflow: auto; box-sizing: border-box; padding: 4px 8px 24px; color: var(--zlm-text-2); }
.cluster-overview__header, .section-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.cluster-overview__header { margin-bottom: 16px; }
.cluster-overview h1, .cluster-overview h2 { margin: 0; color: var(--zlm-text-1); }
.cluster-overview h1 { font-size: 20px; }
.cluster-overview h2 { font-size: 15px; }
.cluster-overview p { margin: 5px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); line-height: 1.6; }
.cluster-overview__refresh { display: flex; align-items: center; gap: 10px; }
.freshness { font-size: var(--zlm-fs-caption); color: var(--zlm-text-3); }
.freshness--warning { color: var(--zlm-warn-600); }
.freshness--danger { color: var(--zlm-danger-600); }
.cluster-status { display: flex; gap: 12px; align-items: center; margin-bottom: 16px; padding: 10px 14px; border: 1px solid; border-radius: var(--zlm-radius-md); font-size: var(--zlm-fs-caption); }
.cluster-status--warning { color: var(--zlm-warn-600); background: var(--zlm-warn-50); border-color: var(--zlm-warn-500); }
.cluster-status--danger { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border-color: var(--zlm-danger-500); }
.overview-kpis { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 16px; margin-bottom: 16px; }
.overview-state { min-height: 280px; display: flex; flex-direction: column; justify-content: center; align-items: center; gap: 10px; color: var(--zlm-text-3); text-align: center; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); }
.overview-state strong { color: var(--zlm-text-1); }
.overview-state--error { color: var(--zlm-danger-600); }
.overview-section { margin-top: 16px; padding: 16px; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); box-shadow: var(--uvp-panel-shadow); }
.section-heading { margin-bottom: 14px; }
.section-heading > span { color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }
.node-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
.node-card { appearance: none; width: 100%; min-width: 0; padding: 14px; text-align: left; color: inherit; background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); cursor: pointer; transition: border-color .18s ease, transform .18s ease; }
.node-card:hover { transform: translateY(-1px); border-color: var(--zlm-brand-500); }
.node-card:focus-visible, .stream-link:focus-visible { outline: 2px solid var(--zlm-brand-500); outline-offset: 2px; }
.node-card__header { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.node-card__name { overflow: hidden; color: var(--zlm-text-1); font-weight: var(--zlm-fw-semibold); text-overflow: ellipsis; white-space: nowrap; }
.node-card__state { display: inline-flex; align-items: center; gap: 5px; font-size: var(--zlm-fs-caption); }
.node-card__state i { width: 7px; height: 7px; border-radius: 50%; background: var(--zlm-success-600); }
.node-card[data-tone="warning"] .node-card__state { color: var(--zlm-warn-600); }
.node-card[data-tone="warning"] .node-card__state i { background: var(--zlm-warn-600); }
.node-card[data-tone="danger"] .node-card__state { color: var(--zlm-danger-600); }
.node-card[data-tone="danger"] .node-card__state i { background: var(--zlm-danger-600); }
.node-card__meta { margin-top: 5px; overflow: hidden; color: var(--zlm-text-4); font-family: var(--zlm-font-mono); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.node-card__metrics { display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; margin: 14px 0 0; }
.node-card__metrics div { min-width: 0; }
.node-card__metrics dt { color: var(--zlm-text-4); font-size: 11px; }
.node-card__metrics dd { margin: 3px 0 0; color: var(--zlm-text-1); font-family: var(--zlm-font-mono); font-weight: var(--zlm-fw-semibold); }
.node-card__error { color: var(--zlm-danger-600) !important; overflow-wrap: anywhere; }
.stream-table { overflow: hidden; border: 1px solid var(--uvp-panel-border); border-radius: var(--zlm-radius-md); }
.stream-link { display: inline-flex; align-items: center; gap: 6px; max-width: 100%; padding: 0; color: var(--zlm-brand-600); background: none; border: 0; cursor: pointer; }
.stream-link span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.stream-sub { margin-top: 2px; color: var(--zlm-text-4); font-family: var(--zlm-font-mono); font-size: 11px; }
.stream-empty { padding: 36px 12px; color: var(--zlm-text-3); text-align: center; }
:deep(.arco-table-cell) { min-width: 0; }
:deep(.arco-table-cell svg) { display: inline; vertical-align: -2px; }
@media (max-width: 1100px) { .overview-kpis { grid-template-columns: repeat(2, minmax(0, 1fr)); } .node-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 720px) { .cluster-overview__header, .section-heading, .cluster-status { flex-direction: column; } .cluster-overview__refresh { align-items: flex-start; flex-wrap: wrap; } .overview-kpis, .node-grid { grid-template-columns: 1fr; } }
</style>
