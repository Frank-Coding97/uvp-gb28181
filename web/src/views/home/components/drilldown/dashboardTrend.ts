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

function formatTrafficBytes(value: number): string {
  if (!Number.isFinite(value)) return "—";
  if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(1)} GB`;
  if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(1)} MB`;
  if (value >= 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${value} B`;
}

export function buildDashboardTrendSpec(metric: DashboardDrilldownMetric, history: DashboardHistoryData): MediaChartSpec {
  const values: DashboardTrendValue[] = [];
  const isTraffic = metric === "media-traffic-today";
  if (metric === "sip-rpm" || metric === "sip-today") {
    for (const point of (history as SIPHistory).points) {
      values.push({
        bucket: point.bucketStart,
        bucketLabel: formatBucketLabel(point.bucketStart, history),
        series: metric === "sip-rpm" ? "RPM" : "请求数",
        value: metric === "sip-rpm" ? point.rpm : point.requests
      });
    }
  } else if (metric === "play-success-24h") {
    for (const point of (history as PlayHistory).points) {
      values.push({
        bucket: point.bucketStart,
        bucketLabel: formatBucketLabel(point.bucketStart, history),
        series: "成功率",
        value: point.rate == null ? null : point.rate * 100
      });
    }
  } else {
    for (const point of (history as TrafficHistory).points) {
      const bucketLabel = formatBucketLabel(point.bucketStart, history);
      values.push({ bucket: point.bucketStart, bucketLabel, series: "上行", value: point.upstreamBytes });
      values.push({ bucket: point.bucketStart, bucketLabel, series: "已结算下行", value: point.downstreamBytes });
    }
  }
  return {
    type: "area",
    // 与工作台其它图表一致：画布透出卡片底色。缺省时 VChart 会刷 Arco 暗色主题的中性黑，
    // 在 UVP 的蓝灰暗色面板上会形成一块"黑补丁"（色温不一致）。
    background: "transparent",
    // ⛔ 只给色板数组是不够的：VChart 给「折线」和给「悬浮图例标记」分配颜色的顺序可能不一致
    //    （实测暗色下：91.7GB 的上行线画成青色、悬浮里却标成蓝色），看图例认色就会认错。
    //    改成显式序数映射：domain 钉死系列、range 钉死颜色，线与标记同用一把尺，不可能再错位。
    ...(isTraffic
      ? {
          color: {
            type: "ordinal",
            domain: ["上行", "已结算下行"],
            range: ["var(--uvp-brand)", "var(--uvp-brand-cyan)"]
          }
        }
      : {}),
    data: [{ id: "dashboard-drilldown", values }],
    // ⛔⛔ 面积图必须显式 stack:false。VChart 的 area 类型默认按系列堆叠,
    //    「上行 / 已结算下行」一旦叠起来,青线的上缘就变成**两者之和**,
    //    于是出现「曲线里下行比上行高、悬浮窗里 530MB < 692MB」的自相矛盾
    //    (2026-10-18 像素实测:16:00 处两线高度比 1.77 ≈ (692+530)/692,
    //     15:00 桶处 1.97 ≈ (141+137)/141,两处都精确命中堆叠假设)。
    stack: false,
    xField: "bucketLabel",
    yField: "value",
    seriesField: "series",
    point: { visible: false },
    line: { style: { curveType: "monotone" } },
    area: { style: { fillOpacity: 0.2, curveType: "monotone" } },
    invalidType: "break",
    axes: [
      { orient: "bottom", type: "band", label: { autoRotate: false, autoHide: true } },
      {
        orient: "left",
        type: "linear",
        nice: true,
        ...(isTraffic ? { title: { text: "流量" }, label: { formatMethod: formatTrafficBytes } } : {})
      }
    ],
    legends: { visible: isTraffic, orient: "top", position: "end" },
    tooltip: isTraffic
      ? {
          activeType: "dimension",
          // 悬浮面板默认用主题的中性灰，与 UVP 蓝灰面板色温不一致；对齐到弹层 token。
          style: {
            panel: {
              backgroundColor: "var(--uvp-popconfirm-bg)",
              border: { color: "var(--uvp-popconfirm-border)", width: 1, radius: 8 }
            }
          },
          dimension: {
            content: [
              {
                key: (datum: DashboardTrendValue) => datum.series,
                value: (datum: DashboardTrendValue) => formatTrafficBytes(Number(datum.value))
              }
            ]
          }
        }
      : {
          mark: { title: { visible: true } },
          style: {
            panel: {
              backgroundColor: "var(--uvp-popconfirm-bg)",
              border: { color: "var(--uvp-popconfirm-border)", width: 1, radius: 8 }
            }
          }
        },
    padding: { left: 12, right: 16, top: 12, bottom: 24 }
  } as MediaChartSpec;
}
