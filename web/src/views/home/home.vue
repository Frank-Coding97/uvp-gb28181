<template>
  <div class="snow-page dashboard-shell">
    <main class="dashboard-page" aria-label="GB28181 视频平台仪表盘">
      <header class="dashboard-header">
        <div class="dashboard-title">
          <span class="dashboard-title__icon" aria-hidden="true"><Gauge :size="21" /></span>
          <div class="dashboard-title__copy">
            <div class="dashboard-title__eyebrow">
              <span>GB28181 视频接入平台</span>
              <span class="dashboard-title__scope"><Eye :size="12" aria-hidden="true" />当前账号数据范围</span>
            </div>
            <h1>仪表盘</h1>
          </div>
        </div>
        <div class="dashboard-header__tools">
          <div class="service-cluster" aria-label="核心服务状态">
            <button
              class="service-chip"
              :class="`service-chip--${sipStatus.tone}`"
              type="button"
              :disabled="sipResource.status === 'forbidden'"
              :title="sipStatus.detail || '查看 SIP 接入配置'"
              @click="navigate('/gb28181/sip/platform')"
            >
              <span class="status-dot" :class="`status-dot--${sipStatus.tone}`" aria-hidden="true" />
              <span class="service-chip__copy"><small>SIP 服务</small><strong>{{ sipStatus.label }}</strong></span>
              <ChevronRight :size="13" aria-hidden="true" />
            </button>
            <button
              class="service-chip"
              :class="`service-chip--${zlmStatus.tone}`"
              type="button"
              :disabled="zlmResource.status === 'forbidden'"
              :title="zlmStatus.detail || '查看流媒体节点'"
              @click="navigate('/gb28181/zlm/nodes')"
            >
              <span class="status-dot" :class="`status-dot--${zlmStatus.tone}`" aria-hidden="true" />
              <span class="service-chip__copy"><small>ZLM 媒体</small><strong>{{ zlmStatus.label }}</strong></span>
              <ChevronRight :size="13" aria-hidden="true" />
            </button>
          </div>
          <div class="last-updated" aria-live="polite">
            <Clock :size="13" aria-hidden="true" />
            <span>最后刷新</span>
            <strong>{{ lastUpdatedText }}</strong>
          </div>
          <button
            class="refresh-button uvp-refresh-btn"
            type="button"
            :disabled="refreshing"
            :aria-label="refreshing ? '正在刷新仪表盘' : '刷新仪表盘'"
            :title="refreshing ? '刷新中' : '刷新仪表盘'"
            @click="refreshDashboard"
          >
            <RefreshCw :size="16" :class="{ spin: refreshing }" aria-hidden="true" />
          </button>
        </div>
      </header>

      <section class="metric-grid" aria-label="接入与治理指标">
        <button
          v-for="card in metricCards"
          :key="card.key"
          class="metric-card"
          :class="[`metric-card--${card.tone}`, { 'metric-card--loading': card.state === 'loading' }]"
          type="button"
          :disabled="card.state === 'forbidden'"
          @click="navigate(card.route)"
        >
          <div class="metric-card__top">
            <span class="metric-card__icon" aria-hidden="true"><component :is="card.icon" :size="19" /></span>
            <span class="metric-card__label">{{ card.label }}</span>
            <ChevronRight class="metric-card__arrow" :size="15" aria-hidden="true" />
          </div>
          <div class="metric-card__value-row">
            <div class="metric-card__value" aria-live="polite">{{ metricText(card.state, card.value) }}</div>
            <div v-if="card.state === 'ready' && card.ratio != null" class="metric-card__ratio">
              <strong>{{ card.ratio }}%</strong>
              <span>在线占比</span>
            </div>
            <span
              v-else-if="card.state === 'ready' && card.signal"
              class="metric-card__signal"
              :class="`metric-card__signal--${card.tone}`"
            >
              {{ card.signal }}
            </span>
          </div>
          <div v-if="card.state === 'ready'" class="metric-card__details">
            <template v-if="card.breakdown">
              <span><i class="detail-dot detail-dot--success" />在线 {{ card.breakdown.online.toLocaleString("zh-CN") }}</span>
              <span><i class="detail-dot detail-dot--muted" />离线 {{ card.breakdown.offline.toLocaleString("zh-CN") }}</span>
            </template>
            <span v-else>{{ card.detail }}</span>
          </div>
          <div v-else class="metric-card__state" :class="`metric-card__state--${card.state}`">
            {{ loadStateLabel(card.state) }}
          </div>
          <div v-if="card.state === 'ready' && card.breakdown && card.ratio != null" class="metric-card__rail" aria-hidden="true">
            <span :style="{ width: `${card.ratio}%` }" />
          </div>
        </button>
      </section>

      <section class="dashboard-content" aria-label="实时运行详情">
        <div class="dashboard-sip">
          <SipDashboardCard v-if="canViewSip" :key="sipCardKey" />
          <div v-else class="sip-unavailable" role="status">
            <ShieldAlert :size="28" aria-hidden="true" />
            <strong>无权查看 SIP 协议监控</strong>
            <span>当前账号未授予 SIP 配置查看权限</span>
          </div>
        </div>

        <aside class="attention-panel" aria-label="需关注事项">
          <header class="attention-panel__header">
            <div>
              <h2>需关注事项</h2>
              <p>当前账号数据范围</p>
            </div>
            <span v-if="attentionItems.length" class="attention-count">{{ attentionItems.length }} 类</span>
          </header>
          <div v-if="attentionLoading" class="attention-loading" aria-live="polite">
            <span v-for="index in 4" :key="index" />
          </div>
          <div v-else-if="attentionItems.length" class="attention-list">
            <button
              v-for="item in attentionItems"
              :key="item.id"
              class="attention-item"
              type="button"
              @click="navigate(item.route)"
            >
              <span class="attention-item__icon" :class="`attention-item__icon--${item.tone}`" aria-hidden="true">
                <TriangleAlert :size="16" />
              </span>
              <span class="attention-item__content">
                <strong>{{ item.title }}</strong>
                <small>{{ item.detail }}</small>
              </span>
              <ChevronRight :size="15" aria-hidden="true" />
            </button>
          </div>
          <div v-else class="attention-empty" role="status">
            <CircleCheck :size="30" aria-hidden="true" />
            <strong>{{ hasAvailableAttentionSource ? "当前没有需要处理的事项" : "暂无可用数据" }}</strong>
            <span>{{ hasAvailableAttentionSource ? "已加载的数据范围内未发现异常" : "请稍后刷新或检查访问权限" }}</span>
          </div>
          <footer v-if="zlmResource.status === 'ready' && zlmSummary.total > 0" class="media-facts">
            <span
              ><Server :size="14" aria-hidden="true" />媒体源
              <strong>{{ zlmSummary.mediaSources.toLocaleString("zh-CN") }}</strong></span
            >
            <span
              ><RadioTower :size="14" aria-hidden="true" />媒体会话
              <strong>{{ zlmSummary.sessions.toLocaleString("zh-CN") }}</strong></span
            >
          </footer>
        </aside>
      </section>
    </main>
  </div>
