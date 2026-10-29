<script setup lang="ts">
/**
 * 通道位置「地图踩点」对话框。
 *
 * 为什么要有它：固定枪机不会上报经纬度，而通道编辑弹窗里那两个输入框要求
 * 用户自己知道经纬度 —— 现场没人会去查这个。踩点让"看到设备在哪"直接变成坐标。
 *
 * 坐标契约与 index.vue 的通道编辑**完全一致**（见 channelPositionForm.ts）：
 *   - 两框都有值 → 设置坐标
 *   - 两者都为 0 → 清除坐标
 * 这里不做第二套解释，校验直接复用 resolveChannelCoordPayload。
 */
import { computed, nextTick, ref, watch } from "vue";
import { MapPin, RotateCcw, Trash2 } from "@lucide/vue";
import { Message } from "@arco-design/web-vue";
import maplibregl, { Marker as MapLibreMarker, type Map as MapLibreMap } from "maplibre-gl";
import "maplibre-gl/dist/maplibre-gl.css";
import { resolveChannelCoordPayload } from "./channelPositionForm";

interface Props {
  visible: boolean;
  /** 通道名，仅用于标题。 */
  channelName: string;
  /** 地图底图样式地址（与设备列表地图同一份，避免两套观感）。 */
  styleUrl: string;
  initialLongitude?: number | null;
  initialLatitude?: number | null;
}

const props = withDefaults(defineProps<Props>(), {
  initialLongitude: null,
  initialLatitude: null
});

const emit = defineEmits<{
  (e: "update:visible", value: boolean): void;
  (e: "confirm", payload: { longitude: number; latitude: number }): void;
}>();

const container = ref<HTMLElement | null>(null);
const longitudeText = ref("");
const latitudeText = ref("");
const mapReady = ref(false);
let map: MapLibreMap | null = null;
let marker: MapLibreMarker | null = null;

const hasInitial = computed(() => !!(props.initialLongitude || props.initialLatitude));

const pickedText = computed(() => {
  const lng = Number(longitudeText.value);
  const lat = Number(latitudeText.value);
  if (!longitudeText.value || !latitudeText.value || Number.isNaN(lng) || Number.isNaN(lat)) return "尚未选点";
  return `${lng.toFixed(5)}, ${lat.toFixed(5)}`;
});

function ensureMarker(longitude: number, latitude: number) {
  if (!map) return;
  if (!marker) {
    marker = new MapLibreMarker({ color: "#2563eb", draggable: true }).setLngLat([longitude, latitude]).addTo(map);
    // 拖动标记点 = 重新选点：坐标框要跟着走，否则"图上拖了、保存的还是旧值"。
    marker.on("dragend", () => {
      const position = marker?.getLngLat();
      if (position) applyPick(position.lng, position.lat, false);
    });
    return;
  }
  marker.setLngLat([longitude, latitude]);
}

/** 写入坐标框并（可选）把地图视野带过去。 */
function applyPick(longitude: number, latitude: number, recenter = true) {
  longitudeText.value = longitude.toFixed(6);
  latitudeText.value = latitude.toFixed(6);
  ensureMarker(longitude, latitude);
  if (recenter && map) {
    map.easeTo({ center: [longitude, latitude], zoom: Math.max(map.getZoom(), 14), duration: 400 });
  }
}

function destroyMap() {
  marker?.remove();
  marker = null;
  map?.remove();
  map = null;
  mapReady.value = false;
}

function createMap() {
  if (!container.value) return;
  const hasCoordinate = !!(props.initialLongitude && props.initialLatitude);
  const center: [number, number] = hasCoordinate
    ? [props.initialLongitude as number, props.initialLatitude as number]
    : [116.3974, 39.9093];

  map = new maplibregl.Map({
    container: container.value,
    style: props.styleUrl,
    center,
    zoom: hasCoordinate ? 15 : 10,
    minZoom: 3,
    maxZoom: 22,
    attributionControl: false
  });
  map.addControl(new maplibregl.NavigationControl({ showCompass: false }), "top-right");

  map.on("load", () => {
    mapReady.value = true;
    // 容器在弹窗里，初始尺寸可能拿不到 —— 加载后强制重算一次，否则底图只画左上角一块。
    map?.resize();
    if (hasCoordinate) ensureMarker(center[0], center[1]);
  });

  map.on("click", event => {
    applyPick(event.lngLat.lng, event.lngLat.lat, false);
  });
}

