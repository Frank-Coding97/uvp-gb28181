<script setup lang="ts">
import { computed, ref } from "vue";
import { useRouter } from "vue-router";
import { Activity, AlertTriangle, ArrowRight, Radio, Server, Users } from "lucide-vue-next";

import { getZLMOverview, type ZLMNodeRuntime, type ZLMOverview, type ZLMRuntimeMedia } from "@/api/gb28181-zlm-runtime";
import StatCard from "../../components/StatCard.vue";
import { formatZLMByteRate, formatZLMProtocol, zlmErrorPresentation, zlmFreshnessPresentation } from "../../components/zlmFormatters";
import { useZLMRuntimePolling } from "../../composables/useZLMRuntimePolling";
import MediaVChart from "../components/MediaVChart.vue";
import {
  buildOverviewChartState,
  createOverviewDistributionSpec,
  createOverviewHealthSpec,
  createOverviewNodeLoadSpec
} from "../chart/overviewChart";
import { nodeOverviewLocation, overviewHealthSummary, streamOverviewLocation } from "../../clusterOverviewState";
import { buildOverviewKpis, overviewKpiValueText } from "./overviewState";

const props = withDefaults(defineProps<{
  active?: boolean;
  autoRefresh?: boolean;
}>(), {
  active: true,
  autoRefresh: true
});

const router = useRouter();
const overview = ref<ZLMOverview | null>(null);
const loading = ref(false);
const loadError = ref<unknown>(null);
// The overview endpoint is cluster-wide. The polling controller still needs a
// positive scope key so it can provide cancellation and generation guards.
const singletonScope = ref<number | null>(1);
const active = computed(() => props.active);
const paused = computed(() => !props.autoRefresh);