</template>

<script setup lang="ts">
import dayjs from "dayjs";
import { computed, markRaw, onMounted, ref, type Component } from "vue";
import { useRouter } from "vue-router";
import {
  BellRing,
  ChevronRight,
  CircleCheck,
  Clock,
  Eye,
  FolderTree,
  Gauge,
  RadioTower,
  RefreshCw,
  Server,
  ShieldAlert,
  TriangleAlert,
  Video
} from "lucide-vue-next";
import { fetchSipSetupStatus, type SipRuntimeStatus } from "@/api/gb28181";
import { listZLMNodes, type ZLMNode } from "@/api/gb28181-zlm";
import { getAnomalyCount, listChannels, listDevices } from "@/views/gb28181/device-mgmt/api";
import { listAlarms, type AlarmListItem } from "@/views/gb28181/alarm-management/api";
import SipDashboardCard from "./components/sip-dashboard/index.vue";
import {
  buildAttentionItems,
  classifyDashboardError,
  describeSipRuntime,
  loadStateLabel,
  metricText,
  ratioPercent,
  resolveDashboardRoute,
  summarizeZlmNodes,
  type DashboardLoadState,
  type DashboardServiceStatus
} from "./dashboardState";

defineOptions({ name: "Home" });

interface Resource<T> {
  status: DashboardLoadState;
  data: T | null;
}

interface DeviceSummary {
  total: number;
  online: number;
  offline: number;
}

type ChannelSummary = DeviceSummary;

interface AlarmSummary {
  total: number;
  latest: AlarmListItem | null;
}

interface MetricCard {
  key: string;
  label: string;
  icon: Component;
  state: DashboardLoadState;
  value: number | null;
  detail: string;
  breakdown?: { online: number; offline: number };
  ratio?: number | null;
  signal?: string;
  tone: "brand" | "warning" | "danger";
  route: string;
}

const router = useRouter();
const refreshing = ref(false);
const loadedOnce = ref(false);
const lastUpdatedAt = ref<Date | null>(null);
const sipCardKey = ref(0);

const devicesResource = ref<Resource<DeviceSummary>>({ status: "loading", data: null });
const channelsResource = ref<Resource<ChannelSummary>>({ status: "loading", data: null });
const anomaliesResource = ref<Resource<number>>({ status: "loading", data: null });
const alarmsResource = ref<Resource<AlarmSummary>>({ status: "loading", data: null });
const sipResource = ref<Resource<SipRuntimeStatus>>({ status: "loading", data: null });
const zlmResource = ref<Resource<ZLMNode[]>>({ status: "loading", data: null });