watch(
  () => props.visible,
  async visible => {
    if (visible) {
      longitudeText.value = props.initialLongitude ? String(props.initialLongitude) : "";
      latitudeText.value = props.initialLatitude ? String(props.initialLatitude) : "";
      await nextTick();
      createMap();
      return;
    }
    destroyMap();
  }
);

function close() {
  emit("update:visible", false);
}

/** 手输坐标后回车/失焦生效：与地图上的标记点保持同一份真值。 */
function syncFromInputs() {
  const resolved = resolveChannelCoordPayload(longitudeText.value, latitudeText.value);
  if ("error" in resolved) return;
  if (resolved.payload.longitude === undefined || resolved.payload.latitude === undefined) return;
  if (resolved.payload.longitude === 0 && resolved.payload.latitude === 0) return;
  ensureMarker(resolved.payload.longitude, resolved.payload.latitude);
  map?.easeTo({ center: [resolved.payload.longitude, resolved.payload.latitude], zoom: Math.max(map.getZoom(), 13) });
}

function handleConfirm() {
  const resolved = resolveChannelCoordPayload(longitudeText.value, latitudeText.value);
  if ("error" in resolved) {
    Message.error(resolved.error);
    return;
  }
  if (resolved.payload.longitude === undefined || resolved.payload.latitude === undefined) {
    Message.warning("请在地图上点选位置，或手动填写经纬度");
    return;
  }
  emit("confirm", { longitude: resolved.payload.longitude, latitude: resolved.payload.latitude });
}

function handleClear() {
  emit("confirm", { longitude: 0, latitude: 0 });
}

function resetView() {
  if (!map) return;
  if (props.initialLongitude && props.initialLatitude) {
    map.easeTo({ center: [props.initialLongitude, props.initialLatitude], zoom: 15 });
    return;
  }
  map.easeTo({ center: [116.3974, 39.9093], zoom: 10 });
}
</script>

<template>
  <a-modal
    :visible="visible"
    :title="`设置位置 · ${channelName || '通道'}`"
    :footer="false"
    :width="720"
    unmount-on-close
    modal-class="channel-location-modal"
    @cancel="close"
  >
    <div class="location-picker">
      <p class="location-picker__hint">
        <MapPin :size="14" />
        在地图上单击选点，或直接填写经纬度。保存后该通道会以「人工」来源出现在设备地图上。
      </p>
      <div ref="container" class="location-picker__map">
        <div v-if="!mapReady" class="location-picker__map-loading">正在加载地图…</div>
      </div>
      <div class="location-picker__row">
        <label class="location-picker__field">
          <span>经度</span>
          <a-input
            v-model="longitudeText"
            size="small"
            placeholder="例如 116.99505"
            @blur="syncFromInputs"
            @press-enter="syncFromInputs"
          />
        </label>
        <label class="location-picker__field">
          <span>纬度</span>
          <a-input
            v-model="latitudeText"
            size="small"
            placeholder="例如 36.66237"
            @blur="syncFromInputs"
            @press-enter="syncFromInputs"
          />
        </label>
        <span class="location-picker__picked">当前：{{ pickedText }}</span>
      </div>
      <div class="location-picker__footer">
        <a-button size="small" @click="resetView">
          <template #icon><RotateCcw :size="14" /></template>
          复位
        </a-button>
        <a-button v-if="hasInitial" size="small" status="danger" @click="handleClear">
          <template #icon><Trash2 :size="14" /></template>
          清除坐标
        </a-button>
        <span class="location-picker__spacer"></span>
        <a-button size="small" @click="close">取消</a-button>
        <a-button size="small" type="primary" @click="handleConfirm">保存位置</a-button>
      </div>
    </div>
  </a-modal>
</template>

<style scoped>
.location-picker {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.location-picker__hint {
  display: flex;
  gap: 6px;
  align-items: center;
  margin: 0;
  font-size: 12px;
  color: #4e5969;
}

.location-picker__map {
  position: relative;
  height: 380px;
  overflow: hidden;
  background: #eef2f7;
  border-radius: 8px;
}

.location-picker__map-loading {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  color: #86909c;
}

.location-picker__row {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
}

.location-picker__field {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  font-size: 12px;
  color: #4e5969;
}

.location-picker__field :deep(.arco-input-wrapper) {
  width: 140px;
}

.location-picker__picked {
  font-size: 12px;
  color: #86909c;
}

.location-picker__footer {
  display: flex;
  gap: 8px;
  align-items: center;
}

.location-picker__spacer {
  flex: 1;
}
</style>
