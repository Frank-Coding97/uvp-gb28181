<script setup lang="ts">
import { computed } from "vue";
import type { DashboardHistoryEnvelope, DashboardHistoryData, PlayHistory, SIPHistory, TrafficHistory } from "@/api/home-dashboard-drilldown";
import MediaVChart from "@/views/gb28181/zlm/workbench/components/MediaVChart.vue";
import type { DashboardDrilldownMetric, DashboardHistoryRange } from "../../dashboardDrilldownState";
import { buildDashboardTrendSpec } from "./dashboardTrend";

const props = defineProps<{
  visible: boolean;
  metric: DashboardDrilldownMetric | null;
  range: DashboardHistoryRange;
  ranges: DashboardHistoryRange[];
  loading: boolean;
  stale: boolean;
  error: string;
  result: DashboardHistoryEnvelope<DashboardHistoryData> | null;
}>();
const emit = defineEmits<{ close: []; range: [value: DashboardHistoryRange]; retry: [] }>();

const title = computed(() => ({
  "sip-rpm": "实时 SIP RPM 趋势",
  "sip-today": "SIP 请求台账",
  "play-success-24h": "点播成功率台账",
  "media-traffic-today": "媒体流量台账"
})[props.metric ?? "sip-rpm"]);
const chartSpec = computed(() => props.metric && props.result ? buildDashboardTrendSpec(props.metric, props.result.data) : null);
const sip = computed(() => props.metric?.startsWith("sip-") ? props.result?.data as SIPHistory | undefined : undefined);
const play = computed(() => props.metric === "play-success-24h" ? props.result?.data as PlayHistory | undefined : undefined);
const traffic = computed(() => props.metric === "media-traffic-today" ? props.result?.data as TrafficHistory | undefined : undefined);
const coverageNotice = computed(() => {
  if (play.value?.summary.staleStarted) return `${formatNumber(play.value.summary.staleStarted)} 条点播记录超过最大超时仍未终态，成功率未将其计入分母。`;
  if (props.result?.coverage !== "partial" || traffic.value) return "";
  return "统计覆盖不完整，图中断点表示该时段数据未知。";
});
const chartStatus = computed(() => {
  if (props.error && !props.result) return "unavailable" as const;
  if (!props.result || props.result.status === "empty") return "empty" as const;
  return props.result.status === "partial" || props.stale ? "partial" as const : "ready" as const;
});
const summary = computed(() => {
  if (sip.value) return `滚动窗口 ${formatNumber(sip.value.rollingRequests)} 次请求，今日 ${formatNumber(sip.value.todayRequests)} 次`;
  if (play.value) return `终态样本 ${formatNumber(play.value.summary.attempts)}，成功 ${formatNumber(play.value.summary.success)}，失败 ${formatNumber(play.value.summary.failure)}${play.value.summary.staleStarted ? `，超时未终态 ${formatNumber(play.value.summary.staleStarted)}` : ""}`;
  if (traffic.value) return `Y 轴为每个时间桶累计流量；上行 ${formatBytes(traffic.value.summary.upstreamBytes)}，已结算下行 ${formatBytes(traffic.value.summary.downstreamBytes)}`;
  return "暂无下钻数据";
});