const deviceIcon = markRaw(RadioTower);
const channelIcon = markRaw(Video);
const anomalyIcon = markRaw(FolderTree);
const alarmIcon = markRaw(BellRing);

const deviceManagementRoute = resolveDashboardRoute(router.getRoutes(), [
  "/gb28181/device-mgmt/index",
  "/gb28181/device-mgmt"
]);
const directoryAnomalyRoute = resolveDashboardRoute(router.getRoutes(), [
  "/gb28181/device-mgmt/anomaly",
  "/gb28181/device-mgmt/anomaly/index",
  "/gb28181/device-mgmt/index",
  "/gb28181/device-mgmt"
]);
const canViewDevices = deviceManagementRoute !== null;
const canViewAlarms = resolveDashboardRoute(router.getRoutes(), ["/gb28181/alarm-management"]) !== null;
const canViewSip = resolveDashboardRoute(router.getRoutes(), ["/gb28181/sip/platform"]) !== null;
const canViewZlm = resolveDashboardRoute(router.getRoutes(), ["/gb28181/zlm/nodes"]) !== null;

const deviceManagementTarget = deviceManagementRoute ?? "/gb28181/device-mgmt/index";
const directoryAnomalyTarget = directoryAnomalyRoute ?? deviceManagementTarget;

const zlmSummary = computed(() => summarizeZlmNodes(zlmResource.value.data ?? []));

const sipStatus = computed<DashboardServiceStatus>(() => {
  if (sipResource.value.status !== "ready" || !sipResource.value.data) {
    return {
      label: loadStateLabel(sipResource.value.status),
      detail: "",
      tone: sipResource.value.status === "error" ? "danger" : "neutral"
    };
  }
  return describeSipRuntime(sipResource.value.data);
});

const zlmStatus = computed<DashboardServiceStatus>(() => {
  if (zlmResource.value.status !== "ready") {
    return {
      label: loadStateLabel(zlmResource.value.status),
      detail: "",
      tone: zlmResource.value.status === "error" ? "danger" : "neutral"
    };
  }
  const summary = zlmSummary.value;
  if (summary.total === 0) return { label: "未配置", detail: "", tone: "neutral" };
  const detail = `${summary.active}/${summary.total} 节点在线 · ${summary.mediaSources} 媒体源 · ${summary.sessions} 会话`;
  if (summary.offline > 0) return { label: "存在离线", detail, tone: "danger" };
  if (summary.maintenance > 0) return { label: "部分维护", detail, tone: "warning" };
  if (summary.nearCapacity > 0) return { label: "容量预警", detail, tone: "warning" };
  return { label: "运行正常", detail, tone: "success" };
});

const metricCards = computed<MetricCard[]>(() => [
  {
    key: "devices",
    label: "接入设备",
    icon: deviceIcon,
    state: devicesResource.value.status,
    value: devicesResource.value.data?.total ?? null,
    detail: "当前账号可见设备",
    breakdown: devicesResource.value.data
      ? { online: devicesResource.value.data.online, offline: devicesResource.value.data.offline }
      : undefined,
    ratio: devicesResource.value.data
      ? ratioPercent(devicesResource.value.data.online, devicesResource.value.data.total)
      : null,
    tone: "brand",
    route: deviceManagementTarget
  },
  {
    key: "channels",
    label: "视频通道",
    icon: channelIcon,
    state: channelsResource.value.status,
    value: channelsResource.value.data?.total ?? null,
    detail: "当前账号可见通道",
    breakdown: channelsResource.value.data
      ? { online: channelsResource.value.data.online, offline: channelsResource.value.data.offline }
      : undefined,
    ratio: channelsResource.value.data
      ? ratioPercent(channelsResource.value.data.online, channelsResource.value.data.total)
      : null,
    tone: "brand",
    route: deviceManagementTarget
  },
  {
    key: "anomalies",
    label: "目录异常",
    icon: anomalyIcon,
    state: anomaliesResource.value.status,
    value: anomaliesResource.value.data,
    detail: "未处理目录结构问题",
    signal: (anomaliesResource.value.data ?? 0) > 0 ? "需处理" : "无异常",
    tone: (anomaliesResource.value.data ?? 0) > 0 ? "warning" : "brand",
    route: directoryAnomalyTarget
  },
  {
    key: "alarms",
    label: "近 24 小时告警",
    icon: alarmIcon,
    state: alarmsResource.value.status,
    value: alarmsResource.value.data?.total ?? null,
    detail: "设备上报业务告警",
    signal: (alarmsResource.value.data?.total ?? 0) > 0 ? "已上报" : "无告警",
    tone: (alarmsResource.value.data?.total ?? 0) > 0 ? "danger" : "brand",
    route: "/gb28181/alarm-management"
  }
]);

