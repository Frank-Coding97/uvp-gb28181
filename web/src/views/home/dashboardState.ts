import type { SipRuntimeState } from "@/api/gb28181";
import type { ZLMNode, ZLMNodeState } from "@/api/gb28181-zlm";

export type DashboardLoadState = "loading" | "ready" | "forbidden" | "disabled" | "error";

type ZlmNodeSummarySource = Pick<ZLMNode, "state" | "nearCapacity"> & {
  stats: Pick<ZLMNode["stats"], "mediaSourceCount" | "sessionCount">;
};

export interface ZlmSummary {
  total: number;
  active: number;
  offline: number;
  maintenance: number;
  nearCapacity: number;
  mediaSources: number;
  sessions: number;
}

export interface AttentionItem {
  id: string;
  title: string;
  detail: string;
  tone: "danger" | "warning";
  route: string;
}

export interface DashboardRoute {
  path: string;
}

export interface DashboardRouteTargets {
  deviceManagement?: string;
  directoryAnomaly?: string;
}

export interface DashboardServiceStatus {
  label: string;
  detail: string;
  tone: "success" | "warning" | "danger" | "neutral";
}

interface AttentionSources {
  sip?: {
    status: DashboardLoadState;
    state?: SipRuntimeState;
    errorSummary?: string;
  };
  devices?: { status: DashboardLoadState; offline?: number };
  channels?: { status: DashboardLoadState; offline?: number };
  anomalies?: { status: DashboardLoadState; count?: number };
  alarms?: { status: DashboardLoadState; total?: number; latestDescription?: string };
  zlm?: { status: DashboardLoadState; nodes?: ZlmNodeSummarySource[] };
}

const stateLabels: Record<DashboardLoadState, string> = {
  loading: "加载中",
  ready: "已更新",
  forbidden: "无权限",
  disabled: "未启用",
  error: "加载失败"
};

const defaultRouteTargets: Required<DashboardRouteTargets> = {
  deviceManagement: "/gb28181/device-mgmt/index",
  directoryAnomaly: "/gb28181/device-mgmt/anomaly"
};

export function resolveDashboardRoute(routes: readonly DashboardRoute[], candidates: readonly string[]): string | null {
  const availablePaths = new Set(routes.map(route => route.path));
  return candidates.find(candidate => availablePaths.has(candidate)) ?? null;
}

export function loadStateLabel(state: DashboardLoadState): string {
  return stateLabels[state];
}

export function metricText(state: DashboardLoadState, value?: number | null): string {
  if (state !== "ready" || value == null || !Number.isFinite(value)) return "--";
  return value.toLocaleString("zh-CN");
}

export function ratioPercent(part: number, total: number): number | null {
  if (!Number.isFinite(part) || !Number.isFinite(total) || part < 0 || total < 0) return null;
  if (total === 0) return 0;
  return Math.round(Math.min(1, part / total) * 100);
}

export function describeSipRuntime(runtime: { state: string; errorSummary?: string }): DashboardServiceStatus {
  switch (runtime.state) {
    case "running":
      return { label: "运行中", detail: "", tone: "success" };
    case "starting":
      return { label: "启动中", detail: "", tone: "warning" };
    case "failed":
      return { label: "运行异常", detail: runtime.errorSummary?.trim() || "", tone: "danger" };
    case "disabled":
      return { label: "未启用", detail: "", tone: "neutral" };
    case "unconfigured":
      return { label: "未配置", detail: "", tone: "warning" };
    case "restart_required":
      return { label: "需要重启", detail: "", tone: "warning" };
    default:
      return { label: "状态未知", detail: "服务状态与当前前端版本不兼容", tone: "warning" };
  }
}

export function classifyDashboardError(error: unknown, optionalService = false): DashboardLoadState {
  const status = (error as { response?: { status?: number } })?.response?.status;
  if (status === 403) return "forbidden";
  if (optionalService && (status === 404 || status === 503)) return "disabled";
  return "error";
}

