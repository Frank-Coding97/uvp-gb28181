<script setup lang="ts">
/**
 * DeviceMapView — 地图视图(D3,Phase 1 SVG 兜底版)
 *
 * 设计思路:
 * - D2 AMap key 等老板;先用纯 SVG 渲染一个"城市级"占位地图,数据真,
 *   只是底图是矢量框架而非真实瓦片
 * - 后续接 AMap 时,只需替换 `<MapCanvas>` 的实现,组件 props 接口保持
 *
 * Phase 1 简化:
 * - bbox 默认覆盖济南(36.5-36.8, 116.9-117.3)
 * - zoom <12 走 clusters,>=12 走 markers
 * - hover marker → 简单 tooltip;click → 抽屉 channel
 * - 视野统计气泡 + 无坐标 banner
 */
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  getMapMarkers,
  getMapClusters,
  getMapNoCoordCount,
  type MarkerVO,
  type ClusterVO,
} from "../../api/device";

const route = useRoute();
const router = useRouter();

const bbox = ref({ minLat: 36.5, maxLat: 36.85, minLng: 116.85, maxLng: 117.3 });
const zoom = ref(11);

const markers = ref<MarkerVO[]>([]);
const clusters = ref<ClusterVO[]>([]);
const noCoordCount = ref(0);
const loading = ref(false);
const err = ref<string | null>(null);

const showClusters = computed(() => zoom.value < 12);

async function load() {
  loading.value = true;
  err.value = null;
  try {
    const [mR, cR, nR] = await Promise.all([
      getMapMarkers({ ...bbox.value, limit: 500 }),
      getMapClusters({ ...bbox.value, zoom: zoom.value }),
      getMapNoCoordCount(),
    ]);
    markers.value = mR?.data?.list ?? [];
    clusters.value = cR?.data?.clusters ?? [];
    noCoordCount.value = nR?.data?.count ?? 0;
  } catch (e) {
    err.value = e instanceof Error ? e.message : "加载失败";
  } finally {
    loading.value = false;
  }
}

onMounted(load);
watch(() => zoom.value, load);

// 坐标 → SVG 视图坐标(简单线性映射,济南级 bbox 够用)
function project(lat: number, lng: number) {
  const x = ((lng - bbox.value.minLng) / (bbox.value.maxLng - bbox.value.minLng)) * 100;
  // SVG y 轴下增,所以纬度反转
  const y = (1 - (lat - bbox.value.minLat) / (bbox.value.maxLat - bbox.value.minLat)) * 100;
  return { x, y };
}

function zoomIn() {
  zoom.value = Math.min(18, zoom.value + 1);
}
function zoomOut() {
  zoom.value = Math.max(6, zoom.value - 1);
}
function reset() {
  zoom.value = 11;
}

function openMarker(m: MarkerVO) {
  router.push({ query: { ...route.query, node: String(m.id), drawerType: "channel" } });
}

const onlineCount = computed(() => markers.value.filter(m => m.status === 1).length);
const offlineCount = computed(() => markers.value.filter(m => m.status === 0).length);

const hovered = ref<MarkerVO | null>(null);
</script>

<template>
  <div class="device-map-view">
    <div v-if="err" class="banner banner-error">{{ err }}</div>

    <div class="map-canvas">
      <svg viewBox="0 0 100 100" preserveAspectRatio="xMidYMid meet" class="bg">
        <defs>
          <pattern id="grid" width="5" height="5" patternUnits="userSpaceOnUse">
            <path d="M 5 0 L 0 0 0 5" fill="none" stroke="rgba(148,163,184,0.06)" stroke-width="0.2" />
          </pattern>
        </defs>
        <rect width="100" height="100" fill="var(--color-bg-1)" />
        <rect width="100" height="100" fill="url(#grid)" />

        <!-- markers / clusters -->
        <template v-if="!showClusters">
          <g
            v-for="m in markers"
            :key="`m-${m.id}`"
            :transform="`translate(${project(m.latitude, m.longitude).x}, ${project(m.latitude, m.longitude).y})`"
            class="marker"
            :class="m.status === 1 ? 'online' : 'offline'"
            @click="openMarker(m)"
            @mouseenter="hovered = m"
            @mouseleave="hovered = null"
          >
            <circle r="1.2" />
            <circle r="2.2" class="halo" />
          </g>
        </template>
        <template v-else>
          <g
            v-for="(c, i) in clusters"
            :key="`c-${i}`"
            :transform="`translate(${project(c.centerLat, c.centerLng).x}, ${project(c.centerLat, c.centerLng).y})`"
            class="cluster"
          >
            <circle r="3" />
            <text dy="0.5" text-anchor="middle">{{ c.count }}</text>
          </g>
        </template>
      </svg>

      <div v-if="hovered" class="tooltip">
        <strong>{{ hovered.name }}</strong>
        <span class="code dm-mono">{{ hovered.channelId }}</span>
        <span :class="['status', hovered.status === 1 ? 'on' : 'off']">
          {{ hovered.status === 1 ? "在线" : "离线" }}
        </span>
      </div>
    </div>

    <div class="toolbox">
      <button type="button" @click="zoomIn" title="放大">+</button>
      <button type="button" @click="zoomOut" title="缩小">−</button>
      <button type="button" @click="reset" title="复位">⊙</button>
      <span class="zoom-label">zoom {{ zoom }}</span>
    </div>

    <div class="stat-box">
      <span class="stat-line">
        <span class="dot online" /> 在线 <strong>{{ onlineCount }}</strong>
      </span>
      <span class="stat-line">
        <span class="dot offline" /> 离线 <strong>{{ offlineCount }}</strong>
      </span>
      <span v-if="showClusters" class="stat-line">
        <span class="dot cluster" /> 聚合 <strong>{{ clusters.length }}</strong>
      </span>
      <span class="stat-line">{{ showClusters ? "cluster" : "marker" }} 模式</span>
    </div>

    <div v-if="noCoordCount > 0" class="no-coord-banner">
      ⓘ 当前过滤下还有 <strong>{{ noCoordCount }}</strong> 个无坐标通道未显示
    </div>

    <div class="phase-hint">
      Phase 1 SVG 占位地图 — 数据真,底图未接 AMap(等 key)/ Leaflet(待装依赖)
    </div>
  </div>