const latestAlarmDescription = computed(() => {
  const latest = alarmsResource.value.data?.latest;
  if (!latest) return "";
  const entity = latest.channel ?? latest.device;
  const entityName = entity?.alias?.trim() || entity?.name?.trim() || entity?.code?.trim() || latest.sourceCode;
  const description = latest.description?.trim() || latest.alarmType?.label || "设备告警";
  return entityName ? `${entityName} · ${description}` : description;
});

const attentionItems = computed(() =>
  buildAttentionItems({
    sip: {
      status: sipResource.value.status,
      state: sipResource.value.data?.state,
      errorSummary: sipResource.value.data?.errorSummary
    },
    devices: { status: devicesResource.value.status, offline: devicesResource.value.data?.offline },
    channels: { status: channelsResource.value.status, offline: channelsResource.value.data?.offline },
    anomalies: { status: anomaliesResource.value.status, count: anomaliesResource.value.data ?? undefined },
    alarms: {
      status: alarmsResource.value.status,
      total: alarmsResource.value.data?.total,
      latestDescription: latestAlarmDescription.value
    },
    zlm: { status: zlmResource.value.status, nodes: zlmResource.value.data ?? undefined }
  }, {
    deviceManagement: deviceManagementTarget,
    directoryAnomaly: directoryAnomalyTarget
  })
);

const resources = [devicesResource, channelsResource, anomaliesResource, alarmsResource, sipResource, zlmResource];
const attentionLoading = computed(
  () => resources.some(resource => resource.value.status === "loading") && attentionItems.value.length === 0
);
const hasAvailableAttentionSource = computed(() => resources.some(resource => resource.value.status === "ready"));
const lastUpdatedText = computed(() => (lastUpdatedAt.value ? dayjs(lastUpdatedAt.value).format("HH:mm:ss") : "--"));

function resetResources(): void {
  devicesResource.value = { status: canViewDevices ? "loading" : "forbidden", data: null };
  channelsResource.value = { status: canViewDevices ? "loading" : "forbidden", data: null };
  anomaliesResource.value = { status: canViewDevices ? "loading" : "forbidden", data: null };
  alarmsResource.value = { status: canViewAlarms ? "loading" : "forbidden", data: null };
  sipResource.value = { status: canViewSip ? "loading" : "forbidden", data: null };
  zlmResource.value = { status: canViewZlm ? "loading" : "forbidden", data: null };
}

function failureState(result: PromiseRejectedResult, optionalService = false): DashboardLoadState {
  return classifyDashboardError(result.reason, optionalService);
}

async function refreshDashboard(): Promise<void> {
  if (refreshing.value) return;
  const remountSipCard = loadedOnce.value;
  refreshing.value = true;
  resetResources();

  const now = dayjs();
  const alarmQuery = {
    page: 1,
    pageSize: 10,
    alarmFrom: now.subtract(24, "hour").toISOString(),
    alarmTo: now.toISOString()
  };

  const [devices, channels, onlineChannels, offlineChannels, anomalies, alarms, sip, zlm] = await Promise.allSettled([
    canViewDevices ? listDevices({ page: 1, pageSize: 1 }) : Promise.resolve(null),
    canViewDevices ? listChannels({ page: 1, pageSize: 1 }) : Promise.resolve(null),
    canViewDevices ? listChannels({ status: "online", page: 1, pageSize: 1 }) : Promise.resolve(null),
    canViewDevices ? listChannels({ status: "offline", page: 1, pageSize: 1 }) : Promise.resolve(null),
    canViewDevices ? getAnomalyCount() : Promise.resolve(null),
    canViewAlarms ? listAlarms(alarmQuery) : Promise.resolve(null),
    canViewSip ? fetchSipSetupStatus() : Promise.resolve(null),
    canViewZlm ? listZLMNodes() : Promise.resolve(null)
  ] as const);

  if (canViewDevices) {
    if (devices.status === "fulfilled" && devices.value?.data) {
      devicesResource.value = {
        status: "ready",
        data: {
          total: devices.value.data.total ?? 0,
          online: devices.value.data.onlineTotal ?? 0,
          offline: devices.value.data.offlineTotal ?? 0
        }
      };
    } else {
      devicesResource.value = {
        status: devices.status === "rejected" ? failureState(devices) : "error",
        data: null
      };
    }

    const channelResults = [channels, onlineChannels, offlineChannels];
    const rejectedChannel = channelResults.find(result => result.status === "rejected");
    if (
      channels.status === "fulfilled" &&
      channels.value?.data &&
      onlineChannels.status === "fulfilled" &&
      onlineChannels.value?.data &&
      offlineChannels.status === "fulfilled" &&
      offlineChannels.value?.data
    ) {
      channelsResource.value = {
        status: "ready",
        data: {
          total: channels.value.data.total ?? 0,
          online: onlineChannels.value.data.total ?? 0,
          offline: offlineChannels.value.data.total ?? 0
        }
      };
    } else {
      channelsResource.value = {
        status: rejectedChannel?.status === "rejected" ? failureState(rejectedChannel) : "error",
        data: null
      };
    }

    if (anomalies.status === "fulfilled" && anomalies.value?.data) {
      anomaliesResource.value = { status: "ready", data: anomalies.value.data.count ?? 0 };
    } else {
      anomaliesResource.value = {
        status: anomalies.status === "rejected" ? failureState(anomalies) : "error",
        data: null
      };
    }
  }

  if (canViewAlarms) {
    if (alarms.status === "fulfilled" && alarms.value?.data) {
      alarmsResource.value = {
        status: "ready",
        data: { total: alarms.value.data.total ?? 0, latest: alarms.value.data.list?.[0] ?? null }
      };
    } else {
      alarmsResource.value = {
        status: alarms.status === "rejected" ? failureState(alarms) : "error",
        data: null
      };
    }
  }

  if (canViewSip) {
    if (sip.status === "fulfilled" && sip.value?.data?.runtime) {
      sipResource.value = { status: "ready", data: sip.value.data.runtime };
    } else {
      sipResource.value = {
        status: sip.status === "rejected" ? failureState(sip) : "error",
        data: null
      };
    }
  }

  if (canViewZlm) {
    if (zlm.status === "fulfilled" && zlm.value?.data) {
      zlmResource.value = { status: "ready", data: zlm.value.data.list ?? [] };
    } else {
      zlmResource.value = {
        status: zlm.status === "rejected" ? failureState(zlm, true) : "error",
        data: null
      };
    }
  }

  lastUpdatedAt.value = new Date();
  refreshing.value = false;
  loadedOnce.value = true;
  if (remountSipCard && canViewSip) sipCardKey.value += 1;
}