export function summarizeZlmNodes(nodes: ZlmNodeSummarySource[]): ZlmSummary {
  const byState = (state: ZLMNodeState) => nodes.filter(node => node.state === state).length;
  const activeNodes = nodes.filter(node => node.state === "active");
  return {
    total: nodes.length,
    active: byState("active"),
    offline: byState("offline"),
    maintenance: byState("maintenance"),
    nearCapacity: activeNodes.filter(node => node.nearCapacity).length,
    mediaSources: activeNodes.reduce((total, node) => total + (node.stats?.mediaSourceCount ?? 0), 0),
    sessions: activeNodes.reduce((total, node) => total + (node.stats?.sessionCount ?? 0), 0)
  };
}

export function buildAttentionItems(sources: AttentionSources, routeTargets: DashboardRouteTargets = {}): AttentionItem[] {
  const items: AttentionItem[] = [];
  const routes = { ...defaultRouteTargets, ...routeTargets };
  const sip = sources.sip;
  if (sip?.status === "ready") {
    if (sip.state === "failed") {
      items.push({
        id: "sip-failed",
        title: "SIP 服务运行异常",
        detail: sip.errorSummary?.trim() || "请检查国标服务配置与监听端口",
        tone: "danger",
        route: "/gb28181/sip/platform"
      });
    } else if (sip.state === "unconfigured" || sip.state === "disabled") {
      items.push({
        id: `sip-${sip.state}`,
        title: sip.state === "unconfigured" ? "SIP 服务尚未配置" : "SIP 服务未启用",
        detail: "完成国标接入配置后才能接收设备信令",
        tone: "warning",
        route: "/gb28181/sip/platform"
      });
    } else if (sip.state === "restart_required") {
      items.push({
        id: "sip-restart",
        title: "SIP 服务需要重启",
        detail: "当前配置尚未完全生效",
        tone: "warning",
        route: "/gb28181/sip/platform"
      });
    }
  }

  if (sources.zlm?.status === "ready") {
    const summary = summarizeZlmNodes(sources.zlm.nodes ?? []);
    if (summary.offline > 0) {
      items.push({
        id: "zlm-offline",
        title: `${summary.offline} 个媒体节点离线`,
        detail: `当前 ${summary.active}/${summary.total} 个节点可用`,
        tone: "danger",
        route: "/gb28181/zlm/nodes"
      });
    }
  }

  if (sources.alarms?.status === "ready" && (sources.alarms.total ?? 0) > 0) {
    items.push({
      id: "recent-alarms",
      title: `近 24 小时收到 ${sources.alarms.total} 条告警`,
      detail: sources.alarms.latestDescription?.trim() || "查看最近设备告警",
      tone: "warning",
      route: "/gb28181/alarm-management"
    });
  }

  if (sources.anomalies?.status === "ready" && (sources.anomalies.count ?? 0) > 0) {
    items.push({
      id: "catalog-anomalies",
      title: `${sources.anomalies.count} 项目录异常待处理`,
      detail: "目录编码、挂载或层级关系需要核查",
      tone: "warning",
      route: routes.directoryAnomaly
    });
  }

  if (sources.devices?.status === "ready" && (sources.devices.offline ?? 0) > 0) {
    items.push({
      id: "devices-offline",
      title: `${sources.devices.offline} 台设备离线`,
      detail: "统计范围为当前账号可见设备",
      tone: "warning",
      route: routes.deviceManagement
    });
  }

  if (sources.channels?.status === "ready" && (sources.channels.offline ?? 0) > 0) {
    items.push({
      id: "channels-offline",
      title: `${sources.channels.offline} 个视频通道离线`,
      detail: "通道状态来自设备最新目录上报",
      tone: "warning",
      route: routes.deviceManagement
    });
  }

  if (sources.zlm?.status === "ready") {
    const summary = summarizeZlmNodes(sources.zlm.nodes ?? []);
    if (summary.nearCapacity > 0) {
      items.push({
        id: "zlm-capacity",
        title: `${summary.nearCapacity} 个媒体节点接近容量上限`,
        detail: "请检查节点会话与媒体源负载",
        tone: "warning",
        route: "/gb28181/zlm/nodes"
      });
    }
  }

  return items.slice(0, 5);
}