const { refresh } = useZLMRuntimePolling<ZLMOverview>({
  nodeId: singletonScope,
  active,
  paused,
  intervalMs: 10_000,
  async load(_scope, signal) {
    loading.value = true;
    try {
      const response = await getZLMOverview(signal);
      if (response.code !== 0 || !response.data) throw new Error(response.message || "集群总览加载失败");
      return response.data;
    } finally {
      // Abort can intentionally bypass the polling error callback; never leave
      // the refresh affordance stuck in a loading state in that case.
      loading.value = false;
    }
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

defineExpose({ refresh });

const health = computed(() => overview.value ? overviewHealthSummary(overview.value) : null);
const chartState = computed(() => buildOverviewChartState(overview.value));
const kpis = computed(() => buildOverviewKpis(overview.value));
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value));
const freshness = computed(() => zlmFreshnessPresentation(overview.value?.asOf));
const visibleStreams = computed(() => overview.value?.streams.slice(0, 12) ?? []);
const mediaSampleKnown = computed(() => (overview.value?.mediaSampledNodeIds.length ?? 0) > 0);
const chartStatus = computed(() => chartState.value.status);
const chartStatusText = computed(() => {
  switch (chartStatus.value) {
    case "empty": return "尚未配置媒体节点";
    case "unavailable": return "当前没有可用节点运行态";
    case "unknown": return "当前尚未完成有效采样";
    case "partial": return "当前仅有部分节点完成采样";
    default: return "";
  }
});
const overallStatus = computed(() => {
  if (loadError.value && overview.value) return "stale" as const;
  if (loadError.value) return "error" as const;
  if (chartStatus.value === "partial") return "partial" as const;
  return "ready" as const;
});

function nodeStateText(node: ZLMNodeRuntime) {
  if (node.state === "maintenance") return "维护";
  if (node.state === "offline") return "离线";
  if (node.status === "unavailable") return "采集失败";
  if (node.status === "partial") return "部分数据";
  if (node.status === "fresh") return "在线";
  return "状态未知";
}

function nodeStateTone(node: ZLMNodeRuntime) {
  if (node.state === "offline" || node.status === "unavailable") return "danger";
  if (node.state === "maintenance" || node.status === "partial") return "warning";
  return "success";
}

function kpiValue(kpi: ReturnType<typeof buildOverviewKpis>[number]) {
  if (kpi.value === null) return undefined;
  return kpi.key === "throughput" ? undefined : kpi.value;
}

function kpiText(kpi: ReturnType<typeof buildOverviewKpis>[number]) {
  if (kpi.key === "throughput" && kpi.value !== null) return formatZLMByteRate(kpi.value);
  return overviewKpiValueText(kpi);
}

function kpiAccent(kpi: ReturnType<typeof buildOverviewKpis>[number]) {
  if (kpi.state === "unknown") return "warning" as const;
  if (kpi.key === "nodes") return "brand" as const;
  if (kpi.key === "streams" || kpi.key === "viewers") return "accent" as const;
  return "default" as const;
}

function gotoNode(nodeId: number) {
  void router.push(nodeOverviewLocation(nodeId));
}

function gotoStream(stream: ZLMRuntimeMedia) {
  void router.push(streamOverviewLocation(stream));
}

function gotoMonitoring(protocol?: string) {
  const query: Record<string, string> = { view: "streams" };
  if (protocol && protocol !== "未知协议") query.schema = protocol;
  void router.push({ path: "/media/monitoring", query });
}

function gotoRecordings() {
  void router.push({ path: "/media/recordings", query: { view: "files" } });
}

function gotoSchedulingFailures() {
  void router.push({ path: "/media/scheduling", query: { view: "logs", result: "failure" } });
}

function streamRowKey(stream: ZLMRuntimeMedia) {
  return [stream.nodeId, stream.media.schema, stream.media.vhost, stream.media.app, stream.media.stream].join("\u001f");
}

function bytesText(value: number | null) {
  return value === null ? "—" : formatZLMByteRate(value);
}
</script>

<template>
  <section class="media-overview-panel" :data-status="overallStatus" aria-label="媒体总览数据面板">
    <header class="panel-intro">
      <div>
        <div class="panel-eyebrow">REAL-TIME MEDIA OVERVIEW</div>
        <h2>集群运行态</h2>
        <p>来自 <code>GET /zlm/overview</code> 的统一快照；节点指标、媒体流与异常保持同一采样口径。</p>
      </div>
      <div class="panel-intro__meta">
        <span v-if="overview" :class="`freshness freshness--${freshness.tone}`" :title="freshness.description">
          {{ freshness.label }} · {{ overview.asOf }}
        </span>
        <button type="button" class="panel-refresh" :disabled="loading" aria-label="刷新媒体总览" @click="refresh">
          <Activity :size="15" :class="{ 'is-spinning': loading }" aria-hidden="true" />
          {{ loading ? "采集中" : "刷新" }}
        </button>
      </div>
    </header>

    <div v-if="health?.kind === 'partial'" class="status-banner status-banner--warning" role="status" :aria-label="health.accessibleLabel">
      <AlertTriangle :size="17" aria-hidden="true" />
      <span><strong>部分节点不可用</strong>：成功 {{ health.successfulCount }} 个，失败 {{ health.failedCount }} 个；当前仍展示已采集数据。</span>
    </div>
    <div v-else-if="health?.kind === 'unavailable'" class="status-banner status-banner--danger" role="alert" :aria-label="health.accessibleLabel">
      <AlertTriangle :size="17" aria-hidden="true" />
      <span><strong>集群运行态暂不可用</strong>：请检查节点连接和权限。</span>
    </div>
    <div v-else-if="health?.kind === 'inactive'" class="status-banner status-banner--warning" role="status" :aria-label="health.accessibleLabel">
      <AlertTriangle :size="17" aria-hidden="true" />
      <span><strong>没有活跃采样节点</strong>：运行指标以破折号和状态文字展示，不按 0 处理。</span>
    </div>
    <div v-else-if="loadError && overview" class="status-banner status-banner--warning" role="status">
      <AlertTriangle :size="17" aria-hidden="true" />
      <span><strong>本次刷新失败</strong>：{{ errorPresentation.label }}；已保留上一次成功数据。</span>
    </div>

    <div v-if="loading && !overview" class="overview-state" role="status" aria-label="正在加载媒体总览">
      <Activity :size="30" class="is-spinning" aria-hidden="true" />
      <strong>正在采集集群运行态…</strong>
      <span>首次采样完成后会显示节点健康与媒体分布。</span>
    </div>
    <div v-else-if="loadError && !overview" class="overview-state overview-state--error" role="alert">
      <Server :size="34" aria-hidden="true" />
      <strong>{{ errorPresentation.label }}</strong>
      <span>{{ errorPresentation.retryable ? "可刷新重试；当前未清空其他工作台。" : "请确认账号权限或重新登录。" }}</span>
      <button v-if="errorPresentation.retryable" type="button" class="primary-button" @click="refresh">重新加载</button>
    </div>
    <div v-else-if="chartState.status === 'empty'" class="overview-state" role="status" aria-label="尚未配置媒体节点">
      <Server :size="34" aria-hidden="true" />
      <strong>还没有媒体节点</strong>
      <span>添加节点并完成探测后，集群指标会出现在这里。</span>
      <button type="button" class="primary-button" @click="router.push('/media/nodes')">前往节点管理</button>
    </div>

    <template v-if="overview && overview.nodes.length">
      <div class="overview-kpis" aria-label="集群关键指标">
        <StatCard
          v-for="kpi in kpis"
          :key="kpi.key"
          :title="kpi.label"
          :value="kpiValue(kpi)"
          :value-text="kpiText(kpi)"
          :trend="kpi.note"
          :trend-type="kpi.state === 'unknown' ? 'danger' : kpi.state === 'partial' ? 'neutral' : 'neutral'"
          :accent="kpiAccent(kpi)"
        />
      </div>

      <div class="overview-chart-grid">
        <MediaVChart
          title="节点负载"
          :spec="createOverviewNodeLoadSpec(chartState)"
          :status="chartStatus"
          :status-text="chartStatusText"
          :summary="chartState.summary"
          :warning="chartState.warning"
          :as-of="chartState.asOf"
          :sampled-label="`指标采样 ${chartState.sampledNodeIds.length} / ${overview.nodes.length} 个节点`"
          :active="active"
        />
        <MediaVChart
          title="节点健康矩阵"
          :spec="createOverviewHealthSpec(chartState)"
          :status="chartStatus"
          :status-text="chartStatusText"
          :summary="`${chartState.summary}；维护、离线和失败节点保持显式状态。`"
          :warning="chartState.warning"
          :as-of="chartState.asOf"
          :sampled-label="`运行态快照 ${overview.asOf}`"
          :active="active"
        />
      </div>

      <section class="overview-section distribution-section" aria-labelledby="overview-distribution-title">
        <div class="section-heading">
          <div>
            <h3 id="overview-distribution-title">媒体分布</h3>
            <p>协议、来源和节点分布只统计已完成媒体采样的流；部分状态不会伪装成全量。</p>
          </div>
          <button type="button" class="text-button" @click="gotoMonitoring()">查看媒体监控 <ArrowRight :size="14" /></button>
        </div>
        <div class="distribution-layout">
          <MediaVChart
            title="协议 / 来源 / 节点"
            :spec="createOverviewDistributionSpec(chartState)"
            :status="chartStatus"
            :status-text="chartStatusText"
            :summary="chartState.summary"
            :warning="chartState.warning"
            :as-of="chartState.asOf"
            :sampled-label="`媒体采样 ${chartState.sampled.streamNodeIds.length} 个节点`"
            :active="active"
          />
          <div class="distribution-links" aria-label="协议分布下钻">
            <div class="distribution-links__heading">协议分布</div>
            <button
              v-for="item in chartState.protocolDistribution"
              :key="item.category"
              type="button"
              class="distribution-link"
              @click="gotoMonitoring(item.category)"
            >
              <span>{{ formatZLMProtocol(item.category) }}</span><strong>{{ item.count }}</strong>
            </button>
            <span v-if="!chartState.protocolDistribution.length" class="distribution-links__empty">未知，尚无媒体采样</span>
          </div>
        </div>
      </section>

      <section class="overview-section" aria-labelledby="overview-nodes-title">
        <div class="section-heading">
          <div>
            <h3 id="overview-nodes-title">节点健康</h3>
            <p>点击节点进入 canonical 节点详情；采集失败原因保留在当前快照中。</p>
          </div>
          <button type="button" class="text-button" @click="router.push('/media/nodes')">全部节点 <ArrowRight :size="14" /></button>
        </div>
        <div class="node-grid">
          <button
            v-for="node in overview.nodes"
            :key="node.nodeId"
            type="button"
            class="node-card"
            :data-tone="nodeStateTone(node)"
            :aria-label="`查看节点 ${node.name}，状态 ${nodeStateText(node)}`"
            @click="gotoNode(node.nodeId)"
          >
            <div class="node-card__header">
              <span class="node-card__name"><Server :size="15" aria-hidden="true" />{{ node.name }}</span>
              <span class="node-card__state"><i />{{ nodeStateText(node) }}</span>
            </div>
            <div class="node-card__meta">#{{ node.nodeId }} · {{ node.freshness }} · {{ node.asOf || "暂无采样" }}</div>
            <dl class="node-card__metrics">
              <div><dt>流</dt><dd>{{ node.mediaFreshness === 'unavailable' || node.streams === undefined ? '—' : node.streams.length }}</dd></div>
              <div><dt>会话</dt><dd>{{ node.metricsComplete ? node.metrics.networkSessionCount : '—' }}</dd></div>
              <div><dt>Net</dt><dd>{{ node.metricsComplete ? `${Math.round(node.metrics.netThreadLoad * 100)}%` : '—' }}</dd></div>
            </dl>
            <p v-if="node.error" class="node-card__error">{{ node.error.message }}</p>
          </button>
        </div>
      </section>

      <section class="overview-section stream-section" aria-labelledby="overview-streams-title">
        <div class="section-heading">
          <div>
            <h3 id="overview-streams-title">在线媒体流</h3>
            <p>最多展示当前快照前 12 条样本；完整筛选、分页和操作在媒体监控中完成。</p>
          </div>
          <span v-if="mediaSampleKnown">{{ visibleStreams.length }} / {{ overview.metrics.streamCount }}</span>
          <span v-else>未知</span>
        </div>
        <div v-if="mediaSampleKnown && visibleStreams.length" class="stream-table">
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
              <a-table-column title="节点" :width="100"><template #cell="{ record }">#{{ record.nodeId }}</template></a-table-column>
              <a-table-column title="协议" :width="110"><template #cell="{ record }">{{ formatZLMProtocol(record.media.schema) }}</template></a-table-column>
              <a-table-column title="读者" :width="100"><template #cell="{ record }"><Users :size="13" /> {{ record.readerCount }}</template></a-table-column>
              <a-table-column title="速率" :width="130"><template #cell="{ record }"><Activity :size="13" /> {{ bytesText(record.bytesSpeed) }}</template></a-table-column>
            </template>
          </a-table>
        </div>
        <div v-else-if="mediaSampleKnown" class="stream-empty">当前采样范围内没有在线媒体流。</div>
        <div v-else class="stream-empty stream-empty--unknown" role="status">媒体流采样未知，不能判断为 0；请刷新或检查节点状态。</div>
      </section>

      <section class="overview-shortcuts" aria-label="媒体运维快捷入口">
        <button type="button" class="shortcut-card" @click="gotoRecordings">
          <span class="shortcut-card__icon"><Radio :size="17" aria-hidden="true" /></span>
          <span><strong>录制中心</strong><small>查看文件与任务状态</small></span>
          <ArrowRight :size="15" aria-hidden="true" />
        </button>
        <button type="button" class="shortcut-card" @click="gotoSchedulingFailures">
          <span class="shortcut-card__icon shortcut-card__icon--warning"><AlertTriangle :size="17" aria-hidden="true" /></span>
          <span><strong>调度异常</strong><small>按失败结果筛查调度日志</small></span>
          <ArrowRight :size="15" aria-hidden="true" />
        </button>
      </section>
    </template>
  </section>
</template>

<style scoped>
.media-overview-panel { min-width: 0; padding: 2px 0 24px; color: var(--zlm-text-2); }
.panel-intro, .section-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.panel-intro { margin-bottom: 14px; padding: 16px 18px; background: linear-gradient(135deg, color-mix(in srgb, var(--zlm-brand-500) 6%, transparent), transparent 54%), var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); }
.panel-eyebrow { color: var(--zlm-brand-600); font-family: var(--zlm-font-mono); font-size: 10px; font-weight: var(--zlm-fw-semibold); letter-spacing: .13em; }
.panel-intro h2 { margin: 4px 0 0; color: var(--zlm-text-1); font-size: 18px; }
.panel-intro p { margin: 5px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); line-height: 1.6; }
.panel-intro code { padding: 1px 4px; color: var(--zlm-brand-600); background: var(--zlm-brand-50); border-radius: 4px; font-family: var(--zlm-font-mono); font-size: 10px; }
.panel-intro__meta { display: flex; flex: none; align-items: center; gap: 10px; }
.freshness { color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); white-space: nowrap; }
.freshness--warning { color: var(--zlm-warn-600); }
.freshness--danger { color: var(--zlm-danger-600); }
.panel-refresh, .text-button, .primary-button { display: inline-flex; align-items: center; gap: 6px; min-height: 32px; padding: 0 10px; color: var(--zlm-brand-600); background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-md); cursor: pointer; }
.panel-refresh:hover, .text-button:hover { border-color: var(--zlm-brand-500); background: var(--zlm-brand-50); }
.panel-refresh:disabled { cursor: wait; opacity: .65; }
.text-button { min-height: 28px; padding: 0 6px; border-color: transparent; background: transparent; white-space: nowrap; }
.primary-button { color: var(--zlm-card); background: var(--zlm-brand-600); border-color: var(--zlm-brand-600); }
.panel-refresh:focus-visible, .text-button:focus-visible, .primary-button:focus-visible, .node-card:focus-visible, .stream-link:focus-visible, .distribution-link:focus-visible, .shortcut-card:focus-visible { outline: 2px solid var(--zlm-brand-500); outline-offset: 2px; }
.status-banner { display: flex; align-items: center; gap: 8px; margin-bottom: 14px; padding: 10px 13px; border: 1px solid; border-radius: var(--zlm-radius-md); font-size: var(--zlm-fs-caption); line-height: 1.5; }
.status-banner--warning { color: var(--zlm-warn-600); background: var(--zlm-warn-50); border-color: var(--zlm-warn-500); }
.status-banner--danger { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border-color: var(--zlm-danger-500); }
.overview-state { display: flex; min-height: 300px; flex-direction: column; align-items: center; justify-content: center; gap: 10px; color: var(--zlm-text-3); text-align: center; background: var(--zlm-card); border: 1px dashed var(--zlm-border); border-radius: var(--zlm-radius-lg); }
.overview-state strong { color: var(--zlm-text-1); }
.overview-state--error { color: var(--zlm-danger-600); }
.overview-kpis { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: 10px; margin-bottom: 14px; }
.overview-chart-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.overview-section { margin-top: 14px; padding: 14px; background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); box-shadow: var(--uvp-panel-shadow); }
.section-heading { margin-bottom: 12px; }
.section-heading h3 { margin: 0; color: var(--zlm-text-1); font-size: 15px; }
.section-heading p { margin: 4px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); line-height: 1.5; }
.distribution-layout { display: grid; grid-template-columns: minmax(0, 1fr) 190px; gap: 12px; align-items: stretch; }
.distribution-links { min-width: 0; padding: 12px; background: var(--zlm-fill-1); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-md); }
.distribution-links__heading { margin-bottom: 8px; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }
.distribution-link { display: flex; width: 100%; align-items: center; justify-content: space-between; gap: 8px; padding: 8px 7px; color: var(--zlm-text-2); text-align: left; background: transparent; border: 0; border-radius: var(--zlm-radius-sm); cursor: pointer; }
.distribution-link:hover { color: var(--zlm-brand-600); background: var(--zlm-brand-50); }
.distribution-link strong { color: var(--zlm-text-1); font-family: var(--zlm-font-mono); }
.distribution-links__empty { display: block; color: var(--zlm-text-4); font-size: 11px; line-height: 1.5; }
.node-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; }
.node-card { appearance: none; width: 100%; min-width: 0; padding: 12px; color: inherit; text-align: left; background: var(--zlm-fill-1); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-md); cursor: pointer; transition: border-color .18s ease, transform .18s ease; }
.node-card:hover { transform: translateY(-1px); border-color: var(--zlm-brand-500); }
.node-card__header { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.node-card__name { display: inline-flex; min-width: 0; align-items: center; gap: 6px; overflow: hidden; color: var(--zlm-text-1); font-weight: var(--zlm-fw-semibold); text-overflow: ellipsis; white-space: nowrap; }
.node-card__state { display: inline-flex; flex: none; align-items: center; gap: 5px; color: var(--zlm-success-600); font-size: var(--zlm-fs-caption); }
.node-card__state i { width: 7px; height: 7px; border-radius: 50%; background: currentColor; }
.node-card[data-tone="warning"] .node-card__state { color: var(--zlm-warn-600); }
.node-card[data-tone="danger"] .node-card__state { color: var(--zlm-danger-600); }
.node-card__meta { margin-top: 5px; overflow: hidden; color: var(--zlm-text-4); font-family: var(--zlm-font-mono); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.node-card__metrics { display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; margin: 12px 0 0; }
.node-card__metrics div { min-width: 0; }
.node-card__metrics dt { color: var(--zlm-text-4); font-size: 10px; }
.node-card__metrics dd { margin: 3px 0 0; color: var(--zlm-text-1); font-family: var(--zlm-font-mono); font-size: 13px; font-weight: var(--zlm-fw-semibold); }
.node-card__error { margin: 9px 0 0 !important; color: var(--zlm-danger-600) !important; overflow-wrap: anywhere; }
.stream-table { overflow: hidden; border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-md); }
.stream-link { display: inline-flex; align-items: center; gap: 6px; max-width: 100%; padding: 0; color: var(--zlm-brand-600); background: transparent; border: 0; cursor: pointer; }
.stream-link span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.stream-sub { margin-top: 2px; color: var(--zlm-text-4); font-family: var(--zlm-font-mono); font-size: 10px; }
.stream-empty { padding: 30px 12px; color: var(--zlm-text-3); text-align: center; font-size: var(--zlm-fs-caption); }
.stream-empty--unknown { color: var(--zlm-warn-600); }
.overview-shortcuts { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; margin-top: 14px; }
.shortcut-card { display: flex; min-width: 0; align-items: center; gap: 10px; padding: 12px 14px; color: var(--zlm-text-2); text-align: left; background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); cursor: pointer; }
.shortcut-card:hover { border-color: var(--zlm-brand-500); }
.shortcut-card > span:nth-child(2) { display: flex; min-width: 0; flex: 1; flex-direction: column; gap: 2px; }
.shortcut-card strong { color: var(--zlm-text-1); font-size: 13px; }
.shortcut-card small { color: var(--zlm-text-3); font-size: 11px; }
.shortcut-card__icon { display: grid; width: 30px; height: 30px; flex: none; place-items: center; color: var(--zlm-brand-600); background: var(--zlm-brand-50); border-radius: var(--zlm-radius-md); }
.shortcut-card__icon--warning { color: var(--zlm-warn-600); background: var(--zlm-warn-50); }
.is-spinning { animation: media-overview-spin .8s linear infinite; }
@keyframes media-overview-spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) { .media-overview-panel *, .media-overview-panel *::before, .media-overview-panel *::after { animation-duration: .01ms !important; transition-duration: .01ms !important; } }
@media (max-width: 1260px) { .overview-kpis { grid-template-columns: repeat(3, minmax(0, 1fr)); } }
@media (max-width: 960px) { .overview-chart-grid, .distribution-layout { grid-template-columns: 1fr; } .node-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 640px) { .panel-intro, .section-heading { flex-direction: column; } .panel-intro__meta { width: 100%; align-items: flex-start; justify-content: space-between; } .overview-kpis, .node-grid, .overview-shortcuts { grid-template-columns: 1fr; } }
</style>