function navigate(path: string): void {
  if (!path || router.currentRoute.value.path === path) return;
  router.push(path);
}

onMounted(refreshDashboard);
</script>

<style lang="scss" scoped>
.dashboard-shell {
  --dashboard-canvas: var(--uvp-shell-muted);
  --dashboard-surface: var(--uvp-panel-bg);
  --dashboard-surface-muted: var(--uvp-list-toolbar-bg);
  --dashboard-border: var(--uvp-panel-border);
  --dashboard-shadow: var(--uvp-panel-shadow);

  box-sizing: border-box;
  height: 100%;
  padding: 16px 18px;
  background: var(--dashboard-canvas);
  border-radius: 8px;
}

.dashboard-page {
  display: grid;
  grid-template-rows: auto auto minmax(0, 1fr);
  gap: 12px;
  min-width: 0;
  min-height: 100%;
  color: var(--uvp-text-primary);
  letter-spacing: 0;
}

.dashboard-header {
  display: flex;
  gap: 16px;
  align-items: center;
  justify-content: space-between;
  min-height: 54px;
}

.dashboard-title {
  display: flex;
  gap: 11px;
  align-items: center;
  min-width: 0;

  h1 {
    margin: 0;
    font-size: 22px;
    font-weight: 700;
    line-height: 1.15;
    color: var(--uvp-text-primary);
    letter-spacing: 0;
  }
}

.dashboard-title__copy {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
}

.dashboard-title__eyebrow,
.dashboard-title__scope {
  display: inline-flex;
  gap: 6px;
  align-items: center;
}

.dashboard-title__eyebrow {
  overflow: hidden;
  font-size: 10px;
  font-weight: 600;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}

.dashboard-title__scope {
  padding-left: 7px;
  border-left: 1px solid var(--dashboard-border);
}

.dashboard-title__icon {
  display: inline-flex;
  flex: 0 0 40px;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border: 1px solid color-mix(in srgb, var(--uvp-brand) 18%, transparent);
  border-radius: 8px;
}

.dashboard-header__tools,
.service-cluster,
.service-chip,
.refresh-button {
  display: inline-flex;
  align-items: center;
}

.dashboard-header__tools {
  gap: 8px;
  min-width: 0;
}

.service-cluster {
  gap: 7px;
}

.service-chip {
  gap: 8px;
  min-width: 124px;
  height: 44px;
  padding: 0 9px 0 11px;
  font: inherit;
  color: var(--uvp-text-primary);
  cursor: pointer;
  background: var(--dashboard-surface);
  border: 1px solid var(--dashboard-border);
  border-radius: 8px;
  box-shadow: 0 2px 8px rgb(15 23 42 / 3%);
  transition:
    background-color 0.18s ease,
    border-color 0.18s ease,
    box-shadow 0.18s ease;

  &:hover:not(:disabled),
  &:focus-visible {
    outline: none;
    background: var(--dashboard-surface-muted);
    border-color: color-mix(in srgb, var(--uvp-brand) 34%, var(--dashboard-border));
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--uvp-brand) 10%, transparent);
  }

  &:disabled {
    cursor: default;
    opacity: 0.7;
  }
}

