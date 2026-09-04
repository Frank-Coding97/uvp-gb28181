import type { MediaChartSpec } from "@/views/gb28181/zlm/workbench/chart/overviewChart";
import type { DashboardHistoryData, PlayHistory, SIPHistory, TrafficHistory } from "@/api/home-dashboard-drilldown";
import type { DashboardDrilldownMetric } from "../../dashboardDrilldownState";

export interface DashboardTrendValue {
  [key: string]: string | number | null;
  bucket: string;
  series: string;
  value: number | null;
}

export function buildDashboardTrendSpec(metric: DashboardDrilldownMetric, history: DashboardHistoryData): MediaChartSpec {
  const values: DashboardTrendValue[] = [];
  if (metric === "sip-rpm" || metric === "sip-today") {
    for (const point of (history as SIPHistory).points) {
      values.push({ bucket: point.bucketStart, series: metric === "sip-rpm" ? "RPM" : "请求数", value: metric === "sip-rpm" ? point.rpm : point.requests });
    }
  } else if (metric === "play-success-24h") {
    for (const point of (history as PlayHistory).points) {
      values.push({ bucket: point.bucketStart, series: "成功率", value: point.rate == null ? null : point.rate * 100 });
    }
  } else {
    for (const point of (history as TrafficHistory).points) {
      values.push({ bucket: point.bucketStart, series: "上行", value: point.upstreamBytes });
      values.push({ bucket: point.bucketStart, series: "下行", value: point.downstreamBytes });
    }
  }
  return {
    type: "area",
    data: [{ id: "dashboard-drilldown", values }],
    xField: "bucket",
    yField: "value",
    seriesField: "series",
    point: { visible: false },
    line: { curveType: "monotone" },
    area: { style: { fillOpacity: 0.2 } },
    invalidType: "break",
    axes: [
      { orient: "bottom", type: "band", label: { autoRotate: true, autoHide: true } },
      { orient: "left", type: "linear", nice: true }
    ],
    legends: { visible: metric === "media-traffic-today", orient: "top", position: "end" },
    tooltip: { mark: { title: { visible: true } } }
  } as MediaChartSpec;
}
