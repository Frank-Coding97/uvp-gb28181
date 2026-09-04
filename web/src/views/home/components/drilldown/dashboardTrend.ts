import type { MediaChartSpec } from "@/views/gb28181/zlm/workbench/chart/overviewChart";
import type { DashboardHistoryData, PlayHistory, SIPHistory, TrafficHistory } from "@/api/home-dashboard-drilldown";
import type { DashboardDrilldownMetric } from "../../dashboardDrilldownState";

export interface DashboardTrendValue {
  [key: string]: string | number | null;
  bucket: string;
  bucketLabel: string;
  series: string;
  value: number | null;
}

function formatBucketLabel(bucket: string, history: DashboardHistoryData): string {
  const match = bucket.match(/^\d{4}-(\d{2})-(\d{2})T(\d{2}):(\d{2})/);
  if (!match) return bucket;
  const [, month, day, hour, minute] = match;
  if (history.range === "7d") return history.bucketSeconds >= 86400 ? `${month}-${day}` : `${month}-${day} ${hour}:${minute}`;
  return `${hour}:${minute}`;
}

export function buildDashboardTrendSpec(metric: DashboardDrilldownMetric, history: DashboardHistoryData): MediaChartSpec {
  const values: DashboardTrendValue[] = [];
  if (metric === "sip-rpm" || metric === "sip-today") {
    for (const point of (history as SIPHistory).points) {
      values.push({ bucket: point.bucketStart, bucketLabel: formatBucketLabel(point.bucketStart, history), series: metric === "sip-rpm" ? "RPM" : "请求数", value: metric === "sip-rpm" ? point.rpm : point.requests });
    }
  } else if (metric === "play-success-24h") {
    for (const point of (history as PlayHistory).points) {
      values.push({ bucket: point.bucketStart, bucketLabel: formatBucketLabel(point.bucketStart, history), series: "成功率", value: point.rate == null ? null : point.rate * 100 });
    }
  } else {
    for (const point of (history as TrafficHistory).points) {
      const bucketLabel = formatBucketLabel(point.bucketStart, history);
      values.push({ bucket: point.bucketStart, bucketLabel, series: "上行", value: point.upstreamBytes });
      values.push({ bucket: point.bucketStart, bucketLabel, series: "下行", value: point.downstreamBytes });
    }
  }
  return {
    type: "area",
    data: [{ id: "dashboard-drilldown", values }],
    xField: "bucketLabel",
    yField: "value",
    seriesField: "series",
    point: { visible: false },
    line: { curveType: "monotone" },
    area: { style: { fillOpacity: 0.2 } },
    invalidType: "break",
    axes: [
      { orient: "bottom", type: "band", label: { autoRotate: false, autoHide: true } },
      { orient: "left", type: "linear", nice: true }
    ],
    legends: { visible: metric === "media-traffic-today", orient: "top", position: "end" },
    tooltip: { mark: { title: { visible: true } } },
    padding: { left: 12, right: 16, top: 12, bottom: 24 }
  } as MediaChartSpec;
}