.service-chip__copy {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
  text-align: left;

  small,
  strong {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  small {
    font-size: 9px;
    color: var(--uvp-text-tertiary);
  }

  strong {
    font-size: 12px;
    font-weight: 650;
    color: var(--uvp-text-primary);
  }
}

.refresh-button {
  flex: 0 0 44px;
  justify-content: center;
  width: 44px;
  height: 44px;
  padding: 0;
  font: inherit;
  color: #ffffff;
  cursor: pointer;
  background: var(--uvp-brand);
  border: 1px solid var(--uvp-brand);
  border-radius: 8px;
  transition:
    background-color 0.16s ease,
    border-color 0.16s ease,
    box-shadow 0.16s ease;

  &:hover:not(:disabled) {
    background: var(--uvp-brand-strong);
    border-color: var(--uvp-brand-strong);
  }

  &:focus-visible {
    outline: none;
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--uvp-brand) 18%, transparent);
  }

  &:disabled {
    cursor: wait;
    opacity: 0.72;
  }
}

.metric-card,
.attention-panel {
  background: var(--dashboard-surface);
  border: 1px solid var(--dashboard-border);
  border-radius: 8px;
  box-shadow: var(--dashboard-shadow);
}

.status-dot {
  flex: 0 0 8px;
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.status-dot--success {
  background: var(--uvp-brand-cyan);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--uvp-brand-cyan) 14%, transparent);
}

.status-dot--warning {
  background: var(--uvp-warning);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--uvp-warning) 13%, transparent);
}

.status-dot--danger {
  background: var(--uvp-danger);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--uvp-danger) 13%, transparent);
}

.status-dot--neutral {
  background: var(--uvp-text-tertiary);
}

.last-updated {
  display: inline-flex;
  flex: 0 0 auto;
  gap: 5px;
  align-items: center;
  padding: 0 2px;
  font-size: 10px;
  color: var(--uvp-text-tertiary);

  strong {
    font-variant-numeric: tabular-nums;
    color: var(--uvp-text-secondary);
  }
}

.metric-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}

.metric-card {
  position: relative;
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 124px;
  padding: 14px 16px 12px;
  overflow: hidden;
  font: inherit;
  color: inherit;
  text-align: left;
  appearance: none;
  cursor: pointer;
  transition:
    background-color 0.18s ease,
    border-color 0.16s ease,
    box-shadow 0.18s ease;

  &:hover:not(:disabled),
  &:focus-visible {
    outline: none;
    background: color-mix(in srgb, var(--dashboard-surface) 95%, var(--uvp-brand) 5%);
    border-color: color-mix(in srgb, var(--uvp-brand) 35%, var(--dashboard-border));
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--uvp-brand) 9%, transparent), var(--dashboard-shadow);
  }

  &:disabled {
    cursor: default;
  }
}

.metric-card__top {
  display: flex;
  gap: 8px;
  align-items: center;
  min-width: 0;
}

.metric-card__icon {
  display: inline-flex;
  flex: 0 0 30px;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-radius: 7px;
}

.metric-card--warning .metric-card__icon {
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
}
.metric-card--danger .metric-card__icon {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
}

.metric-card__label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 13px;
  font-weight: 600;
  color: var(--uvp-text-secondary);
  white-space: nowrap;
}

.metric-card__arrow {
  flex: 0 0 auto;
  margin-left: auto;
  color: var(--uvp-text-tertiary);
}

.metric-card__value-row {
  display: flex;
  gap: 12px;
  align-items: flex-end;
  justify-content: space-between;
  min-width: 0;
  margin-top: 10px;
}

.metric-card__value {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 30px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  line-height: 1;
  color: var(--uvp-text-primary);
  letter-spacing: 0;
  white-space: nowrap;
}

.metric-card__ratio {
  display: flex;
  flex: 0 0 auto;
  flex-direction: column;
  align-items: flex-end;
  padding-bottom: 1px;

  strong {
    font-size: 13px;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
    color: var(--uvp-brand-cyan);
  }

  span {
    margin-top: 1px;
    font-size: 9px;
    color: var(--uvp-text-tertiary);
  }
}

.metric-card__signal {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  min-height: 22px;
  padding: 0 7px;
  font-size: 10px;
  font-weight: 650;
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--uvp-brand-cyan) 20%, transparent);
  border-radius: 6px;
}

.metric-card__signal--warning {
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
  border-color: var(--uvp-warning-border);
}

.metric-card__signal--danger {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
  border-color: var(--uvp-danger-border);
}

.metric-card__details,
.metric-card__state {
  display: flex;
  gap: 12px;
  align-items: center;
  min-height: 16px;
  margin-top: 6px;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}