</template>

<style lang="scss" scoped>
.device-map-view {
  position: relative;
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.banner-error {
  padding: var(--space-2) var(--space-3);
  background: var(--status-danger-fade);
  color: var(--status-danger);
  border-radius: var(--dm-radius-1);
  font-size: var(--font-12);
  margin-bottom: var(--space-2);
}

.map-canvas {
  flex: 1;
  position: relative;
  min-height: 0;
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-2);
  border-radius: var(--dm-radius-2);
  overflow: hidden;
}

.bg {
  width: 100%;
  height: 100%;
  display: block;
}

.marker {
  cursor: pointer;

  circle {
    transition: r var(--duration-fast) var(--ease-out);
  }

  &.online circle {
    fill: var(--status-online);
  }

  &.online .halo {
    fill: var(--status-online);
    opacity: 0.18;
  }

  &.offline circle {
    fill: var(--status-offline);
  }

  &.offline .halo {
    fill: var(--status-offline);
    opacity: 0.12;
  }

  &:hover circle:first-child {
    r: 1.6;
  }
}

.cluster {
  cursor: pointer;

  circle {
    fill: var(--primary-fade-24);
    stroke: var(--primary-5);
    stroke-width: 0.4;
  }

  text {
    fill: var(--color-text-1);
    font-size: 2px;
    font-weight: 600;
    font-family: var(--font-mono);
  }
}

.tooltip {
  position: absolute;
  top: var(--space-3);
  left: 50%;
  transform: translateX(-50%);
  display: flex;
  gap: var(--space-2);
  align-items: center;
  padding: 6px 12px;
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-2);
  border-radius: var(--dm-radius-1);
  font-size: var(--font-12);
  box-shadow: var(--shadow-card);
  pointer-events: none;

  strong {
    color: var(--color-text-1);
  }

  .code {
    color: var(--color-text-4);
    font-size: 11px;
  }

  .status {
    padding: 1px 6px;
    border-radius: var(--dm-radius-1);
    font-size: 10px;

    &.on {
      background: var(--status-online-fade);
      color: var(--status-online);
    }
    &.off {
      background: var(--status-offline-fade);
      color: var(--status-offline);
    }
  }
}

.toolbox {
  position: absolute;
  top: var(--space-3);
  right: var(--space-3);
  display: flex;
  flex-direction: column;
  gap: 2px;
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-2);
  border-radius: var(--dm-radius-1);
  padding: 4px;
  box-shadow: var(--shadow-card);

  button {
    width: 28px;
    height: 28px;
    border: 0;
    background: transparent;
    color: var(--color-text-2);
    cursor: pointer;
    font-size: 14px;
    border-radius: 2px;
    transition: background var(--duration-fast) var(--ease-out);

    &:hover {
      background: var(--color-bg-3);
    }
  }

  .zoom-label {
    margin-top: 2px;
    padding: 2px 4px;
    color: var(--color-text-4);
    font-size: 10px;
    font-family: var(--font-mono);
    text-align: center;
  }
}

.stat-box {
  position: absolute;
  bottom: var(--space-3);
  left: var(--space-3);
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-2);
  border-radius: var(--dm-radius-1);
  padding: var(--space-2) var(--space-3);
  font-size: var(--font-12);
  display: flex;
  flex-direction: column;
  gap: 4px;
  box-shadow: var(--shadow-card);

  .stat-line {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--color-text-3);

    strong {
      color: var(--color-text-1);
      font-family: var(--font-mono);
      margin-left: 2px;
    }
  }

  .dot {
    display: inline-block;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    flex-shrink: 0;

    &.online {
      background: var(--status-online);
    }
    &.offline {
      background: var(--status-offline);
    }
    &.cluster {
      background: var(--primary-5);
    }
  }
}

.no-coord-banner {
  position: absolute;
  top: var(--space-3);
  left: var(--space-3);
  padding: 6px 12px;
  background: var(--status-warning-fade);
  color: var(--status-warning);
  border-radius: var(--dm-radius-1);
  font-size: var(--font-12);

  strong {
    font-family: var(--font-mono);
  }
}

.phase-hint {
  position: absolute;
  bottom: var(--space-3);
  right: var(--space-3);
  color: var(--color-text-4);
  font-size: 10px;
}
</style>