function formatNumber(value: number) { return value.toLocaleString("zh-CN"); }
function formatPercent(value: number | null) { return value == null ? "--" : `${(value * 100).toFixed(1)}%`; }
function formatBytes(value: number) {
  if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(1)} GB`;
  if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(1)} MB`;
  if (value >= 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${value} B`;
}
</script>

<template>
  <a-modal :visible="visible" modal-class="uvp-system-dialog" :title="title" :width="980" :footer="false" unmount-on-close @cancel="emit('close')">
    <div class="drilldown-dialog" :aria-busy="loading">
      <div class="drilldown-toolbar">
        <div class="drilldown-ranges" aria-label="时间范围">
          <button v-for="item in ranges" :key="item" type="button" :class="{ active: item === range }" @click="emit('range', item)">{{ item === "1h" ? "1 小时" : item === "24h" ? "24 小时" : "7 天" }}</button>
        </div>
        <button v-if="error" type="button" class="retry" @click="emit('retry')">重试</button>
      </div>
      <p v-if="error" class="drilldown-notice" :class="{ stale }" role="status">{{ stale ? `刷新失败，继续展示上次数据：${error}` : error }}</p>
      <p v-if="coverageNotice" class="drilldown-notice" role="status">{{ coverageNotice }}</p>
      <MediaVChart
        :class="{ 'traffic-chart': !!traffic }"
        title="趋势"
        :spec="chartSpec"
        :status="chartStatus"
        :status-text="loading && !result ? '正在加载趋势' : '当前范围暂无数据'"
        :summary="summary"
        :warning="stale ? '当前展示的是上次成功结果' : null"
        :as-of="traffic ? null : result?.asOf"
        :active="visible"
      />

      <section v-if="sip" class="ledger-section">
        <h3>方法与方向台账</h3>
        <div class="ledger-scroll"><table><thead><tr><th>方法</th><th>方向</th><th>请求</th><th>事务</th><th>成功</th><th>失败</th></tr></thead><tbody>
          <tr v-for="row in sip.ledger" :key="`${row.method}/${row.direction}`"><td>{{ row.method }}</td><td>{{ row.direction }}</td><td>{{ formatNumber(row.requests) }}</td><td>{{ formatNumber(row.transactions) }}</td><td>{{ formatNumber(row.success) }}</td><td>{{ formatNumber(row.failure) }}</td></tr>
        </tbody></table></div>
      </section>
      <section v-else-if="play" class="ledger-section">
        <h3>结果分布</h3>
        <div class="summary-grid"><span><small>成功率</small><strong>{{ formatPercent(play.summary.rate) }}</strong></span><span><small>进行中</small><strong>{{ formatNumber(play.summary.started) }}</strong></span><span v-if="play.summary.staleStarted"><small>超时未终态</small><strong>{{ formatNumber(play.summary.staleStarted) }}</strong></span><span v-for="row in play.failureStages" :key="row.key"><small>失败 · {{ row.key }}</small><strong>{{ formatNumber(row.count) }}</strong></span></div>
      </section>
    </div>
  </a-modal>
</template>

<style scoped>
.drilldown-dialog{display:grid;gap:14px;max-height:min(76vh,760px);overflow-y:auto;color:var(--uvp-text-primary)}
.drilldown-toolbar{display:flex;align-items:center;justify-content:space-between;gap:12px}.drilldown-ranges{display:flex;gap:6px}.drilldown-ranges button,.retry{padding:6px 12px;color:var(--uvp-text-secondary);cursor:pointer;background:var(--uvp-panel-bg);border:1px solid var(--uvp-panel-border);border-radius:7px}.drilldown-ranges button.active{color:#fff;background:var(--uvp-brand);border-color:var(--uvp-brand)}
.drilldown-notice{padding:8px 10px;margin:0;color:var(--uvp-warning);background:color-mix(in srgb,var(--uvp-warning) 10%,transparent);border-radius:7px}.drilldown-notice.stale{color:var(--uvp-danger)}
.traffic-chart :deep(.media-vchart__canvas){height:400px;min-height:400px}
.ledger-section{min-width:0}.ledger-section h3{margin:0 0 9px;font-size:14px}.ledger-scroll{overflow-x:auto;border:1px solid var(--uvp-panel-border);border-radius:8px}table{width:100%;border-collapse:collapse;font-size:12px}th,td{padding:9px 10px;text-align:left;border-bottom:1px solid var(--uvp-panel-border)}th{color:var(--uvp-text-secondary);background:var(--uvp-page-bg)}tbody tr:last-child td{border-bottom:0}.summary-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(130px,1fr));gap:8px}.summary-grid span{display:flex;flex-direction:column;gap:4px;padding:10px;background:var(--uvp-page-bg);border-radius:8px}.summary-grid small{color:var(--uvp-text-tertiary)}
@media(max-width:600px){.drilldown-dialog{max-height:80vh}.drilldown-toolbar{align-items:flex-start;flex-direction:column}.drilldown-ranges{width:100%}.drilldown-ranges button{flex:1;padding-inline:5px}.traffic-chart :deep(.media-vchart__canvas){height:300px;min-height:300px}.summary-grid{grid-template-columns:1fr 1fr}}
</style>