.metric-card__rail {
  height: 3px;
  margin-top: auto;
  overflow: hidden;
  background: color-mix(in srgb, var(--uvp-text-tertiary) 16%, transparent);
  border-radius: 3px;

  span {
    display: block;
    height: 100%;
    background: var(--uvp-brand-cyan);
    border-radius: inherit;
    transition: width 0.25s ease;
  }
}

.metric-card__details span {
  display: inline-flex;
  gap: 5px;
  align-items: center;
}

.detail-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
}
.detail-dot--success {
  background: var(--uvp-brand-cyan);
}
.detail-dot--muted {
  background: var(--uvp-text-tertiary);
}
.metric-card__state--forbidden,
.metric-card__state--disabled {
  color: var(--uvp-warning);
}
.metric-card__state--error {
  color: var(--uvp-danger);
}

.dashboard-content {
  display: grid;
  grid-template-columns: minmax(0, 2.2fr) minmax(310px, 0.8fr);
  gap: 12px;
  min-width: 0;
  min-height: 0;
}

.dashboard-sip,
.dashboard-sip :deep(.sip-card),
.attention-panel {
  min-width: 0;
  height: 100%;
  min-height: 0;
}

.dashboard-sip :deep(.sip-card) {
  box-sizing: border-box;
  gap: 11px;
  padding: 14px 16px;
  overflow: hidden;
  background: var(--dashboard-surface);
  border-color: var(--dashboard-border);
  border-radius: 8px;
  box-shadow: var(--dashboard-shadow);
}

.dashboard-sip :deep(.summary-bar) {
  gap: 20px;
  padding: 9px 2px 12px;
}
.dashboard-sip :deep(.summary-bar__stats) {
  gap: 26px;
}
.dashboard-sip :deep(.tx-grid) {
  gap: 8px;
}
.dashboard-sip :deep(.tx-cell) {
  min-height: 68px;
  padding: 8px 10px;
  border-radius: 6px;
}
.dashboard-sip :deep(.pulse) {
  flex: 1 1 150px;
  gap: 8px;
  min-height: 150px;
  padding: 11px 13px;
  border-radius: 7px;
}

.sip-unavailable {
  display: flex;
  flex-direction: column;
  gap: 7px;
  align-items: center;
  justify-content: center;
  height: 100%;
  min-height: 320px;
  color: var(--uvp-text-tertiary);
  background: var(--dashboard-surface);
  border: 1px solid var(--dashboard-border);
  border-radius: 8px;

  strong {
    font-size: 14px;
    color: var(--uvp-text-primary);
  }
  span {
    font-size: 12px;
  }
}

.attention-panel {
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  padding: 16px;
  overflow: hidden;
}

.attention-panel__header {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  justify-content: space-between;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--dashboard-border);

  h2 {
    margin: 0;
    font-size: 14px;
    font-weight: 650;
    color: var(--uvp-text-primary);
    letter-spacing: 0;
  }
  p {
    margin: 3px 0 0;
    font-size: 11px;
    color: var(--uvp-text-tertiary);
  }
}

.attention-count {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  min-height: 22px;
  padding: 0 7px;
  font-size: 11px;
  font-weight: 600;
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
  border: 1px solid var(--uvp-warning-border);
  border-radius: 6px;
}

.attention-list {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
  padding-top: 4px;
}

.attention-item {
  display: grid;
  grid-template-columns: 30px minmax(0, 1fr) 15px;
  gap: 9px;
  align-items: center;
  min-height: 58px;
  padding: 8px 2px;
  font: inherit;
  color: var(--uvp-text-tertiary);
  text-align: left;
  cursor: pointer;
  background: transparent;
  border: 0;
  border-bottom: 1px solid var(--dashboard-border);
  border-radius: 6px;
  transition:
    background-color 0.18s ease,
    color 0.18s ease;

  &:last-child {
    border-bottom: 0;
  }
  &:hover,
  &:focus-visible {
    color: var(--uvp-brand);
    outline: none;
    background: var(--dashboard-surface-muted);
  }
}

.attention-item__icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
  border-radius: 7px;
}

.attention-item__icon--danger {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
}

.attention-item__content {
  min-width: 0;

  strong,
  small {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  strong {
    font-size: 12px;
    font-weight: 600;
    color: var(--uvp-text-primary);
  }
  small {
    margin-top: 3px;
    font-size: 10px;
    color: var(--uvp-text-tertiary);
  }
}

.attention-empty,
.attention-loading {
  flex: 1;
  min-height: 220px;
}

.attention-empty {
  display: flex;
  flex-direction: column;
  gap: 7px;
  align-items: center;
  justify-content: center;
  color: var(--uvp-brand-cyan);

  strong {
    font-size: 13px;
    color: var(--uvp-text-primary);
  }
  span {
    font-size: 11px;
    color: var(--uvp-text-tertiary);
    text-align: center;
  }
}

.attention-loading {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 14px 0;

  span {
    height: 46px;
    background: var(--dashboard-surface-muted);
    border-radius: 7px;
    animation: loading-pulse 1.4s ease-in-out infinite;
  }
}

.media-facts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  padding-top: 10px;
  border-top: 1px solid var(--dashboard-border);

  span {
    display: inline-flex;
    gap: 5px;
    align-items: center;
    min-width: 0;
    font-size: 10px;
    color: var(--uvp-text-tertiary);
  }
  strong {
    font-variant-numeric: tabular-nums;
    color: var(--uvp-text-primary);
  }
}

.spin {
  animation: spin 0.9s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@keyframes loading-pulse {
  0%,
  100% {
    opacity: 0.45;
  }
  50% {
    opacity: 1;
  }
}

@media (width >= 1101px) and (height <= 799px) {
  .dashboard-shell {
    padding: 10px 14px;
    overflow: hidden;
  }

  .dashboard-page {
    height: 100%;
  }

  .dashboard-header {
    min-height: 44px;
  }

  .dashboard-title__icon {
    flex-basis: 34px;
    width: 34px;
    height: 34px;
  }

  .dashboard-title h1 {
    font-size: 20px;
  }

  .service-chip,
  .refresh-button {
    height: 38px;
  }

  .service-chip {
    min-width: 116px;
  }

  .refresh-button {
    flex-basis: 38px;
    width: 38px;
  }

  .metric-card {
    min-height: 104px;
    padding: 11px 13px 9px;
  }

  .metric-card__value-row {
    margin-top: 7px;
  }

  .metric-card__value {
    font-size: 26px;
  }

  .dashboard-sip :deep(.sip-card) {
    gap: 7px;
    padding: 10px 12px;
  }

  .dashboard-sip :deep(.summary-bar) {
    gap: 14px;
    padding: 6px 2px 8px;
  }

  .dashboard-sip :deep(.pulse) {
    flex: 0 0 auto;
    min-height: 110px;
    padding: 8px 10px;
  }

  .dashboard-sip :deep(.pulse__chart),
  .dashboard-sip :deep(.pulse__empty) {
    height: 70px;
    min-height: 70px;
  }

  .dashboard-sip :deep(.tx-grid) {
    align-content: start;
  }

  .dashboard-sip :deep(.tx-cell) {
    min-height: 54px;
    padding: 6px 8px;
  }
}

@media (width >= 1200px) and (height >= 800px) {
  .dashboard-shell {
    overflow: hidden;
  }
  .dashboard-page {
    height: 100%;
  }
}

@media (width <= 1100px) {
  .dashboard-shell {
    height: auto;
    min-height: 100%;
  }
  .dashboard-page {
    display: flex;
    flex-direction: column;
  }
  .dashboard-content {
    grid-template-columns: minmax(0, 1fr);
  }
  .dashboard-sip :deep(.sip-card),
  .attention-panel {
    height: auto;
  }
  .metric-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .dashboard-header__tools {
    flex-wrap: wrap;
    justify-content: flex-end;
  }
  .last-updated {
    display: none;
  }
}

@media (width <= 720px) {
  .dashboard-shell {
    padding: 12px;
  }
  .dashboard-header {
    flex-direction: column;
    gap: 10px;
    align-items: stretch;
  }
  .dashboard-title__scope {
    display: none;
  }
  .dashboard-title h1 {
    font-size: 20px;
  }
  .dashboard-header__tools {
    display: flex;
    flex-wrap: nowrap;
    width: 100%;
  }
  .service-cluster {
    display: grid;
    flex: 1;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    min-width: 0;
  }
  .service-chip {
    width: 100%;
    min-width: 0;
  }
  .metric-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .metric-card {
    min-height: 118px;
  }
  .dashboard-sip :deep(.sip-card) {
    overflow: visible;
  }
  .dashboard-sip :deep(.sip-card__head),
  .dashboard-sip :deep(.pulse__head) {
    flex-direction: column;
    gap: 7px;
    align-items: flex-start;
  }
  .dashboard-sip :deep(.summary-bar) {
    grid-template-columns: minmax(0, 1fr);
    gap: 10px;
    align-items: flex-start;
  }
  .dashboard-sip :deep(.summary-bar__divider) {
    width: 100%;
    height: 1px;
  }
  .dashboard-sip :deep(.summary-bar__stats) {
    justify-content: space-between;
    width: 100%;
  }
  .dashboard-sip :deep(.tx-grid) {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .dashboard-sip :deep(.pulse__legend) {
    flex-wrap: wrap;
  }
}

@media (prefers-reduced-motion: reduce) {
  .metric-card,
  .metric-card__rail span,
  .service-chip,
  .refresh-button {
    transition: none;
  }
  .spin,
  .attention-loading span {
    animation: none;
  }
}
</style>
