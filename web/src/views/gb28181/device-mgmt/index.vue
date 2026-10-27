<script setup lang="ts">
import { computed, createVNode, nextTick, onMounted, reactive, ref, render, shallowRef, watch, onUnmounted } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Message, Modal } from "@arco-design/web-vue";
import maplibregl, { LngLatBounds, Marker as MapLibreMarker, type Map as MapLibreMap } from "maplibre-gl";
import "maplibre-gl/dist/maplibre-gl.css";
import {
  Activity,
  ArrowLeft,
  BarChart3,
  Copy,
  Crosshair,
  Eye,
  Grid2X2,
  History,
  Info,
  Joystick,
  Layers,
  List,
  Loader2,
  MapPinned as MapIcon,
  MoreHorizontal,
  Play,
  Pencil,
  RadioTower,
  RefreshCcw,
  Search,
  Square,
  Trash2,
  Video,
  X,
  Plus,
  Cctv,
  Camera,
  Bell,
  CircleQuestionMark
} from "@lucide/vue";
import { startPlay, stopPlay, type PlayResult } from "@/api/gb28181";
import { listZLMNodes, type ZLMNode } from "@/api/gb28181-zlm";
import {
  batchDeleteChannels,
  batchDeleteDevices,
  createDevice,
  deleteChannel,
  deleteDevice,
  getChannel,
  getChannelTimeline,
  getDevice,
  getDeviceCatalogRefreshProgress,
  listChannelMounts,
  listChannels,
  listDevices,
  listDeviceStatusEvents,
  listMapClusters,
  listMapMarkers,
  listMapTrajectory,
  getMapCenter,
  saveMapCenter,
  listDeviceSubscriptions,
  refreshDeviceCatalog,
  updateCloudRecording,
  updateChannelStreamTransport,
  updateChannel,
  updateDevice,
  type BatchDeleteResult,
  type AssetKind,
  type ChannelMount,
  type ChannelVO,
  type CatalogRefreshProgress,
  type CreateDeviceDTO,
  type DeviceVO,
  type DeviceOperationResult,
  type UpgradeOperation,
  type DeviceStatusEvent,
  type DeviceSubscription,
  type DirectoryDevice,
  type DirectoryNode,
  type MapCluster,
  type MapMarker,
  type MapTreeChannel,
  type MapTreeDevice,
  type OnlineStatus,
  type ProtocolOverride,
  type TimelineSlot
} from "./api";
import { getDictItemsByDictCodeAPI, type SystemDictItem } from "@/api/dictionary";
import { usePlaybackConsoleStore } from "@/store/modules/playback-console";
import { useThemeConfig } from "@/store/modules/theme-config";
import { hasPermission } from "@/hooks/usePermission";
import { storeToRefs } from "pinia";
import SubscriptionDialog from "./SubscriptionDialog.vue";
import DeviceMaintenanceMenu from "./DeviceMaintenanceMenu.vue";
import DeviceRebootDialog from "./DeviceRebootDialog.vue";
import DeviceFirmwareUpgradeDialog from "./DeviceFirmwareUpgradeDialog.vue";
import DeviceMaintenanceRecordsDialog from "./DeviceMaintenanceRecordsDialog.vue";
import DeviceStatusFactsPanel from "./DeviceStatusFactsPanel.vue";
import StorageCardStatusPanel from "./StorageCardStatusPanel.vue";
import CatalogRefreshProgressDialog from "./CatalogRefreshProgressDialog.vue";
import DeviceControlPanel from "./DeviceControlPanel.vue";
import SnapshotConfigPanel from "./SnapshotConfigPanel.vue";
import { buildFactChannelOptions } from "./deviceFactChannel";
import { createMaintenanceActivity, isRebootPending, isUpgradeUnresolved } from "./maintenanceActivity";
import DirectoryPanel from "./components/DirectoryPanel.vue";
import MapChannelTree, { type MapScopeSelection } from "./components/MapChannelTree.vue";
import TrafficTrend from "./components/TrafficTrend.vue";
import ViewerTable from "./components/ViewerTable.vue";
import { cloudRecordingStateMeta, mergeCloudRecordingState } from "./cloudRecordingState";
import { catalogShapeFromAttributes, catalogShapeText } from "./channelAttributeText";
import { useChannelAttributeEntries } from "./useChannelAttributeDict";
import { coordPatchAfterSave, positionSourceText, resolveChannelCoordPayload } from "./channelPositionForm";
import { buildMapCenter, formatMapCenter, normalizeMapCenter, type MapCenter } from "./mapView";
import {
  describeEmptyTrajectory,
  interpolateAlongTrajectory,
  trajectoryDistanceMeters,
  trajectoryEndpoints,
  trajectoryLineFeature,
  type TrackPoint
} from "./mapTrajectory";
import MapTrajectoryControls from "./MapTrajectoryControls.vue";
import ChannelLocationPicker from "./ChannelLocationPicker.vue";
import PlayWindow from "../components/PlayWindow.vue";
import {
  createDirectoryState,
  directoryQuery,
  findDirectoryNode,
  normalizeDirectoryView,
  selectDirectory,
  switchDirectoryView,
  type DirectoryState
} from "./directoryState";
import { normalizeProtocolOverride, protocolOverrideAfterSave } from "./protocolOverrideState";
import { consumeDeviceMgmtReturnSnapshot, saveDeviceMgmtReturnSnapshot } from "../device-record-playback/returnSnapshot";
import { useDeviceStatusLabel } from "../useDeviceStatusDict";
import { useGuide } from "@/components/s-guide/useGuide";
import { DEVICE_GUIDE_STEPS, DEVICE_GUIDE_STORAGE_KEY } from "./deviceGuide";
import { DICT_CODE_MEDIA_NODE_STATE, MEDIA_NODE_STATE_LABEL_FALLBACK } from "../mediaNodeState";
import { useDictLabel } from "@/hooks/useDictOptions";

type ViewMode = "list" | "card" | "map";
type DrawerTarget = { type: "channel"; id: number } | { type: "device"; id: number };
type DeviceListReturnSnapshot = {
  assetKind: AssetKind;
  viewMode: ViewMode;
  navigationMode: "device" | "directory";
  selectedDevice: DeviceVO | null;
  selectedDeviceSummary: DirectoryDevice | null;
  keyword: string;
  deviceIdFilter: string;
  statusFilter: OnlineStatus | undefined;
  directoryState: DirectoryState;
  selectedDirectories: Record<"administrative" | "department" | "business", DirectoryNode | null>;
  page: number;
  listPageSize: number;
  cardPageSize: number;
  selectedRowKeys: number[];
};

const viewModeStorageKey = "uvp.gb28181.device-mgmt.view-mode";
const AUTO_REFRESH_INTERVAL_SECONDS = 10;
function initialViewMode(): ViewMode {
  if (typeof window === "undefined") return "card";
  try {
    const stored = window.localStorage.getItem(viewModeStorageKey);
    return stored === "list" || stored === "card" || stored === "map" ? stored : "card";
  } catch {
    return "card";
  }
}
const viewMode = ref<ViewMode>(initialViewMode());
const router = useRouter();
const route = useRoute();
const returnSnapshot = consumeDeviceMgmtReturnSnapshot(typeof route.query.returnKey === "string" ? route.query.returnKey : null);
const assetKind = ref<AssetKind>(returnSnapshot?.assetKind || (viewMode.value === "map" ? "channel" : "device"));
const navigationMode = ref<"device" | "directory">("directory");
const selectedDevice = ref<DeviceVO | null>(null);
const selectedDeviceSummary = ref<DirectoryDevice | null>(null);
const selectedDeviceContext = computed(() => selectedDevice.value || selectedDeviceSummary.value);
const deviceListReturnSnapshot = ref<DeviceListReturnSnapshot | null>(null);
let restoringDeviceListSnapshot = false;
let directoryDeviceRequestVersion = 0;
const channelScope = computed(() =>
  navigationMode.value === "device" ? { deviceId: deviceIdFilter.value || undefined } : directoryQuery(directoryState.value)
);
const keyword = ref("");
const keywordInput = ref<HTMLInputElement | null>(null);
const deviceIdFilter = ref("");
const statusFilter = ref<OnlineStatus | undefined>();
// 「在线状态」文案由 `device_status` 字典驱动；媒体节点状态由 `media_node_state` 字典驱动。
const deviceStatusLabel = useDeviceStatusLabel();
const mediaNodeStateLabel = useDictLabel(DICT_CODE_MEDIA_NODE_STATE, MEDIA_NODE_STATE_LABEL_FALLBACK);
const directoryState = ref(createDirectoryState());
const selectedDirectories = ref<Record<"administrative" | "department" | "business", DirectoryNode | null>>({
  administrative: null,
  department: null,
  business: null
});
const selectedDirectory = computed(() =>
  navigationMode.value === "directory" ? selectedDirectories.value[directoryState.value.view] : null
);
const directoryPanelRef = ref<InstanceType<typeof DirectoryPanel> | null>(null);
const mapChannelTreeRef = ref<InstanceType<typeof MapChannelTree> | null>(null);
/** 从左侧通道树点过来、正在地图上高亮的那一路通道。 */
const mapSelectedChannelId = ref<number | null>(null);
/**
 * 地图左栏「范围」下拉的值，形如 `department|<key>`。
 *
 * 刻意从 directoryState 派生而不是另存一份：范围一改，markers / clusters /
 * 可定位通道树 三处必须同时收窄，多存一份状态就迟早漂成两套口径。
 */
const mapScopeValue = computed(() => {
  const view = directoryState.value.view;
  const node = selectedDirectories.value[view];
  // 行政区划也算（它同样能筛地图）；下拉里没有对应项时由 mapScopeLabel 补一项。
  return node ? `${view}|${node.key}` : "";
});
/**
 * 范围的可读名。行政区划也能筛地图，但下拉里只放 部门/业务分组（行政区划层级深、条目多），
 * 所以行政区划生效时靠这个标签在下拉里补一项，避免"下拉写着全部、地图却只显示一个区"。
 */
const mapScopeLabel = computed(() => {
  const node = selectedDirectory.value;
  if (!node) return "";
  const viewLabel = { administrative: "行政区划", department: "部门", business: "业务分组" }[directoryState.value.view];
  return `${viewLabel} / ${node.name}`;
});
const drawerVisible = ref(false);
const subscriptionDialogVisible = ref(false);
const subscriptionDevice = ref<DeviceVO | null>(null);
const rebootVisible = ref(false);
const upgradeVisible = ref(false);
const recordsVisible = ref(false);
const recordsType = ref<"reboot" | "upgrade">("reboot");
const recordsOperationId = ref<string>();
const maintenanceActivity = createMaintenanceActivity();
const maintenanceChecking = ref(false);
const maintenanceCheckError = ref("");
let maintenanceOpenVersion = 0;
let maintenancePoll: ReturnType<typeof setInterval> | undefined;
const maintenanceDevice = ref<DeviceVO | null>(null);
const rebootOperationId = ref<string>();
const currentRebootResult = computed(() => {
  const operation = maintenanceDevice.value ? maintenanceActivity.devices[maintenanceDevice.value.id]?.reboot : undefined;
  return operation &&
    (isRebootPending(operation) || (rebootOperationId.value && operation.operationId === rebootOperationId.value))
    ? operation
    : null;
});
const drawerTarget = ref<DrawerTarget | null>(null);
const drawerLoading = ref(false);
const drawerWidth = computed(() => (drawerTarget.value?.type === "device" ? "min(760px, 92vw)" : "min(640px, 92vw)"));
const channelOperationVisible = ref(false);
const channelOperationTarget = ref<ChannelVO | null>(null);
type ChannelOperationTab = "control" | "snapshot" | "status" | "storage";
const channelOperationTab = ref<ChannelOperationTab>("control");
const channelOperationOptions = computed(() =>
  channelOperationTarget.value ? buildFactChannelOptions([channelOperationTarget.value]) : []
);
const rowsLoading = ref(false);
const mapLoading = ref(false);
const page = ref(1);
const listPageSize = ref(10);
const cardPageSize = ref(9);
if (returnSnapshot) {
  viewMode.value = returnSnapshot.viewMode;
  navigationMode.value = returnSnapshot.navigationMode || (returnSnapshot.deviceIdFilter ? "device" : "directory");
  keyword.value = returnSnapshot.keyword;
  deviceIdFilter.value = returnSnapshot.deviceIdFilter;
  statusFilter.value = returnSnapshot.statusFilter;
  const restoredDirectoryView = normalizeDirectoryView(returnSnapshot.directoryView);
  directoryState.value = {
    ...directoryState.value,
    view: restoredDirectoryView,
    selectedKey: {
      ...directoryState.value.selectedKey,
      [restoredDirectoryView]: restoredDirectoryView === returnSnapshot.directoryView ? returnSnapshot.directorySelectedKey : null
    }
  };
  page.value = returnSnapshot.page;
  listPageSize.value = returnSnapshot.listPageSize;
  cardPageSize.value = returnSnapshot.cardPageSize;
}
const pageSize = computed({
  get: () => (viewMode.value === "card" ? cardPageSize.value : listPageSize.value),
  set: (value: number) => {
    if (viewMode.value === "card") cardPageSize.value = value;
    else listPageSize.value = value;
  }
});
const total = ref(0);
const selectedRowKeys = ref<number[]>([]);
const themeStore = useThemeConfig();
const { darkMode } = storeToRefs(themeStore);
const mapZoom = ref(10);
const mapMinZoom = 5;
const mapMaxZoom = 22;
const mapBaseStyle = ref<"light" | "dark">(darkMode.value ? "dark" : "light");
// 底图**不再提供手动切换**（2026-10-22 决定暂时去掉）：两套 JSON 都是 OpenFreeMap 的矢量瓦片
// （道路/建筑/注记），只差配色、没有卫星影像，留着下拉容易被当成"能切卫星"。
// 现在底图只跟随系统主题：watch(darkMode) → reloadMapStyle()。
const mapSearchQuery = ref("");
// 单独画通道标记点的最小 zoom；低于它改用聚合气泡（两者互斥，见 renderMapOverlays）。
const mapMarkerMinZoom = 14;
// 单点（或所有点重合）时 fitBounds 会把缩放一路顶到 mapMaxZoom(22)，直接落到街景级。
// 给个"街区级"上限，同时必须高于 mapMarkerMinZoom，否则 fit 完反而看不到标记点。
const mapSinglePointFitZoom = 16;
// 聚合气泡按数量分级(尺寸 + 字号)。行业通行做法是分 3~4 档:
// 固定尺寸下 "1" 和 "1000" 视觉重量一样,而且四位数会撑破圆形。
function clusterBubbleTier(count: number) {
  if (count <= 9) return { size: 34, font: 12 };
  if (count <= 99) return { size: 40, font: 13 };
  if (count <= 999) return { size: 48, font: 14 };
  return { size: 56, font: 14 };
}
function clusterBubbleLabel(count: number) {
  return count > 999 ? "999+" : String(count);
}
const mapContainer = ref<HTMLElement | null>(null);
const mapReady = ref(false);
const mapFirstRender = ref(false);
const mapError = ref("");
const mapCursorLongitude = ref(116.3974);
const mapCursorLatitude = ref(39.9093);

// ── 地图业务闭环：卡片 / 视频 / 轨迹 / 默认中心 / 踩点 ──────────────────
//
// 这块状态刻意都放在本文件而不是子组件：地图实例、marker、popup 三者必须共享
// 同一个"当前选中的通道"，拆开就会互相抢生命周期（典型表现：关卡片后轨迹还在画）。

/** 当前打开的地图卡片气泡。⛔ 不是 ref：它是 MapLibre 的实例对象，不需要响应式代理。 */
let mapPopup: maplibregl.Popup | null = null;

/**
 * 实时视频小窗的挂载点：MapLibre popup 里那个容器 div。
 * ⛔ 必须是 `shallowRef`：这张卡片是 **Teleport 进 popup 的**，普通 `ref` 会把 DOM
 *    节点包成响应式代理，MapLibre 拿代理节点做 appendChild/remove 时很容易出现
 *    "父节点不认识这个子节点"的怪状态（弹窗关不掉、播放器实例泄漏）。
 */
const videoPopupHost = shallowRef<HTMLElement | null>(null);
/** 当前视频小窗的 popup。⛔ 同 mapPopup：MapLibre 实例不进响应式。 */
let mapVideoPopup: maplibregl.Popup | null = null;
const videoLoading = ref(false);
/** `status` 一起存：小窗里要按在线状态决定给不给「播放控制台」。 */
const videoTarget = ref<{ id: number; deviceId: string; channelId: string; name: string; status: number } | null>(null);
const videoPlaying = ref<PlayResult | null>(null);

const trajectoryVisible = ref(false);
const trajectoryLoading = ref(false);
const trajectoryTarget = ref<{
  id: number;
  channelId: string;
  deviceId: string;
  name: string;
  hasCoordinate: boolean;
} | null>(null);
const trajectoryPoints = ref<TrackPoint[]>([]);
const trajectoryEmptyText = ref("");
const trajectoryTruncated = ref(false);
const trajectoryDistance = ref(0);
const trajectoryProgress = ref(0);
const trajectoryPlaying = ref(false);
const trajectorySpeed = ref(1);
const trajectoryCurrentTime = ref("");

const mapCenterSaving = ref(false);
/**
 * 「设为默认中心」是**两步**：先进入选点态（地图上点一下挑位置），确认后才落库。
 * 早先是"点按钮＝把当前视野存下来"，语义和按钮名对不上 —— 用户以为是在地图上挑点，
 * 结果存的是他恰好拖到的地方。缩放仍然沿用当前级别（默认视野本来就是"一块区域"）。
 */
const mapCenterPicking = ref(false);
const mapCenterPick = ref<{ longitude: number; latitude: number } | null>(null);

const locationPickerVisible = ref(false);
const locationPickerTarget = ref<{ id: number; name: string; longitude: number; latitude: number } | null>(null);

const canViewDevices = computed(() => hasPermission("gb28181:device:view"));
const canStartPlayback = computed(() => hasPermission("gb28181:play:start"));
const canQueryDeviceRecords = computed(() => hasPermission("gb28181:device-record:query"));
const canViewTraffic = computed(() => hasPermission("gb28181:traffic:view"));
const canViewMaintenance = computed(() => hasPermission("gb28181:device:maintenance:view"));
const canRebootDevice = computed(() => hasPermission("gb28181:device:reboot"));
/**
 * 「存储卡」页的**格式化**权限（`gb28181:device:format_sd`）。
 *
 * ⛔ 它与读权限（canViewDeviceFacts = gb28181:ptz:view）是两个码，不能合并：
 *    能"看这张卡还剩多少空间"不等于能"把卡上的录像全抹掉"。
 * ⛔ 也不与 `gb28181:device:control` 合并 —— 后端为此专门开了独立路由
 *    `POST /channel/:id/storage-cards/format`（塞进 device-control 的 action
 *    在 casbin 层就与普通设备控制同码，独立权限码会形同虚设）。
 */
const canFormatStorageCard = computed(() => hasPermission("gb28181:device:format_sd"));
const canUpgradeDevice = computed(() => hasPermission("gb28181:device:upgrade"));
const canAddDevice = computed(() => hasPermission("gb28181:device:add"));
const canEditDevice = computed(() => hasPermission("gb28181:device:edit"));
const canDeleteDevice = computed(() => hasPermission("gb28181:device:delete"));
const canDeleteChannel = computed(() => hasPermission("gb28181:channel:delete"));
const canRefreshCatalog = computed(() => hasPermission("gb28181:device:catalog:refresh"));
const canManageSubscriptions = computed(() => hasPermission("gb28181:device:subscription:manage"));
const canEditChannel = computed(() => hasPermission("gb28181:channel:edit"));
const canEditTransport = computed(() => hasPermission("gb28181:channel:stream-transport:update"));
const canUpdateRecording = computed(() => hasPermission("gb28181:channel:recording:update"));
const canStopPlayback = computed(() => hasPermission("gb28181:play:stop"));

/**
 * 新手引导。
 *
 * 只在有设备查看权限时才有意义 —— 没权限时页面连工具栏都不渲染，锚点全取不到，
 * 弹出来就是几个孤零零的居中气泡（`buildGuideSteps` 会把找不到锚点的步骤丢掉）。
 *
 * 存储键见 `deviceGuide.ts`：带上版本号，改引导内容时改版本号即可让老用户重看一遍。
 */
const guide = useGuide({ steps: DEVICE_GUIDE_STEPS, storageKey: DEVICE_GUIDE_STORAGE_KEY });
/** 手动入口。已经在放的时候再点一次不做任何事（避免把浮层关掉）。 */
function startDeviceGuide() {
  guide.start();
}
/**
 * 「设备状态」「存储卡」两页的读权限。
 * ⛔ 与 ptz 控制分开：这两个接口读的是 `gb28181:ptz:view`（见 routes.go 里
 *    channel/:id/device-status、channel/:id/storage-cards 的登记），不是 device:view。
 *    按 device:view 放行会让只有设备查看权的人点开就吃 403。
 */
const canViewDeviceFacts = computed(() => hasPermission("gb28181:ptz:view"));
/**
 * 「设备控制」页的权限：下发命令走 `gb28181:device:control`，抓拍配置走 `gb28181:device:snapshot`
 * —— 与播放控制台侧栏「高级」原来那两枚权限码**完全一致**（搬家不改门禁）。
 * ⛔ 页签放在 `canViewDeviceFacts` 之外：只给控制权、不给 ptz:view 的账号照样要能下发。
 */
const canControlDevice = computed(() => hasPermission("gb28181:device:control"));
const canSnapshotDevice = computed(() => hasPermission("gb28181:device:snapshot"));
const canUseDeviceControl = computed(() => canControlDevice.value || canSnapshotDevice.value);
const defaultLightMapStyleUrl = `${import.meta.env.BASE_URL}map-styles/uvp-light.json`;
const mapStyleUrls = {
  light: (import.meta.env.VITE_MAP_STYLE_LIGHT_URL as string | undefined) || defaultLightMapStyleUrl,
  dark: (import.meta.env.VITE_MAP_STYLE_DARK_URL as string | undefined) || `${import.meta.env.BASE_URL}map-styles/uvp-dark.json`
};
const onlineChannelTotal = ref(0);
const offlineChannelTotal = ref(0);
const onlineDeviceTotal = ref(0);
const offlineDeviceTotal = ref(0);
const statEntityLabel = computed(() => (assetKind.value === "device" ? "设备" : "通道"));
const statOnlineTotal = computed(() => (assetKind.value === "device" ? onlineDeviceTotal.value : onlineChannelTotal.value));
const statOfflineTotal = computed(() => (assetKind.value === "device" ? offlineDeviceTotal.value : offlineChannelTotal.value));
const statTotal = computed(() => statOnlineTotal.value + statOfflineTotal.value);
const statOnlineRatePercent = computed(() =>
  statTotal.value === 0 ? 0 : Math.round((statOnlineTotal.value / statTotal.value) * 100)
);
const ringDisplayRate = ref(0);
let ringAnimFrame: number | null = null;

function animateRingRate(target: number) {
  if (ringAnimFrame !== null) {
    cancelAnimationFrame(ringAnimFrame);
    ringAnimFrame = null;
  }
  if (window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) {
    ringDisplayRate.value = target;
    return;
  }
  const start = ringDisplayRate.value;
  const startTime = performance.now();
  const duration = 750;
  const tick = (now: number) => {
    const t = Math.min(1, (now - startTime) / duration);
    const eased = 1 - Math.pow(1 - t, 3);
    ringDisplayRate.value = Math.round(start + (target - start) * eased);
    if (t < 1) {
      ringAnimFrame = requestAnimationFrame(tick);
    } else {
      ringDisplayRate.value = target;
      ringAnimFrame = null;
    }
  };
  ringAnimFrame = requestAnimationFrame(tick);
}

watch(statOnlineRatePercent, value => animateRingRate(value));
const autoRefreshCountdown = ref(AUTO_REFRESH_INTERVAL_SECONDS);
const refreshCountdownTimer = ref<number | null>(null);
const isMacPlatform = computed(() => typeof navigator !== "undefined" && /Mac|iPhone|iPad|iPod/.test(navigator.platform));

let keywordSearchTimer: ReturnType<typeof setTimeout> | null = null;
let suppressKeywordSearch = false;
let mapInstance: MapLibreMap | null = null;
let mapMoveHandler: (() => void) | null = null;
let mapMouseMoveHandler: ((event: maplibregl.MapMouseEvent) => void) | null = null;
let mapAutoFitPending = true;
// 底图加载 12s 兜底定时器句柄(window.setTimeout 返回 number),销毁地图时必须清除,防跨实例污染
let mapFirstRenderTimer: number | null = null;
// 切换底图时"揭遮罩"的兜底定时器（见 reloadMapStyle），同样要在销毁时清掉
let mapStyleSwitchTimer: number | null = null;
/**
 * 「选取默认中心」点地图取坐标的监听句柄。
 * ⛔ 这个监听是**常驻**的（attachMapLifecycle 里挂一次），进出选点态只翻标志位，
 *    不去 on/off 增删 —— 增删会和 destroyMap 的清理抢时序，出现"销毁后回调还在跑"。
 */
let mapClickHandler: ((event: maplibregl.MapMouseEvent) => void) | null = null;
/** 选点针：跟着用户点到的位置走。退出选点态必须摘掉，否则下次进来会留着上一次的针。 */
let mapCenterPickMarker: MapLibreMarker | null = null;
// 地图数据请求代次:响应返回时与当前代次不一致则丢弃
let mapDataSeq = 0;
const mapMarkers = new Map<number, MapLibreMarker>();
const mapClusters = new Map<string, MapLibreMarker>();

const channels = ref<ChannelVO[]>([]);
const devices = ref<DeviceVO[]>([]);
const markers = ref<MapMarker[]>([]);
const clusters = ref<MapCluster[]>([]);
const channelDetail = ref<ChannelVO | null>(null);
const deviceDetail = ref<DeviceVO | null>(null);
const deviceSubscriptions = ref<DeviceSubscription[]>([]);
const statusEventVisible = ref(false);
const statusEventDevice = ref<DeviceVO | null>(null);
const statusEventList = ref<DeviceStatusEvent[]>([]);
const statusEventTotal = ref(0);
const statusEventPage = ref(1);
const statusEventPageSize = 10;
const statusEventLoading = ref(false);
const statusEventLoadingMore = ref(false);
const statusEventError = ref("");
const statusEventLoadMoreError = ref("");
const statusEventScroll = ref<HTMLElement | null>(null);
const statusEventHasMore = computed(() => statusEventList.value.length < statusEventTotal.value);
let statusEventRequestVersion = 0;
const runtimeActiveTab = ref<"status" | "traffic" | "viewers">("status");
const runtimeChannelCode = ref("");
const runtimeChannels = ref<ChannelVO[]>([]);
const runtimeChannelsLoading = ref(false);
const runtimeChannelsError = ref("");
const channelMounts = ref<ChannelMount[]>([]);
const timeline = ref<TimelineSlot[]>([]);
const editDeviceVisible = ref(false);
const editDeviceForm = ref<{
  deviceId: string;
  alias: string;
  name: string;
  zlmNodeId: number;
  protocolOverride: ProtocolOverride;
}>({
  deviceId: "",
  alias: "",
  name: "",
  zlmNodeId: 0,
  protocolOverride: "auto"
});
const editingDevice = ref(false);
const editingDeviceId = ref("");
const zlmNodes = ref<ZLMNode[]>([]);
const zlmNodesLoading = ref(false);
const zlmNodesError = ref("");
const zlmNodeOptions = computed(() => {
  const options = [
    { value: 0, label: "自动调度", disabled: false },
    ...zlmNodes.value.map(mediaNode => ({
      value: mediaNode.id,
      label: `${mediaNode.name} · ${mediaNode.host}:${mediaNode.apiPort} · ${mediaNodeStateLabel(mediaNode.state)}`,
      disabled: mediaNode.state !== "active" && mediaNode.id !== editDeviceForm.value.zlmNodeId
    }))
  ];
  if (editDeviceForm.value.zlmNodeId > 0 && !zlmNodes.value.some(mediaNode => mediaNode.id === editDeviceForm.value.zlmNodeId)) {
    options.push({
      value: editDeviceForm.value.zlmNodeId,
      label: `节点 #${editDeviceForm.value.zlmNodeId} · 已删除或无权限`,
      disabled: true
    });
  }
  return options;
});
let originalProtocolOverride: ProtocolOverride = "auto";
let originalZLMNodeID = 0;
const editChannelVisible = ref(false);
// 坐标来源: '' 无 / 'catalog' 目录 / 'mobile' 实时上报 / 'manual' 人工录入
const editChannelPositionSource = ref("");
// 坐标输入用 string 承载: 空串 = 本次不修改坐标, 数字 = 设置, 两者都填 0 = 清除。
// 用 a-input-number 的话空值表达能力弱, 这里自己控格式。
const editChannelLongitude = ref("");
const editChannelLatitude = ref("");
function emptyEditChannelForm() {
  return {
    channelId: "",
    deviceId: "",
    alias: "",
    name: "",
    manufacturer: "",
    model: "",
    ptzType: 0,
    streamTransport: "UDP",
    onDemandLive: true
  };
}
const editChannelForm = ref(emptyEditChannelForm());
const editingChannel = ref(false);
const editingChannelId = ref(0);
const ptzTypeOptions = ref<SystemDictItem[]>([]);
const playbackConsole = usePlaybackConsoleStore();
const cloudRecordingLoading = ref<Set<number>>(new Set());
const viewOptions: Array<{ label: string; value: ViewMode; icon: any }> = [
  { label: "列表", value: "list", icon: List },
  { label: "卡片", value: "card", icon: Grid2X2 },
  { label: "地图", value: "map", icon: MapIcon }
];
const hasFilters = computed(() =>
  Boolean(
    keyword.value || (deviceIdFilter.value && !deviceListReturnSnapshot.value) || statusFilter.value || selectedDirectory.value
  )
);
const selectedCount = computed(() => selectedRowKeys.value.length);
const tablePagination = computed(() => ({
  current: page.value,
  pageSize: pageSize.value,
  total: total.value,
  showPageSize: true,
  showTotal: true,
  showJumper: true,
  pageSizeOptions: [10, 20, 50, 100]
}));

watch(viewMode, async mode => {
  deviceListReturnSnapshot.value = null;
  ++channelRequestVersion;
  ++deviceRequestVersion;
  rowsLoading.value = false;
  selectedRowKeys.value = [];
  page.value = 1;
  if (mode === "map") {
    assetKind.value = "channel";
    await nextTick(ensureMap);
  } else {
    // ⛔ 切走时要连轨迹面板一起关：地图实例被销毁后轨迹图层已经没了，
    //    留着面板会变成"显示着一条轨迹，但图上什么都没有"。
    closeTrajectory();
    closeMarkerCard();
    destroyMap();
  }
  void refreshMainData();
  void refreshStats();
});
watch(viewMode, mode => {
  try {
    window.localStorage.setItem(viewModeStorageKey, mode);
  } catch {
    // Storage can be unavailable in privacy-restricted browser contexts.
  }
});
watch(darkMode, value => {
  mapBaseStyle.value = value ? "dark" : "light";
  reloadMapStyle();
});
watch(statusFilter, () => {
  if (restoringDeviceListSnapshot) return;
  page.value = 1;
  refreshMainData();
  refreshStats();
});
watch(keyword, () => {
  if (suppressKeywordSearch) {
    suppressKeywordSearch = false;
    return;
  }
  cancelKeywordSearch();
  keywordSearchTimer = setTimeout(() => {
    keywordSearchTimer = null;
    onSearch();
  }, 300);
});

function relTime(value?: string | null) {
  if (!value || value.startsWith("0001-01-01")) return "未上报";
  const time = new Date(value).getTime();
  if (Number.isNaN(time)) return "未上报";
  const diff = Math.max(0, Math.floor((Date.now() - time) / 1000));
  if (diff < 60) return `${diff || 1} 秒前`;
  if (diff < 3600) return `${Math.floor(diff / 60)} 分钟前`;
  if (diff < 86400) return `${Math.floor(diff / 3600)} 小时前`;
  return `${Math.floor(diff / 86400)} 天前`;
}
function displayName(item: { alias?: string; name?: string; channelId?: string; deviceId?: string }) {
  return item.alias?.trim() || item.name?.trim() || item.channelId || item.deviceId || "未命名";
}

function snapshotImageUrl(channel: Pick<ChannelVO, "snapshotUrl" | "snapshotAt">) {
  const url = channel.snapshotUrl?.trim() ?? "";
  const snapshotAt = channel.snapshotAt?.trim();
  if (!url || !snapshotAt) return url;
  const separator = url.includes("?") ? "&" : "?";
  return `${url}${separator}snapshotAt=${encodeURIComponent(snapshotAt)}`;
}
function deviceNameText(item: { alias?: string | null; name?: string | null }) {
  return item.alias?.trim() || item.name?.trim() || "-";
}
function channelPercentage(record: DeviceVO): string {
  if (!record.channelCount) return "0%";
  return `${((record.channelOnlineCount / record.channelCount) * 100).toFixed(0)}%`;
}
function channelStatusClass(record: DeviceVO): string {
  if (!record.channelCount) return "empty";
  const percentage = (record.channelOnlineCount / record.channelCount) * 100;
  if (percentage === 0) return "offline";
  if (percentage < 50) return "warning";
  return "healthy";
}
function modelVersionText(item: { model?: string; firmware?: string }) {
  return [item.model, item.firmware].filter(Boolean).join(" / ") || "-";
}
function vendorText(item: { manufacturer?: string; model?: string }) {
  return [item.manufacturer, item.model].filter(Boolean).join(" / ") || "未上报";
}
function locationText(item: ChannelVO | MapMarker) {
  return !item.latitude || !item.longitude ? "无坐标" : `${item.longitude.toFixed(5)}, ${item.latitude.toFixed(5)}`;
}
function dateTime(value?: string | null) {
  if (!value || value.startsWith("0001-01-01")) return "-";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return "-";
  return d.toLocaleString("zh-CN", { hour12: false });
}
function endpointText(item: { ip?: string; port?: number; transport?: string }) {
  if (!item.ip) return "-";
  const addr = item.port ? `${item.ip}:${item.port}` : item.ip;
  const protocol = item.transport ? item.transport.toLowerCase() : "udp";
  return `${protocol}://${addr}`;
}
function transportText(value?: string | null) {
  return value ? value.toUpperCase() : "-";
}
function streamTransportText(value?: string | null) {
  return value ? value.toUpperCase() : "-";
}
function cameraTypeText(ptzType?: number | null) {
  if (ptzType === null || ptzType === undefined) return "未知";
  const item = ptzTypeOptions.value.find(opt => Number(opt.value) === ptzType);
  return item?.name || "未知";
}
// 通道详情里的「设备上报属性」区块(GB/T 28181 附录 A / §9.3.1)。
// 2016 与 2022 的版本独有属性(2016: 位置类型/用途;2022: 光电成像类型/采集部位类型)并存展示,
// 哪一组有值就说明设备报的是哪一版目录形态 —— 不读设备声明的 effectiveGbVersion(它有 default:2016,
// 对 2022 设备会误判)。映射与判定逻辑都在 channelAttributeText.ts,此处只取数;
// 六个值域改由字典驱动(只读展示),查表来自 useChannelAttributeDict。
const channelAttributeEntriesOf = useChannelAttributeEntries();
const channelAttributeRows = computed(() => channelAttributeEntriesOf(channelDetail.value ?? {}));
const channelCatalogShapeText = computed(() => catalogShapeText(catalogShapeFromAttributes(channelDetail.value ?? {})));
function channelAttributeScopeText(scope: "both" | "2016" | "2022") {
  if (scope === "2016") return "仅 2016";
  if (scope === "2022") return "仅 2022";
  return "两版共有";
}
function protocolVersionText(value?: string | null) {
  if (value === "2022") return "GB/T 28181-2022";
  if (value === "2016") return "GB/T 28181-2016";
  return "未知 / 默认 2016";
}
function protocolSourceText(value?: string | null) {
  return (
    ({ register: "设备注册上报", override: "手动覆盖", history: "历史档案", default: "平台默认" } as Record<string, string>)[
      value || ""
    ] || "未说明"
  );
}
function protocolVersionTooltip(item: {
  effectiveVersion?: string | null;
  effectiveVersionSource?: string | null;
  reportedVersion?: string | null;
}) {
  return [
    protocolVersionText(item.effectiveVersion),
    protocolSourceText(item.effectiveVersionSource),
    item.reportedVersion ? `X-GB-Ver ${item.reportedVersion}` : ""
  ]
    .filter(Boolean)
    .join(" · ");
}
function copyText(value?: string | null) {
  const text = (value || "").trim();
  if (!text) return;
  if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
    navigator.clipboard.writeText(text).then(
      () => Message.success({ content: "已复制", duration: 1500 }),
      () => Message.error({ content: "复制失败", duration: 1500 })
    );
  }
}
const deviceSubscriptionSummary = computed(() => {
  const byKind = new Map(deviceSubscriptions.value.map(subscription => [subscription.kind, subscription]));
  return [
    { kind: "catalog", label: "目录", status: byKind.get("catalog")?.status || "disabled" },
    { kind: "mobile_position", label: "位置", status: byKind.get("mobile_position")?.status || "disabled" },
    { kind: "alarm", label: "报警", status: byKind.get("alarm")?.status || "disabled" },
    { kind: "ptz_precise_position", label: "PTZ 位置", status: byKind.get("ptz_precise_position")?.status || "disabled" }
  ];
});

function subscriptionStatusText(status: DeviceSubscription["status"] | "disabled") {
  return { disabled: "未启用", pending: "建立中", active: "已启用", degraded: "异常", expired: "已过期" }[status];
}
function onlineRatePercent(rate?: number) {
  if (rate == null || Number.isNaN(rate)) return 0;
  if (rate <= 1) return Math.round(rate * 100);
  return Math.round(rate);
}
function keepaliveIntervalText(seconds?: number) {
  if (!seconds || seconds <= 0) return "-";
  if (seconds < 60) return `${seconds} 秒`;
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return s === 0 ? `${m} 分钟` : `${m} 分 ${s} 秒`;
}
function showDeviceChannels(record: DeviceVO) {
  if (!canViewDevices.value) return;
  if (assetKind.value === "device") {
    deviceListReturnSnapshot.value = {
      assetKind: assetKind.value,
      viewMode: viewMode.value,
      navigationMode: navigationMode.value,
      selectedDevice: selectedDevice.value,
      selectedDeviceSummary: selectedDeviceSummary.value,
      keyword: keyword.value,
      deviceIdFilter: deviceIdFilter.value,
      statusFilter: statusFilter.value,
      directoryState: {
        ...directoryState.value,
        selectedKey: { ...directoryState.value.selectedKey },
        expandedKeys: { ...directoryState.value.expandedKeys },
        listReset: { ...directoryState.value.listReset }
      },
      selectedDirectories: { ...selectedDirectories.value },
      page: page.value,
      listPageSize: listPageSize.value,
      cardPageSize: cardPageSize.value,
      selectedRowKeys: [...selectedRowKeys.value]
    };
  }
  drawerVisible.value = false;
  setKeywordWithoutSearch("");
  statusFilter.value = undefined;
  assetKind.value = "channel";
  onDeviceSelect(record);
}
async function returnToDeviceList() {
  const snapshot = deviceListReturnSnapshot.value;
  if (!snapshot) return;

  deviceListReturnSnapshot.value = null;
  restoringDeviceListSnapshot = true;
  ++directoryDeviceRequestVersion;
  ++channelRequestVersion;
  ++deviceRequestVersion;
  assetKind.value = snapshot.assetKind;
  navigationMode.value = snapshot.navigationMode;
  selectedDevice.value = snapshot.selectedDevice;
  selectedDeviceSummary.value = snapshot.selectedDeviceSummary;
  deviceIdFilter.value = snapshot.deviceIdFilter;
  statusFilter.value = snapshot.statusFilter;
  directoryState.value = {
    ...snapshot.directoryState,
    selectedKey: { ...snapshot.directoryState.selectedKey },
    expandedKeys: { ...snapshot.directoryState.expandedKeys },
    listReset: { ...snapshot.directoryState.listReset }
  };
  selectedDirectories.value = { ...snapshot.selectedDirectories };
  setKeywordWithoutSearch(snapshot.keyword);
  viewMode.value = snapshot.viewMode;
  listPageSize.value = snapshot.listPageSize;
  cardPageSize.value = snapshot.cardPageSize;
  await nextTick();
  restoringDeviceListSnapshot = false;
  page.value = snapshot.page;
  selectedRowKeys.value = [...snapshot.selectedRowKeys];
  await Promise.all([refreshMainData(), refreshStats()]);
}
function deviceSummary(device: DeviceVO): DirectoryDevice {
  return {
    id: device.id,
    deviceId: device.deviceId,
    name: device.name,
    alias: device.alias,
    online: device.online
  };
}
function clearDirectoryNodeSelection() {
  const view = directoryState.value.view;
  directoryState.value = selectDirectory(directoryState.value, null);
  selectedDirectories.value = { ...selectedDirectories.value, [view]: null };
}
function onDeviceSelect(device: DeviceVO | null) {
  if (!canViewDevices.value) return;
  ++directoryDeviceRequestVersion;
  if (device) assetKind.value = "channel";
  navigationMode.value = device ? "device" : "directory";
  selectedDevice.value = device;
  selectedDeviceSummary.value = device ? deviceSummary(device) : null;
  deviceIdFilter.value = device?.deviceId || "";
  if (device) clearDirectoryNodeSelection();
  changeChannelScope();
}
async function onDirectoryDeviceSelect(device: DirectoryDevice) {
  if (!canViewDevices.value) return;
  assetKind.value = "channel";
  const requestVersion = ++directoryDeviceRequestVersion;
  navigationMode.value = "device";
  selectedDevice.value = null;
  selectedDeviceSummary.value = device;
  deviceIdFilter.value = device.deviceId;
  clearDirectoryNodeSelection();
  changeChannelScope();
  try {
    const response = await getDevice(device.id);
    if (requestVersion !== directoryDeviceRequestVersion || deviceIdFilter.value !== device.deviceId) return;
    if (response.code === 0 && response.data && response.data.deviceId === device.deviceId) {
      selectedDevice.value = response.data;
      selectedDeviceSummary.value = deviceSummary(response.data);
    } else {
      Message.warning("设备详情加载失败，仍可查看该设备通道");
    }
  } catch {
    if (requestVersion === directoryDeviceRequestVersion) Message.warning("设备详情加载失败，仍可查看该设备通道");
  }
}
function changeChannelScope() {
  page.value = 1;
  selectedRowKeys.value = [];
  ++channelRequestVersion;
  ++statsRequestVersion;
  total.value = 0;
  onlineChannelTotal.value = 0;
  offlineChannelTotal.value = 0;
  channels.value = [];
  markers.value = [];
  clusters.value = [];
  renderMapOverlays();
  mapAutoFitPending = true;
  void refreshMainData();
  void refreshStats();
}
async function refreshSelectedDevice() {
  const device = selectedDevice.value;
  if (!device) return;
  try {
    const response = await getDevice(device.id);
    if (selectedDevice.value?.id !== device.id) return;
    if (response.code === 0 && response.data) {
      selectedDevice.value = response.data;
      selectedDeviceSummary.value = deviceSummary(response.data);
    } else onDeviceSelect(null);
  } catch {
    /* 保留最后一次有效状态，下一次刷新重试。 */
  }
}
async function onDevicesChanged() {
  await refreshSelectedDevice();
  await refreshCurrent();
}

async function loadPtzTypeDict() {
  if (!canEditChannel.value) return;
  try {
    const res = await getDictItemsByDictCodeAPI("ptz_type");
    if (res.code === 0) {
      ptzTypeOptions.value = res.data?.list || [];
    }
  } catch (error: any) {
    console.error("摄像头类型字典加载失败:", error);
  }
}

/**
 * 地图左栏「范围」下拉：复用列表页同一份目录状态。
 *
 * ⛔ 不另起一套 mapScope 状态 —— markers/clusters/通道树 都从 directoryQuery 取范围，
 *    多一份状态就一定会有"树里收窄了、地图没变"的时刻。
 */
function applyMapScope(payload: MapScopeSelection | null) {
  if (!canViewDevices.value) return;
  navigationMode.value = "directory";
  ++directoryDeviceRequestVersion;
  selectedDevice.value = null;
  selectedDeviceSummary.value = null;
  deviceIdFilter.value = "";
  if (!payload) {
    clearDirectorySelection();
    return;
  }
  directoryState.value = selectDirectory(switchDirectoryView(directoryState.value, payload.view), payload.node.key);
  selectedDirectories.value = { ...selectedDirectories.value, [payload.view]: payload.node };
  changeChannelScope();
}

/**
 * 左栏点通道 → 飞到那个点，并弹出点位卡片（播放 / 轨迹 / 改位置都在卡片上）。
 *
 * ⛔ 坐标必须由通道自带：markers 是**按视野**查的，视野外的通道在 markers 里根本不存在，
 *    只靠 openMarkerCard 回查详情会直接 return ⇒ "点了没反应"。
 * ⛔ 缩放只往上抬到单点档，不要无条件跳到 16 —— 用户已经在 18 级看清门牌时，点一下反而被拉远。
 */
function locateMapChannel(channel: MapTreeChannel) {
  if (!mapInstance) return;
  mapInstance.flyTo({
    center: [channel.longitude, channel.latitude],
    zoom: Math.max(mapInstance.getZoom(), mapSinglePointFitZoom),
    essential: true
  });
  openMarkerCard(
    {
      id: channel.id,
      channelId: channel.channelId,
      deviceId: channel.deviceId,
      name: channel.name,
      status: channel.status
    },
    { longitude: channel.longitude, latitude: channel.latitude }
  );
}

/** 左栏点设备：只有一个点位就直接飞过去，多个就框住它们（执法仪通常只有一路）。 */
function focusMapDevice(device: MapTreeDevice) {
  if (!mapInstance || !device.channels.length) return;
  if (device.channels.length === 1) {
    locateMapChannel(device.channels[0]);
    return;
  }
  const bounds = new LngLatBounds();
  device.channels.forEach(channel => bounds.extend([channel.longitude, channel.latitude]));
  mapInstance.fitBounds(bounds, { padding: 80, maxZoom: mapSinglePointFitZoom, essential: true });
}

/**
 * 进业务分组之前用户在看什么。**只在我们强制切过的时候有值**，离开该视图时还回去，
 * 免得多切一圈回来「明明看的是设备，却莫名其妙变成通道列表」。
 */
const assetKindBeforeBusinessView = ref<AssetKind | null>(null);

/**
 * 业务分组视图（国标附录 J 的 215/216 目录）里只有目录和通道，没有设备节点。
 * 该视图下若仍按「设备」渲染，选中目录会得到一张上级平台的设备卡片（只把标题
 * 换成目录名、内容还是设备ID/厂商/地址/注册时间），和所点的东西对不上；这里
 * 统一落到通道视图。
 *
 * ⚠️ 反向也要还原：切回行政区划 / 部门目录时把之前的选择还回去，否则「切目录视图
 * 不改你在看什么」这条既有契约会在逛过一次业务分组之后失效。
 * ⚠️ 只改 `assetKind`、不调 `setAssetKind()`：后者自带一次完整刷新，而两个调用点
 * 紧接着自己就会 `changeChannelScope()`，那样会重复发一轮请求。
 *
 * @returns 是否真的改动了视图（调用点据此决定要不要额外刷新）
 */
function syncAssetKindForDirectoryView(view = directoryState.value.view): boolean {
  if (view === "business") {
    if (assetKind.value === "channel") return false;
    assetKindBeforeBusinessView.value = assetKind.value;
    assetKind.value = "channel";
    return true;
  }
  const restore = assetKindBeforeBusinessView.value;
  assetKindBeforeBusinessView.value = null;
  if (!restore || restore === assetKind.value) return false;
  assetKind.value = restore;
  return true;
}
function onDirectorySelect(node: DirectoryNode) {
  if (!canViewDevices.value) return;
  // 业务分组树里只有目录和通道，没有设备节点；若仍按「设备」渲染，点目录会得到
  // 一张挂着目录名的上级平台设备卡片，和所点的东西对不上。
  syncAssetKindForDirectoryView();
  navigationMode.value = "directory";
  ++directoryDeviceRequestVersion;
  selectedDevice.value = null;
  selectedDeviceSummary.value = null;
  deviceIdFilter.value = "";
  selectedDirectories.value = { ...selectedDirectories.value, [directoryState.value.view]: node };
  changeChannelScope();
}
async function onDirectoryViewChange() {
  if (!canViewDevices.value) return;
  syncAssetKindForDirectoryView();
  ++directoryDeviceRequestVersion;
  navigationMode.value = "directory";
  selectedDevice.value = null;
  selectedDeviceSummary.value = null;
  deviceIdFilter.value = "";
  page.value = 1;
  selectedRowKeys.value = [];
  await nextTick();
  changeChannelScope();
}

function clearDirectorySelection(refresh = true) {
  const view = directoryState.value.view;
  directoryState.value = selectDirectory(directoryState.value, null);
  selectedDirectories.value = { ...selectedDirectories.value, [view]: null };
  page.value = 1;
  selectedRowKeys.value = [];
  if (refresh && canViewDevices.value) changeChannelScope();
}
function onDirectoryTreeLoaded(view: "administrative" | "department" | "business", tree: DirectoryNode[]) {
  const selectedKey = directoryState.value.selectedKey[view];
  if (selectedKey) {
    selectedDirectories.value = {
      ...selectedDirectories.value,
      [view]: findDirectoryNode(tree, selectedKey) || selectedDirectories.value[view]
    };
  }
}
function setViewMode(mode: ViewMode) {
  viewMode.value = mode;
  if (mode === "map") mapAutoFitPending = true;
}
function cancelKeywordSearch() {
  if (keywordSearchTimer !== null) {
    clearTimeout(keywordSearchTimer);
    keywordSearchTimer = null;
  }
}
function setKeywordWithoutSearch(value: string) {
  if (keyword.value === value) return;
  suppressKeywordSearch = true;
  keyword.value = value;
}
function onSearch() {
  cancelKeywordSearch();
  page.value = 1;
  void refreshMainData();
  void refreshStats();
}
function clearKeyword() {
  setKeywordWithoutSearch("");
  onSearch();
  keywordInput.value?.focus();
}
function focusKeyword(event: KeyboardEvent) {
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
    event.preventDefault();
    keywordInput.value?.focus();
    keywordInput.value?.select();
  }
}
function clearDeviceFilter() {
  if (deviceListReturnSnapshot.value && assetKind.value === "channel") {
    void returnToDeviceList();
    return;
  }
  onDeviceSelect(null);
}
function resetFilters() {
  if (deviceListReturnSnapshot.value && assetKind.value === "channel") {
    void returnToDeviceList();
    return;
  }
  setKeywordWithoutSearch("");
  ++directoryDeviceRequestVersion;
  navigationMode.value = "directory";
  selectedDevice.value = null;
  selectedDeviceSummary.value = null;
  deviceIdFilter.value = "";
  statusFilter.value = undefined;
  clearDirectorySelection();
  refreshStats();
}
function setAssetKind(kind: AssetKind) {
  if (kind === assetKind.value) return;
  deviceListReturnSnapshot.value = null;
  ++directoryDeviceRequestVersion;
  navigationMode.value = "directory";
  selectedDevice.value = null;
  selectedDeviceSummary.value = null;
  deviceIdFilter.value = "";
  page.value = 1;
  selectedRowKeys.value = [];
  assetKind.value = kind;
  void refreshMainData();
  void refreshStats();
}
function onPageChange(next: number) {
  page.value = next;
  refreshMainData();
}
function onPageSizeChange(next: number) {
  pageSize.value = next;
  page.value = 1;
  refreshMainData();
}

async function refreshMainData() {
  if (!canViewDevices.value) return;
  if (viewMode.value === "map") return loadMapData();
  return assetKind.value === "device" ? loadDevicesData() : loadChannelsData();
}

function mapBoundsParams() {
  if (!mapInstance) return {};
  const bounds = mapInstance.getBounds();
  return {
    minLat: bounds.getSouth(),
    maxLat: bounds.getNorth(),
    minLng: bounds.getWest(),
    maxLng: bounds.getEast()
  };
}

function currentMapStyleUrl() {
  return mapBaseStyle.value === "dark" ? mapStyleUrls.dark : mapStyleUrls.light;
}

function resetMapView() {
  // 「复位」= 回到平台默认视野。没配默认视野时才回内置的北京视野。
  void applyDefaultMapCenter().then(applied => {
    if (!applied) mapInstance?.flyTo({ center: [116.3974, 39.9093], zoom: 10, essential: true });
  });
}

/**
 * 换底图 = `setStyle` + 把盖在地图上的「正在加载地图」遮罩收掉。
 *
 * 触发方只有一个：`watch(darkMode)`（底图跟系统主题走）。手动切换下拉已于 2026-10-22 去掉。
 *
 * ⛔⛔ 这里最容易漏的一步：`style.load` 只说明**样式表到位**，而遮罩的显示条件是
 *   `!mapReady || !mapFirstRender`；`mapFirstRender` 原本只有 `idle` 事件才会置 true。
 *   早先这一版只挂了 `style.load` ⇒ 换完底图 `mapFirstRender` 永远是 false，
 *   整块地图被 `.map-state`（`inset:0` + 不透明底）盖住，看起来就是"切换不生效"。
 * ⛔ 再加一个兜底定时器：底图瓦片服务慢或不可达时 `idle` 可能很久不来，不能让遮罩一直挂着。
 * ⚠️ `renderMapOverlays()` 只在 `style.load` 时跑一次（重建标记点）；`idle` / 兜底只负责揭遮罩，
 *    重复调用会把标记点再画一遍。
 */
function reloadMapStyle() {
  if (!mapInstance) return;
  mapInstance.setStyle(currentMapStyleUrl());
  mapReady.value = false;
  mapFirstRender.value = false;

  let settled = false;
  const revealMap = () => {
    if (settled) return;
    settled = true;
    if (mapStyleSwitchTimer !== null) {
      window.clearTimeout(mapStyleSwitchTimer);
      mapStyleSwitchTimer = null;
    }
    mapReady.value = true;
    mapFirstRender.value = true;
    mapInstance?.resize();
  };
  mapInstance.once("style.load", () => {
    revealMap();
    renderMapOverlays();
  });
  mapInstance.once("idle", revealMap);
  // 4s 足够本地样式表（≈120KB）走完一轮；等不到就直接揭遮罩，宁可短暂空白也不要一直挂着。
  mapStyleSwitchTimer = window.setTimeout(revealMap, 4000);
}

function searchMapCameras() {
  keyword.value = mapSearchQuery.value.trim();
  page.value = 1;
  mapAutoFitPending = true;
  loadMapData();
}

function removeMapMarkers() {
  mapMarkers.forEach(marker => {
    const icon = marker.getElement().querySelector(".map-channel-pip");
    if (icon) render(null, icon);
    marker.remove();
  });
  mapMarkers.clear();
  mapClusters.forEach(marker => marker.remove());
  mapClusters.clear();
}

function destroyMap() {
  // 销毁时清除底图加载兜底定时器,防止旧实例的回调污染新地图状态
  if (mapFirstRenderTimer !== null) {
    clearTimeout(mapFirstRenderTimer);
    mapFirstRenderTimer = null;
  }
  if (mapStyleSwitchTimer !== null) {
    clearTimeout(mapStyleSwitchTimer);
    mapStyleSwitchTimer = null;
  }
  // 使在途的地图数据请求响应失效,避免旧响应覆盖当前视图
  mapDataSeq += 1;
  if (mapInstance && mapMoveHandler) mapInstance.off("moveend", mapMoveHandler);
  if (mapInstance && mapMouseMoveHandler) mapInstance.off("mousemove", mapMouseMoveHandler);
  if (mapInstance && mapClickHandler) mapInstance.off("click", mapClickHandler);
  mapMoveHandler = null;
  mapMouseMoveHandler = null;
  mapClickHandler = null;
  closeMarkerCard();
  // 视频小窗也挂在 popup 上，地图没了它就是孤儿：先停流再销毁地图，
  // 否则 PlayWindow 被连 DOM 一起扔掉，播放器实例不会走 onBeforeUnmount 的释放逻辑。
  void closeVideoCard();
  stopTrajectoryPlayback();
  trajectoryPlayMarker = null;
  // 选点态跨实例不保留：reset 掉状态、摘掉针、恢复光标（否则重建后的地图还是十字准星）
  stopMapCenterPicking();
  removeMapMarkers();
  mapInstance?.remove();
  mapInstance = null;
  mapReady.value = false;
  mapFirstRender.value = false;
  mapError.value = "";
}

/** 聚合点击只放大地图；同坐标聚合进入最大层级展示独立标记。 */
function expandCluster(cluster: MapCluster) {
  if (!mapInstance) return;
  closeMarkerCard();
  const bounds = new LngLatBounds([cluster.minLng, cluster.minLat], [cluster.maxLng, cluster.maxLat]);
  const framed = mapInstance.cameraForBounds(bounds, { padding: 80 });
  const samePosition = cluster.minLat === cluster.maxLat && cluster.minLng === cluster.maxLng;
  const targetZoom = framed?.zoom ?? mapMaxZoom;
  // 分散的大范围聚合先框住成员；不能因相机已经框住而反复停在原层级。
  const zoom = samePosition
    ? mapMaxZoom
    : Math.min(mapMaxZoom, targetZoom > mapInstance.getZoom() + 0.5 ? targetZoom : mapInstance.getZoom() + 2);
  mapInstance.flyTo({
    center: samePosition ? [cluster.centerLng, cluster.centerLat] : (framed?.center ?? [cluster.centerLng, cluster.centerLat]),
    zoom,
    essential: true
  });
}

function createClusterElement(cluster: MapCluster) {
  const element = document.createElement("button");
  element.type = "button";
  element.className = "map-cluster-marker";
  // 数量分级：尺寸/字号随量级变化，>=1000 用 "999+"，否则四位数会撑破圆形。
  const tier = clusterBubbleTier(cluster.count);
  element.textContent = clusterBubbleLabel(cluster.count);
  element.style.setProperty("--cluster-size", `${tier.size}px`);
  element.style.setProperty("--cluster-font-size", `${tier.font}px`);
  element.style.setProperty("--cluster-rate", `${Math.round(cluster.onlineRate * 100)}%`);
  element.title = `${cluster.count} 路通道 · 在线 ${cluster.onlineCount}`;
  element.setAttribute("aria-label", element.title);
  element.addEventListener("click", event => {
    event.stopPropagation();
    expandCluster(cluster);
  });
  return element;
}

/**
 * 在地图上画一个标记点所需的最小信息。
 *
 * 刻意用 `Pick` 而不是整个 `MapMarker`：marker 列表接口还带着来源/新鲜度（详情面板要用），
 * 而聚合接口里"落单的那一个通道"只给身份和状态。让标记点渲染只依赖这个最小集合，
 * 两条来源才能复用同一段代码，不必为聚合再造一个形状。
 */
type MarkerPoint = Pick<MapMarker, "id" | "channelId" | "deviceId" | "name" | "status">;

function createMarkerElement(marker: MarkerPoint, offset?: [number, number]) {
  const element = document.createElement("button");
  element.type = "button";
  element.className = `map-channel-marker${marker.status === 1 ? " online" : ""}`;
  element.title = `${displayName(marker)} · ${marker.channelId}`;
  element.setAttribute("aria-label", element.title);
  const icon = document.createElement("span");
  icon.className = "map-channel-pip";
  render(createVNode(Cctv, { size: 15, strokeWidth: 2.1, "aria-hidden": "true" }), icon);
  element.append(icon);
  // 点标记点不再直接开详情抽屉：先弹一张业务卡片，播视频/看轨迹/改位置都在卡片上。
  // ⛔ 不要改成"直接播放" —— 同一台执法仪既有实时画面又有轨迹，"点一下到底干什么"
  //    必须由用户在卡片上选，否则想查轨迹的人会先被拉起一路实时流。
  element.addEventListener("click", event => {
    event.stopPropagation();
    openMarkerCard(marker, undefined, offset);
  });
  return element;
}

/** 卡片上展示的完整信息。聚合里落单的点只带身份，来源/新鲜度要从 markers 列表补。 */
function markerCardDetail(marker: MarkerPoint): MapMarker | null {
  return markers.value.find(item => item.id === marker.id) || null;
}

function closeMarkerCard() {
  mapPopup?.remove();
  mapPopup = null;
  mapSelectedChannelId.value = null;
}

/**
 * 打开通道卡片气泡。
 *
 * 用 MapLibre Popup 而不是自绘浮层：气泡会跟着地图自动平移，不需要在 moveend 里
 * 手算屏幕坐标；关掉地图（destroyMap）时也会随地图一起销毁。
 *
 * `position` 用于"从左侧通道树点过来"的通道：它的坐标可能不在当前视野的 markers 列表里
 * （markers 按视野查询），回查不到就会直接 return ⇒ 表现为"点了没反应"。
 */
function openMarkerCard(marker: MarkerPoint, position?: { longitude: number; latitude: number }, offset?: [number, number]) {
  if (!mapInstance) return;
  const detail = markerCardDetail(marker);
  const longitude = position?.longitude ?? detail?.longitude;
  const latitude = position?.latitude ?? detail?.latitude;
  if (longitude === undefined || latitude === undefined) return;

  closeMarkerCard();
  // 视频小窗与卡片是互斥的两块浮层：点另一个点位时它必须让位（并停掉那一路流），
  // 否则两张 popup 会叠在同一片地图上，还白烧一路实时流。
  void closeVideoCard();
  // 高亮状态放在这里而不是 locateMapChannel：地图上直接点标记点同样要能在左栏亮起来。
  mapSelectedChannelId.value = marker.id;

  const card = document.createElement("div");
  card.className = "map-channel-card";

  const head = document.createElement("div");
  head.className = "map-channel-card__head";
  const dot = document.createElement("span");
  dot.className = `map-channel-card__dot${marker.status === 1 ? " online" : ""}`;
  const title = document.createElement("span");
  title.className = "map-channel-card__name";
  title.textContent = displayName(marker);
  head.append(dot, title);

  const meta = document.createElement("div");
  meta.className = "map-channel-card__meta";
  const metaParts = [
    marker.status === 1 ? "在线" : "离线",
    marker.channelId,
    detail?.positionSource ? `坐标来源：${positionSourceText(detail)}` : ""
  ].filter(Boolean);
  meta.textContent = metaParts.join(" · ");

  const actions = document.createElement("div");
  actions.className = "map-channel-card__actions";

  // ⛔ 离线通道不给点「播放视频」：设备侧根本不推流，点了只会白等到点播超时再报错。
  //    无权限与离线是两种原因，title 要分开说，否则用户会去要权限。
  const playDisabled = !canStartPlayback.value || marker.status !== 1;
  const playTitle = !canStartPlayback.value ? "无播放权限" : marker.status === 1 ? "" : "通道离线，无法播放";

  const actionConfigs: Array<{ key: string; label: string; disabled?: boolean; title?: string }> = [
    { key: "play", label: "播放视频", disabled: playDisabled, title: playTitle },
    { key: "trajectory", label: "查看轨迹" },
    { key: "location", label: "设置位置", disabled: !canEditChannel.value, title: canEditChannel.value ? "" : "无通道编辑权限" },
    { key: "detail", label: "通道详情" }
  ];
  actionConfigs.forEach(config => {
    const button = document.createElement("button");
    button.type = "button";
    button.className = `map-channel-card__action map-channel-card__action--${config.key}`;
    button.textContent = config.label;
    button.disabled = !!config.disabled;
    if (config.title) button.title = config.title;
    button.addEventListener("click", () => {
      // 坐标直接从小窗的锚点传下去：卡片本身就贴在点位上，不必再回查 markers 列表。
      if (config.key === "play") void playFromMap(marker, { longitude, latitude });
      if (config.key === "trajectory") openTrajectory(marker, detail, position);
      if (config.key === "location") openLocationPickerForMarker(marker, detail, position);
      if (config.key === "detail") {
        closeMarkerCard();
        void openChannel(marker);
      }
    });
    actions.append(button);
  });

  card.append(head, meta, actions);
  mapPopup = new maplibregl.Popup({ closeButton: true, offset: offset ? [offset[0], offset[1] - 16] : 16, maxWidth: "280px" })
    .setLngLat([longitude, latitude])
    .setDOMContent(card)
    .addTo(mapInstance);
}

function renderMapOverlays() {
  if (!mapInstance || !mapReady.value) return;
  removeMapMarkers();
  // 底图 setStyle 会把 source/layer 全部清掉（轨迹折线也在其中），
  // 这里按需补画。⛔ 不能放在函数末尾：下面 zoom 低于阈值时是提前 return 的。
  restoreTrajectoryOverlay();
  // 两种呈现**互斥**：低于阈值用服务端聚合，达到阈值后按屏幕距离画通道点或重叠组。
  // 最大层级不再保留聚合，重叠通道只做屏幕偏移，真实坐标保持不变。
  // ⛔ 不要让它们同时画 —— 历史上聚合气泡是无条件渲染、标记点按 zoom 渲染，
  //    于是 zoom≥阈值 时同一批通道会同时出现"气泡 N"和 N 个标记点，重叠在同一位置。
  if (mapZoom.value < mapMarkerMinZoom) {
    clusters.value.forEach(cluster => {
      // ⭐ 落单的通道**不是聚合**：count==1 直接画成通道标记点。
      // 行业惯例（Leaflet.markercluster / Supercluster / 高德 都是这么做的）：
      // 只有 >=2 才聚合。让唯一的那路通道顶着一个写着 "1" 的气泡，
      // 用户会以为平台在聚合一个根本不该聚合的东西，而且点它只会放大 ——
      // 看起来就像"点了这个数字之后标记点没了"。
      if (cluster.count === 1 && cluster.single) {
        mapMarkers.set(
          cluster.single.id,
          new MapLibreMarker({ element: createMarkerElement(cluster.single), anchor: "center" })
            .setLngLat([cluster.centerLng, cluster.centerLat])
            .addTo(mapInstance!)
        );
        return;
      }
      const key = `${cluster.centerLat}:${cluster.centerLng}:${cluster.count}`;
      mapClusters.set(
        key,
        new MapLibreMarker({ element: createClusterElement(cluster), anchor: "center" })
          .setLngLat([cluster.centerLng, cluster.centerLat])
          .addTo(mapInstance!)
      );
    });
    return;
  }
  // 按屏幕距离合并会遮挡彼此的标记；数量仍是通道数，不按设备或坐标去重。
  const groups: Array<{ x: number; y: number; members: MapMarker[] }> = [];
  [...markers.value]
    .sort((a, b) => a.id - b.id)
    .forEach(marker => {
      const point = mapInstance!.project([marker.longitude, marker.latitude]);
      const group = groups.find(item => Math.hypot(item.x - point.x, item.y - point.y) < 36);
      if (group) group.members.push(marker);
      else groups.push({ x: point.x, y: point.y, members: [marker] });
    });
  groups.forEach(({ x, y, members }) => {
    const marker = members[0];
    if (members.length === 1 || mapZoom.value >= mapMaxZoom - 0.01) {
      const radius = Math.max(36, (members.length * 40) / (2 * Math.PI));
      members.forEach((channel, index) => {
        const point = mapInstance!.project([channel.longitude, channel.latitude]);
        const angle = (index * 2 * Math.PI) / members.length - Math.PI / 2;
        const offset: [number, number] =
          members.length === 1 ? [0, 0] : [x + radius * Math.cos(angle) - point.x, y + radius * Math.sin(angle) - point.y];
        mapMarkers.set(
          channel.id,
          new MapLibreMarker({
            element: createMarkerElement(channel, members.length > 1 ? offset : undefined),
            anchor: "center",
            offset
          })
            .setLngLat([channel.longitude, channel.latitude])
            .addTo(mapInstance!)
        );
      });
      return;
    }
    const onlineCount = members.filter(item => item.status === 1).length;
    const element = createClusterElement({
      centerLat: marker.latitude,
      centerLng: marker.longitude,
      minLat: Math.min(...members.map(item => item.latitude)),
      maxLat: Math.max(...members.map(item => item.latitude)),
      minLng: Math.min(...members.map(item => item.longitude)),
      maxLng: Math.max(...members.map(item => item.longitude)),
      count: members.length,
      onlineCount,
      onlineRate: onlineCount / members.length,
      single: null
    });
    mapClusters.set(
      String(marker.id),
      new MapLibreMarker({ element, anchor: "center" }).setLngLat([marker.longitude, marker.latitude]).addTo(mapInstance!)
    );
  });
}

// ── 从地图卡片起播 ─────────────────────────────────────────────────────

/**
 * 卡片「播放视频」：在**地图上**贴一块实时视频小窗。
 *
 * ⛔ 不要退回 `a-modal`：960px 的模态盖掉半块地图，而点视频的人往往正是一边看画面
 *    一边在地图上找下一个点位。小窗贴着通道点位、跟着地图平移，才是这一屏该有的形态。
 * ⛔ 关闭小窗必须 stopPlay：实时流在 ZLM 侧是有生命的，只关 UI 会让流一直挂着，
 *    按需直播关了的情况下尤其明显（设备侧收流不停、媒体节点 CPU 白烧）。
 * ⛔ 离线通道不给起流：设备侧根本不推流，点了只会白等到点播超时再报错。
 */
async function playFromMap(marker: MarkerPoint, position?: { longitude: number; latitude: number }) {
  if (!canStartPlayback.value) {
    Message.warning("没有播放权限");
    return;
  }
  if (marker.status !== 1) {
    Message.warning("通道离线，无法播放");
    return;
  }
  if (!marker.deviceId) {
    Message.error("缺少设备编码，无法点播");
    return;
  }
  const detail = markerCardDetail(marker);
  const longitude = position?.longitude ?? detail?.longitude;
  const latitude = position?.latitude ?? detail?.latitude;
  if (longitude === undefined || latitude === undefined) {
    Message.warning("该通道没有坐标，无法定位到地图上");
    return;
  }

  closeMarkerCard();
  openVideoCard(marker, { longitude, latitude });
  videoLoading.value = true;
  try {
    const res = await startPlay(marker.deviceId, marker.channelId);
    if (res.code !== 0) {
      Message.error(res.message || "点播失败");
      void closeVideoCard();
      return;
    }
    videoPlaying.value = res.data;
  } catch (error: any) {
    Message.error(error?.message || "点播失败");
    void closeVideoCard();
  } finally {
    videoLoading.value = false;
  }
}

/**
 * 把视频小窗挂到地图点位上（用 MapLibre Popup，随地图自动平移/销毁）。
 *
 * 卡片内容是**模板里 Teleport 进来的**：MapLibre 只认 DOM 节点，直接把 `PlayWindow`
 * 这类 Vue 组件塞进 `setDOMContent` 会变成无主节点（拿不到 props 更新、不会走 unmount）。
 */
function openVideoCard(marker: MarkerPoint, position: { longitude: number; latitude: number }) {
  if (!mapInstance) {
    Message.warning("地图尚未加载完成");
    return;
  }
  void closeVideoCard();
  videoTarget.value = {
    id: marker.id,
    deviceId: marker.deviceId,
    channelId: marker.channelId,
    name: displayName(marker),
    status: marker.status
  };
  const host = document.createElement("div");
  host.className = "map-video-card-host";
  videoPopupHost.value = host;
  mapVideoPopup = new maplibregl.Popup({ closeButton: true, offset: 24, maxWidth: "304px" })
    .setLngLat([position.longitude, position.latitude])
    .setDOMContent(host)
    .addTo(mapInstance);
  mapVideoPopup.on("close", () => {
    // 点 × 或地图自己把它关掉：此时 mapVideoPopup 还是它自己，交给 resetVideoCard 收尾。
    // closeVideoCard 会先把 mapVideoPopup 置空再 remove，所以不会重复进来。
    if (mapVideoPopup) void resetVideoCard();
  });
}

/** 关掉视频小窗（含停流）。返回停流的 promise，给"要先停再开控制台"的调用方 await。 */
function closeVideoCard(): Promise<void> {
  const popup = mapVideoPopup;
  mapVideoPopup = null;
  popup?.remove();
  return resetVideoCard();
}

/**
 * 复位小窗状态并停流。
 * ⛔ `stopVideoStream()` 在第一个 await 之前就把 `videoPlaying` 清空了，
 *    所以这里 fire-and-forget 不会污染"下一个"小窗的流句柄。
 */
function resetVideoCard(): Promise<void> {
  mapVideoPopup = null;
  videoPopupHost.value = null;
  videoLoading.value = false;
  videoTarget.value = null;
  return stopVideoStream();
}

/**
 * 小窗上的「播放控制台」：交给全局播放控制台，小窗让位。
 *
 * ⛔ 必须先停小窗这一路流再开控制台：同一通道同时挂两路实时流，ZLM 侧会用同一个 SSRC
 *    互相顶掉，表现是"刚打开控制台，小窗那路瞬间黑屏/花屏"，日志里两串 streamId 抢通道。
 */
async function openPlaybackConsoleFromMap() {
  const target = videoTarget.value;
  if (!target) return;
  if (!canStartPlayback.value) {
    Message.warning("没有播放权限");
    return;
  }
  if (target.status !== 1) {
    Message.warning("通道离线，无法播放");
    return;
  }
  await closeVideoCard();
  playbackConsole.open({
    id: target.id,
    channelId: target.channelId,
    deviceId: target.deviceId,
    name: target.name,
    status: target.status
  });
}

/** 停掉当前小窗里的流（不负责关 UI）。 */
async function stopVideoStream() {
  const streamId = videoPlaying.value?.streamId;
  videoPlaying.value = null;
  if (!streamId) return;
  try {
    await stopPlay(streamId);
  } catch (error) {
    // 停流失败不打断用户：流可能已被 ZLM 按需回收，这里只是兜底。
    console.warn("停流失败", error);
  }
}

function onMapVideoError(message: string) {
  Message.warning(`播放器：${message}`);
}

// ── 轨迹 ───────────────────────────────────────────────────────────────

const TRAJECTORY_SOURCE_ID = "uvp-trajectory";
const TRAJECTORY_LINE_LAYER_ID = "uvp-trajectory-line";
const TRAJECTORY_ENDS_SOURCE_ID = "uvp-trajectory-ends";
const TRAJECTORY_ENDS_LAYER_ID = "uvp-trajectory-ends";
/**
 * 1× 速度下整条轨迹走完的时长。倍速只缩这个值，不改变插值口径。
 *
 * 取 40s 而不是 20s：倍速档位是 1×/4×/16×（评审定稿），16× 时 40s 只剩 2.5s，
 * 刚好是"一眼扫完整条路线"的可读下限；若基准取 20s，16× 只有 1.25s，什么也看不清。
 */
const TRAJECTORY_PLAY_MS = 40000;

let trajectoryPlayMarker: MapLibreMarker | null = null;
let trajectoryRaf: number | null = null;
let trajectoryPlayStartedAt = 0;
let trajectoryPlayFromProgress = 0;

function formatClock(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

function openTrajectory(marker: MarkerPoint, detail: MapMarker | null, position?: { longitude: number; latitude: number }) {
  closeMarkerCard();
  void closeVideoCard();
  trajectoryTarget.value = {
    id: marker.id,
    channelId: marker.channelId,
    deviceId: marker.deviceId,
    name: displayName(marker),
    // 从左侧通道树点进来的通道可能不在当前视野的 markers 里（detail 为 null），
    // 但它的坐标一定在 —— 不能因此提示"该通道没有坐标"。
    hasCoordinate: position ? true : !!(detail?.longitude || detail?.latitude)
  };
  trajectoryVisible.value = true;
  trajectoryPoints.value = [];
  trajectoryEmptyText.value = "";
  trajectoryTruncated.value = false;
  trajectoryDistance.value = 0;
  trajectoryProgress.value = 0;
  trajectoryCurrentTime.value = "";
  stopTrajectoryPlayback();
  clearTrajectoryOverlay();
  // 默认窗口与子组件初始值一致：最近 24 小时（后端 trajectoryDefaultWindow 同口径）。
  void loadTrajectory();
}

async function loadTrajectory(range?: { startTime: string; endTime: string }) {
  const target = trajectoryTarget.value;
  if (!target) return;
  trajectoryLoading.value = true;
  stopTrajectoryPlayback();
  try {
    const res = await listMapTrajectory({
      channelId: target.id,
      startTime: range?.startTime,
      endTime: range?.endTime
    });
    if (res.code !== 0) {
      Message.error(res.message || "轨迹查询失败");
      return;
    }
    const data = res.data;
    trajectoryPoints.value = data.list || [];
    trajectoryTruncated.value = !!data.truncated;
    trajectoryDistance.value = trajectoryDistanceMeters(trajectoryPoints.value);
    trajectoryProgress.value = 0;
    trajectoryCurrentTime.value = "";
    trajectoryEmptyText.value = trajectoryPoints.value.length
      ? ""
      : describeEmptyTrajectory({
          historyEnabled: data.historyEnabled,
          hasCoordinate: target.hasCoordinate
        });
    clearTrajectoryOverlay();
    drawTrajectoryOverlay();
    fitTrajectoryBounds();
  } catch (error: any) {
    Message.error(error?.message || "轨迹查询失败");
  } finally {
    trajectoryLoading.value = false;
  }
}

function closeTrajectory() {
  trajectoryVisible.value = false;
  trajectoryTarget.value = null;
  stopTrajectoryPlayback();
  clearTrajectoryOverlay();
}

function clearTrajectoryOverlay() {
  if (!mapInstance) return;
  trajectoryPlayMarker?.remove();
  trajectoryPlayMarker = null;
  // 图层必须先于 source 删除（MapLibre 不允许删还有图层引用的 source）。
  if (mapInstance.getLayer(TRAJECTORY_ENDS_LAYER_ID)) mapInstance.removeLayer(TRAJECTORY_ENDS_LAYER_ID);
  if (mapInstance.getLayer(TRAJECTORY_LINE_LAYER_ID)) mapInstance.removeLayer(TRAJECTORY_LINE_LAYER_ID);
  if (mapInstance.getSource(TRAJECTORY_ENDS_SOURCE_ID)) mapInstance.removeSource(TRAJECTORY_ENDS_SOURCE_ID);
  if (mapInstance.getSource(TRAJECTORY_SOURCE_ID)) mapInstance.removeSource(TRAJECTORY_SOURCE_ID);
}

/** 画折线 + 起止点。⛔ 起止点用数据驱动的 circle 层，不要用两个 DOM marker。 */
function drawTrajectoryOverlay() {
  if (!mapInstance || !mapReady.value) return;
  const points = trajectoryPoints.value;
  const feature = trajectoryLineFeature(points);
  const ends = trajectoryEndpoints(points);
  if (!feature || !ends) return;
  if (mapInstance.getSource(TRAJECTORY_SOURCE_ID)) return;

  mapInstance.addSource(TRAJECTORY_SOURCE_ID, { type: "geojson", data: feature as any });
  mapInstance.addLayer({
    id: TRAJECTORY_LINE_LAYER_ID,
    type: "line",
    source: TRAJECTORY_SOURCE_ID,
    layout: { "line-cap": "round", "line-join": "round" },
    paint: { "line-color": "#2563eb", "line-width": 4, "line-opacity": 0.85 }
  });

  const endFeatures = {
    type: "FeatureCollection" as const,
    features: [
      {
        type: "Feature" as const,
        properties: { kind: "start" },
        geometry: { type: "Point" as const, coordinates: [ends.start.longitude, ends.start.latitude] }
      },
      {
        type: "Feature" as const,
        properties: { kind: "end" },
        geometry: { type: "Point" as const, coordinates: [ends.end.longitude, ends.end.latitude] }
      }
    ]
  };
  mapInstance.addSource(TRAJECTORY_ENDS_SOURCE_ID, { type: "geojson", data: endFeatures as any });
  mapInstance.addLayer({
    id: TRAJECTORY_ENDS_LAYER_ID,
    type: "circle",
    source: TRAJECTORY_ENDS_SOURCE_ID,
    paint: {
      // 起点绿、终点红（⛔ 与国内股票涨跌配色无关，这里是"出发/到达"的通用语义）。
      "circle-color": ["match", ["get", "kind"], "start", "#16a34a", "#dc2626"] as any,
      "circle-radius": 7,
      "circle-stroke-color": "#ffffff",
      "circle-stroke-width": 2
    }
  });
}

/**
 * 底图样式切换后重建轨迹图层。
 *
 * setStyle 会把所有 source/layer 清掉（包括轨迹），但 DOM marker 还在 ——
 * 不重建的表现是"线上没了、回放点还在飘"。
 */
function restoreTrajectoryOverlay() {
  if (!trajectoryPoints.value.length) return;
  if (mapInstance?.getSource(TRAJECTORY_SOURCE_ID)) return;
  drawTrajectoryOverlay();
}

function fitTrajectoryBounds() {
  if (!mapInstance || trajectoryPoints.value.length === 0) return;
  const bounds = new LngLatBounds();
  trajectoryPoints.value.forEach(point => bounds.extend([point.longitude, point.latitude]));
  mapInstance.fitBounds(bounds, {
    padding: { top: 90, bottom: 140, left: 80, right: 80 },
    maxZoom: trajectoryPoints.value.length === 1 ? mapSinglePointFitZoom : mapMaxZoom,
    duration: 600
  });
}

function applyTrajectoryProgress(progress: number) {
  const position = interpolateAlongTrajectory(trajectoryPoints.value, progress);
  trajectoryProgress.value = progress;
  if (!position || !mapInstance) return;
  trajectoryCurrentTime.value = formatClock(position.eventTime);
  if (!trajectoryPlayMarker) {
    trajectoryPlayMarker = new MapLibreMarker({ color: "#f59e0b" })
      .setLngLat([position.longitude, position.latitude])
      .addTo(mapInstance);
    return;
  }
  trajectoryPlayMarker.setLngLat([position.longitude, position.latitude]);
}

function stopTrajectoryPlayback() {
  trajectoryPlaying.value = false;
  if (trajectoryRaf !== null) {
    cancelAnimationFrame(trajectoryRaf);
    trajectoryRaf = null;
  }
}

function toggleTrajectoryPlayback() {
  if (trajectoryPlaying.value) {
    stopTrajectoryPlayback();
    return;
  }
  if (trajectoryPoints.value.length < 2) return;
  trajectoryPlaying.value = true;
  // 播完后再点播放 = 从头再放一遍，而不是"停在终点不动"。
  trajectoryPlayFromProgress = trajectoryProgress.value >= 1 ? 0 : trajectoryProgress.value;
  trajectoryPlayStartedAt = performance.now();
  trajectoryRaf = requestAnimationFrame(stepTrajectoryPlayback);
}

function stepTrajectoryPlayback() {
  if (!trajectoryPlaying.value) return;
  const from = trajectoryPlayFromProgress;
  const speed = trajectorySpeed.value > 0 ? trajectorySpeed.value : 1;
  const remainingMs = Math.max(1, (1 - from) * (TRAJECTORY_PLAY_MS / speed));
  const elapsed = performance.now() - trajectoryPlayStartedAt;
  const progress = from + (elapsed / remainingMs) * (1 - from);
  if (progress >= 1) {
    applyTrajectoryProgress(1);
    stopTrajectoryPlayback();
    return;
  }
  applyTrajectoryProgress(progress);
  trajectoryRaf = requestAnimationFrame(stepTrajectoryPlayback);
}

function onTrajectoryProgressInput(progress: number) {
  stopTrajectoryPlayback();
  applyTrajectoryProgress(progress);
}

function onTrajectorySpeedChange(speed: number) {
  trajectorySpeed.value = speed;
  // 变速时把"已播到哪"钉住，否则会在下一帧按新速度重算总时长，进度突然跳一段。
  if (trajectoryPlaying.value) {
    trajectoryPlayFromProgress = trajectoryProgress.value;
    trajectoryPlayStartedAt = performance.now();
  }
}

// ── 默认中心点 ─────────────────────────────────────────────────────────

/**
 * 应用平台保存的默认视野。
 *
 * ⛔ 成功后必须把 mapAutoFitPending 置 false：首次进入时 loadMapData 默认会
 *    fitBounds 到数据包围盒，那个动作会把用户设的中心点直接覆盖掉 ——
 *    表现就是"设了中心点，进来还是全图"。
 * ⛔ 也不做"只应用一次"的锁：切走菜单再进地图应当回到中心点，
 *    否则用户在别的页面绕一圈回来，视野停在上次随便拖到的地方。
 */
async function applyDefaultMapCenter(): Promise<boolean> {
  if (!mapInstance) return false;
  try {
    const res = await getMapCenter();
    const center: MapCenter | null = res.code === 0 ? normalizeMapCenter(res.data?.center) : null;
    if (!center) return false;
    // ⛔ 先清掉"待适应数据视野"，再移动相机：jumpTo 会触发 moveend，
    //    而 moveend 里的 loadMapData 若看见 mapAutoFitPending=true 就会 fitBounds，
    //    把中心点当场顶掉（表现是"设了中心点，进来还是全图"）。
    mapAutoFitPending = false;
    mapInstance.jumpTo({ center: [center.longitude, center.latitude], zoom: center.zoom });
    mapZoom.value = Math.round(center.zoom);
    return true;
  } catch (error) {
    // 读不到就用内置默认视野，不打扰用户。
    console.warn("读取地图默认中心失败", error);
    return false;
  }
}

/**
 * 选点态下"当前打算存哪个点"的展示文本。
 * ⚠️ 不复用 `formatMapCenter`：那个带 `Z{zoom}`，而缩放是"跟着当前地图走"的，
 *    选点过程中滚一下轮子数字就变，会让人以为选点被改掉了。
 */
const mapCenterPickText = computed(() => {
  const pick = mapCenterPick.value;
  return pick ? `${pick.longitude.toFixed(5)}, ${pick.latitude.toFixed(5)}` : "";
});

/**
 * 把选点针摆到当前选中的坐标上（首次创建，之后只挪位置）。
 *
 * ⛔⛔ 水滴形状必须画在**内层元素**上，外层 `element` 只当定位壳子。
 *   原因：MapLibre 靠 `.maplibregl-marker { position: absolute; top:0; left:0 }` 定位，
 *   再往同一个元素上写 `transform: translate(x,y)` 挪位置。
 *   一旦外层自己也写 `position`/`transform`（比如为了画旋转的水滴），就会把 MapLibre
 *   那两条覆盖掉 —— 元素掉回普通文档流、被画布容器裁掉，**地图上什么都看不见**，
 *   但提示条和坐标是正常的（2026-10-22 上线后老板截图就是"进了选点态却看不到点"）。
 */
function renderMapCenterPickMarker() {
  if (!mapInstance || !mapCenterPick.value) return;
  const lngLat: [number, number] = [mapCenterPick.value.longitude, mapCenterPick.value.latitude];
  if (!mapCenterPickMarker) {
    const element = document.createElement("div");
    element.className = "map-center-pin";
    element.setAttribute("aria-hidden", "true");
    // 水滴用内联 SVG 画：CSS 拿 rotate + 单角不倒圆凑出来的"水滴"在 24px 下就是个圆疙瘩，
    // 尖端还容易被抗锯齿吃掉。这里的 path **尖端精确落在 viewBox 底边 y=24**，
    // 配合 anchor:"bottom" ⇒ 针尖 = 坐标点，不用再补偏移。
    element.innerHTML =
      '<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true" focusable="false">' +
      '<path class="map-center-pin__body" d="M12 1.5C8.05 1.5 4.85 4.7 4.85 8.65 4.85 14.1 12 24 12 24s7.15-9.9 7.15-15.35c0-3.95-3.2-7.15-7.15-7.15Z"/>' +
      '<circle class="map-center-pin__dot" cx="12" cy="8.5" r="2.6"/>' +
      "</svg>";
    // ⛔⛔ **必须 `.setLngLat()` 在 `.addTo()` 之前**，而且这里不能拆成两句。
    //   MapLibre 5.x 的 `addTo()` 会**立刻**跑一次 `Marker._update()`，`_update` 里第一件事就是
    //   `smartWrap(this._lngLat, this._flatPos, ...)`。若此时 `_lngLat` 还是 undefined，就会抛
    //   `TypeError: Cannot read properties of undefined (reading 'lng')`。
    //   后果极具迷惑性：异常发生在 `element.style.transform = ...` **之前**，
    //   于是标记永远停在 `top:0;left:0`（视觉上"卡在左上角"），
    //   而且 `_flatPos` 也没初始化 ⇒ 之后每次 `setLngLat()` 重试都在同一行再抛一次
    //   ⇒ 点地图"选不中"（其实 pick 值有更新，只是针永远不动）。
    //   ⚠️ 官方示例全是 `.setLngLat([...]).addTo(map)` 这个顺序，别改回去。
    //   （2026-10-22 老板实测报的是"标记点一直在左上角、没法从地图上选点"。）
    mapCenterPickMarker = new MapLibreMarker({ element, anchor: "bottom" }).setLngLat(lngLat).addTo(mapInstance);
    return;
  }
  mapCenterPickMarker.setLngLat(lngLat);
}

function removeMapCenterPickMarker() {
  mapCenterPickMarker?.remove();
  mapCenterPickMarker = null;
}

/** 进入"在地图上挑一个点"的选点态（第一步，不落库）。 */
function startMapCenterPicking() {
  if (!mapInstance) {
    Message.warning("地图尚未加载完成");
    return;
  }
  if (!canEditDevice.value) {
    Message.warning("没有修改平台默认视野的权限");
    return;
  }
  // 选点期间不能挂着通道卡片：那玩意儿会挡住地图，也容易让人以为点卡片就是选点。
  closeMarkerCard();
  mapCenterPicking.value = true;
  // 默认选中当前中心：用户若只是"把现在这一块存下来"，直接点保存即可，不必先把地图挪一遍。
  const current = mapInstance.getCenter();
  mapCenterPick.value = { longitude: current.lng, latitude: current.lat };
  renderMapCenterPickMarker();
  setMapCursor("crosshair");
}

function stopMapCenterPicking() {
  mapCenterPicking.value = false;
  mapCenterPick.value = null;
  removeMapCenterPickMarker();
  setMapCursor("");
}

function setMapCursor(cursor: string) {
  const canvas = mapInstance?.getCanvas();
  if (canvas) canvas.style.cursor = cursor;
}

/**
 * 选点态下点地图：记下坐标，把针挪过去，并让镜头平滑移到该点 ——
 * 点完立刻就能看到"以后进来画面正中对的就是这里"。
 * ⛔ 非选点态直接返回：这个监听常驻（不动态增删），靠标志位判定，避免增删与销毁竞态。
 * ✅ 点通道标记/聚合气泡不会误触发：它们的 DOM 监听里都 `stopPropagation()` 了。
 */
function onMapClick(event: maplibregl.MapMouseEvent) {
  if (!mapCenterPicking.value) return;
  mapCenterPick.value = { longitude: event.lngLat.lng, latitude: event.lngLat.lat };
  renderMapCenterPickMarker();
  mapInstance?.easeTo({ center: [event.lngLat.lng, event.lngLat.lat], duration: 320 });
}

async function confirmMapCenterPick() {
  if (!mapInstance || !mapCenterPick.value) return;
  const center = buildMapCenter({
    longitude: mapCenterPick.value.longitude,
    latitude: mapCenterPick.value.latitude,
    zoom: mapInstance.getZoom()
  });
  mapCenterSaving.value = true;
  try {
    const res = await saveMapCenter(center);
    if (res.code !== 0) {
      Message.error(res.message || "保存默认中心失败");
      return;
    }
    stopMapCenterPicking();
    Message.success(`已设为默认中心：${formatMapCenter(center)}`);
  } catch (error: any) {
    Message.error(error?.message || "保存默认中心失败");
  } finally {
    mapCenterSaving.value = false;
  }
}

/** 选点态的键盘操作：Esc 取消、Enter 保存（都和用户肌肉记忆一致）。 */
function handleMapCenterPickKeydown(event: KeyboardEvent) {
  if (!mapCenterPicking.value) return;
  if (event.key === "Escape") {
    event.preventDefault();
    stopMapCenterPicking();
  } else if (event.key === "Enter") {
    event.preventDefault();
    void confirmMapCenterPick();
  }
}

// ── 通道位置踩点 ───────────────────────────────────────────────────────

function openLocationPickerForMarker(
  marker: MarkerPoint,
  detail: MapMarker | null,
  position?: { longitude: number; latitude: number }
) {
  if (!canEditChannel.value) {
    Message.warning("没有通道编辑权限");
    return;
  }
  closeMarkerCard();
  void closeVideoCard();
  locationPickerTarget.value = {
    id: marker.id,
    name: displayName(marker),
    // 同 openTrajectory：视野外的通道拿不到 detail，坐标由调用方带进来
    longitude: position?.longitude ?? detail?.longitude ?? 0,
    latitude: position?.latitude ?? detail?.latitude ?? 0
  };
  locationPickerVisible.value = true;
}

/** 从通道编辑弹窗打开踩点：把当前输入框里的值带过去，避免"两边不一致"。 */
function openLocationPickerFromEdit() {
  if (!editingChannelId.value) return;
  locationPickerTarget.value = {
    id: editingChannelId.value,
    name: editChannelForm.value.alias || editChannelForm.value.name || "通道",
    longitude: Number(editChannelLongitude.value) || 0,
    latitude: Number(editChannelLatitude.value) || 0
  };
  locationPickerVisible.value = true;
}

/**
 * 踩点结果回填。
 *
 * ⛔ 从通道编辑弹窗进来时只回填、不保存：用户在弹窗里可能还要改别名等其它字段，
 *    直接落库会让"我还没点保存怎么就生效了"。
 * ⛔ 从地图卡片进来时没有其它待改字段，直接落库更符合预期（点完就生效）。
 */
async function handleLocationConfirm(payload: { longitude: number; latitude: number }) {
  const target = locationPickerTarget.value;
  locationPickerVisible.value = false;
  if (!target) return;

  if (editChannelVisible.value && editingChannelId.value === target.id) {
    editChannelLongitude.value = payload.longitude === 0 ? "0" : String(payload.longitude);
    editChannelLatitude.value = payload.latitude === 0 ? "0" : String(payload.latitude);
    Message.success("已填入坐标，点「保存」后生效");
    return;
  }

  try {
    const res = await updateChannel(target.id, { longitude: payload.longitude, latitude: payload.latitude });
    if (res.code !== 0) {
      Message.error(res.message || "保存位置失败");
      return;
    }
    const cleared = payload.longitude === 0 && payload.latitude === 0;
    Message.success(cleared ? "已清除该通道坐标" : "位置已保存");
    // 地图数据重新拉一遍，而不是本地改一个点：清除坐标后该点必须从地图上消失，
    // 本地 patch 很容易漏掉"从有到无"这一支。
    await loadMapData();
    // 左栏通道树的增删与"未设置位置"计数同样会变：补完位置这台设备就进树/出提示了。
    void mapChannelTreeRef.value?.refresh();
  } catch (error: any) {
    Message.error(error?.message || "保存位置失败");
  }
}

function fitMapToData() {
  if (!mapInstance || !markers.value.length) return;
  const bounds = new LngLatBounds();
  markers.value.forEach(marker => bounds.extend([marker.longitude, marker.latitude]));
  // 单点视野是退化矩形，fitBounds 会顶到 maxZoom；钳到 mapSinglePointFitZoom 更适合看单个点位
  mapInstance.fitBounds(bounds, {
    padding: 60,
    maxZoom: markers.value.length === 1 ? mapSinglePointFitZoom : mapMaxZoom,
    duration: 500
  });
}

function ensureMap() {
  if (mapInstance || !mapContainer.value) return;
  const container = mapContainer.value;
  // 容器尚未完成布局时延到下一帧，避免以 0 尺寸初始化导致画布空白。
  if (container.clientWidth === 0 || container.clientHeight === 0) {
    requestAnimationFrame(ensureMap);
    return;
  }
  mapError.value = "";
  mapReady.value = false;
  mapFirstRender.value = false;
  mapInstance = createMapInstance(container);
  attachMapLifecycle();
}

function createMapInstance(container: HTMLElement): MapLibreMap {
  const instance = new maplibregl.Map({
    container,
    style: currentMapStyleUrl(),
    center: [116.3974, 39.9093],
    zoom: mapZoom.value,
    minZoom: mapMinZoom,
    maxZoom: mapMaxZoom,
    attributionControl: false,
    hash: false
  });
  if (typeof maplibregl.ScaleControl === "function") {
    instance.addControl(new maplibregl.ScaleControl({ maxWidth: 120, unit: "metric" }), "bottom-left");
  }
  return instance;
}

function attachMapLifecycle() {
  if (!mapInstance) return;
  mapInstance.once("load", () => {
    mapReady.value = true;
    requestAnimationFrame(() => mapInstance?.resize());
    // 先定位到平台默认中心，再决定要不要"适应数据视野"。
    // ⛔ 顺序不能反：loadMapData 里首次会 fitBounds 到数据包围盒，
    //    那一步会把刚设好的中心点直接覆盖掉。
    void applyDefaultMapCenter().then(applied => {
      if (!applied) loadMapData();
    });
    // 底图瓦片长期未就绪时兜底，避免一直停在 loading。
    mapFirstRenderTimer = window.setTimeout(() => {
      mapFirstRenderTimer = null;
      if (!mapFirstRender.value && !mapError.value) {
        mapFirstRender.value = true;
        mapError.value = "地图底图加载超时，请检查网络后刷新";
      }
    }, 12000);
  });
  mapInstance.once("idle", () => {
    mapFirstRender.value = true;
  });
  mapMoveHandler = () => {
    if (!mapInstance) return;
    mapZoom.value = Math.round(mapInstance.getZoom());
    loadMapData();
  };
  mapInstance.on("moveend", mapMoveHandler);
  mapMouseMoveHandler = event => {
    mapCursorLongitude.value = event.lngLat.lng;
    mapCursorLatitude.value = event.lngLat.lat;
  };
  mapInstance.on("mousemove", mapMouseMoveHandler);
  mapClickHandler = onMapClick;
  mapInstance.on("click", mapClickHandler);
  mapInstance.on("error", () => {
    if (!mapReady.value) {
      mapError.value = "底图加载失败，请检查网络或配置 VITE_MAP_STYLE_URL";
    } else if (!mapFirstRender.value && !mapError.value) {
      mapError.value = "地图底图加载异常，请稍后刷新";
    }
  });
}

let channelRequestVersion = 0;
let deviceRequestVersion = 0;
async function loadChannelsData() {
  if (!canViewDevices.value) return;
  const version = ++channelRequestVersion;
  rowsLoading.value = true;
  try {
    const res = await listChannels({
      q: keyword.value.trim() || undefined,
      ...channelScope.value,
      status: statusFilter.value,
      page: page.value,
      pageSize: pageSize.value
    });
    if (version !== channelRequestVersion) return;
    if (res.code === 0) {
      channels.value = res.data?.list || [];
      total.value = res.data?.total || 0;
    }
  } catch (error: any) {
    if (version === channelRequestVersion) Message.error(error?.message || "通道列表加载失败");
  } finally {
    if (version === channelRequestVersion) rowsLoading.value = false;
  }
}

async function loadDevicesData() {
  if (!canViewDevices.value) return;
  const version = ++deviceRequestVersion;
  rowsLoading.value = true;
  try {
    const response = await listDevices({
      q: keyword.value.trim() || undefined,
      ...directoryQuery(directoryState.value),
      status: statusFilter.value,
      page: page.value,
      pageSize: pageSize.value
    });
    if (version !== deviceRequestVersion) return;
    if (response.code === 0) {
      devices.value = response.data?.list || [];
      total.value = response.data?.total || 0;
    }
  } catch (error: any) {
    if (version === deviceRequestVersion) Message.error(error?.message || "设备列表加载失败");
  } finally {
    if (version === deviceRequestVersion) rowsLoading.value = false;
  }
}

async function loadMapData() {
  if (!canViewDevices.value) return;
  if (viewMode.value !== "map") return;
  // 记录本次请求代次:响应返回时若已有更新的请求或地图已销毁,丢弃旧结果,
  // 避免慢响应把过期的点位/数量覆盖到当前视图
  const seq = ++mapDataSeq;
  mapLoading.value = true;
  // 首次(或切回地图)这一轮**不按当前视野过滤**。
  //
  // ⛔ 自动定位的目的就是把视野移到数据上，若首轮就按视野过滤，那么"数据全在视野外"
  //    时返回的列表是空的 ⇒ 下面的 fitBounds 永远不会触发 ⇒ 视野不动 ⇒ 数据永远进不了
  //    视野。这是个死锁，表现就是"地图上什么都看不到"。初始视野固定在北京 zoom10，
  //    只要所有带坐标的通道都不在这个框里就必然踩中。
  // 只在首轮放开；拿到数据 fit 之后会触发 moveend → 下一轮自然恢复按视野查询。
  const autoFitPending = mapAutoFitPending;
  try {
    const query = {
      ...(autoFitPending ? {} : mapBoundsParams()),
      q: keyword.value.trim() || undefined,
      ...channelScope.value,
      status: statusFilter.value
    };
    const [markerRes, clusterRes] = await Promise.all([
      listMapMarkers({ ...query, limit: 800 }),
      listMapClusters({ ...query, zoom: mapZoom.value })
    ]);
    if (seq !== mapDataSeq) return;
    if (markerRes.code === 0) markers.value = markerRes.data?.list || [];
    if (clusterRes.code === 0) clusters.value = clusterRes.data?.clusters || [];
    total.value = markerRes.data?.total || 0;
    renderMapOverlays();
    if (autoFitPending) {
      // 有没有数据都要落地这个标记:真没数据时若还留着,每轮都会退化成一次全量查询
      mapAutoFitPending = false;
      if (markers.value.length) fitMapToData();
    }
  } catch (error: any) {
    if (seq !== mapDataSeq) return;
    Message.error(error?.message || "地图数据加载失败");
  } finally {
    if (seq === mapDataSeq) mapLoading.value = false;
  }
}

let statsRequestVersion = 0;
async function refreshStats() {
  if (!canViewDevices.value) return;
  const version = ++statsRequestVersion;
  const scope = {
    q: keyword.value.trim() || undefined,
    ...(assetKind.value === "device" ? directoryQuery(directoryState.value) : channelScope.value)
  };
  const isDevice = assetKind.value === "device";
  const [onlineRes, offlineRes] = await Promise.allSettled(
    isDevice
      ? [
          listDevices({ ...scope, status: "online", page: 1, pageSize: 1 }),
          listDevices({ ...scope, status: "offline", page: 1, pageSize: 1 })
        ]
      : [
          listChannels({ ...scope, status: "online", page: 1, pageSize: 1 }),
          listChannels({ ...scope, status: "offline", page: 1, pageSize: 1 })
        ]
  );
  if (version !== statsRequestVersion) return;
  if (onlineRes.status === "fulfilled" && onlineRes.value.code === 0) {
    const value = onlineRes.value.data?.total || 0;
    if (isDevice) onlineDeviceTotal.value = value;
    else onlineChannelTotal.value = value;
  }
  if (offlineRes.status === "fulfilled" && offlineRes.value.code === 0) {
    const value = offlineRes.value.data?.total || 0;
    if (isDevice) offlineDeviceTotal.value = value;
    else offlineChannelTotal.value = value;
  }
}

// 形参只要 id:列表行(ChannelVO)、地图标记点(MapMarker)、聚合里落单的通道(MarkerPoint)
// 三种来源都会调它,而这里确实只用得到 id。
async function openChannel(record: { id: number }) {
  if (!canViewDevices.value) return;
  drawerTarget.value = { type: "channel", id: record.id };
  drawerVisible.value = true;
  drawerLoading.value = true;
  try {
    const [detailRes, mountRes, timelineRes] = await Promise.all([
      getChannel(record.id),
      listChannelMounts(record.id),
      getChannelTimeline(record.id)
    ]);
    if (detailRes.code === 0) channelDetail.value = detailRes.data;
    if (mountRes.code === 0) channelMounts.value = mountRes.data?.list || [];
    if (timelineRes.code === 0) timeline.value = timelineRes.data?.slots || [];
  } catch (error: any) {
    Message.error(error?.message || "详情加载失败");
  } finally {
    drawerLoading.value = false;
  }
}

async function openDevice(record: DeviceVO) {
  if (!canViewDevices.value) return;
  drawerTarget.value = { type: "device", id: record.id };
  drawerVisible.value = true;
  drawerLoading.value = true;
  deviceSubscriptions.value = [];
  try {
    const [detailRes, subscriptionsRes] = await Promise.all([
      getDevice(record.id),
      canManageSubscriptions.value ? listDeviceSubscriptions(record.id) : Promise.resolve(null)
    ]);
    if (detailRes.code === 0) deviceDetail.value = detailRes.data;
    if (subscriptionsRes?.code === 0) deviceSubscriptions.value = subscriptionsRes.data?.list || [];
  } catch (error: any) {
    Message.error(error?.message || "详情加载失败");
  } finally {
    drawerLoading.value = false;
  }
}

function openChannelOperations(channel: ChannelVO) {
  channelOperationTarget.value = channel;
  channelOperationTab.value = canControlDevice.value ? "control" : canSnapshotDevice.value ? "snapshot" : "status";
  channelOperationVisible.value = true;
}

async function prepareMaintenance(record: DeviceVO) {
  const version = ++maintenanceOpenVersion;
  maintenanceDevice.value = record;
  drawerVisible.value = false;
  recordsVisible.value = false;
  maintenanceChecking.value = true;
  maintenanceCheckError.value = "";
  try {
    const [detail] = await Promise.all([getDevice(record.id), maintenanceActivity.refresh(record.id)]);
    if (version !== maintenanceOpenVersion) return;
    if (detail.code !== 0 || !detail.data) throw new Error("设备详情加载失败");
    onMaintenanceDeviceUpdated(detail.data);
  } catch {
    if (version === maintenanceOpenVersion) maintenanceCheckError.value = "未能核查设备状态，请关闭后重新打开重试。";
  } finally {
    if (version === maintenanceOpenVersion) maintenanceChecking.value = false;
  }
}

function openDeviceUpgrade(record: DeviceVO) {
  if (!canViewMaintenance.value || !canUpgradeDevice.value) return;
  rebootVisible.value = false;
  upgradeVisible.value = true;
  void prepareMaintenance(record);
}

function openDeviceReboot(record: DeviceVO) {
  if (!canViewMaintenance.value || !canRebootDevice.value) return;
  upgradeVisible.value = false;
  rebootOperationId.value = undefined;
  rebootVisible.value = true;
  void prepareMaintenance(record);
}

function openMaintenanceRecords(record: DeviceVO, type: "reboot" | "upgrade" = "reboot", operationId?: string) {
  if (!canViewMaintenance.value) return;
  ++maintenanceOpenVersion;
  maintenanceChecking.value = false;
  drawerVisible.value = false;
  rebootVisible.value = false;
  upgradeVisible.value = false;
  maintenanceDevice.value = record;
  recordsType.value = type;
  recordsOperationId.value = operationId;
  recordsVisible.value = true;
}

function viewMaintenanceRecord(type: "reboot" | "upgrade", operationId?: string) {
  if (maintenanceDevice.value) openMaintenanceRecords(maintenanceDevice.value, type, operationId);
}

function maintenanceBlockReason(action: "reboot" | "upgrade") {
  if (maintenanceChecking.value) return "正在核查设备维护状态，请稍候。";
  if (maintenanceCheckError.value) return maintenanceCheckError.value;
  return maintenanceDevice.value ? maintenanceActivity.reason(maintenanceDevice.value.id, action) : "";
}

function onMaintenanceDeviceUpdated(updatedDevice: DeviceVO) {
  if (selectedDevice.value?.id === updatedDevice.id) selectedDevice.value = updatedDevice;
  if (deviceDetail.value?.id === updatedDevice.id) deviceDetail.value = updatedDevice;
  if (maintenanceDevice.value?.id === updatedDevice.id) maintenanceDevice.value = updatedDevice;
}

function onUpgradeOperation(operation: UpgradeOperation) {
  maintenanceActivity.rememberUpgrade(operation.deviceId, operation);
}

function onRebootOperation(operation: DeviceOperationResult) {
  rebootOperationId.value = operation.operationId;
  if (maintenanceDevice.value) maintenanceActivity.rememberReboot(maintenanceDevice.value.id, operation);
}

function onMaintenanceUncertain(action: "reboot" | "upgrade") {
  if (maintenanceDevice.value) maintenanceActivity.markUncertain(maintenanceDevice.value.id, action);
}

function onMaintenanceFirmwareUpdated(firmware: string) {
  if (maintenanceDevice.value) onMaintenanceDeviceUpdated({ ...maintenanceDevice.value, firmware });
}

function openKnownMaintenance(record: DeviceVO) {
  const state = maintenanceActivity.devices[record.id];
  if (isUpgradeUnresolved(state?.upgrade) && canUpgradeDevice.value) openDeviceUpgrade(record);
  else {
    const upgrade = state?.uncertain === "upgrade" || isUpgradeUnresolved(state?.upgrade);
    openMaintenanceRecords(
      record,
      upgrade ? "upgrade" : "reboot",
      upgrade ? state?.upgrade?.operationId : state?.reboot?.operationId
    );
  }
}

function openSubscriptionManager(record: DeviceVO) {
  if (!canManageSubscriptions.value) return;
  subscriptionDevice.value = record;
  subscriptionDialogVisible.value = true;
}

async function handleSubscriptionChanged() {
  if (!canManageSubscriptions.value) return;
  void directoryPanelRef.value?.refresh();
  if (!deviceDetail.value || subscriptionDevice.value?.id !== deviceDetail.value.id) return;
  try {
    const res = await listDeviceSubscriptions(deviceDetail.value.id);
    if (res.code === 0) deviceSubscriptions.value = res.data?.list || [];
  } catch (error) {
    console.warn("刷新设备订阅摘要失败", error);
  }
}

async function loadStatusEvents(append = false) {
  if (!canViewTraffic.value) return;
  const device = statusEventDevice.value;
  if (!device || statusEventLoading.value || statusEventLoadingMore.value) return;
  const requestVersion = statusEventRequestVersion;
  const requestPage = append ? statusEventPage.value + 1 : 1;
  if (append) {
    statusEventLoadingMore.value = true;
    statusEventLoadMoreError.value = "";
  } else {
    statusEventLoading.value = true;
    statusEventError.value = "";
    statusEventLoadMoreError.value = "";
  }
  try {
    const res = await listDeviceStatusEvents(device.id, {
      page: requestPage,
      pageSize: statusEventPageSize
    });
    if (res.code !== 0) throw new Error(res.message || "状态轨迹加载失败");
    if (requestVersion !== statusEventRequestVersion) return;
    const nextList = res.data?.list || [];
    if (append) {
      const loadedIds = new Set(statusEventList.value.map(event => event.id));
      statusEventList.value.push(...nextList.filter(event => !loadedIds.has(event.id)));
      statusEventPage.value = requestPage;
    } else {
      statusEventList.value = nextList;
      statusEventPage.value = 1;
    }
    statusEventTotal.value = res.data?.total || 0;
  } catch (error: any) {
    if (requestVersion !== statusEventRequestVersion) return;
    if (append) {
      statusEventLoadMoreError.value = error?.message || "更多状态轨迹加载失败";
    } else {
      statusEventError.value = error?.message || "状态轨迹加载失败";
      statusEventList.value = [];
      statusEventTotal.value = 0;
    }
  } finally {
    if (requestVersion === statusEventRequestVersion) {
      if (append) statusEventLoadingMore.value = false;
      else statusEventLoading.value = false;
    }
  }
}

async function loadRuntimeChannels(record: DeviceVO) {
  if (!canViewTraffic.value) return;
  const requestVersion = statusEventRequestVersion;
  runtimeChannelsLoading.value = true;
  runtimeChannelsError.value = "";
  try {
    const result = await listChannels({ deviceId: record.deviceId, page: 1, pageSize: 200 });
    if (requestVersion !== statusEventRequestVersion) return;
    if (result.code !== 0) throw new Error(result.message || "设备通道加载失败");
    runtimeChannels.value = result.data?.list || [];
    if (runtimeChannelCode.value && !runtimeChannels.value.some(channel => channel.channelId === runtimeChannelCode.value))
      runtimeChannelCode.value = "";
  } catch (error: any) {
    if (requestVersion !== statusEventRequestVersion) return;
    runtimeChannels.value = [];
    runtimeChannelsError.value = error?.message || "设备通道加载失败";
  } finally {
    if (requestVersion === statusEventRequestVersion) runtimeChannelsLoading.value = false;
  }
}

function openStatusEvents(record: DeviceVO) {
  if (!canViewTraffic.value) return;
  statusEventRequestVersion += 1;
  statusEventDevice.value = record;
  statusEventPage.value = 1;
  statusEventList.value = [];
  statusEventTotal.value = 0;
  statusEventLoading.value = false;
  statusEventLoadingMore.value = false;
  runtimeActiveTab.value = "status";
  runtimeChannelCode.value = "";
  runtimeChannels.value = [];
  runtimeChannelsError.value = "";
  statusEventVisible.value = true;
  loadStatusEvents();
  nextTick(() => {
    if (statusEventScroll.value) statusEventScroll.value.scrollTop = 0;
  });
}

function loadMoreStatusEvents() {
  if (!canViewTraffic.value) return;
  if (!statusEventHasMore.value || statusEventLoading.value || statusEventLoadingMore.value) return;
  loadStatusEvents(true);
}

function onStatusEventScroll(event: Event) {
  const target = event.currentTarget as HTMLElement;
  const distanceToBottom = target.scrollHeight - target.scrollTop - target.clientHeight;
  if (distanceToBottom <= 80) loadMoreStatusEvents();
}

function closeStatusEvents() {
  statusEventRequestVersion += 1;
  statusEventDevice.value = null;
  statusEventList.value = [];
  statusEventError.value = "";
  statusEventLoadMoreError.value = "";
  statusEventTotal.value = 0;
  statusEventPage.value = 1;
  statusEventLoading.value = false;
  statusEventLoadingMore.value = false;
  runtimeActiveTab.value = "status";
  runtimeChannelCode.value = "";
  runtimeChannels.value = [];
  runtimeChannelsError.value = "";
}

watch(runtimeActiveTab, tab => {
  if (!canViewTraffic.value) return;
  if (
    tab === "status" &&
    statusEventVisible.value &&
    statusEventList.value.length === 0 &&
    !statusEventLoading.value &&
    !statusEventError.value
  ) {
    loadStatusEvents();
  }
  if (
    tab !== "status" &&
    statusEventVisible.value &&
    statusEventDevice.value &&
    runtimeChannels.value.length === 0 &&
    !runtimeChannelsLoading.value &&
    !runtimeChannelsError.value
  ) {
    loadRuntimeChannels(statusEventDevice.value);
  }
});

function statusEventTime(value?: string | null) {
  if (!value || value.startsWith("0001-01-01")) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "-";
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

function eventColor(eventType: DeviceStatusEvent["eventType"]) {
  return (
    {
      register_online: "#10b981",
      register_renewed: "var(--uvp-brand)",
      heartbeat_recovered: "var(--uvp-brand-cyan)",
      heartbeat_timeout: "var(--uvp-danger)",
      unregister_offline: "var(--uvp-text-tertiary)"
    } as const
  )[eventType];
}

function eventSourceText(source: DeviceStatusEvent["source"]) {
  return (
    (
      {
        register: "REGISTER",
        unregister: "REGISTER 注销",
        keepalive: "Keepalive",
        offline_scanner: "离线扫描器"
      } as const
    )[source] || source
  );
}

function eventMetaText(event: DeviceStatusEvent) {
  const parts: string[] = [eventSourceText(event.source)];
  if (event.ip) parts.push(`${event.transport || "UDP"} ${event.ip}${event.port ? `:${event.port}` : ""}`);
  if (event.registerExpires != null) parts.push(`有效期 ${event.registerExpires} 秒`);
  return parts.join(" · ");
}

// 通道播放状态判定:后端 gb_channel.stream_id 非空 = 当前正在播放.
// 依赖 10s 自动轮询 refreshMainData() 天然刷新,进程重启 / hook 丢包场景由
// 后端 play/reconciler 5min 兜底对账 goroutine 修正,前端不做额外核对.
function isChannelPlaying(record: ChannelVO): boolean {
  return !!record.streamId && record.streamId.trim() !== "";
}

// 通道级停止播放 loading 态,按 channel.id 单独存,防止用户点两下重复弹 Modal.
const stoppingChannels = ref<Set<number>>(new Set());

// 强制停止当前主流(管理员级动作).
// 语义:多用户可各自点"播放"发起独立流,但此按钮会断掉 gb_channel.stream_id 记录的
// 那一路"当前主流" —— 如果有其他人正在通过该流观看,他们会被同时断开.
// Modal 里必须有明确警告让点击者知道副作用.
async function handleStopChannel(record: ChannelVO) {
  if (!canStopPlayback.value) return;
  if (!record.streamId) return;
  Modal.warning({
    title: `确认强制停止通道 ${record.name} 的当前直播?`,
    content: "如果有其他人正在观看此通道,他们会被同时断开。停止后可重新点击播放。",
    okText: "确认强制停止",
    cancelText: "取消",
    hideCancel: false,
    onOk: async () => {
      stoppingChannels.value.add(record.id);
      try {
        const response = await stopPlay(record.streamId);
        Message.success(response.message || "已停止当前直播");
        refreshMainData();
      } catch (e: any) {
        // stopPlay 失败不刷新列表 —— 避免把"实际还在播"错误清成"空闲"
        Message.error(e?.message || "停止播放失败,请稍后重试");
      } finally {
        stoppingChannels.value.delete(record.id);
      }
    }
  });
}

function playChannel(record: ChannelVO) {
  if (!canStartPlayback.value) return;
  playbackConsole.open(record);
}

function openRecordQuery(record: ChannelVO) {
  if (!canQueryDeviceRecords.value) return;
  const returnKey = saveDeviceMgmtReturnSnapshot({
    version: 1,
    viewMode: viewMode.value,
    assetKind: "channel",
    navigationMode: navigationMode.value,
    selectedDeviceId: selectedDevice.value?.id,
    keyword: keyword.value,
    deviceIdFilter: deviceIdFilter.value,
    statusFilter: statusFilter.value,
    directoryView: directoryState.value.view,
    directorySelectedKey: directoryState.value.selectedKey[directoryState.value.view],
    page: page.value,
    listPageSize: listPageSize.value,
    cardPageSize: cardPageSize.value
  });
  router.push({ name: "gb28181-device-record-playback", params: { channelId: record.id }, query: { returnKey } });
}
function isInteractiveDblclick(event: MouseEvent) {
  const target = event.target;
  return (
    target instanceof Element && Boolean(target.closest("button, a, input, textarea, select, [role='button'], [role='combobox']"))
  );
}
function onDeviceDblclick(record: DeviceVO, event: MouseEvent) {
  if (!isInteractiveDblclick(event)) showDeviceChannels(record);
}
function onChannelDblclick(record: ChannelVO, event: MouseEvent) {
  if (!isInteractiveDblclick(event)) playChannel(record);
}

const deleting = ref(false);
const refreshingCatalog = reactive<Record<number, boolean>>({});
const catalogRefreshVisible = ref(false);
const catalogRefreshProgress = ref<CatalogRefreshProgress | null>(null);
const catalogRefreshDevice = ref<DeviceVO | null>(null);
let catalogRefreshTimer: ReturnType<typeof setTimeout> | null = null;
let catalogRefreshPollVersion = 0;

function clearCatalogRefreshTimer() {
  if (catalogRefreshTimer !== null) {
    clearTimeout(catalogRefreshTimer);
    catalogRefreshTimer = null;
  }
}

function closeCatalogRefreshProgress() {
  catalogRefreshPollVersion += 1;
  clearCatalogRefreshTimer();
  if (catalogRefreshDevice.value) refreshingCatalog[catalogRefreshDevice.value.id] = false;
  catalogRefreshVisible.value = false;
}

function isCatalogRefreshTerminal(status?: CatalogRefreshProgress["status"]) {
  return status === "completed" || status === "failed" || status === "timeout";
}

async function pollCatalogRefresh(device: DeviceVO, operationId: string, version: number) {
  if (version !== catalogRefreshPollVersion || !catalogRefreshVisible.value) return;
  try {
    const res = await getDeviceCatalogRefreshProgress(device.id, operationId);
    if (version !== catalogRefreshPollVersion) return;
    if (res.code !== 0 || !res.data) throw new Error(res.message || "查询刷新进度失败");
    catalogRefreshProgress.value = res.data;
    if (isCatalogRefreshTerminal(res.data.status)) {
      clearCatalogRefreshTimer();
      refreshingCatalog[device.id] = false;
      if (res.data.status === "completed") await refreshMainData();
      return;
    }
    catalogRefreshTimer = setTimeout(() => void pollCatalogRefresh(device, operationId, version), 400);
  } catch (error: any) {
    if (version !== catalogRefreshPollVersion) return;
    clearCatalogRefreshTimer();
    refreshingCatalog[device.id] = false;
    Message.error(error?.message || "查询刷新进度失败");
  }
}
const createDeviceVisible = ref(false);
const createDeviceForm = reactive<CreateDeviceDTO>({
  deviceId: "",
  name: "",
  password: ""
});
const createDeviceFormRef = ref();
const creatingDevice = ref(false);
const createDeviceRules = {
  deviceId: [
    { required: true, message: "请输入设备国标 ID" },
    {
      match: /^\d{20}$/,
      message: "设备国标 ID 必须为 20 位数字"
    }
  ]
};

function afterDeleteSuccess() {
  selectedRowKeys.value = [];
  refreshMainData();
  refreshStats();
}

async function handleRefreshDeviceCatalog(record: DeviceVO) {
  if (!canRefreshCatalog.value) return;
  if (refreshingCatalog[record.id]) return;
  if (!record.online) {
    Message.warning("设备离线,无法下发 Catalog 查询");
    return;
  }
  refreshingCatalog[record.id] = true;
  catalogRefreshDevice.value = record;
  catalogRefreshProgress.value = null;
  catalogRefreshVisible.value = true;
  const version = ++catalogRefreshPollVersion;
  clearCatalogRefreshTimer();
  try {
    const res = await refreshDeviceCatalog(record.id);
    if (res.code !== 0 || !res.data?.operationId) throw new Error(res.message || "下发失败");
    void pollCatalogRefresh(record, res.data.operationId, version);
  } catch (error: any) {
    catalogRefreshPollVersion += 1;
    catalogRefreshVisible.value = false;
    Message.error(error?.message || "下发失败");
    refreshingCatalog[record.id] = false;
  }
}

function openCreateDeviceModal() {
  if (!canAddDevice.value) return;
  createDeviceForm.deviceId = "";
  createDeviceForm.name = "";
  createDeviceForm.password = "";
  createDeviceVisible.value = true;
  createDeviceFormRef.value?.clearValidate?.();
}

async function handleCreateDevice() {
  if (!canAddDevice.value) return;
  const errors = await createDeviceFormRef.value?.validate?.();
  if (errors) return;
  creatingDevice.value = true;
  try {
    const res = await createDevice({
      deviceId: createDeviceForm.deviceId.trim(),
      name: createDeviceForm.name?.trim() || undefined,
      password: createDeviceForm.password?.trim() || undefined
    });
    if (res.code === 0) {
      Message.success("设备已创建");
      createDeviceVisible.value = false;
      void directoryPanelRef.value?.refresh();
      refreshMainData();
      refreshStats();
    } else {
      Message.error(res.message || "创建失败");
    }
  } catch (error: any) {
    Message.error(error?.message || "创建失败");
  } finally {
    creatingDevice.value = false;
  }
}

function openEditDeviceModal(record: DeviceVO) {
  if (!canEditDevice.value) return;
  originalProtocolOverride = normalizeProtocolOverride(record.protocolOverride);
  originalZLMNodeID = record.zlmNodeId || 0;
  editDeviceForm.value = {
    deviceId: record.deviceId,
    alias: record.alias || "",
    name: record.name || "",
    zlmNodeId: record.zlmNodeId || 0,
    protocolOverride: originalProtocolOverride
  };
  editingDeviceId.value = record.deviceId;
  editDeviceVisible.value = true;
  loadZLMNodeOptions();
}

async function loadZLMNodeOptions() {
  if (!canEditDevice.value) return;
  zlmNodesLoading.value = true;
  zlmNodesError.value = "";
  try {
    const res = await listZLMNodes();
    if (res.code !== 0) throw new Error(res.message || "ZLM 节点加载失败");
    zlmNodes.value = res.data?.list || [];
  } catch (error: any) {
    zlmNodes.value = [];
    zlmNodesError.value = error?.message || "ZLM 节点加载失败";
  } finally {
    zlmNodesLoading.value = false;
  }
}

function openEditChannelModal(record: ChannelVO) {
  if (!canEditChannel.value) return;
  editChannelForm.value = {
    channelId: record.channelId,
    deviceId: record.deviceId,
    alias: record.alias || "",
    name: record.name || "",
    manufacturer: record.manufacturer || "",
    model: record.model || "",
    ptzType: record.ptzType || 0,
    streamTransport: record.streamTransport || "UDP",
    onDemandLive: record.onDemandLive !== false
  };
  // 已有坐标时预填, 让"当前是什么"和"要改成什么"在同一处看得见。
  // 来源是 mobile 时也预填 —— 人工填过就等于接管, 由用户决定要不要覆盖实时值。
  const hasCoord = !!(record.longitude || record.latitude);
  editChannelLongitude.value = hasCoord ? String(record.longitude) : "";
  editChannelLatitude.value = hasCoord ? String(record.latitude) : "";
  editChannelPositionSource.value = record.positionSource || "";
  editingChannelId.value = record.id;
  editChannelVisible.value = true;
}

function cancelEditChannel() {
  editChannelVisible.value = false;
  editingChannelId.value = 0;
  editChannelForm.value = emptyEditChannelForm();
  editChannelLongitude.value = "";
  editChannelLatitude.value = "";
  editChannelPositionSource.value = "";
}

// 人工坐标入参归一: '' 成对空 = 不动; 成对数字 = 设置; 单给 0 = 非法。
function resolveCoordPayload() {
  return resolveChannelCoordPayload(editChannelLongitude.value, editChannelLatitude.value);
}

async function handleEditChannel() {
  if (!canEditChannel.value) return;
  if (!editingChannelId.value) return;
  const coord = resolveCoordPayload();
  if ("error" in coord) {
    Message.error(coord.error);
    return;
  }
  editingChannel.value = true;
  try {
    const [ptzRes, transportRes] = await Promise.all([
      updateChannel(editingChannelId.value, {
        alias: editChannelForm.value.alias,
        ptzType: editChannelForm.value.ptzType,
        onDemandLive: editChannelForm.value.onDemandLive,
        ...coord.payload
      }),
      canEditTransport.value
        ? updateChannelStreamTransport(editingChannelId.value, editChannelForm.value.streamTransport as any)
        : Promise.resolve({ code: 0, message: "" })
    ]);
    if (ptzRes.code !== 0 || transportRes.code !== 0) {
      Message.error(ptzRes.code !== 0 ? ptzRes.message || "摄像头类型更新失败" : transportRes.message || "流传输模式更新失败");
      return;
    }
    const coordPatch = coordPatchAfterSave(coord.payload);
    const item = channels.value.find(channel => channel.id === editingChannelId.value);
    if (item) {
      item.alias = editChannelForm.value.alias;
      item.ptzType = editChannelForm.value.ptzType;
      if (canEditTransport.value) item.streamTransport = editChannelForm.value.streamTransport;
      item.onDemandLive = editChannelForm.value.onDemandLive;
      if (coordPatch) Object.assign(item, coordPatch);
    }
    if (channelDetail.value?.id === editingChannelId.value) {
      channelDetail.value = {
        ...channelDetail.value,
        alias: editChannelForm.value.alias,
        ptzType: editChannelForm.value.ptzType,
        ...(canEditTransport.value ? { streamTransport: editChannelForm.value.streamTransport } : {}),
        onDemandLive: editChannelForm.value.onDemandLive,
        ...(coordPatch ?? {})
      };
    }
    Message.success("通道信息已更新");
    cancelEditChannel();
  } catch (error: any) {
    Message.error(error?.message || "通道信息更新失败");
  } finally {
    editingChannel.value = false;
  }
}

function cancelEditDevice() {
  editDeviceVisible.value = false;
  editDeviceForm.value = { deviceId: "", alias: "", name: "", zlmNodeId: 0, protocolOverride: "auto" };
  editingDeviceId.value = "";
  zlmNodesError.value = "";
  originalProtocolOverride = "auto";
  originalZLMNodeID = 0;
}

function rollbackProtocolOverride() {
  editDeviceForm.value.protocolOverride = protocolOverrideAfterSave(
    originalProtocolOverride,
    editDeviceForm.value.protocolOverride,
    false
  );
}

async function handleEditDevice() {
  if (!canEditDevice.value) return;
  if (!editingDeviceId.value) return;
  editingDevice.value = true;
  try {
    const zlmNodeUpdate =
      editDeviceForm.value.zlmNodeId === originalZLMNodeID ? {} : { zlmNodeId: editDeviceForm.value.zlmNodeId };
    const res = await updateDevice(editingDeviceId.value, {
      alias: editDeviceForm.value.alias,
      protocolOverride: editDeviceForm.value.protocolOverride,
      ...zlmNodeUpdate
    });
    if (res.code === 0) {
      originalProtocolOverride = protocolOverrideAfterSave(originalProtocolOverride, editDeviceForm.value.protocolOverride, true);
      originalZLMNodeID = editDeviceForm.value.zlmNodeId;
      Message.success("设备信息已更新");
      void directoryPanelRef.value?.refresh();
      if (selectedDevice.value?.deviceId === editingDeviceId.value) {
        const updated = await getDevice(selectedDevice.value.id);
        if (updated.code === 0 && updated.data?.deviceId === deviceIdFilter.value) {
          selectedDevice.value = updated.data;
          selectedDeviceSummary.value = deviceSummary(updated.data);
        }
      }
      editDeviceVisible.value = false;
      refreshMainData();
      // 如果抽屉打开着,同步更新抽屉内容
      if (drawerVisible.value && deviceDetail.value?.deviceId === editingDeviceId.value) {
        // Do not optimistically display a protocol override as effective before the server re-reads it.
        deviceDetail.value = {
          ...deviceDetail.value,
          alias: editDeviceForm.value.alias,
          zlmNodeId: editDeviceForm.value.zlmNodeId
        };
      }
    } else {
      rollbackProtocolOverride();
      Message.error(res.message || "更新失败");
    }
  } catch (error: any) {
    rollbackProtocolOverride();
    Message.error(error?.message || "更新失败");
  } finally {
    editingDevice.value = false;
  }
}

function reportBatchResult(res: BatchDeleteResult, total: number) {
  const okCount = res.succeeded.length;
  const failCount = res.failed.length;
  if (failCount === 0) {
    Message.success(`已删除 ${okCount} 项`);
    return;
  }
  if (okCount === 0) {
    Message.error(`全部失败:${res.failed[0]?.error || "未知错误"}`);
    return;
  }
  Message.warning(`成功 ${okCount} / ${total},失败 ${failCount}:${res.failed[0]?.error || ""}`);
}

async function handleDeleteDevice(record: DeviceVO) {
  if (!canDeleteDevice.value) return;
  Modal.warning({
    title: "删除设备",
    content: `即将删除设备「${deviceNameText(record)}(${record.deviceId})」及其所有通道、目录挂载。此操作不可恢复,是否继续?`,
    hideCancel: false,
    okText: "删除",
    cancelText: "取消",
    okButtonProps: { status: "danger" },
    onOk: async () => {
      deleting.value = true;
      try {
        const res = await deleteDevice(record.id);
        if (res.code === 0) {
          Message.success("设备已删除");
          if (selectedDevice.value?.id === record.id) onDeviceSelect(null);
          void onDevicesChanged();
          afterDeleteSuccess();
        } else {
          Message.error(res.message || "删除失败");
        }
      } catch (error: any) {
        Message.error(error?.message || "删除失败");
      } finally {
        deleting.value = false;
      }
    }
  });
}

async function handleDeleteChannel(record: ChannelVO) {
  if (!canDeleteChannel.value) return;
  Modal.warning({
    title: "删除通道",
    content: `即将删除通道「${displayName(record)}(${record.channelId})」及其所有目录挂载。此操作不可恢复,是否继续?`,
    hideCancel: false,
    okText: "删除",
    cancelText: "取消",
    okButtonProps: { status: "danger" },
    onOk: async () => {
      deleting.value = true;
      try {
        const res = await deleteChannel(record.id);
        if (res.code === 0) {
          Message.success("通道已删除");
          afterDeleteSuccess();
        } else {
          Message.error(res.message || "删除失败");
        }
      } catch (error: any) {
        Message.error(error?.message || "删除失败");
      } finally {
        deleting.value = false;
      }
    }
  });
}

async function handleStreamTransportChange(channelId: number, streamTransport: string) {
  if (!canEditTransport.value) return;
  try {
    const res = await updateChannelStreamTransport(channelId, streamTransport as any);
    if (res.code === 0) {
      Message.success("流传输模式已更新");
      // 更新本地数据
      const item = channels.value.find(c => c.id === channelId);
      if (item) item.streamTransport = streamTransport;
    } else {
      Message.error(res.message || "更新失败");
    }
  } catch (error: any) {
    Message.error(error?.message || "更新失败");
  }
}

async function handlePtzTypeChange(channelId: number, ptzTypeValue: string) {
  if (!canEditChannel.value) return;
  try {
    const ptzType = Number(ptzTypeValue);
    const res = await updateChannel(channelId, { ptzType });
    if (res.code === 0) {
      Message.success("摄像头类型已更新");
      // 更新本地数据
      const item = channels.value.find(c => c.id === channelId);
      if (item) item.ptzType = ptzType;
    } else {
      Message.error(res.message || "更新失败");
    }
  } catch (error: any) {
    Message.error(error?.message || "更新失败");
  }
}

async function handleAudioEnabledChange(channelId: number, audioEnabled: boolean) {
  if (!canEditChannel.value) return;
  try {
    const res = await updateChannel(channelId, { audioEnabled });
    if (res.code === 0) {
      Message.success(audioEnabled ? "已开启音频,下次点播生效" : "已关闭音频,下次点播生效");
      const item = channels.value.find(c => c.id === channelId);
      if (item) item.audioEnabled = audioEnabled;
      if (channelDetail.value?.id === channelId) {
        channelDetail.value = { ...channelDetail.value, audioEnabled };
      }
    } else {
      Message.error(res.message || "更新失败");
    }
  } catch (error: any) {
    Message.error(error?.message || "更新失败");
  }
}

async function handleOnDemandLiveChange(channelId: number, onDemandLive: boolean) {
  if (!canEditChannel.value) return;
  try {
    const res = await updateChannel(channelId, { onDemandLive });
    if (res.code === 0) {
      Message.success(onDemandLive ? "已开启按需直播" : "已关闭按需直播");
      const item = channels.value.find(c => c.id === channelId);
      if (item) item.onDemandLive = onDemandLive;
      if (channelDetail.value?.id === channelId) {
        channelDetail.value = { ...channelDetail.value, onDemandLive };
      }
    } else {
      Message.error(res.message || "更新失败");
    }
  } catch (error: any) {
    Message.error(error?.message || "更新失败");
  }
}

function isCloudRecordingLoading(channelId: number) {
  return cloudRecordingLoading.value.has(channelId);
}

function setCloudRecordingLoading(channelId: number, loading: boolean) {
  const next = new Set(cloudRecordingLoading.value);
  if (loading) next.add(channelId);
  else next.delete(channelId);
  cloudRecordingLoading.value = next;
}

function recordingMeta(record: ChannelVO) {
  const meta = cloudRecordingStateMeta(record.cloudRecordingState, record.cloudRecordingError);
  return { ...meta, loading: meta.loading || isCloudRecordingLoading(record.id) };
}

async function handleCloudRecordingChange(channelId: number, enabled: boolean) {
  if (!canUpdateRecording.value) return;
  if (isCloudRecordingLoading(channelId)) return;
  setCloudRecordingLoading(channelId, true);
  try {
    const res = await updateCloudRecording(channelId, enabled);
    if (res.code !== 0) {
      Message.error(res.message || "更新云端录像失败");
      return;
    }
    const item = channels.value.find(channel => channel.id === channelId);
    if (item) mergeCloudRecordingState(item, res.data);
    if (channelDetail.value?.id === channelId) {
      const detail = { ...channelDetail.value };
      mergeCloudRecordingState(detail, res.data);
      channelDetail.value = detail;
    }
    Message.success(enabled ? "云端录像已开启，下次点播生效" : "云端录像已关闭");
  } catch (error: any) {
    Message.error(error?.message || "更新云端录像失败");
  } finally {
    setCloudRecordingLoading(channelId, false);
  }
}

async function handleBatchDelete() {
  if (assetKind.value === "device" && !canDeleteDevice.value) return;
  if (assetKind.value === "channel" && !canDeleteChannel.value) return;
  const ids = [...selectedRowKeys.value];
  if (ids.length === 0) return;
  const target = assetKind.value === "device" ? "设备" : "通道";
  const cascade = assetKind.value === "device" ? "所属通道、目录挂载" : "所有目录挂载";
  Modal.warning({
    title: `批量删除${target}`,
    content: `即将删除 ${ids.length} 个${target}及其${cascade}。此操作不可恢复,是否继续?`,
    hideCancel: false,
    okText: "全部删除",
    cancelText: "取消",
    okButtonProps: { status: "danger" },
    onOk: async () => {
      deleting.value = true;
      try {
        const deleteBatch = assetKind.value === "device" ? batchDeleteDevices : batchDeleteChannels;
        const res = await deleteBatch(ids);
        if (res.code === 0 && res.data) {
          reportBatchResult(res.data, ids.length);
          afterDeleteSuccess();
        } else {
          Message.error(res.message || "批量删除失败");
        }
      } catch (error: any) {
        Message.error(error?.message || "批量删除失败");
      } finally {
        deleting.value = false;
      }
    }
  });
}

function resetAutoRefreshCountdown() {
  autoRefreshCountdown.value = AUTO_REFRESH_INTERVAL_SECONDS;
}

function startAutoRefresh() {
  if (refreshCountdownTimer.value) return;
  resetAutoRefreshCountdown();
  refreshCountdownTimer.value = window.setInterval(() => {
    if (autoRefreshCountdown.value <= 1) {
      resetAutoRefreshCountdown();
      void refreshCurrent(true);
    } else {
      autoRefreshCountdown.value -= 1;
    }
  }, 1000);
}

function stopAutoRefresh() {
  if (refreshCountdownTimer.value) {
    clearInterval(refreshCountdownTimer.value);
    refreshCountdownTimer.value = null;
  }
}

async function refreshCurrent(background = false) {
  if (!canViewDevices.value) return;
  resetAutoRefreshCountdown();
  await Promise.all([
    refreshMainData(),
    refreshStats(),
    refreshSelectedDevice(),
    background === true ? directoryPanelRef.value?.sync() : directoryPanelRef.value?.refresh()
  ]);
}

onMounted(async () => {
  window.addEventListener("keydown", focusKeyword);
  // 选点态的 Enter/Esc：作用是"确认/放弃挑好的点"，和上面那个 `keydown` 不冲突（各管各的标志位）。
  window.addEventListener("keydown", handleMapCenterPickKeydown);
  if (returnSnapshot?.selectedDeviceId && navigationMode.value === "device" && canViewDevices.value) {
    try {
      const restored = await getDevice(returnSnapshot.selectedDeviceId);
      if (restored.code === 0 && restored.data && restored.data.deviceId === deviceIdFilter.value) {
        selectedDevice.value = restored.data;
        selectedDeviceSummary.value = deviceSummary(restored.data);
      }
    } catch {
      Message.warning("设备信息恢复失败，可从目录树重新选择");
    }
  }
  maintenancePoll = setInterval(() => {
    if (!canViewMaintenance.value) return;
    for (const id of Object.keys(maintenanceActivity.devices).map(Number)) {
      const state = maintenanceActivity.devices[id];
      if (!state.error && !state.uncertain && maintenanceActivity.label(id)) void maintenanceActivity.refresh(id);
    }
  }, 5000);
  if (viewMode.value === "map") {
    await nextTick(ensureMap);
  }
  await Promise.all([refreshMainData(), refreshStats()]);
  if (canEditChannel.value) await loadPtzTypeDict();
  startAutoRefresh();
  // 首次访问自动讲一遍。
  // ⛔ 必须等 `nextTick` —— 卡片/表格是 v-for 渲染出来的，请求回来那一刻 DOM 还没落地，
  //    此时 `buildGuideSteps` 会把「操作按钮」这类锚点全判成不可见而丢掉整步。
  //    `autoStart` 内部还会再校验两件事：这台机器是不是第一次来、可用步骤够不够，
  //    不满足就直接返回 false（不会弹空壳）。
  if (canViewDevices.value) {
    await nextTick();
    guide.autoStart();
  }
});

onUnmounted(() => {
  ++maintenanceOpenVersion;
  ++channelRequestVersion;
  ++statsRequestVersion;
  clearInterval(maintenancePoll);
  closeCatalogRefreshProgress();
  window.removeEventListener("keydown", focusKeyword);
  window.removeEventListener("keydown", handleMapCenterPickKeydown);
  cancelKeywordSearch();
  stopAutoRefresh();
  if (ringAnimFrame !== null) cancelAnimationFrame(ringAnimFrame);
  destroyMap();
  // ⛔ 离开页面必须把地图卡片拉起来的实时流停掉：ZLM 侧那条流不会因为
  //    前端路由跳走而自己消失，不停就是"人走了、设备还在往服务器推流"。
  void stopVideoStream();
});
</script>

<template>
  <div class="device-mgmt-page">
    <CatalogRefreshProgressDialog
      :visible="catalogRefreshVisible"
      :device-name="catalogRefreshDevice?.alias || catalogRefreshDevice?.name || ''"
      :device-id="catalogRefreshDevice?.deviceId || ''"
      :progress="catalogRefreshProgress"
      @close="closeCatalogRefreshProgress"
    />
    <div class="workspace">
      <s-layout-search class="workspace-toolbar">
        <template #fields>
          <div class="workspace-toolbar-row">
            <div class="device-stats">
              <div class="stat-ring">
                <div
                  class="stat-ring__donut"
                  :style="{
                    background: `conic-gradient(from -90deg, var(--uvp-brand-cyan) 0 ${ringDisplayRate}%, var(--uvp-danger) ${ringDisplayRate}% 100%)`
                  }"
                >
                  <span class="stat-ring__value">{{ ringDisplayRate }}%</span>
                </div>
                <div class="stat-ring__legend">
                  <span class="stat-ring__entity">{{ statEntityLabel }}</span>
                  <span class="stat-ring__item"><span class="stat-ring__dot online"></span>在线 {{ statOnlineTotal }}</span>
                  <span class="stat-ring__item"><span class="stat-ring__dot offline"></span>离线 {{ statOfflineTotal }}</span>
                </div>
              </div>
            </div>
            <div class="toolbar-actions">
              <div v-if="viewMode !== 'map'" class="segmented asset-kind-switch" aria-label="资产类型">
                <button
                  type="button"
                  data-asset-kind="device"
                  data-guide="asset-device"
                  :class="{ active: assetKind === 'device' }"
                  @click="setAssetKind('device')"
                >
                  <RadioTower :size="14" aria-hidden="true" />
                  <span>设备</span>
                </button>
                <button
                  type="button"
                  data-asset-kind="channel"
                  data-guide="asset-channel"
                  :class="{ active: assetKind === 'channel' }"
                  @click="setAssetKind('channel')"
                >
                  <Video :size="14" aria-hidden="true" />
                  <span>通道</span>
                </button>
              </div>
              <div class="view-switch" aria-label="展示形态">
                <button
                  v-for="item in viewOptions"
                  :key="item.value"
                  type="button"
                  :data-guide="`view-${item.value}`"
                  :class="{ active: viewMode === item.value }"
                  :aria-label="`${item.label}视图`"
                  :aria-pressed="viewMode === item.value"
                  :title="`${item.label}视图`"
                  @click="setViewMode(item.value)"
                >
                  <component :is="item.icon" :size="14" />
                </button>
              </div>
              <div class="cmdk">
                <Search :size="14" />
                <input
                  ref="keywordInput"
                  v-model="keyword"
                  type="text"
                  aria-label="搜索设备名称或通道名称或编码"
                  placeholder="搜索设备 / 通道 / 编码 ..."
                  @keydown.enter.prevent="onSearch"
                />
                <a-tooltip v-if="keyword" content="清空搜索" position="bottom">
                  <button class="cmdk-clear" type="button" aria-label="清空搜索" @click="clearKeyword"><X :size="14" /></button>
                </a-tooltip>
                <a-tooltip :content="`按 ${isMacPlatform ? 'Command' : 'Ctrl'} + K 聚焦搜索框`" position="bottom">
                  <span class="kbd"
                    ><kbd>{{ isMacPlatform ? "⌘" : "Ctrl" }}</kbd
                    ><kbd>K</kbd></span
                  >
                </a-tooltip>
              </div>
              <a-select
                v-model="statusFilter"
                allow-clear
                class="toolbar-select status-select"
                placeholder="状态"
                :style="{ width: '126px' }"
              >
                <a-option value="online">在线</a-option>
                <a-option value="offline">离线</a-option>
              </a-select>
              <!-- 「使用引导」手动入口。放成图标按钮而不是带文字的按钮：工具栏已经 6 个控件，
                   再加一个 84px 的文字按钮就会在 1366 宽上把搜索框挤到换行。 -->
              <a-tooltip content="使用引导：把这页的按钮讲一遍" position="bottom">
                <button
                  class="btn-ghost guide-trigger"
                  type="button"
                  data-guide-trigger
                  aria-label="使用引导"
                  @click="startDeviceGuide"
                >
                  <CircleQuestionMark :size="15" />
                </button>
              </a-tooltip>
              <button
                :disabled="!canViewDevices"
                class="btn-ghost refresh-control uvp-page-action-btn uvp-refresh-btn"
                data-testid="refresh-control"
                type="button"
                :title="`自动刷新倒计时 ${autoRefreshCountdown} 秒`"
                :aria-label="`刷新${assetKind === 'device' ? '设备' : '通道'}列表，自动刷新倒计时 ${autoRefreshCountdown} 秒`"
                @click="refreshCurrent()"
              >
                <RefreshCcw :size="14" :class="{ spin: rowsLoading || mapLoading }" />
                刷新 <span class="refresh-countdown">{{ autoRefreshCountdown }}s</span>
              </button>
              <button
                :disabled="!canAddDevice"
                class="btn-primary create-device-btn uvp-page-action-btn uvp-create-btn"
                type="button"
                @click="openCreateDeviceModal"
              >
                <Plus :size="14" /> 新建设备
              </button>
            </div>
          </div>
        </template>
      </s-layout-search>

      <aside class="catalog-pane">
        <!-- 地图视角换「可定位通道树」：地图只认有坐标的通道，摆一棵设备目录树等于
             让人一路点到"没有点位"的枪机上。列表/卡片视角仍用原目录树。 -->
        <MapChannelTree
          v-if="canViewDevices && viewMode === 'map'"
          ref="mapChannelTreeRef"
          v-model:keyword="keyword"
          :scope-value="mapScopeValue"
          :scope-label="mapScopeLabel"
          :selected-channel-id="mapSelectedChannelId"
          :status="statusFilter || ''"
          @locate="locateMapChannel"
          @focus="focusMapDevice"
          @scope-change="applyMapScope"
        />
        <DirectoryPanel
          v-else-if="canViewDevices"
          ref="directoryPanelRef"
          data-guide="directory"
          :resource-filter="{ assetKind, q: keyword.trim(), status: statusFilter }"
          v-model="directoryState"
          :selected-device-id="selectedDevice?.id || selectedDeviceSummary?.id || null"
          @select="onDirectorySelect"
          @device-select="onDirectoryDeviceSelect"
          @view-change="onDirectoryViewChange"
          @tree-loaded="onDirectoryTreeLoaded"
        />
      </aside>

      <main class="content-pane" :class="{ 'content-pane-map': viewMode === 'map' }">
        <header v-if="selectedDeviceContext" class="channel-workspace-header has-device">
          <div class="workspace-context-icon"><RadioTower :size="22" /></div>
          <div class="workspace-context-copy">
            <span class="workspace-eyebrow">当前设备</span>
            <div class="workspace-context-title">
              <h2>{{ displayName(selectedDeviceContext) }}</h2>
              <span class="status-pill" :class="{ online: selectedDeviceContext.online }"
                ><span class="status-dot"></span>{{ deviceStatusLabel(selectedDeviceContext.online) }}</span
              >
            </div>
            <p>
              {{
                `${selectedDeviceContext.deviceId}${selectedDevice ? ` · ${endpointText(selectedDevice)}` : " · 正在读取设备信息"}`
              }}
            </p>
          </div>
          <div v-if="selectedDevice" class="device-context-actions" role="group" aria-label="设备操作">
            <a-button size="small" class="device-action tone-view" @click="openDevice(selectedDevice)"
              ><Eye :size="14" />设备详情</a-button
            >
            <a-button
              size="small"
              class="device-action tone-view"
              :disabled="!canRefreshCatalog || !selectedDevice.online"
              :loading="refreshingCatalog[selectedDevice.id]"
              @click="handleRefreshDeviceCatalog(selectedDevice)"
              ><RefreshCcw :size="14" />刷新目录</a-button
            >
            <a-button
              size="small"
              class="device-action tone-view"
              :disabled="!canViewTraffic"
              @click="openStatusEvents(selectedDevice)"
              ><Activity :size="14" />运行监控</a-button
            >
            <a-dropdown trigger="click" position="br" :popup-max-height="false">
              <a-button size="small" class="device-action tone-config" aria-label="设备更多操作"
                ><MoreHorizontal :size="14" />更多</a-button
              >
              <template #content>
                <a-doption :disabled="!canManageSubscriptions" @click="openSubscriptionManager(selectedDevice)"
                  >订阅管理</a-doption
                >
                <a-doption :disabled="!canEditDevice" @click="openEditDeviceModal(selectedDevice)">编辑设备</a-doption>
                <a-doption :disabled="!canUpgradeDevice || !canViewMaintenance" @click="openDeviceUpgrade(selectedDevice)"
                  >固件升级</a-doption
                >
                <a-doption :disabled="!canRebootDevice || !canViewMaintenance" @click="openDeviceReboot(selectedDevice)"
                  >设备重启</a-doption
                >
                <a-doption :disabled="!canViewMaintenance" @click="openMaintenanceRecords(selectedDevice)">维护记录</a-doption>
                <a-doption
                  :disabled="deleting || !canDeleteDevice"
                  :style="{ color: 'var(--uvp-danger)' }"
                  @click="handleDeleteDevice(selectedDevice)"
                  >删除设备</a-doption
                >
              </template>
            </a-dropdown>
          </div>
        </header>
        <button
          v-if="selectedDevice && maintenanceActivity.label(selectedDevice.id)"
          class="maintenance-task-link"
          type="button"
          :disabled="!canViewMaintenance"
          @click="openKnownMaintenance(selectedDevice)"
        >
          {{ maintenanceActivity.label(selectedDevice.id) }} · 查看
        </button>
        <div class="filter-chips">
          <button
            v-if="deviceIdFilter && deviceListReturnSnapshot && assetKind === 'channel'"
            class="filter-chip device-drilldown-chip"
            type="button"
            aria-label="返回设备列表"
            @click="returnToDeviceList"
          >
            <span>所属设备: {{ selectedDeviceContext ? displayName(selectedDeviceContext) : deviceIdFilter }}</span>
            <ArrowLeft :size="12" aria-hidden="true" />
          </button>
          <span v-else-if="deviceIdFilter" class="filter-chip">
            <span>所属设备: {{ selectedDeviceContext ? displayName(selectedDeviceContext) : deviceIdFilter }}</span>
            <button class="close" type="button" aria-label="清除所属设备筛选" @click="clearDeviceFilter"><X :size="12" /></button>
          </span>
          <span v-if="statusFilter" class="filter-chip"
            >状态: {{ deviceStatusLabel(statusFilter === "online") }}
            <button class="close" @click="statusFilter = undefined">×</button></span
          >
          <span v-if="selectedDirectory" class="filter-chip"
            >目录: {{ selectedDirectory.name }} <button class="close" @click="clearDirectorySelection()">×</button></span
          >
          <button v-if="hasFilters" class="clear-all" type="button" @click="resetFilters">清除筛选</button>
        </div>

        <div v-if="selectedCount" class="batch-bar">
          <div class="batch-info">
            <strong>{{ selectedCount }}</strong> 项已选
          </div>
          <div class="batch-ops">
            <button
              class="btn-danger"
              type="button"
              :disabled="deleting || (assetKind === 'device' ? !canDeleteDevice : !canDeleteChannel)"
              @click="handleBatchDelete"
            >
              批量删除
            </button>
          </div>
        </div>

        <div v-if="viewMode === 'list'" class="view-body table-view">
          <a-table
            v-if="assetKind === 'channel'"
            v-model:selected-keys="selectedRowKeys"
            :data="channels"
            :loading="rowsLoading"
            :pagination="tablePagination"
            row-key="id"
            :scroll="{ x: 2340, y: '85%' }"
            :row-selection="{ type: 'checkbox', showCheckedAll: true }"
            class="uvp-data-table"
            @page-change="onPageChange"
            @page-size-change="onPageSizeChange"
            @row-dblclick="onChannelDblclick"
          >
            <template #columns>
              <a-table-column title="通道名称" :width="180">
                <template #cell="{ record }">
                  <a-tooltip :content="displayName(record)" position="top">
                    <div class="text-ellipsis">{{ displayName(record) }}</div>
                  </a-tooltip>
                </template>
              </a-table-column>
              <a-table-column title="通道编号" :width="210">
                <template #cell="{ record }">
                  <a-tooltip :content="record.channelId" position="top">
                    <div class="text-ellipsis">{{ record.channelId }}</div>
                  </a-tooltip>
                </template>
              </a-table-column>
              <a-table-column title="所属设备" :width="210">
                <template #cell="{ record }"
                  ><a-tooltip :content="record.deviceId"
                    ><span class="mono text-ellipsis">{{ record.deviceId }}</span></a-tooltip
                  ></template
                >
              </a-table-column>
              <a-table-column title="快照" :width="120" align="center">
                <template #cell="{ record }">
                  <a-image
                    v-if="record.snapshotUrl"
                    :src="snapshotImageUrl(record)"
                    :width="100"
                    :height="60"
                    fit="cover"
                    :preview="true"
                    class="snapshot-thumb"
                  />
                  <div v-else class="thumb small list-snapshot-empty">
                    <Video :size="14" />
                    <span>暂无快照</span>
                  </div>
                </template>
              </a-table-column>
              <a-table-column title="状态" :width="130">
                <template #cell="{ record }">
                  <div class="status-cell">
                    <span class="status-inline" :class="{ online: record.status === 1 }">
                      <span class="status-dot"></span>
                      <span>{{ deviceStatusLabel(record.status === 1) }}</span>
                    </span>
                    <span v-if="isChannelPlaying(record)" class="status-inline playing">
                      <span class="status-dot"></span>
                      <span>直播中</span>
                    </span>
                  </div>
                </template>
              </a-table-column>
              <a-table-column title="摄像头类型" :width="150">
                <template #cell="{ record }">
                  <template v-if="canEditChannel">
                    <a-select
                      :model-value="String(record.ptzType || 0)"
                      size="small"
                      style="width: 130px"
                      @change="(value: string) => handlePtzTypeChange(record.id, value)"
                    >
                      <a-option v-for="opt in ptzTypeOptions" :key="opt.value" :value="opt.value">
                        {{ opt.name }}
                      </a-option>
                    </a-select>
                  </template>
                  <span v-else>{{ cameraTypeText(record.ptzType) }}</span>
                </template>
              </a-table-column>
              <a-table-column title="流传输模式" :width="150">
                <template #cell="{ record }">
                  <template v-if="canEditTransport">
                    <a-select
                      :model-value="record.streamTransport || 'UDP'"
                      size="small"
                      style="width: 130px"
                      @change="(value: string) => handleStreamTransportChange(record.id, value)"
                    >
                      <a-option value="UDP">UDP</a-option>
                      <a-option value="TCP-Active">TCP-Active</a-option>
                      <a-option value="TCP-Passive">TCP-Passive</a-option>
                    </a-select>
                  </template>
                  <span v-else>{{ streamTransportText(record.streamTransport) }}</span>
                </template>
              </a-table-column>
              <a-table-column title="音频" :width="90" align="center">
                <template #cell="{ record }">
                  <template v-if="canEditChannel">
                    <a-switch
                      :model-value="record.audioEnabled"
                      size="small"
                      checked-text="开"
                      unchecked-text="关"
                      @change="(value: boolean) => handleAudioEnabledChange(record.id, value)"
                    />
                  </template>
                  <span v-else>{{ record.audioEnabled ? "开" : "关" }}</span>
                </template>
              </a-table-column>
              <a-table-column title="按需直播" :width="110" align="center">
                <template #cell="{ record }">
                  <template v-if="canEditChannel">
                    <a-switch
                      :model-value="record.onDemandLive !== false"
                      size="small"
                      checked-text="开"
                      unchecked-text="关"
                      @change="(value: boolean) => handleOnDemandLiveChange(record.id, value)"
                    />
                  </template>
                  <span v-else>{{ record.onDemandLive !== false ? "开" : "关" }}</span>
                </template>
              </a-table-column>
              <a-table-column title="云端录像" :width="160">
                <template #cell="{ record }">
                  <div class="cloud-recording-control">
                    <template v-if="canUpdateRecording">
                      <a-switch
                        :model-value="record.cloudRecordingEnabled"
                        :loading="recordingMeta(record).loading"
                        :disabled="recordingMeta(record).loading"
                        :aria-label="`云端录像:${recordingMeta(record).label}`"
                        size="small"
                        @change="(value: boolean) => handleCloudRecordingChange(record.id, value)"
                      />
                    </template>
                    <a-tooltip :content="recordingMeta(record).tooltip || recordingMeta(record).label" position="top">
                      <span class="cloud-recording-state" :class="`tone-${recordingMeta(record).tone}`">
                        {{ recordingMeta(record).label }}
                      </span>
                    </a-tooltip>
                  </div>
                </template>
              </a-table-column>
              <a-table-column title="位置信息" :width="200">
                <template #cell="{ record }">
                  <a-tooltip :content="locationText(record)" position="top">
                    <span class="coord-cell">
                      <span class="relative text-ellipsis">{{ locationText(record) }}</span>
                      <span v-if="positionSourceText(record)" class="coord-source">{{ positionSourceText(record) }}</span>
                    </span>
                  </a-tooltip>
                </template>
              </a-table-column>
              <a-table-column title="更新 / 创建时间" :width="190">
                <template #cell="{ record }">
                  <div class="time-cell">
                    <span>{{ dateTime(record.updatedAt) }}</span>
                    <span>{{ dateTime(record.createdAt) }}</span>
                  </div>
                </template>
              </a-table-column>
              <!-- 操作列宽必须放得下「最多动作数」这一行：通道最多 7 个动作
                   (播放/录像/操作/停止/详情/编辑/删除)，单个动作 50px(4+13+3+26+4)、动作间 1px、
                   单元格左右各 16px 内边距 ⇒ 7×50+6×1+32 = 388px。
                   旧值 350 只够 6 个：任一通道「直播中」时多出「停止」，该行比列宽多出 38px，
                   会把「删除」顶出列外被表格右缘裁掉。列宽改动后同步维护 :scroll.x
                   (上面通道表的 scroll.x = Σ列宽 2090 + 勾选列 40 = 2130)。 -->
              <a-table-column title="操作" :width="400" fixed="right">
                <template #cell="{ record }">
                  <div class="uvp-table-actions">
                    <a-link
                      :disabled="!canStartPlayback"
                      class="uvp-table-action uvp-table-action--preview"
                      @click="playChannel(record)"
                    >
                      <template #icon><Play :size="13" /></template>
                      <span>播放</span>
                    </a-link>
                    <a-link
                      :disabled="!canQueryDeviceRecords"
                      class="uvp-table-action uvp-table-action--record"
                      @click="openRecordQuery(record)"
                    >
                      <template #icon><History :size="13" /></template>
                      <span>录像</span>
                    </a-link>
                    <a-link
                      :disabled="!canUseDeviceControl && !canSnapshotDevice && !canViewDeviceFacts"
                      class="uvp-table-action uvp-table-action--more"
                      @click="openChannelOperations(record)"
                    >
                      <template #icon><Joystick :size="13" /></template>
                      <span>操作</span>
                    </a-link>
                    <a-link
                      v-if="isChannelPlaying(record)"
                      class="uvp-table-action uvp-table-action--stop"
                      :loading="stoppingChannels.has(record.id)"
                      :disabled="!canStopPlayback"
                      @click="handleStopChannel(record)"
                    >
                      <template #icon><Square :size="13" /></template>
                      <span>停止</span>
                    </a-link>
                    <a-link class="uvp-table-action uvp-table-action--detail" @click="openChannel(record)">
                      <template #icon><Eye :size="13" /></template>
                      <span>详情</span>
                    </a-link>
                    <a-link
                      :disabled="!canEditChannel"
                      class="uvp-table-action uvp-table-action--edit"
                      @click="openEditChannelModal(record)"
                    >
                      <template #icon><Pencil :size="13" /></template>
                      <span>编辑</span>
                    </a-link>
                    <a-link
                      class="uvp-table-action uvp-table-action--delete"
                      :disabled="deleting || !canDeleteChannel"
                      @click="handleDeleteChannel(record)"
                    >
                      <template #icon><Trash2 :size="13" /></template>
                      <span>删除</span>
                    </a-link>
                  </div>
                </template>
              </a-table-column>
            </template>
          </a-table>
          <a-table
            v-else
            v-model:selected-keys="selectedRowKeys"
            :data="devices"
            :loading="rowsLoading"
            :pagination="tablePagination"
            row-key="id"
            :scroll="{ x: 2040, y: '85%' }"
            :row-selection="{ type: 'checkbox', showCheckedAll: true }"
            class="uvp-data-table device-data-table"
            @page-change="onPageChange"
            @page-size-change="onPageSizeChange"
            @row-dblclick="onDeviceDblclick"
          >
            <template #columns>
              <a-table-column title="设备名称" :width="170">
                <template #cell="{ record }">
                  <a-tooltip :content="deviceNameText(record)">
                    <div class="device-name" data-guide="dblclick-device">
                      <span class="pri text-ellipsis">{{ deviceNameText(record) }}</span>
                      <button
                        v-if="maintenanceActivity.label(record.id)"
                        class="maintenance-task-link"
                        type="button"
                        :disabled="!canViewMaintenance"
                        @click.stop="openKnownMaintenance(record)"
                      >
                        {{ maintenanceActivity.label(record.id) }} · 查看
                      </button>
                    </div>
                  </a-tooltip>
                </template>
              </a-table-column>
              <a-table-column title="设备编号" :width="190">
                <template #cell="{ record }">
                  <a-tooltip :content="record.deviceId"
                    ><span class="code-main text-ellipsis">{{ record.deviceId }}</span></a-tooltip
                  >
                </template>
              </a-table-column>
              <a-table-column title="厂商" :width="120">
                <template #cell="{ record }"
                  ><span class="text-ellipsis">{{ record.manufacturer || "-" }}</span></template
                >
              </a-table-column>
              <a-table-column title="状态" :width="110" align="center">
                <template #cell="{ record }">
                  <button
                    v-if="canViewTraffic"
                    class="status-inline status-trigger"
                    :class="{ online: record.online }"
                    type="button"
                    :aria-label="`查看设备${record.deviceId}运行监控`"
                    @click.stop="openStatusEvents(record)"
                  >
                    <History :size="13" aria-hidden="true" />
                    <span>{{ deviceStatusLabel(!!record.online) }}</span>
                  </button>
                  <span v-else class="status-inline status-readonly" :class="{ online: record.online }">
                    <span class="status-dot"></span>{{ deviceStatusLabel(!!record.online) }}
                  </span>
                </template>
              </a-table-column>
              <a-table-column title="通道数" :width="140">
                <template #cell="{ record }">
                  <div class="channel-progress">
                    <div class="progress-bar" :class="channelStatusClass(record)">
                      <div class="progress-fill" :style="{ width: channelPercentage(record) }"></div>
                    </div>
                    <span class="channel-text">{{ record.channelOnlineCount }}/{{ record.channelCount }}</span>
                  </div>
                </template>
              </a-table-column>
              <a-table-column title="来源地址" :width="200">
                <template #cell="{ record }"
                  ><span class="code-main mono text-ellipsis">{{ endpointText(record) }}</span></template
                >
              </a-table-column>
              <a-table-column title="型号 / 版本" :width="160">
                <template #cell="{ record }"
                  ><span class="text-ellipsis">{{ modelVersionText(record) }}</span></template
                >
              </a-table-column>
              <a-table-column title="国标版本" :width="150">
                <template #cell="{ record }">
                  <a-tooltip :content="protocolVersionTooltip(record)">
                    <span class="text-ellipsis">{{ protocolVersionText(record.effectiveVersion) }}</span>
                  </a-tooltip>
                </template>
              </a-table-column>
              <a-table-column title="注册时间" :width="170">
                <template #cell="{ record }">{{ dateTime(record.registerTime) }}</template>
              </a-table-column>
              <a-table-column title="心跳时间" :width="170">
                <template #cell="{ record }"
                  ><span :class="{ warn: !record.online }">{{ dateTime(record.keepaliveTime) }}</span></template
                >
              </a-table-column>
              <a-table-column title="操作" :width="420" fixed="right">
                <template #cell="{ record }">
                  <div class="uvp-table-actions" data-guide="row-actions">
                    <a-link class="uvp-table-action uvp-table-action--preview" @click="showDeviceChannels(record)">
                      <template #icon><Camera :size="13" /></template><span>通道</span>
                    </a-link>
                    <a-link
                      class="uvp-table-action uvp-table-action--sync"
                      :disabled="refreshingCatalog[record.id] || !record.online || !canRefreshCatalog"
                      @click="handleRefreshDeviceCatalog(record)"
                    >
                      <template #icon
                        ><Loader2 v-if="refreshingCatalog[record.id]" :size="13" class="spin" /><RefreshCcw v-else :size="13"
                      /></template>
                      <span>刷新</span>
                    </a-link>
                    <a-link
                      class="uvp-table-action uvp-table-action--subscribe"
                      :disabled="!canManageSubscriptions"
                      @click.stop="openSubscriptionManager(record)"
                    >
                      <template #icon><Bell :size="13" /></template><span>订阅</span>
                    </a-link>
                    <a-link class="uvp-table-action uvp-table-action--detail" @click="openDevice(record)">
                      <template #icon><Eye :size="13" /></template><span>详情</span>
                    </a-link>
                    <a-link
                      class="uvp-table-action uvp-table-action--edit"
                      :disabled="!canEditDevice"
                      @click="openEditDeviceModal(record)"
                    >
                      <template #icon><Pencil :size="13" /></template><span>编辑</span>
                    </a-link>
                    <a-link
                      class="uvp-table-action uvp-table-action--delete"
                      :disabled="deleting || !canDeleteDevice"
                      @click="handleDeleteDevice(record)"
                    >
                      <template #icon><Trash2 :size="13" /></template><span>删除</span>
                    </a-link>
                    <a-dropdown trigger="click" position="br">
                      <a-link class="uvp-table-action uvp-table-action--more" :disabled="!canViewMaintenance">
                        <span>维护</span><MoreHorizontal :size="13" />
                      </a-link>
                      <template #content>
                        <DeviceMaintenanceMenu
                          :can-upgrade="canUpgradeDevice"
                          :can-reboot="canRebootDevice"
                          @upgrade="openDeviceUpgrade(record)"
                          @records="openMaintenanceRecords(record)"
                          @reboot="openDeviceReboot(record)"
                        />
                      </template>
                    </a-dropdown>
                  </div>
                </template>
              </a-table-column>
            </template>
          </a-table>
        </div>

        <div v-else-if="viewMode === 'card'" class="view-body card-view">
          <a-empty
            v-if="!rowsLoading && (assetKind === 'device' ? devices.length === 0 : channels.length === 0)"
            class="card-empty"
            :description="assetKind === 'device' ? '暂无符合条件的设备' : '暂无符合条件的通道'"
          />
          <div v-else class="card-grid">
            <template v-if="assetKind === 'device'">
              <article
                v-for="item in devices"
                :key="item.id"
                class="device-card device-summary-card"
                data-guide="dblclick-device"
                @dblclick="onDeviceDblclick(item, $event)"
              >
                <div class="device-card-head">
                  <span class="device-card-icon"><Camera :size="18" /></span>
                  <div class="device-card-title">
                    <a-tooltip :content="deviceNameText(item)" position="top"
                      ><strong class="text-ellipsis">{{ deviceNameText(item) }}</strong></a-tooltip
                    >
                    <a-tooltip :content="item.deviceId" position="top"
                      ><span class="code text-ellipsis">{{ item.deviceId }}</span></a-tooltip
                    >
                  </div>
                </div>
                <a-tooltip v-if="canViewTraffic" content="运行监控" position="top">
                  <button
                    class="device-status-ribbon"
                    :class="{ online: item.online }"
                    type="button"
                    :aria-label="`查看设备${item.deviceId}运行监控`"
                    @click.stop="openStatusEvents(item)"
                  >
                    {{ deviceStatusLabel(!!item.online) }}
                  </button>
                </a-tooltip>
                <span v-else class="device-status-ribbon device-status-readonly" :class="{ online: item.online }">
                  {{ deviceStatusLabel(!!item.online) }}
                </span>
                <div class="device-card-info">
                  <div>
                    <span>设备 ID</span><strong class="mono text-ellipsis">{{ item.deviceId }}</strong>
                  </div>
                  <div>
                    <span>厂商</span><strong class="text-ellipsis">{{ item.manufacturer || "未上报" }}</strong>
                  </div>
                  <div>
                    <span>型号</span><strong class="text-ellipsis">{{ item.model || "未上报" }}</strong>
                  </div>
                  <div>
                    <span>国标版本</span><strong class="text-ellipsis">{{ protocolVersionText(item.effectiveVersion) }}</strong>
                  </div>
                  <div>
                    <span>地址</span><strong class="mono text-ellipsis">{{ endpointText(item) }}</strong>
                  </div>
                  <div class="device-card-channel">
                    <span>通道数量</span>
                    <div class="device-card-channel-value">
                      <div class="channel-progress">
                        <div class="progress-bar" :class="channelStatusClass(item)">
                          <div class="progress-fill" :style="{ width: channelPercentage(item) }"></div>
                        </div>
                        <span class="channel-text">{{ item.channelOnlineCount }}/{{ item.channelCount }}</span>
                      </div>
                    </div>
                  </div>
                  <div>
                    <span>最近心跳</span><strong :class="{ warn: !item.online }">{{ dateTime(item.keepaliveTime) }}</strong>
                  </div>
                  <div>
                    <span>注册时间</span><strong>{{ dateTime(item.registerTime) }}</strong>
                  </div>
                </div>
                <button
                  v-if="maintenanceActivity.label(item.id)"
                  class="maintenance-task-link"
                  type="button"
                  :disabled="!canViewMaintenance"
                  @click.stop="openKnownMaintenance(item)"
                >
                  {{ maintenanceActivity.label(item.id) }} · 查看
                </button>
                <div class="card-actions device-card-actions" data-guide="row-actions">
                  <a-tooltip content="查看通道" position="top"
                    ><button class="icon-btn small framed primary" type="button" @click.stop="showDeviceChannels(item)">
                      <Camera :size="13" /></button
                  ></a-tooltip>
                  <a-tooltip content="查看详情" position="top"
                    ><button class="icon-btn small framed" type="button" @click.stop="openDevice(item)"><Eye :size="13" /></button
                  ></a-tooltip>
                  <a-tooltip :content="item.online ? '刷新通道目录' : '设备离线,无法刷新'" position="top">
                    <button
                      class="icon-btn small framed info uvp-refresh-btn"
                      type="button"
                      :disabled="refreshingCatalog[item.id] || !item.online || !canRefreshCatalog"
                      @click.stop="handleRefreshDeviceCatalog(item)"
                    >
                      <Loader2 v-if="refreshingCatalog[item.id]" :size="13" class="spin" /><RefreshCcw v-else :size="13" />
                    </button>
                  </a-tooltip>
                  <a-tooltip content="订阅管理" position="top"
                    ><button
                      class="icon-btn small framed subscription"
                      type="button"
                      :disabled="!canManageSubscriptions"
                      @click.stop="openSubscriptionManager(item)"
                    >
                      <Bell :size="13" /></button
                  ></a-tooltip>
                  <a-tooltip content="编辑设备" position="top"
                    ><button
                      class="icon-btn small framed warning"
                      type="button"
                      :disabled="!canEditDevice"
                      @click.stop="openEditDeviceModal(item)"
                    >
                      <Pencil :size="13" /></button
                  ></a-tooltip>
                  <a-tooltip content="删除设备" position="top"
                    ><button
                      class="icon-btn small framed danger"
                      type="button"
                      :disabled="deleting || !canDeleteDevice"
                      @click.stop="handleDeleteDevice(item)"
                    >
                      <Trash2 :size="13" /></button
                  ></a-tooltip>
                  <a-dropdown trigger="click" position="br" :popup-max-height="false">
                    <button class="icon-btn small framed" type="button" aria-label="更多设备操作" :disabled="!canViewMaintenance">
                      <MoreHorizontal :size="13" />
                    </button>
                    <template #content>
                      <DeviceMaintenanceMenu
                        :can-upgrade="canUpgradeDevice"
                        :can-reboot="canRebootDevice"
                        @upgrade="openDeviceUpgrade(item)"
                        @records="openMaintenanceRecords(item)"
                        @reboot="openDeviceReboot(item)"
                      />
                    </template>
                  </a-dropdown>
                </div>
              </article>
            </template>
            <template v-else>
              <article
                v-for="item in channels"
                :key="item.id"
                class="device-card channel-summary-card"
                @dblclick="onChannelDblclick(item, $event)"
              >
                <div class="channel-snapshot" :class="{ offline: item.status !== 1 }">
                  <a-image
                    v-if="item.snapshotUrl"
                    :src="snapshotImageUrl(item)"
                    fit="cover"
                    :preview="true"
                    class="channel-snapshot-image"
                  />
                  <div v-else class="snapshot-empty">
                    <Video :size="30" />
                    <span>暂无快照</span>
                  </div>
                  <span v-if="isChannelPlaying(item)" class="channel-snapshot-live-badge">
                    <span class="live-dot"></span>
                    <span>直播中</span>
                  </span>
                </div>
                <div class="channel-card-body">
                  <a-tooltip :content="displayName(item)" position="top"
                    ><strong class="channel-card-title text-ellipsis">{{ displayName(item) }}</strong></a-tooltip
                  >
                  <div class="channel-card-info">
                    <div>
                      <span>通道 ID</span
                      ><a-tooltip :content="item.channelId" position="top"
                        ><strong class="mono text-ellipsis">{{ item.channelId }}</strong></a-tooltip
                      >
                    </div>
                    <div>
                      <span>所属设备</span
                      ><a-tooltip :content="item.deviceId" position="top"
                        ><strong class="mono text-ellipsis">{{ item.deviceId }}</strong></a-tooltip
                      >
                    </div>
                    <div>
                      <span>厂商 / 型号</span><strong class="text-ellipsis">{{ vendorText(item) }}</strong>
                    </div>
                    <div>
                      <span>位置</span><strong>{{ locationText(item) }}</strong>
                    </div>
                    <div>
                      <span>摄像头类型</span>
                      <template v-if="canEditChannel">
                        <a-select
                          :model-value="String(item.ptzType || 0)"
                          size="small"
                          class="channel-card-inline-select"
                          @click.stop
                          @dblclick.stop
                          @change="(value: string) => handlePtzTypeChange(item.id, value)"
                        >
                          <a-option v-for="opt in ptzTypeOptions" :key="opt.value" :value="opt.value">
                            {{ opt.name }}
                          </a-option>
                        </a-select>
                      </template>
                      <strong v-else>{{ cameraTypeText(item.ptzType) }}</strong>
                    </div>
                    <div>
                      <span>流传输模式</span>
                      <template v-if="canEditTransport">
                        <a-select
                          :model-value="item.streamTransport || 'UDP'"
                          size="small"
                          class="channel-card-inline-select"
                          @click.stop
                          @dblclick.stop
                          @change="(value: string) => handleStreamTransportChange(item.id, value)"
                        >
                          <a-option value="UDP">UDP</a-option>
                          <a-option value="TCP-Active">TCP-Active</a-option>
                          <a-option value="TCP-Passive">TCP-Passive</a-option>
                        </a-select>
                      </template>
                      <strong v-else>{{ streamTransportText(item.streamTransport) }}</strong>
                    </div>
                  </div>
                  <div class="channel-card-switches">
                    <div class="switch-item">
                      <span>音频</span>
                      <template v-if="canEditChannel">
                        <a-switch
                          :model-value="item.audioEnabled"
                          :aria-label="`音频:${item.audioEnabled ? '开' : '关'}`"
                          size="small"
                          checked-text="开"
                          unchecked-text="关"
                          @click.stop
                          @dblclick.stop
                          @change="(value: boolean) => handleAudioEnabledChange(item.id, value)"
                        />
                      </template>
                      <span v-else>{{ item.audioEnabled ? "开" : "关" }}</span>
                    </div>
                    <div class="switch-item">
                      <span>按需直播</span>
                      <template v-if="canEditChannel">
                        <a-switch
                          :model-value="item.onDemandLive !== false"
                          :aria-label="`按需直播:${item.onDemandLive !== false ? '开' : '关'}`"
                          size="small"
                          checked-text="开"
                          unchecked-text="关"
                          @click.stop
                          @dblclick.stop
                          @change="(value: boolean) => handleOnDemandLiveChange(item.id, value)"
                        />
                      </template>
                      <span v-else>{{ item.onDemandLive !== false ? "开" : "关" }}</span>
                    </div>
                    <div class="switch-item">
                      <a-tooltip :content="recordingMeta(item).tooltip || recordingMeta(item).label" position="top">
                        <span :class="`tone-${recordingMeta(item).tone}`">云端录像</span>
                      </a-tooltip>
                      <template v-if="canUpdateRecording">
                        <a-switch
                          :model-value="item.cloudRecordingEnabled"
                          :loading="recordingMeta(item).loading"
                          :disabled="recordingMeta(item).loading"
                          :aria-label="`云端录像:${recordingMeta(item).label}`"
                          size="small"
                          @click.stop
                          @dblclick.stop
                          @change="(value: boolean) => handleCloudRecordingChange(item.id, value)"
                        />
                      </template>
                    </div>
                  </div>
                  <div class="card-actions channel-card-actions">
                    <span class="channel-card-status" :class="{ online: item.status === 1 }">{{
                      deviceStatusLabel(item.status === 1)
                    }}</span>
                    <a-tooltip content="点播" position="top">
                      <button
                        class="icon-btn small framed primary"
                        type="button"
                        aria-label="点播"
                        :disabled="!canStartPlayback"
                        @click.stop="playChannel(item)"
                      >
                        <Play :size="13" />
                      </button>
                    </a-tooltip>
                    <a-tooltip content="查询设备录像" position="top">
                      <button
                        class="icon-btn small framed record-query-entry"
                        type="button"
                        aria-label="查询设备录像"
                        :disabled="!canQueryDeviceRecords"
                        @click.stop="openRecordQuery(item)"
                        @dblclick.stop
                      >
                        <History :size="13" />
                      </button>
                    </a-tooltip>
                    <a-tooltip content="通道操作" position="top">
                      <button
                        class="icon-btn small framed"
                        type="button"
                        aria-label="通道操作"
                        :disabled="!canUseDeviceControl && !canSnapshotDevice && !canViewDeviceFacts"
                        @click.stop="openChannelOperations(item)"
                      >
                        <Joystick :size="13" />
                      </button>
                    </a-tooltip>
                    <a-tooltip v-if="isChannelPlaying(item)" content="强制停止当前直播(会断开其他观看者)" position="top">
                      <button
                        class="icon-btn small framed stop"
                        type="button"
                        aria-label="停止当前直播"
                        :disabled="stoppingChannels.has(item.id) || !canStopPlayback"
                        @click.stop="handleStopChannel(item)"
                      >
                        <Square :size="13" />
                      </button>
                    </a-tooltip>
                    <a-tooltip content="编辑通道" position="top"
                      ><button
                        class="icon-btn small framed warning"
                        type="button"
                        aria-label="编辑通道"
                        :disabled="!canEditChannel"
                        @click.stop="openEditChannelModal(item)"
                      >
                        <Pencil :size="13" /></button
                    ></a-tooltip>
                    <a-tooltip content="删除通道" position="top"
                      ><button
                        class="icon-btn small framed danger"
                        type="button"
                        aria-label="删除通道"
                        :disabled="deleting || !canDeleteChannel"
                        @click.stop="handleDeleteChannel(item)"
                      >
                        <Trash2 :size="13" /></button
                    ></a-tooltip>
                  </div>
                </div>
              </article>
            </template>
          </div>
          <a-pagination
            v-if="total > 0"
            class="card-pagination uvp-pagination-bar"
            :current="page"
            :page-size="pageSize"
            :total="total"
            :page-size-options="[9, 12, 24, 48]"
            show-page-size
            show-total
            show-jumper
            @change="onPageChange"
            @page-size-change="onPageSizeChange"
          />
        </div>

        <div v-else class="view-body map-view" :class="{ 'map-dark': darkMode }" :data-basemap="mapBaseStyle">
          <div ref="mapContainer" class="map-container"></div>
          <div class="map-floating-actions">
            <a-input
              v-model="mapSearchQuery"
              class="map-search-box"
              allow-clear
              placeholder="搜索地点 / 道路 / 建筑 / 摄像头"
              @press-enter="searchMapCameras"
            >
              <template #prefix><Search :size="15" /></template>
            </a-input>
            <span class="map-camera-count">摄像头：{{ total }}</span>
            <div class="map-toolbar-spacer"></div>
            <a-button class="map-action map-action--refresh" @click="loadMapData">刷新</a-button>
            <a-button class="map-action map-action--reset" @click="resetMapView">复位</a-button>
            <a-button class="map-action map-action--fit" type="primary" @click="fitMapToData">适应视野</a-button>
            <!-- 平台默认中心（执法仪场景每次进来先看到辖区那一片）：点进去选地图上的点，再确认保存。 -->
            <a-button
              v-if="canEditDevice"
              class="map-action map-action--center"
              :disabled="mapCenterSaving || mapCenterPicking"
              title="在地图上点选一个位置，设为平台默认视野"
              @click="startMapCenterPicking"
            >
              设为默认中心
            </a-button>
          </div>
          <!-- 选点态提示条：告诉用户「刚才是进入了一个模式」，并把确认/取消放在眼前。
               ⛔ 不能用 a-modal 之类的遮罩做这件事 —— 它会把地图挡住，而这里恰恰要求用户去点地图。 -->
          <div v-if="mapCenterPicking" class="map-center-picker" role="group" aria-label="选择平台默认中心">
            <span class="map-center-picker__hint">
              <Crosshair :size="14" />
              在地图上点击选择中心点
            </span>
            <code v-if="mapCenterPickText" class="map-center-picker__coord">{{ mapCenterPickText }}</code>
            <span class="map-center-picker__keys">Enter 保存 · Esc 取消</span>
            <a-button
              class="map-center-picker__confirm"
              type="primary"
              size="mini"
              :loading="mapCenterSaving"
              :disabled="!mapCenterPick"
              @click="confirmMapCenterPick"
            >
              保存为中心
            </a-button>
            <a-button class="map-center-picker__cancel" size="mini" :disabled="mapCenterSaving" @click="stopMapCenterPicking">
              取消
            </a-button>
          </div>
          <div class="map-position-readout" aria-live="polite">
            <span>{{ mapCursorLongitude.toFixed(5) }}, {{ mapCursorLatitude.toFixed(5) }}</span>
            <strong>Z{{ mapZoom }}</strong>
          </div>
          <MapTrajectoryControls
            :visible="trajectoryVisible"
            :channel-name="trajectoryTarget?.name || ''"
            :loading="trajectoryLoading"
            :points="trajectoryPoints"
            :distance-meters="trajectoryDistance"
            :truncated="trajectoryTruncated"
            :empty-text="trajectoryEmptyText"
            :playing="trajectoryPlaying"
            :progress="trajectoryProgress"
            :speed="trajectorySpeed"
            :current-time="trajectoryCurrentTime"
            :can-play="canStartPlayback"
            @close="closeTrajectory"
            @search="loadTrajectory"
            @toggle="toggleTrajectoryPlayback"
            @update:progress="onTrajectoryProgressInput"
            @update:speed="onTrajectorySpeedChange"
          />
          <div v-if="mapError" class="map-state map-state-error"><Info :size="16" /> {{ mapError }}</div>
          <div v-else-if="!mapReady || !mapFirstRender" class="map-state"><Loader2 :size="16" class="spin" /> 正在加载地图</div>
        </div>
      </main>
    </div>

    <!-- 地图上的实时视频小窗。
         形态刻意与「点通道图标弹出的功能卡片」一致（同一套头/说明/动作按钮），因为它们是
         同一块地图上的同一族浮层；只是这块多了一块画面，并且多一个去控制台的入口。
         ⛔ 内容必须 Teleport 进 popup：MapLibre 只认 DOM 节点，把 PlayWindow 直接塞进
            setDOMContent 的结果是「组件成了无主节点」——props 不会更新、也不会走 unmount。 -->
    <Teleport v-if="videoPopupHost" :to="videoPopupHost">
      <div class="map-video-card">
        <div class="map-video-card__head">
          <span class="map-channel-card__dot" :class="{ online: videoTarget?.status === 1 }"></span>
          <span class="map-channel-card__name">{{ videoTarget?.name || "" }}</span>
        </div>
        <div class="map-video-card__stage">
          <div v-if="videoLoading" class="map-video-card__state">
            <Loader2 :size="16" class="spin" />
            正在点播…
          </div>
          <PlayWindow v-else :url="videoPlaying?.httpFlvUrl || ''" :has-audio="false" @error="onMapVideoError" />
        </div>
        <div class="map-video-card__meta">
          <span>{{ videoTarget?.status === 1 ? "在线" : "离线" }}</span>
          <span>{{ videoTarget?.channelId }}</span>
          <span v-if="videoPlaying">流 {{ videoPlaying.streamId }}</span>
        </div>
        <div class="map-video-card__actions">
          <button
            type="button"
            class="map-channel-card__action map-channel-card__action--console"
            :disabled="!canStartPlayback || videoTarget?.status !== 1"
            :title="!canStartPlayback ? '无播放权限' : videoTarget?.status !== 1 ? '通道离线，无法播放' : ''"
            @click="openPlaybackConsoleFromMap"
          >
            播放控制台
          </button>
          <button type="button" class="map-channel-card__action map-channel-card__action--stop" @click="closeVideoCard">
            停止播放
          </button>
        </div>
      </div>
    </Teleport>

    <!-- 通道位置踩点：固定枪机不会上报经纬度，这是给它们补坐标的唯一顺手入口。 -->
    <ChannelLocationPicker
      v-model:visible="locationPickerVisible"
      :channel-name="locationPickerTarget?.name || ''"
      :style-url="currentMapStyleUrl()"
      :initial-longitude="locationPickerTarget?.longitude || 0"
      :initial-latitude="locationPickerTarget?.latitude || 0"
      @confirm="handleLocationConfirm"
    />

    <a-drawer
      v-model:visible="drawerVisible"
      class="uvp-system-drawer"
      body-class="uvp-system-dialog__body"
      :width="drawerWidth"
      :footer="false"
      unmount-on-close
    >
      <template #title>
        <span v-if="drawerTarget?.type === 'channel'">通道详情</span>
        <span v-else-if="drawerTarget?.type === 'device'">设备详情</span>
        <span v-else>目录节点</span>
      </template>
      <a-spin :loading="drawerLoading">
        <div v-if="drawerTarget?.type === 'channel' && channelDetail" class="drawer-body">
          <div class="drawer-headline">
            <span class="drawer-icon"><Video :size="18" /></span>
            <div>
              <h3>{{ displayName(channelDetail) }}</h3>
              <p>{{ channelDetail.channelId }}</p>
            </div>
            <span class="status-pill" :class="{ online: channelDetail.status === 1 }">{{
              deviceStatusLabel(channelDetail.status === 1)
            }}</span>
          </div>
          <div class="kv-grid">
            <span>所属设备</span><strong>{{ channelDetail.deviceId }}</strong> <span>厂商型号</span
            ><strong>{{ vendorText(channelDetail) }}</strong> <span>摄像头类型</span
            ><strong>{{ cameraTypeText(channelDetail.ptzType) }}</strong> <span>父级通道</span
            ><strong>{{ channelDetail.parentId || "无" }}</strong> <span>坐标</span
            ><strong
              >{{ locationText(channelDetail)
              }}<em v-if="positionSourceText(channelDetail)" class="coord-source">{{
                positionSourceText(channelDetail)
              }}</em></strong
            >
            <span>坐标更新时间</span
            ><strong>{{ channelDetail.positionUpdatedAt ? dateTime(channelDetail.positionUpdatedAt) : "无" }}</strong>
            <span>流传输模式</span><strong>{{ streamTransportText(channelDetail.streamTransport) }}</strong>
            <span>按需直播</span>
            <template v-if="canEditChannel">
              <a-switch
                :model-value="channelDetail.onDemandLive !== false"
                size="small"
                checked-text="开"
                unchecked-text="关"
                @change="(value: boolean) => channelDetail && handleOnDemandLiveChange(channelDetail.id, value)"
              />
            </template>
            <strong v-else>{{ channelDetail.onDemandLive !== false ? "开" : "关" }}</strong>
            <span>当前流</span><strong>{{ channelDetail.streamId || "未播放" }}</strong>
          </div>
          <div v-if="channelMounts.length" class="mount-list">
            <div v-for="mount in channelMounts" :key="mount.id" class="mount-item">
              <Layers :size="14" />
              <div>
                <strong>{{ mount.displayName || mount.parentName }}</strong>
                <span>{{ mount.parentPath || "未记录路径" }}</span>
              </div>
            </div>
          </div>
          <div v-if="timeline.length" class="timeline-strip">
            <span v-for="slot in timeline" :key="slot.start" :class="{ online: slot.status === 'online' }"></span>
          </div>
          <section class="info-group catalog-attr-group">
            <div class="group-label">
              设备上报属性
              <span class="attr-shape">{{ channelCatalogShapeText }}</span>
            </div>
            <div class="field-grid">
              <template v-for="row in channelAttributeRows" :key="row.key">
                <span class="k">
                  {{ row.label }}
                  <em v-if="row.scope !== 'both'" class="attr-scope">{{ channelAttributeScopeText(row.scope) }}</em>
                </span>
                <span class="v" :class="{ unreported: !row.reported }">{{ row.value }}</span>
              </template>
            </div>
          </section>
          <section class="info-group meta-group">
            <div class="group-label">元数据</div>
            <div class="field-grid">
              <span class="k">创建时间</span>
              <span class="v mono">{{ dateTime(channelDetail.createdAt) }}</span>
              <span class="k">更新时间</span>
              <span class="v mono">{{ dateTime(channelDetail.updatedAt) }}</span>
            </div>
          </section>
          <div class="drawer-foot">
            <a-button :disabled="!canEditChannel" type="primary" @click="openEditChannelModal(channelDetail)">
              <template #icon><Pencil :size="14" /></template>
              <template #default>编辑通道</template>
            </a-button>
            <a-button @click="copyText(channelDetail.channelId)">
              <template #icon><Copy :size="14" /></template>
              <template #default>复制编码</template>
            </a-button>
          </div>
        </div>
        <div v-else-if="drawerTarget?.type === 'device' && deviceDetail" class="drawer-body">
          <div class="device-idbar">
            <span class="idbar-icon"><RadioTower :size="18" /></span>
            <div class="idbar-main">
              <h3>{{ displayName(deviceDetail) }}</h3>
              <p v-if="deviceDetail.alias && deviceDetail.name && deviceDetail.alias !== deviceDetail.name" class="idbar-alt">
                <span class="muted">设备上报:</span> {{ deviceDetail.name }}
              </p>
              <p class="idbar-code mono">
                {{ deviceDetail.deviceId }}
              </p>
              <p class="idbar-sum">{{ vendorText(deviceDetail) }} · {{ endpointText(deviceDetail) }}</p>
            </div>
            <div class="idbar-status">
              <span class="status-pill" :class="{ online: deviceDetail.online }">
                <span class="status-dot" />
                {{ deviceStatusLabel(!!deviceDetail.online) }}
              </span>
            </div>
          </div>

          <div class="device-seg device-readonly" data-seg-panel="overview">
            <div class="seg-sect">
              <div class="seg-head">
                <b>状态</b>
                <span class="seg-right">
                  <!-- ⛔ 用 `a-badge`（`:status` + `:text`）而不是自造"圆点 + 文案"：
                          本仓「在线状态」一律这么写（`RecordingPlansPanel.vue:65`、
                          `ChannelAssignmentDialog.vue:165`），点色跟着框架的语义色走。 -->
                  <a-badge
                    class="seg-diag"
                    :status="deviceDetail.online ? 'success' : 'normal'"
                    :text="deviceDetail.online ? '已注册' : '未注册 / 已离线'"
                  />
                  <span v-if="deviceDetail.keepaliveTime" class="seg-cnt"
                    >最近心跳 {{ relTime(deviceDetail.keepaliveTime) }}</span
                  >
                </span>
              </div>
              <div class="seg-rows">
                <span class="k">注册</span>
                <span class="v">
                  {{ deviceDetail.online ? "已注册" : "未注册 / 已离线" }}
                  <span v-if="deviceDetail.registerTime" class="sub mono">{{ dateTime(deviceDetail.registerTime) }}</span>
                  <span v-if="deviceDetail.online && deviceDetail.registerExpireAt" class="sub"
                    >· 注册过期 {{ dateTime(deviceDetail.registerExpireAt) }}</span
                  >
                </span>
                <span class="k">心跳</span>
                <span class="v">
                  间隔 {{ keepaliveIntervalText(deviceDetail.keepaliveInterval) }}
                  <span v-if="deviceDetail.keepaliveTime" class="sub mono">{{ dateTime(deviceDetail.keepaliveTime) }}</span>
                </span>
                <template v-if="!deviceDetail.online && deviceDetail.offlineAt">
                  <span class="k">离线</span>
                  <span class="v mono"
                    >{{ dateTime(deviceDetail.offlineAt)
                    }}<span class="sub">（{{ relTime(deviceDetail.offlineAt) }}）</span></span
                  >
                </template>
                <span class="k">传输</span>
                <span class="v">
                  {{ transportText(deviceDetail.transport) }}
                  <span class="sub"
                    >{{ protocolVersionText(deviceDetail.effectiveVersion) }} ·
                    {{ protocolSourceText(deviceDetail.effectiveVersionSource) }}</span
                  >
                  <span v-if="deviceDetail.reportedVersion" class="sub mono">· X-GB-Ver {{ deviceDetail.reportedVersion }}</span>
                </span>
              </div>
            </div>

            <div class="seg-sect">
              <div class="seg-head">
                <b>通道</b>
                <span class="seg-right">
                  <span class="seg-cnt">{{ deviceDetail.channelOnlineCount }} / {{ deviceDetail.channelCount }} 在线</span>
                </span>
              </div>
              <div class="seg-rows">
                <span class="k">在线率</span>
                <span class="v seg-rate-row">
                  <!-- ⛔ 用 `a-progress` 而不是自造"div + 渐变 i"进度条：本仓进度一律是它
                          （`RecordingDownloadCenter.vue:36`、`upload/image-upload.vue:26`），
                          颜色也才会跟着主题的 primary 走。
                          ⚠️ Arco 的 `percent` 是 **0~1**，不是百分数（本仓既有用法同样 /100）。 -->
                  <span class="seg-rate">
                    <a-progress
                      :percent="onlineRatePercent(deviceDetail.onlineRate) / 100"
                      status="success"
                      size="small"
                      :show-text="false"
                    />
                  </span>
                  <span class="sub">{{ onlineRatePercent(deviceDetail.onlineRate) }}%</span>
                </span>
              </div>
            </div>

            <div class="seg-sect">
              <div class="seg-head">
                <b>下载能力</b>
                <span class="seg-right">
                  <span class="seg-cnt">
                    {{ deviceDetail.videoCapability?.observedChannelCount || 0 }} /
                    {{ deviceDetail.videoCapability?.channelCount ?? deviceDetail.channelCount }} 通道已获取
                  </span>
                </span>
              </div>
              <div class="seg-rows">
                <span class="k">下载速度</span>
                <span class="v">
                  <strong v-if="deviceDetail.videoCapability?.maxDownloadSpeed">
                    {{ deviceDetail.videoCapability.maxDownloadSpeed }} 倍
                  </strong>
                  <span v-else class="sub">未获取</span>
                  <span v-if="deviceDetail.videoCapability?.downloadSpeeds?.length" class="sub">
                    支持 {{ deviceDetail.videoCapability.downloadSpeeds.join(" / ") }} 倍
                  </span>
                </span>
                <span class="k">分辨率</span>
                <span class="v">
                  {{
                    deviceDetail.videoCapability?.resolutions?.length
                      ? deviceDetail.videoCapability.resolutions.join(" / ")
                      : "未获取"
                  }}
                </span>
                <span class="k">最近获取</span>
                <span class="v mono">
                  {{ deviceDetail.videoCapability?.observedAt ? dateTime(deviceDetail.videoCapability.observedAt) : "未获取" }}
                </span>
              </div>
            </div>

            <div v-if="canManageSubscriptions" class="seg-sect">
              <div class="seg-head">
                <b>订阅状态</b>
              </div>
              <div class="seg-rows">
                <span class="k">状态</span>
                <span class="v">
                  <span
                    v-for="subscription in deviceSubscriptionSummary"
                    :key="subscription.kind"
                    class="subscription-chip"
                    :class="`status-${subscription.status}`"
                  >
                    <span>{{ subscription.label }}</span>
                    <strong>{{ subscriptionStatusText(subscription.status) }}</strong>
                  </span>
                </span>
              </div>
            </div>

            <div class="seg-sect">
              <div class="seg-head"><b>接入</b></div>
              <div class="seg-rows">
                <span class="k">设备地址</span>
                <span class="v mono">{{ endpointText(deviceDetail) }}</span>
                <span class="k">归属组织</span>
                <span class="v">{{ deviceDetail.ownerDeptName || "未分配" }}</span>
                <span class="k">节点策略</span>
                <span class="v">{{ deviceDetail.zlmNodeId ? `节点 ${deviceDetail.zlmNodeId}` : "自动调度" }}</span>
                <span class="k">厂商</span>
                <span class="v">
                  {{ vendorText(deviceDetail) }}
                  <span v-if="deviceDetail.firmware" class="sub mono">{{ deviceDetail.firmware }}</span>
                </span>
                <span class="k">记录</span>
                <span class="v"
                  ><span class="sub plain"
                    >创建 {{ dateTime(deviceDetail.createdAt) }} · 更新 {{ dateTime(deviceDetail.updatedAt) }}</span
                  ></span
                >
              </div>
            </div>

            <div v-if="deviceDetail.basicParam" class="seg-sect">
              <div class="seg-head"><b>基础参数</b></div>
              <div class="seg-rows">
                <span class="k">设备名称</span>
                <span class="v">{{ deviceDetail.basicParam.name || "未上报" }}</span>
                <span class="k">注册有效期</span>
                <span class="v">{{
                  deviceDetail.basicParam.expiration ? `${deviceDetail.basicParam.expiration} 秒` : "未上报"
                }}</span>
                <span class="k">心跳间隔</span>
                <span class="v">{{
                  deviceDetail.basicParam.heartBeatInterval ? `${deviceDetail.basicParam.heartBeatInterval} 秒` : "未上报"
                }}</span>
                <span class="k">心跳次数</span>
                <span class="v">{{ deviceDetail.basicParam.heartBeatCount ?? "未上报" }}</span>
              </div>
            </div>
          </div>
        </div>
      </a-spin>
    </a-drawer>

    <a-drawer
      v-model:visible="channelOperationVisible"
      class="uvp-system-drawer"
      body-class="uvp-system-dialog__body"
      width="min(720px, 94vw)"
      title="通道操作"
      :footer="false"
    >
      <div v-if="channelOperationTarget" class="channel-operation-entry">
        <div class="channel-operation-context">
          <strong>{{ displayName(channelOperationTarget) }}</strong>
          <span>{{ channelOperationTarget.channelId }} · {{ channelOperationTarget.deviceId }}</span>
        </div>
        <a-radio-group v-model="channelOperationTab" type="button" class="channel-operation-tabs">
          <a-radio :disabled="!canUseDeviceControl" value="control">即时控制</a-radio>
          <a-radio :disabled="!canSnapshotDevice" value="snapshot">抓拍</a-radio>
          <a-radio :disabled="!canViewDeviceFacts" value="status">设备状态</a-radio>
          <a-radio :disabled="!canViewDeviceFacts" value="storage">存储卡</a-radio>
        </a-radio-group>
        <DeviceControlPanel
          v-if="channelOperationTab === 'control' && canUseDeviceControl"
          v-model:channel-id="channelOperationTarget.id"
          :channel-options="channelOperationOptions"
          :active="channelOperationVisible"
          :show-picker="false"
          :can-control="canControlDevice"
        />
        <SnapshotConfigPanel
          v-else-if="channelOperationTab === 'snapshot' && canSnapshotDevice"
          v-model:channel-id="channelOperationTarget.id"
          :channel-options="channelOperationOptions"
          :show-picker="false"
          :can-snapshot="canSnapshotDevice"
        />
        <DeviceStatusFactsPanel
          v-else-if="channelOperationTab === 'status' && canViewDeviceFacts"
          v-model:channel-id="channelOperationTarget.id"
          :channel-options="channelOperationOptions"
          :active="channelOperationVisible"
          :show-picker="false"
          :can-view="canViewDeviceFacts"
        />
        <StorageCardStatusPanel
          v-else-if="channelOperationTab === 'storage' && canViewDeviceFacts"
          v-model:channel-id="channelOperationTarget.id"
          :channel-options="channelOperationOptions"
          :active="channelOperationVisible"
          :show-picker="false"
          :can-view="canViewDeviceFacts"
          :can-format="canFormatStorageCard"
          :device-label="channelOperationTarget.deviceId"
        />
      </div>
    </a-drawer>

    <DeviceRebootDialog
      v-if="canViewMaintenance"
      v-model:visible="rebootVisible"
      :device="maintenanceDevice"
      :can-reboot="canRebootDevice"
      :result="currentRebootResult"
      :blocked-reason="maintenanceBlockReason('reboot')"
      @device-updated="onMaintenanceDeviceUpdated"
      @operation-updated="onRebootOperation"
      @submission-uncertain="onMaintenanceUncertain('reboot')"
      @view-records="viewMaintenanceRecord('reboot', $event)"
    />
    <DeviceFirmwareUpgradeDialog
      v-if="canViewMaintenance"
      v-model:visible="upgradeVisible"
      :device="maintenanceDevice"
      :can-upgrade="canUpgradeDevice"
      :reboot-busy="maintenanceDevice ? isRebootPending(maintenanceActivity.devices[maintenanceDevice.id]?.reboot) : false"
      :blocked-reason="maintenanceBlockReason('upgrade')"
      @firmware-updated="onMaintenanceFirmwareUpdated"
      @operation-updated="onUpgradeOperation"
      @submission-uncertain="onMaintenanceUncertain('upgrade')"
      @view-records="viewMaintenanceRecord('upgrade', $event)"
    />
    <DeviceMaintenanceRecordsDialog
      v-if="canViewMaintenance"
      v-model:visible="recordsVisible"
      :device="maintenanceDevice"
      :initial-type="recordsType"
      :operation-id="recordsOperationId"
    />

    <SubscriptionDialog
      v-if="canManageSubscriptions"
      v-model:visible="subscriptionDialogVisible"
      :device-id="subscriptionDevice?.id"
      :device-name="subscriptionDevice ? displayName(subscriptionDevice) : ''"
      @changed="handleSubscriptionChanged"
    />

    <a-modal
      v-if="canViewTraffic"
      v-model:visible="statusEventVisible"
      modal-class="uvp-system-dialog status-event-dialog"
      title="设备运行监控"
      :width="960"
      :footer="false"
      unmount-on-close
      @close="closeStatusEvents"
    >
      <div v-if="statusEventDevice" class="status-event-body">
        <header class="status-event-summary">
          <div class="status-event-device">
            <span class="summary-icon"><Activity :size="18" /></span>
            <div>
              <span class="runtime-eyebrow">设备运行概览</span>
              <strong>{{ displayName(statusEventDevice) }}</strong>
              <span class="mono">{{ statusEventDevice.deviceId }}</span>
            </div>
          </div>
          <span class="status-pill status-event-current-status" :class="{ online: statusEventDevice.online }">
            <span class="status-dot"></span>
            {{ deviceStatusLabel(!!statusEventDevice.online) }}
          </span>
        </header>
        <section class="runtime-overview" aria-label="设备运行概览">
          <div class="runtime-overview-card">
            <span>设备状态</span>
            <strong :class="{ online: statusEventDevice.online }">{{
              statusEventDevice.online ? "运行正常" : "当前离线"
            }}</strong>
          </div>
          <div class="runtime-overview-card">
            <span>在线通道</span>
            <strong>{{ statusEventDevice.channelOnlineCount }}/{{ statusEventDevice.channelCount }}</strong>
          </div>
          <div class="runtime-overview-card">
            <span>最近心跳</span>
            <strong>{{ dateTime(statusEventDevice.keepaliveTime) }}</strong>
          </div>
          <div class="runtime-overview-card">
            <span>来源地址</span>
            <strong class="mono">{{ endpointText(statusEventDevice) }}</strong>
          </div>
        </section>
        <a-tabs v-model:active-key="runtimeActiveTab" class="runtime-tabs">
          <a-tab-pane key="status">
            <template #title
              ><span class="runtime-tab-title"><History :size="15" />状态轨迹</span></template
            >
            <div class="status-event-section-head">
              <div>
                <strong>设备上下线记录</strong>
                <span>最近注册 {{ dateTime(statusEventDevice.registerTime) }}</span>
              </div>
              <span>{{ statusEventTotal }} 条</span>
            </div>
            <div ref="statusEventScroll" class="status-event-scroll" @scroll.passive="onStatusEventScroll">
              <a-spin :loading="statusEventLoading" class="status-event-content">
                <div v-if="statusEventError" class="status-event-state error" role="alert">
                  <strong>上下线记录加载失败</strong>
                  <span>{{ statusEventError }}</span>
                  <a-button size="small" @click="loadStatusEvents()">重试</a-button>
                </div>
                <a-empty v-else-if="!statusEventLoading && statusEventList.length === 0" description="暂无上下线记录" />
                <a-timeline v-else class="status-event-timeline">
                  <a-timeline-item
                    v-for="event in statusEventList"
                    :key="event.id"
                    :label="statusEventTime(event.occurredAt)"
                    :dot-color="eventColor(event.eventType)"
                    dot-type="hollow"
                  >
                    <div class="status-event-item" :data-event="event.eventType" :title="eventMetaText(event)">
                      <strong>{{ event.eventName }}</strong>
                    </div>
                  </a-timeline-item>
                </a-timeline>
              </a-spin>
              <div v-if="statusEventList.length" class="status-event-load-more">
                <span v-if="statusEventLoadingMore"><Loader2 :size="14" class="spin" /> 正在加载更多</span>
                <span v-else-if="statusEventLoadMoreError" class="error"
                  >{{ statusEventLoadMoreError }} <button type="button" @click="loadMoreStatusEvents">重试</button></span
                >
                <span v-else-if="!statusEventHasMore">已展示全部 {{ statusEventTotal }} 条</span>
                <span v-else>向下滚动加载更多 · 已加载 {{ statusEventList.length }}/{{ statusEventTotal }} 条</span>
              </div>
            </div>
          </a-tab-pane>
          <a-tab-pane key="traffic">
            <template #title
              ><span class="runtime-tab-title"><BarChart3 :size="15" />流量统计</span></template
            >
            <div class="runtime-filter">
              <span>统计范围</span>
              <div class="runtime-filter-control">
                <a-select
                  v-model="runtimeChannelCode"
                  :loading="runtimeChannelsLoading"
                  allow-clear
                  placeholder="全部通道（设备汇总）"
                >
                  <a-option v-for="channel in runtimeChannels" :key="channel.channelId" :value="channel.channelId"
                    >{{ displayName(channel) }} · {{ channel.channelId }}</a-option
                  >
                </a-select>
                <small>不选择通道时展示整台设备的汇总数据</small>
              </div>
            </div>
            <div v-if="runtimeChannelsError" class="runtime-channel-state" role="alert">
              <span>{{ runtimeChannelsError }}，当前仍可查看设备汇总</span>
              <a-button size="mini" @click="loadRuntimeChannels(statusEventDevice)">重试</a-button>
            </div>
            <TrafficTrend
              v-if="runtimeActiveTab === 'traffic'"
              :device-id="statusEventDevice.deviceId"
              :channel-id="runtimeChannelCode || undefined"
            />
          </a-tab-pane>
          <a-tab-pane key="viewers">
            <template #title
              ><span class="runtime-tab-title"><Eye :size="15" />当前观看</span></template
            >
            <div class="runtime-filter">
              <span>观看通道</span>
              <div class="runtime-filter-control">
                <a-select
                  v-model="runtimeChannelCode"
                  :loading="runtimeChannelsLoading"
                  allow-clear
                  placeholder="全部通道（默认）"
                >
                  <a-option v-for="channel in runtimeChannels" :key="channel.channelId" :value="channel.channelId"
                    >{{ displayName(channel) }} · {{ channel.channelId }}</a-option
                  >
                </a-select>
                <small>默认展示设备全部通道，可按通道筛选</small>
              </div>
            </div>
            <div v-if="runtimeChannelsError" class="runtime-channel-state error" role="alert">
              <span>{{ runtimeChannelsError }}，当前仍展示全部通道</span>
              <a-button size="mini" @click="loadRuntimeChannels(statusEventDevice)">重试</a-button>
            </div>
            <ViewerTable
              v-if="runtimeActiveTab === 'viewers'"
              :device-id="statusEventDevice.deviceId"
              :channel-id="runtimeChannelCode || undefined"
            />
          </a-tab-pane>
        </a-tabs>
      </div>
    </a-modal>

    <a-modal
      v-if="canAddDevice"
      v-model:visible="createDeviceVisible"
      modal-class="uvp-system-dialog"
      title="新建设备"
      :width="480"
      :mask-closable="false"
      unmount-on-close
      @cancel="createDeviceVisible = false"
    >
      <a-form ref="createDeviceFormRef" :model="createDeviceForm" :rules="createDeviceRules" layout="vertical">
        <a-form-item field="deviceId" label="设备国标 ID" validate-trigger="blur">
          <a-input v-model="createDeviceForm.deviceId" placeholder="请输入 20 位国标设备 ID" :maxlength="20" allow-clear>
            <template #suffix>
              <span class="input-counter" :class="{ done: (createDeviceForm.deviceId?.length || 0) === 20 }">
                {{ createDeviceForm.deviceId?.length || 0 }} / 20
              </span>
            </template>
          </a-input>
        </a-form-item>
        <a-form-item field="name" label="设备名称">
          <a-input v-model="createDeviceForm.name" placeholder="选填,方便识别" allow-clear />
        </a-form-item>
        <a-form-item field="password" label="设备密码">
          <a-input-password v-model="createDeviceForm.password" placeholder="选填,用于一设备一密码场景" allow-clear />
        </a-form-item>
      </a-form>
      <template #footer>
        <a-button @click="createDeviceVisible = false">取消</a-button>
        <a-button type="primary" :loading="creatingDevice" @click="handleCreateDevice">创建</a-button>
      </template>
    </a-modal>

    <!-- 编辑设备 Modal -->
    <a-modal
      v-if="canEditDevice"
      v-model:visible="editDeviceVisible"
      modal-class="uvp-system-dialog"
      title="编辑设备"
      :width="480"
      :mask-closable="false"
      unmount-on-close
      @cancel="cancelEditDevice"
    >
      <a-form ref="editDeviceFormRef" :model="editDeviceForm" layout="vertical">
        <a-form-item label="设备国标 ID">
          <a-input :model-value="editDeviceForm.deviceId" disabled allow-clear class="code-main mono" />
        </a-form-item>
        <a-form-item label="设备上报名称">
          <a-input :model-value="editDeviceForm.name" disabled allow-clear placeholder="设备未上报" />
          <template #extra>
            <span class="form-hint">来自设备 DeviceInfo 应答,不可编辑</span>
          </template>
        </a-form-item>
        <a-form-item field="alias" label="设备别名">
          <a-input v-model="editDeviceForm.alias" placeholder="选填,给设备起一个好记的名字" allow-clear />
          <template #extra>
            <span class="form-hint">优先展示,不会被设备重新注册覆盖</span>
          </template>
        </a-form-item>
        <a-form-item field="zlmNodeId" label="ZLM 节点">
          <a-select
            v-model="editDeviceForm.zlmNodeId"
            :options="zlmNodeOptions"
            :loading="zlmNodesLoading"
            placeholder="请选择媒体节点"
            allow-search
          />
          <template #extra>
            <span v-if="zlmNodesError" class="form-hint form-hint-error" role="alert">
              {{ zlmNodesError }}，<a-link @click="loadZLMNodeOptions">重新加载</a-link>
            </span>
            <span v-else class="form-hint">自动调度由集群选择节点；指定节点不可用时自动回退集群调度，仅影响新开的媒体流。</span>
          </template>
        </a-form-item>
        <a-form-item field="protocolOverride" label="协议版本覆盖">
          <a-select
            v-model="editDeviceForm.protocolOverride"
            :options="[
              { label: '自动（按设备上报）', value: 'auto' },
              { label: 'GB/T 28181-2016', value: '2016' },
              { label: 'GB/T 28181-2022', value: '2022' }
            ]"
          />
          <template #extra>
            <span class="form-hint">仅影响后续新操作；自动模式优先使用设备 X-GB-Ver，缺失时默认 2016。</span>
          </template>
        </a-form-item>
      </a-form>
      <template #footer>
        <a-button @click="cancelEditDevice">取消</a-button>
        <a-button type="primary" :loading="editingDevice" @click="handleEditDevice">保存</a-button>
      </template>
    </a-modal>

    <!-- 编辑通道 Modal -->
    <a-modal
      v-if="canEditChannel"
      v-model:visible="editChannelVisible"
      modal-class="uvp-system-dialog"
      title="编辑通道"
      :width="480"
      :mask-closable="false"
      unmount-on-close
      @cancel="cancelEditChannel"
    >
      <a-form :model="editChannelForm" layout="vertical">
        <a-form-item label="通道编号">
          <a-input :model-value="editChannelForm.channelId" disabled allow-clear class="code-main mono" />
        </a-form-item>
        <a-form-item label="所属设备">
          <a-input :model-value="editChannelForm.deviceId" disabled allow-clear class="code-main mono" />
        </a-form-item>
        <a-form-item label="通道名称">
          <a-input :model-value="editChannelForm.name" disabled allow-clear placeholder="设备上报名称" />
          <template #extra><span class="form-hint">来自设备 Catalog 应答,不可编辑</span></template>
        </a-form-item>
        <a-form-item label="通道别名">
          <a-input v-model="editChannelForm.alias" placeholder="选填,给通道起一个好记的名称" allow-clear />
          <template #extra><span class="form-hint">优先展示,不会被设备 Catalog 上报覆盖</span></template>
        </a-form-item>
        <a-form-item label="厂商 / 型号">
          <a-input :model-value="vendorText(editChannelForm)" disabled allow-clear />
          <template #extra><span class="form-hint">来自设备 Catalog 应答,不可编辑</span></template>
        </a-form-item>
        <a-form-item label="摄像头类型">
          <a-select v-model="editChannelForm.ptzType">
            <a-option v-for="opt in ptzTypeOptions" :key="opt.value" :value="Number(opt.value)">{{ opt.name }}</a-option>
          </a-select>
        </a-form-item>
        <a-form-item label="通道坐标">
          <div class="coord-inputs">
            <a-input v-model="editChannelLongitude" placeholder="经度，如 116.99505" allow-clear />
            <a-input v-model="editChannelLatitude" placeholder="纬度，如 36.66237" allow-clear />
            <a-button size="small" @click="openLocationPickerFromEdit">
              <template #icon><MapIcon :size="14" /></template>
              地图选点
            </a-button>
          </div>
          <template #extra>
            <span class="form-hint">
              留空表示本次不修改坐标；两者都填 0 可清除。「地图选点」可直接在地图上点击确定位置。当前来源：{{
                positionSourceText({ positionSource: editChannelPositionSource }) || "无坐标"
              }}
            </span>
          </template>
        </a-form-item>
        <a-form-item label="流传输模式">
          <template v-if="canEditTransport">
            <a-select v-model="editChannelForm.streamTransport">
              <a-option value="UDP">UDP</a-option>
              <a-option value="TCP-Active">TCP-Active</a-option>
              <a-option value="TCP-Passive">TCP-Passive</a-option>
            </a-select>
          </template>
          <span v-else>{{ streamTransportText(editChannelForm.streamTransport) }}</span>
        </a-form-item>
        <a-form-item label="按需直播">
          <a-switch v-model="editChannelForm.onDemandLive" checked-text="无人观看自动关闭" unchecked-text="持续保持直播" />
        </a-form-item>
      </a-form>
      <template #footer>
        <a-button @click="cancelEditChannel">取消</a-button>
        <a-button type="primary" :loading="editingChannel" @click="handleEditChannel">保存</a-button>
      </template>
    </a-modal>
  </div>
</template>

<style scoped lang="scss">
.channel-workspace-header {
  display: flex;
  flex: 0 0 auto;
  gap: 12px;
  align-items: center;
  min-width: 0;
  padding: 8px 0 16px;
}
.workspace-context-icon {
  display: grid;
  flex: 0 0 44px;
  place-items: center;
  height: 44px;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border: 1px solid color-mix(in srgb, var(--uvp-brand) 16%, transparent);
  border-radius: 12px;
}
.workspace-context-copy {
  flex: 1;
  min-width: 0;
}
.workspace-eyebrow {
  font-size: 10px;
  color: var(--uvp-text-tertiary);
}
.workspace-context-title {
  display: flex;
  gap: 10px;
  align-items: center;
  min-width: 0;
}
.workspace-context-title h2 {
  margin: 3px 0;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 17px;
  font-weight: 650;
  color: var(--uvp-text-primary);
  white-space: nowrap;
}
.workspace-context-title .status-pill {
  flex-shrink: 0;
}
.workspace-context-copy p {
  margin: 3px 0 0;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}
.device-context-actions {
  box-sizing: border-box;
  display: flex;
  flex: 0 0 auto;
  flex-wrap: nowrap;
  gap: 8px;
  min-width: 0;
}
.device-context-actions :deep(.device-action.arco-btn) {
  --action-color: var(--uvp-brand);

  display: inline-flex;
  gap: 6px;
  align-items: center;
  justify-content: center;
  height: 32px;
  padding: 0 12px;
  color: var(--action-color);
  white-space: nowrap;
  background: color-mix(in srgb, var(--action-color) 9%, var(--uvp-panel-bg));
  border: 1px solid color-mix(in srgb, var(--action-color) 26%, var(--uvp-panel-border));
  border-radius: 7px;
  transition:
    background 0.15s,
    border-color 0.15s;
}
.device-context-actions :deep(.device-action.tone-config) {
  --action-color: var(--uvp-brand-cyan);
}
.device-context-actions :deep(.device-action.arco-btn:not(.arco-btn-disabled):hover) {
  background: color-mix(in srgb, var(--action-color) 16%, var(--uvp-panel-bg));
  border-color: var(--action-color);
}
.device-context-actions :deep(.device-action.arco-btn-disabled) {
  opacity: 0.45;
}
.content-pane-map .channel-workspace-header {
  padding: 12px 14px;
  background: var(--uvp-panel-bg);
  border-bottom: 1px solid var(--uvp-panel-border);
}

@media (width <= 1180px) {
  .channel-workspace-header {
    flex-wrap: wrap;
  }
  .device-context-actions {
    width: 100%;
    padding-left: 56px;
  }
}

@media (width <= 720px) {
  .device-context-actions {
    gap: 4px;
    padding-left: 0;
    overflow-x: auto;
  }
  .device-context-actions :deep(.device-action.arco-btn) {
    flex-shrink: 0;
    padding: 0 8px;
  }
  .workspace-context-title h2 {
    font-size: 15px;
  }
}

.device-mgmt-page {
  display: flex;
  flex-direction: column;
  min-width: 0;
  height: 100%;
  min-height: 0;
  padding: 0;
  overflow: hidden;
}

.device-stats {
  display: inline-flex;
  flex: 0 0 auto;
  gap: 8px;
  align-items: center;
}
.stat-ring {
  display: inline-flex;
  gap: 12px;
  align-items: center;
}
.stat-ring__donut {
  position: relative;
  display: grid;
  place-items: center;
  width: 42px;
  height: 42px;
  border-radius: 50%;
}
.stat-ring__donut::after {
  position: absolute;
  inset: 7px;
  content: "";
  background: var(--uvp-panel-bg);
  border-radius: 50%;
}
.stat-ring__value {
  position: relative;
  z-index: 1;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11px;
  font-weight: 700;
  line-height: 1;
  color: var(--uvp-text-primary);
}
.stat-ring__legend {
  display: grid;
  gap: 3px;
}
.stat-ring__entity {
  font-size: 10px;
  font-weight: 700;
  color: var(--uvp-text-tertiary);
  letter-spacing: 0.04em;
}
.stat-ring__item {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  font-size: 13px;
  font-weight: 620;
  color: var(--uvp-text-secondary);
}
.stat-ring__dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}
.stat-ring__dot.online {
  background: var(--uvp-brand-cyan);
  animation: stat-ring-pulse 1.6s ease-out infinite;
}
.stat-ring__dot.offline {
  background: var(--uvp-danger);
  animation: stat-ring-pulse-danger 1.6s ease-out infinite;
}

@keyframes stat-ring-pulse {
  0% {
    box-shadow: 0 0 0 0 color-mix(in srgb, var(--uvp-brand-cyan) 45%, transparent);
  }
  70%,
  100% {
    box-shadow: 0 0 0 6px color-mix(in srgb, var(--uvp-brand-cyan) 0%, transparent);
  }
}

@keyframes stat-ring-pulse-danger {
  0% {
    box-shadow: 0 0 0 0 color-mix(in srgb, var(--uvp-danger) 45%, transparent);
  }
  70%,
  100% {
    box-shadow: 0 0 0 6px color-mix(in srgb, var(--uvp-danger) 0%, transparent);
  }
}

@media (prefers-reduced-motion: reduce) {
  .stat-ring__dot.online,
  .stat-ring__dot.offline {
    animation: none;
  }
}
.workspace-toolbar-row {
  display: flex;
  gap: 16px;
  align-items: flex-start;
  width: 100%;
  min-width: 0;
}
.toolbar-actions {
  display: flex;
  flex: 1 1 auto;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
  justify-content: flex-end;
  min-width: 0;
  margin-left: auto;
}
.cmdk {
  display: flex;
  flex: 1 1 210px;
  gap: 8px;
  align-items: center;
  width: min(360px, 100%);
  min-width: 210px;
  max-width: 360px;
  height: 40px;
  padding: 0 12px;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-search-control-bg);
  border: 1px solid var(--uvp-search-secondary-btn-border);
  border-radius: 10px;
}
.cmdk:focus-within {
  border-color: color-mix(in srgb, var(--uvp-brand) 40%, transparent);
  box-shadow: var(--uvp-search-control-focus-shadow);
}
.cmdk input {
  flex: 1;
  min-width: 0;
  color: var(--uvp-text-primary);
  outline: none;
  background: transparent;
  border: 0;
}
.cmdk-clear {
  display: inline-grid;
  flex: 0 0 auto;
  place-items: center;
  width: 22px;
  height: 22px;
  color: var(--uvp-text-tertiary);
  background: transparent;
  border: 0;
  border-radius: 5px;
}
.cmdk-clear:hover {
  color: var(--uvp-text-primary);
  background: var(--uvp-brand-soft);
}
.cmdk .kbd {
  display: inline-flex;
  gap: 2px;
  margin-left: auto;
  font-size: 11px;
}
.cmdk kbd {
  padding: 1px 5px;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 4px;
}
.view-switch {
  display: inline-flex;
  gap: 4px;
  padding: 3px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 10px;
}
.view-switch button,
.icon-btn {
  display: inline-grid;
  place-items: center;
  width: 32px;
  height: 32px;
  color: var(--uvp-text-tertiary);
  background: transparent;
  border: 0;
  border-radius: 8px;
}
.view-switch button.active,
.icon-btn:hover {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
}
.view-switch button:focus-visible,
.btn-primary:focus-visible,
.btn-ghost:focus-visible {
  outline: 2px solid var(--uvp-brand);
  outline-offset: 2px;
}
.icon-btn.small {
  width: 24px;
  height: 24px;
}
.btn-primary,
.btn-ghost,
.btn-group,
.btn-danger,
.play-cta {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  justify-content: center;
  height: 32px;
  padding: 0 12px;
  white-space: nowrap;
  border-radius: 10px;
}
.btn-primary {
  font-weight: 600;
  color: var(--uvp-solid-text);
  background: var(--uvp-solid-bg);
  border: 0;
}
.btn-primary.long {
  width: 100%;
}
.btn-group {
  font-weight: 600;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border: 1px solid color-mix(in srgb, var(--uvp-brand) 26%, transparent);
}
.btn-group:hover {
  background: color-mix(in srgb, var(--uvp-brand) 16%, var(--uvp-panel-bg));
}
.btn-group:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}
.btn-danger {
  font-weight: 600;
  color: #ffffff;
  background: #ef4444;
  border: 0;
}
.btn-danger:hover {
  background: #dc2626;
}
.btn-danger:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}
.btn-ghost {
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: var(--uvp-search-secondary-btn-bg);
  border: 1px solid var(--uvp-search-secondary-btn-border);
  transition:
    color 0.15s ease,
    background 0.15s ease,
    border-color 0.15s ease,
    transform 0.1s ease;
}
.btn-ghost:hover {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-color: color-mix(in srgb, var(--uvp-brand) 32%, transparent);
}
.btn-ghost:active {
  background: color-mix(in srgb, var(--uvp-brand) 14%, var(--uvp-brand-soft));
  transform: scale(0.96);
}
.btn-ghost.active {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-color: color-mix(in srgb, var(--uvp-brand) 28%, transparent);
}
.workspace {
  display: grid;
  flex: 1;
  grid-template-rows: auto minmax(0, 1fr);
  grid-template-columns: 320px minmax(0, 1fr);
  gap: 0;
  min-height: 0;
}
.workspace-toolbar {
  grid-column: 1 / -1;
  min-width: 0;
  margin-bottom: 0;
}
.workspace-toolbar :deep(.uvp-search-panel__surface) {
  grid-template-columns: minmax(0, 1fr);
  column-gap: 0;
  background: transparent;
  border: 0;
  border-bottom: 1px solid var(--uvp-panel-border);
  border-radius: 0;
  box-shadow: none;
}
.workspace-toolbar :deep(.uvp-search-panel__fields) {
  flex: 1 1 100%;
  width: 100%;
}
.workspace-toolbar :deep(.arco-input-wrapper),
.workspace-toolbar :deep(.arco-select-view),
.workspace-toolbar :deep(.arco-picker) {
  box-sizing: border-box;
  background: var(--uvp-search-control-bg) !important;
  border: 1px solid var(--uvp-search-secondary-btn-border) !important;
  border-radius: 10px !important;
  box-shadow: var(--uvp-search-control-shadow) !important;
}
.workspace-toolbar :deep(.arco-input-wrapper:focus-within),
.workspace-toolbar :deep(.arco-select-view-focus),
.workspace-toolbar :deep(.arco-picker-focused) {
  border-color: var(--uvp-brand) !important;
  box-shadow: var(--uvp-search-control-focus-shadow) !important;
}
.workspace-toolbar :deep(.arco-input::placeholder),
.workspace-toolbar :deep(.arco-select-view-input::placeholder),
.workspace-toolbar :deep(.arco-picker input::placeholder) {
  color: var(--uvp-text-tertiary) !important;
  opacity: 1;
}
.workspace-toolbar :deep(.arco-btn) {
  box-sizing: border-box;
  border-radius: 10px;
}
.workspace-toolbar .create-device-btn {
  height: 40px;
  border-radius: 10px;
}
.catalog-pane,
.content-pane {
  min-width: 0;
  min-height: 0;
}
.catalog-pane {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border-right: 1px solid var(--uvp-panel-border);
}
.spin {
  animation: spin 0.8s linear infinite;
}
.drawer-icon {
  display: inline-grid;
  flex: 0 0 auto;
  place-items: center;
  width: 22px;
  height: 22px;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-radius: 7px;
}
.catalog-pane :deep(.directory-panel) {
  flex: 1;
  width: 100%;
  min-width: 0;
  height: 0;
  background: transparent;
  border-right: 0;
}
.content-pane {
  display: flex;
  flex-direction: column;
  padding: 14px 14px 12px;
}
.content-pane.content-pane-map {
  padding: 0;
}
.content-pane.content-pane-map > .filter-chips,
.content-pane.content-pane-map > .batch-bar {
  display: none;
}
.refresh-control {
  width: 104px;
  min-width: 104px;
}
.refresh-countdown {
  font-variant-numeric: tabular-nums;
  color: var(--uvp-text-tertiary);
}
.segmented {
  display: inline-flex;
  flex: 0 0 auto;
  gap: 2px;
  padding: 3px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 10px;
  box-shadow: var(--uvp-search-secondary-btn-shadow);
}
.segmented button {
  min-width: 52px;
  height: 30px;
  padding: 0 12px;
  font-size: 12px;
  line-height: 30px;
  color: var(--uvp-text-secondary);
  white-space: nowrap;
  background: transparent;
  border: 0;
  border-radius: 8px;
}
.segmented button.active {
  font-weight: 620;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
}
.asset-kind-switch button {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  justify-content: center;
  line-height: normal;
}
.toolbar-select {
  flex: 0 0 auto;
}
.status-select {
  min-width: 126px;
}
.toolbar-actions > .btn-ghost {
  flex: 0 0 auto;
  min-width: 84px;
  height: 40px;
  border-radius: 8px;
}

/* 「使用引导」图标按钮：正方形。⛔ 不要去改上面那条 `min-width: 84px` ——
   刷新按钮还指着它；这里靠多一个 `.guide-trigger` 类提高特异性来收窄。 */
.toolbar-actions > .btn-ghost.guide-trigger {
  width: 40px;
  min-width: 40px;
  padding: 0;
  color: var(--uvp-text-tertiary);
}
.toolbar-actions > .btn-ghost.guide-trigger:hover {
  color: var(--uvp-brand);
}
.filter-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  padding-top: 12px;
  padding-bottom: 12px;
}
.filter-chip,
.clear-all {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  height: 28px;
  padding: 0 10px;
  font-size: 12px;
  border-radius: 10px;
}
.filter-chip {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border: 1px solid color-mix(in srgb, var(--uvp-brand) 24%, transparent);
}
.device-drilldown-chip {
  cursor: pointer;
}
.device-drilldown-chip:hover {
  background: color-mix(in srgb, var(--uvp-brand-soft) 78%, var(--uvp-brand));
}
.device-drilldown-chip:focus-visible {
  outline: 2px solid var(--uvp-brand);
  outline-offset: 2px;
}
.filter-chip .close {
  color: inherit;
  background: transparent;
  border: 0;
}
.clear-all {
  color: var(--uvp-text-secondary);
  background: transparent;
  border: 0;
}
.batch-bar {
  display: flex;
  gap: 10px;
  align-items: center;
  justify-content: space-between;
  min-height: 54px;
  padding: 0 14px;
  margin-bottom: 10px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 12px;
}
.batch-info strong {
  color: var(--uvp-brand);
}
.batch-ops {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  justify-content: flex-end;
}
.view-body {
  flex: 1;
  min-width: 0;
  min-height: 0;
}
.table-view {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.uvp-data-table :deep(.arco-table-body.arco-scrollbar-container) {
  height: calc(100% - 15px);
}
.thumb {
  display: grid;
  place-items: center;
  width: 48px;
  height: 27px;
  overflow: hidden;
  background: linear-gradient(135deg, #0f172a, #1d4ed8);
  border-radius: 6px;
}
.thumb.small {
  width: 50px;
  height: 32px;
  margin: 0 auto;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
}
.thumb.small .placeholder {
  color: var(--uvp-text-tertiary);
}

/* 表格快照缩略图:100x60,arco a-image 自带 preview 弹层 */
.snapshot-thumb :deep(.arco-image-img) {
  cursor: zoom-in;
  border-radius: 4px;
}
.list-snapshot-empty {
  display: inline-flex;
  flex-direction: column;
  gap: 1px;
  font-size: 10px;
  line-height: 12px;
  color: var(--uvp-text-tertiary);
}
.thumb.brand-thumb {
  width: 40px;
  height: 40px;
  background: linear-gradient(
    135deg,
    color-mix(in srgb, var(--uvp-brand) 86%, #ffffff 14%),
    color-mix(in srgb, var(--uvp-brand-cyan) 82%, #0f172a 18%)
  );
  border-radius: 10px;
}
.placeholder {
  font-size: 10px;
  font-weight: 700;
  color: #ffffff;
}
.brand-cell {
  display: inline-flex;
  gap: 10px;
  align-items: center;
  min-width: 0;
}
.brand-name {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 13px;
  font-weight: 600;
  color: var(--uvp-text-primary);
  white-space: nowrap;
}
.device-name {
  display: grid;
  gap: 2px;
}
.device-name .pri {
  min-width: 0;
}
.pri {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  font-weight: 600;
  color: var(--uvp-text-primary);
}
.pri.mono,
.sec,
.code-main,
.code-sub {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.sec {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.code-cell,
.time-cell {
  display: grid;
  gap: 3px;
  min-width: 0;
}
.code-main {
  font-size: 12px;
  font-weight: 620;
  color: var(--uvp-text-primary);
  white-space: nowrap;
}
.code-main.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.code-sub {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}
.time-cell span:first-child {
  font-size: 12px;
  color: var(--uvp-text-secondary);
}
.time-cell span:last-child {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.time-cell .warn {
  color: var(--uvp-warning);
}
.mount-badge {
  display: inline-flex;
  align-items: center;
  height: 18px;
  padding: 0 6px;
  font-size: 11px;
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
  border-radius: 999px;
}
.vendor-cell {
  display: grid;
  gap: 2px;
}
.vendor-cell .v {
  color: var(--uvp-text-primary);
}
.vendor-cell .m {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.tag {
  display: inline-flex;
  align-items: center;
  height: 24px;
  padding: 0 8px;
  font-size: 12px;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-radius: 999px;
}
.tag.muted {
  color: var(--uvp-text-secondary);
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
}
.status-pill {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  justify-content: center;
  min-width: 60px;
  padding: 4px 10px;
  font-size: 12px;
  font-weight: 650;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 999px;
}
.status-pill.online {
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 12%, transparent);
  border-color: color-mix(in srgb, var(--uvp-brand-cyan) 28%, transparent);
}
.relative {
  font-size: 12px;
  color: var(--uvp-text-secondary);
}
.relative.warn {
  color: var(--uvp-warning);
}
.status-inline {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  font-size: 12px;
  font-weight: 600;
  color: var(--uvp-text-secondary);
}
.status-inline .status-dot {
  width: 8px;
  height: 8px;
  background: #ef4444;
  box-shadow: none;
}
.status-inline.online {
  color: var(--uvp-text-primary);
}
.status-inline.online .status-dot {
  background: #10b981;
}

// 直播中徽章:红色 + 浅红底,跟"在线绿点"呼应
.status-inline.playing {
  padding: 2px 8px;
  color: #d14343;
  background: rgb(209 67 67 / 8%);
  border-radius: 10px;
}
.status-inline.playing .status-dot {
  background: #d14343;
}

// 状态列容器:让"在线/离线"+ "直播中"两个徽章水平排列
.status-cell {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
}
.status-trigger {
  flex-wrap: nowrap;
  justify-content: center;
  width: 64px;
  min-width: 64px;
  height: 24px;
  padding: 0 6px;
  line-height: 1;
  color: var(--uvp-danger);
  white-space: nowrap;
  cursor: pointer;
  background: var(--uvp-danger-soft);
  border: 1px solid var(--uvp-danger-border);
  border-radius: 6px;
  box-shadow: inset 0 1px 0 color-mix(in srgb, #ffffff 72%, transparent);
  transition:
    color 0.16s ease,
    background 0.16s ease,
    border-color 0.16s ease,
    box-shadow 0.16s ease,
    transform 0.16s ease;
}
.status-trigger.online {
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 9%, var(--uvp-panel-bg));
  border-color: color-mix(in srgb, var(--uvp-brand-cyan) 34%, var(--uvp-panel-border));
}
.status-trigger:hover {
  color: var(--uvp-danger);
  background: color-mix(in srgb, var(--uvp-danger) 12%, var(--uvp-panel-bg));
  border-color: color-mix(in srgb, var(--uvp-danger) 62%, var(--uvp-panel-border));
  box-shadow: 0 4px 10px -6px color-mix(in srgb, var(--uvp-danger) 55%, transparent);
  transform: translateY(-1px);
}
.status-trigger.online:hover {
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 15%, var(--uvp-panel-bg));
  border-color: color-mix(in srgb, var(--uvp-brand-cyan) 64%, var(--uvp-panel-border));
  box-shadow: 0 4px 10px -6px color-mix(in srgb, var(--uvp-brand-cyan) 60%, transparent);
}
.status-trigger:active {
  box-shadow: none;
  transform: translateY(0);
}
.status-trigger:focus-visible {
  outline: 2px solid currentColor;
  outline-offset: 2px;
}
.status-trigger-icon {
  flex: 0 0 auto;
}
.status-trigger-label {
  display: block;
  flex: 0 0 auto;
  white-space: nowrap;
}
.channel-progress {
  display: flex;
  gap: 8px;
  align-items: center;
}
.progress-bar {
  position: relative;
  flex: 1;
  height: 6px;
  overflow: hidden;
  background: var(--uvp-panel-border);
  border-radius: 3px;
}
.progress-fill {
  height: 100%;
  border-radius: 3px;
  transition: width 0.3s ease;
}
.progress-bar.healthy .progress-fill {
  background: #10b981;
}
.progress-bar.warning .progress-fill {
  background: #f59e0b;
}
.progress-bar.offline .progress-fill {
  background: #ef4444;
}
.progress-bar.empty .progress-fill {
  background: #6b7280;
}
.channel-text {
  min-width: 36px;
  font-size: 12px;
  font-weight: 500;
  color: var(--uvp-text-secondary);
  white-space: nowrap;
}

/* Device rows use the same relaxed density as the channel rows with their select controls. */
.device-data-table :deep(.arco-table-td) {
  height: 60px;
  font-size: 13px;
  font-weight: 400;
}
.device-data-table .pri,
.device-data-table .code-main,
.device-data-table .relative,
.device-data-table .status-inline,
.device-data-table .channel-text {
  font-family: inherit;
  font-size: 13px;
  font-weight: 400;
}

/* Text ellipsis for long content */
.text-ellipsis {
  display: block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.device-name.text-ellipsis {
  max-width: 100%;
}
.play-cta {
  font-size: 12px;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border: 1px solid color-mix(in srgb, var(--uvp-brand) 24%, transparent);
}
.uvp-data-table :deep(.uvp-table-actions) {
  gap: 1px;
  white-space: nowrap;
}
.uvp-data-table :deep(.uvp-table-action) {
  flex: 0 0 auto;
  gap: 3px;
  padding-inline: 4px;
  line-height: 1;
}
.uvp-data-table :deep(.uvp-table-action .arco-link-icon) {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  margin-right: 0;
  line-height: 0;
}
.uvp-data-table :deep(.uvp-table-action .arco-link-icon svg) {
  display: block;
}
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--preview) {
  color: #2563eb;
}
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--preview:hover) {
  color: #1d4ed8;
  background: rgb(37 99 235 / 8%);
}
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--record) {
  color: #6b4f9b;
}
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--record:hover) {
  color: #5a3f89;
  background: rgb(107 79 155 / 8%);
}
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--detail) {
  color: #0f7490;
}
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--detail:hover) {
  color: #0e7490;
  background: rgb(14 116 144 / 8%);
}
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--sync) {
  color: #0f766e;
}
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--sync:hover) {
  color: #0f675f;
  background: rgb(15 118 110 / 8%);
}
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--subscribe) {
  color: var(--uvp-brand);
}
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--subscribe:hover) {
  color: var(--uvp-brand-strong);
  background: var(--uvp-brand-soft);
}
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--edit) {
  color: #b7791f;
}
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--edit:hover) {
  color: #9a6b18;
  background: rgb(183 121 31 / 9%);
}
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--stop) {
  color: #dc2626;
}
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--stop:hover) {
  color: #b91c1c;
  background: rgb(220 38 38 / 8%);
}
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--more) {
  color: #6b4f9b;
}
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--more:hover) {
  color: #5a3f89;
  background: rgb(107 79 155 / 8%);
}
:global(.arco-dropdown:has(.device-action-menu-item)) {
  padding: 4px;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
  box-shadow: 0 12px 26px -16px rgb(15 23 42 / 40%);
}
:global(.arco-dropdown:has(.device-action-menu-item) .arco-dropdown-list) {
  padding: 0;
}
:global(.device-card-maintenance-dropdown .arco-scrollbar-thumb-direction-vertical) {
  display: none;
}
:global(.arco-dropdown:has(.device-action-menu-item) .device-action-menu-item) {
  display: flex;
  gap: 10px;
  align-items: center;
  min-width: 112px;
  height: 32px;
  padding: 0 9px;
  font-size: 13px;
  line-height: 32px;
  color: var(--uvp-text-secondary);
  border-radius: 6px;
}
:global(.arco-dropdown:has(.device-action-menu-item) .device-action-menu-item:hover) {
  color: var(--uvp-brand-strong);
  background: color-mix(in srgb, var(--uvp-brand) 7%, var(--uvp-list-toolbar-bg));
}
.btn-ghost.compact {
  height: 28px;
  padding: 0 10px;
  font-size: 12px;
  border-radius: 8px;
}
.icon-btn.framed.primary {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border: 1px solid color-mix(in srgb, var(--uvp-brand) 24%, transparent);
}
.icon-btn.framed.primary:hover {
  color: var(--uvp-solid-text);
  background: var(--uvp-solid-bg);
  border-color: var(--uvp-solid-border);
}
.icon-btn.framed.subscription {
  color: #6b4f9b;
  background: rgb(107 79 155 / 8%);
  border: 1px solid rgb(107 79 155 / 22%);
}
.icon-btn.framed.subscription:hover {
  color: #ffffff;
  background: #6b4f9b;
  border-color: #6b4f9b;
}
.icon-btn.framed.record-query-entry {
  color: #6b4f9b;
  background: rgb(107 79 155 / 8%);
  border: 1px solid rgb(107 79 155 / 22%);
}
.icon-btn.framed.record-query-entry:hover {
  color: #ffffff;
  background: #6b4f9b;
  border-color: #6b4f9b;
}
.icon-btn.framed.info {
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 8%, transparent);
  border: 1px solid color-mix(in srgb, var(--uvp-brand-cyan) 20%, var(--uvp-search-secondary-btn-border));
}
.icon-btn.framed.info:hover {
  color: #ffffff;
  background: var(--uvp-brand-cyan);
  border-color: var(--uvp-brand-cyan);
}
.icon-btn.framed.warning {
  color: #f59e0b;
  background: color-mix(in srgb, #f59e0b 8%, transparent);
  border: 1px solid color-mix(in srgb, #f59e0b 20%, var(--uvp-search-secondary-btn-border));
}
.icon-btn.framed.warning:hover {
  color: #ffffff;
  background: #f59e0b;
  border-color: #f59e0b;
}

// 停止播放按钮:深红 #dc2626 warning-danger 系,比"删除"#ef4444 更沉,视觉可辨
.icon-btn.framed.stop {
  color: #dc2626;
  background: color-mix(in srgb, #dc2626 8%, transparent);
  border: 1px solid color-mix(in srgb, #dc2626 24%, var(--uvp-search-secondary-btn-border));
}
.icon-btn.framed.stop:hover:not(:disabled) {
  color: #ffffff;
  background: #dc2626;
  border-color: #dc2626;
}
.icon-btn.framed.stop:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}
.icon-btn.framed {
  color: var(--uvp-text-secondary);
  background: var(--uvp-search-secondary-btn-bg);
  border: 1px solid var(--uvp-search-secondary-btn-border);
}
.icon-btn.framed:hover {
  color: var(--uvp-brand);
  border-color: color-mix(in srgb, var(--uvp-brand) 28%, var(--uvp-search-secondary-btn-border));
}
.icon-btn.framed.danger {
  color: #ef4444;
  background: color-mix(in srgb, #ef4444 8%, transparent);
  border: 1px solid color-mix(in srgb, #ef4444 20%, var(--uvp-search-secondary-btn-border));
}
.icon-btn.framed.danger:hover {
  color: #ffffff;
  background: #ef4444;
  border-color: #ef4444;
}
.card-view {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 0;
  overflow: hidden;
}
.card-grid {
  display: grid;
  flex: 1;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  grid-auto-rows: max-content;
  gap: 12px;
  align-content: start;
  align-items: start;
  min-height: 0;
  padding-right: 4px;
  overflow: hidden auto;
}
.card-empty {
  display: flex;
  flex: 1;
  flex-direction: column;
  align-items: center;
  justify-content: center;
}
.card-pagination {
  display: flex;
  flex: 0 0 auto;
  justify-content: flex-end;
  padding: 2px 0 0;
}
.device-card {
  overflow: hidden;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 12px;
}
.status-dot {
  width: 10px;
  height: 10px;
  background: var(--uvp-warning);
  border-radius: 999px;
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--uvp-warning) 15%, transparent);
}
.status-dot.online {
  background: #10b981;
  box-shadow: 0 0 0 3px rgb(16 185 129 / 14%);
}
.card-actions {
  display: flex;
  gap: 8px;
  align-items: center;
}
.device-summary-card {
  position: relative;
  display: grid;
  grid-template-rows: auto auto max-content;
  gap: 10px;
  align-self: start;
  min-height: 250px;
  padding: 12px 12px 6px;
}
.device-card-head {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 8px;
  align-items: center;
  padding-right: 86px;
}
.device-card-icon {
  display: grid;
  place-items: center;
  width: 36px;
  height: 36px;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-radius: 8px;
}
.device-card-title {
  display: grid;
  gap: 3px;
  min-width: 0;
}
.device-card-title strong {
  color: var(--uvp-text-primary);
}
.device-card-title .code {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.device-status-ribbon {
  position: absolute;
  top: 10px;
  right: -34px;
  z-index: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 110px;
  height: 26px;
  font-size: 12px;
  font-weight: 600;
  line-height: 1;
  color: #ffffff;
  cursor: pointer;
  background: #ef4444;
  border: 0;
  transform: rotate(45deg);
  transition:
    filter 0.16s ease,
    box-shadow 0.16s ease;
}
.device-status-ribbon:hover {
  box-shadow: 0 4px 10px -7px rgb(15 23 42 / 55%);
  filter: brightness(0.94);
}
.device-status-ribbon:focus-visible {
  outline: 2px solid currentColor;
  outline-offset: 2px;
}
.device-status-ribbon.online {
  background: #10b981;
}
.device-card-info {
  display: grid;
  grid-template-columns: 68px minmax(0, 1fr);
  gap: 7px 10px;
  font-size: 12px;
}
.device-card-info > div {
  display: contents;
}
.device-card-info span {
  color: var(--uvp-text-tertiary);
}
.device-card-info strong {
  min-width: 0;
  font-weight: 500;
  color: var(--uvp-text-primary);
}
.device-card-info .mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.device-card-channel-value {
  min-width: 0;
}
.device-card-channel-value .channel-progress {
  width: 100%;
}
.device-card-channel-value .channel-text {
  font-weight: 500;
}
.device-card-actions {
  gap: 6px;
  justify-content: flex-end;
  padding-top: 3px;
  border-top: 1px solid var(--uvp-panel-border);
}
.device-card-actions .icon-btn.small {
  width: 28px;
  height: 28px;
}
.channel-summary-card {
  min-width: 0;
}
.channel-snapshot {
  position: relative;
  display: grid;
  place-items: center;
  height: 148px;
  overflow: hidden;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-list-toolbar-bg);
  border-bottom: 1px solid var(--uvp-panel-border);
}
.channel-snapshot::before {
  position: absolute;
  inset: 0;
  content: "";
  background-image:
    linear-gradient(45deg, color-mix(in srgb, var(--uvp-panel-border) 42%, transparent) 25%, transparent 25%),
    linear-gradient(-45deg, color-mix(in srgb, var(--uvp-panel-border) 42%, transparent) 25%, transparent 25%),
    linear-gradient(45deg, transparent 75%, color-mix(in srgb, var(--uvp-panel-border) 42%, transparent) 75%),
    linear-gradient(-45deg, transparent 75%, color-mix(in srgb, var(--uvp-panel-border) 42%, transparent) 75%);
  background-position:
    0 0,
    0 8px,
    8px -8px,
    -8px 0;
  background-size: 16px 16px;
  opacity: 0.38;
}

/* 通道卡片快照:a-image 铺满 148px 容器,点击走 arco 内置 preview 弹层 */
.channel-snapshot-image {
  position: absolute;
  inset: 0;
  z-index: 1;
  width: 100% !important;
  height: 100% !important;
}
.channel-snapshot-image :deep(.arco-image-img) {
  width: 100%;
  height: 100%;
  cursor: zoom-in;
  object-fit: cover;
}
.snapshot-empty {
  position: relative;
  z-index: 1;
  display: grid;
  gap: 7px;
  justify-items: center;
  font-size: 12px;
}
.snapshot-empty svg {
  opacity: 0.7;
}
.channel-card-body {
  display: grid;
  gap: 10px;
  padding: 11px 12px 12px;
}
.channel-card-title {
  min-width: 0;
  font-size: 14px;
  line-height: 20px;
  color: var(--uvp-text-primary);
}
.channel-card-info {
  display: grid;
  grid-template-columns: 68px minmax(0, 1fr);
  gap: 5px 8px;
  font-size: 12px;
  line-height: 18px;
}
.channel-card-info > div {
  display: contents;
}
.channel-card-info span {
  color: var(--uvp-text-tertiary);
}
.channel-card-info strong {
  min-width: 0;
  font-weight: 500;
  color: var(--uvp-text-primary);
}
:deep(.channel-card-inline-select.arco-select-view-single) {
  width: 130px !important;
}
.channel-card-info .mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.channel-card-switches {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  padding-top: 2px;
  font-size: 12px;
  line-height: 18px;
}
.channel-card-switches .switch-item {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  min-width: 0;
  white-space: nowrap;
}
.channel-card-switches .switch-item > span,
.channel-card-switches .switch-item :deep(.arco-tooltip) > span {
  color: var(--uvp-text-tertiary);
}
.channel-card-switches .switch-item .tone-success {
  color: var(--uvp-success, #10b981);
}
.channel-card-switches .switch-item .tone-warning {
  color: var(--uvp-warning);
}
.channel-card-switches .switch-item .tone-danger {
  color: var(--uvp-danger);
}
.channel-card-actions {
  gap: 7px;
  padding-top: 9px;
  border-top: 1px solid var(--uvp-panel-border);
}
.channel-card-status {
  position: relative;
  display: inline-flex;
  gap: 5px;
  align-items: center;
  margin-right: auto;
  font-size: 12px;
  font-weight: 500;
  color: #ef4444;
}
.channel-card-status::before {
  position: relative;
  z-index: 1;
  width: 10px;
  height: 10px;
  content: "";
  background: #ef4444;
  border-radius: 50%;
  box-shadow: 0 0 0 3px rgb(239 68 68 / 13%);
}
.channel-card-status::after {
  position: absolute;
  top: 50%;
  left: -5px;
  width: 20px;
  height: 20px;
  content: "";
  border: 1px solid currentColor;
  border-radius: 50%;
  opacity: 0.65;
  transform: translateY(-50%) scale(0.55);
  animation: channel-status-ripple 2s ease-out infinite;
}
.channel-card-status.online {
  color: #10b981;
}
.channel-card-status.online::before {
  background: #10b981;
  box-shadow: 0 0 0 3px rgb(16 185 129 / 14%);
}

// 卡片右上角直播中徽章:absolute 定位在快照上,深色底 + 白字保证快照亮暗背景下都可读
.channel-snapshot-live-badge {
  position: absolute;
  top: 8px;
  right: 8px;
  z-index: 2;
  display: inline-flex;
  gap: 4px;
  align-items: center;
  padding: 3px 8px;
  font-size: 11px;
  font-weight: 600;
  line-height: 1;
  color: #ffffff;
  letter-spacing: 0.5px;
  pointer-events: none;
  background: rgb(220 38 38 / 92%);
  border-radius: 4px;
  box-shadow: 0 2px 6px rgb(0 0 0 / 20%);
}
.channel-snapshot-live-badge .live-dot {
  width: 6px;
  height: 6px;
  background: #ffffff;
  border-radius: 50%;
  box-shadow: 0 0 0 2px rgb(255 255 255 / 30%);
  animation: live-pulse 1.4s ease-in-out infinite;
}

@keyframes live-pulse {
  0%,
  100% {
    opacity: 1;
    transform: scale(1);
  }
  50% {
    opacity: 0.4;
    transform: scale(0.85);
  }
}

@media (prefers-reduced-motion: reduce) {
  .channel-snapshot-live-badge .live-dot {
    animation: none;
  }
}

@keyframes channel-status-ripple {
  0% {
    opacity: 0.65;
    transform: translateY(-50%) scale(0.55);
  }
  75%,
  100% {
    opacity: 0;
    transform: translateY(-50%) scale(1);
  }
}

@media (prefers-reduced-motion: reduce) {
  .channel-card-status::after {
    animation: none;
  }
}
.map-view {
  position: relative;
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  min-height: 460px;
  overflow: hidden;
}
.map-container {
  position: absolute;
  inset: 0;
}
.map-floating-actions {
  position: absolute;
  top: 14px;
  right: 14px;
  left: 14px;
  z-index: 2;
  display: flex;
  gap: 8px;
  align-items: center;
  min-height: 44px;
  padding: 6px 10px;
  background: rgb(255 255 255 / 88%);
  border: 1px solid rgb(148 163 184 / 35%);
  border-radius: 10px;
  box-shadow: 0 4px 16px rgb(15 23 42 / 12%);
  backdrop-filter: blur(8px);
}

/* 搜索框：换成 Arco input 后外观由这里接管 —— 工具栏是「白玻璃 / 暗玻璃」两套底，
 * 不能吃 Arco 默认的灰底，否则在暗色地图上会白成一块。 */
.map-search-box {
  width: 268px;
  padding: 0 10px;
  color: #94a3b8;
  background: rgb(248 250 252 / 92%);
  border: 1px solid #dbe3ed;
  border-radius: 7px;
}
.map-search-box :deep(.arco-input) {
  font-size: 13px;
  color: #334155;
}
.map-search-box :deep(.arco-input::placeholder) {
  color: #94a3b8;
}
.map-camera-count {
  padding: 7px 10px;
  font-size: 12px;
  color: #2563eb;
  white-space: nowrap;
  background: #eff6ff;
  border: 1px solid #bfdbfe;
  border-radius: 7px;
}
.map-toolbar-spacer {
  flex: 1;
}

/* 工具条按钮：四个动作各给一个语义色，不再是一排相同的白底 ——
 *   刷新 = 天蓝（只重取数据，不改视野）
 *   复位 = 琥珀（会把视野拉回去，属于"改变现状"）
 *   适应视野 = 实心主色（最高频动作，保持视觉焦点）
 *   设为默认中心 = 绿（写配置，成功语义）
 * ⛔ 浅底一律「同色文字 + 低透明同色底 + 同色描边」：四颗实心饱和色并排像调色板，
 *    而且白字压琥珀/青绿对比度只有 2:1 上下。
 * ⛔ 不要用 `--uvp-brand` 当底色：它是**文字色**（暗色下是浅蓝），白字压上去只有 2.5:1。 */
.map-action.arco-btn {
  height: 32px;
  padding: 0 14px;
  font-size: 13px;
  border: 1px solid transparent;
  border-radius: 7px;
  box-shadow: none;
}
.map-action--refresh.arco-btn {
  color: #0369a1;
  background: rgb(2 132 199 / 12%);
  border-color: rgb(2 132 199 / 34%);
}
.map-action--refresh.arco-btn:hover {
  color: #075985;
  background: rgb(2 132 199 / 20%);
  border-color: rgb(2 132 199 / 48%);
}
.map-action--reset.arco-btn {
  color: #b45309;
  background: rgb(217 119 6 / 13%);
  border-color: rgb(217 119 6 / 36%);
}
.map-action--reset.arco-btn:hover {
  color: #92400e;
  background: rgb(217 119 6 / 21%);
  border-color: rgb(217 119 6 / 50%);
}
.map-action--center.arco-btn {
  color: #047857;
  background: rgb(5 150 105 / 12%);
  border-color: rgb(5 150 105 / 34%);
}
.map-action--center.arco-btn:hover {
  color: #065f46;
  background: rgb(5 150 105 / 20%);
  border-color: rgb(5 150 105 / 48%);
}
.map-action--fit.arco-btn {
  font-weight: 600;
  color: var(--uvp-solid-text);
  background: var(--uvp-solid-bg);
  border-color: var(--uvp-solid-bg);
}
.map-action--fit.arco-btn:hover {
  background: var(--uvp-solid-hover-bg);
  border-color: var(--uvp-solid-hover-bg);
}

/* 上面几条按 class 定色，优先级高于 Arco 自己的 .arco-btn-disabled ⇒ 禁用态必须显式补。 */
.map-action.arco-btn:disabled,
.map-action.arco-btn.arco-btn-disabled {
  color: var(--uvp-text-tertiary);
  cursor: not-allowed;
  background: color-mix(in srgb, var(--uvp-text-tertiary) 10%, transparent);
  border-color: color-mix(in srgb, var(--uvp-text-tertiary) 26%, transparent);
  opacity: 0.65;
}

/* ── 「选取默认中心」的选点条 + 选点针 ────────────────────────────────
 * 选点是一个**临时模式**：按钮点下去之后界面必须明显变了个样子，否则用户以为自己
 * 还在浏览，随手拖两下地图就退出了，回头根本不知道刚才进了什么模式。
 * ⛔ 不能用弹窗/modal 做这条提示：它要挡在地图上，而这里恰恰要求用户去点地图。 */
.map-center-picker {
  position: absolute;
  top: 68px; /* 落在 14px 顶部工具条（44px 高）下面一点 */
  left: 50%;
  z-index: 3;
  display: flex;
  gap: 10px;
  align-items: center;
  padding: 7px 8px 7px 14px;
  font-size: 12px;
  color: #0f172a;
  white-space: nowrap;
  background: rgb(255 255 255 / 94%);
  border: 1px solid rgb(5 150 105 / 45%);
  border-radius: 999px;
  box-shadow: 0 6px 20px rgb(15 23 42 / 18%);
  backdrop-filter: blur(8px);
  transform: translateX(-50%);
}
.map-center-picker__hint {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  font-weight: 600;
  color: #047857;
}
.map-center-picker__coord {
  padding: 2px 8px;
  font:
    12px/1.2 ui-monospace,
    SFMono-Regular,
    Menlo,
    Monaco,
    Consolas,
    monospace;
  color: #334155;
  background: rgb(241 245 249 / 92%);
  border-radius: 6px;
}
.map-center-picker__keys {
  color: #64748b;
}
.map-center-picker .arco-btn {
  height: 26px;
  padding: 0 12px;
  font-size: 12px;
  border-radius: 999px;
}

/* 主按钮压成实心深绿（不是 --uvp-brand）：和工具条那颗「设为默认中心」同色系，
 * 白字对它 5.5:1 够读；用浅绿底会掉到 3.3:1。 */
.map-center-picker__confirm.arco-btn-primary {
  background: #047857;
  border-color: #047857;
}
.map-center-picker__confirm.arco-btn-primary:hover {
  background: #065f46;
  border-color: #065f46;
}

/* 「取消」也显式定色，不吃 Arco 的 secondary 默认底：
 * 这条浮层自带绿框白/深底，混进一颗纯白按钮会显得像别的组件掉进来了。 */
.map-center-picker__cancel.arco-btn {
  color: #475569;
  background: rgb(241 245 249 / 92%);
  border-color: rgb(148 163 184 / 45%);
}
.map-center-picker__cancel.arco-btn:hover {
  color: #334155;
  background: rgb(226 232 240 / 95%);
  border-color: rgb(148 163 184 / 60%);
}
.map-view.map-dark .map-center-picker {
  color: #e2e8f0;
  background: linear-gradient(105deg, rgb(12 30 48 / 96%), rgb(18 28 53 / 94%));
  border-color: rgb(52 211 153 / 55%);
  box-shadow: 0 6px 22px rgb(0 0 0 / 45%);
}
.map-view.map-dark .map-center-picker__hint {
  color: #6ee7b7;
}
.map-view.map-dark .map-center-picker__coord {
  color: #cbd5e1;
  background: rgb(148 163 184 / 18%);
}
.map-view.map-dark .map-center-picker__keys {
  color: #94a3b8;
}
.map-view.map-dark .map-center-picker__cancel.arco-btn {
  color: #cbd5e1;
  background: rgb(148 163 184 / 16%);
  border-color: rgb(148 163 184 / 34%);
}
.map-view.map-dark .map-center-picker__cancel.arco-btn:hover {
  color: #e2e8f0;
  background: rgb(148 163 184 / 26%);
  border-color: rgb(148 163 184 / 48%);
}

/* ── 选点针（水滴形）────────────────────────────────────────────────
 * ⛔ 元素是 document.createElement 造的、由 MapLibre 插进 .map-container，拿不到 data-v-*
 *    ⇒ 必须走 :deep()，否则退化成白底方块。
 *
 * ⛔⛔ 外层 `.map-center-pin` **绝对不能写 position / transform**：
 *    MapLibre 靠 `.maplibregl-marker{position:absolute;top:0;left:0}` + `transform: translate()` 摆标记，
 *    而本组件的样式表在 maplibre 之后加载、同优先级 ⇒ 外层只要写一句 `position: relative`
 *    就会把 `absolute` 顶掉，标记掉回普通文档流、被画布容器裁掉 ——
 *    表现就是"提示条和坐标都出来了，地图上却没有点"（2026-10-22 线上就这么挂的）。
 *    ⇒ 摆形状的活全部放进 SVG 里，外层只留 24×24 的盒子。
 *
 * 形状：内联 SVG 水滴，path 的尖端**精确落在 viewBox 底边 y=24**，
 *      再配合 `anchor: "bottom"` ⇒ 针尖与坐标点零偏移（实测 0.5px 内，属亚像素）。 */
:deep(.map-center-pin) {
  width: 24px;
  height: 24px;

  /* ⛔ 必须让点击穿透：选完点会 easeTo 到该点，针正好压在画面中心，
   *    它要是能收事件，用户想「再点一下往旁边挪挪」就点在针上、地图收不到这一下。 */
  pointer-events: none;
}

/* ⛔⛔ SVG 默认是 inline 元素，按文字基线排 —— 底下会多出 3~4px 的 descender 空隙，
 *    整个水滴被抬起来，尖端就对不上坐标点了（实测差 3px 多，肉眼几乎看不出来但一定偏）。
 *    必须 display:block 把它压回盒子底边。 */
:deep(.map-center-pin svg) {
  display: block;
}
:deep(.map-center-pin__body) {
  /* 用 filter 而不是 box-shadow：SVG 里 box-shadow 不生效 */
  filter: drop-shadow(0 2px 3px rgb(15 23 42 / 35%));
  fill: #059669;

  /* 白描边让水滴在浅底图（灰白路网）和深底图上都能"立"起来 */
  stroke: #ffffff;
  stroke-width: 1.6px;
}
:deep(.map-center-pin__dot) {
  fill: #ffffff;
}

/* 暗色底图上 #059669 偏闷，提亮一档才看得清。 */
.map-view.map-dark :deep(.map-center-pin__body) {
  filter: drop-shadow(0 2px 5px rgb(0 0 0 / 55%));
  fill: #10b981;
}

.map-view.map-dark .map-floating-actions {
  background: linear-gradient(105deg, rgb(12 30 48 / 96%), rgb(18 28 53 / 94%) 58%, rgb(12 52 61 / 92%));
  border-color: rgb(78 211 226 / 52%);
  box-shadow:
    0 0 0 1px rgb(96 165 250 / 12%),
    0 8px 26px rgb(0 0 0 / 42%),
    0 0 22px rgb(45 212 191 / 12%);
}
.map-view.map-dark .map-search-box {
  color: #7c8ea3;
  background: rgb(16 25 35 / 94%);
  border-color: #31465b;
}
.map-view.map-dark .map-search-box :deep(.arco-input),
.map-view.map-dark .map-search-box :deep(.arco-input::placeholder) {
  color: #a8b6c7;
}
.map-view.map-dark .map-search-box :deep(.arco-input-clear-icon) {
  color: #7c8ea3;
}
.map-view.map-dark .map-camera-count {
  color: #8ff7e5;
  background: linear-gradient(135deg, rgb(13 83 91 / 72%), rgb(30 64 126 / 68%));
  border-color: rgb(45 212 191 / 64%);
  box-shadow: inset 0 0 12px rgb(45 212 191 / 10%);
}

/* 暗色地图上的语义色：底色只是"那束光"，文字提亮到浅色一档才读得清。 */
.map-view.map-dark .map-action--refresh.arco-btn {
  color: #7dd3fc;
  background: rgb(56 189 248 / 16%);
  border-color: rgb(56 189 248 / 42%);
}
.map-view.map-dark .map-action--refresh.arco-btn:hover {
  color: #bae6fd;
  background: rgb(56 189 248 / 24%);
  border-color: rgb(56 189 248 / 58%);
}
.map-view.map-dark .map-action--reset.arco-btn {
  color: #fbbf24;
  background: rgb(251 191 36 / 16%);
  border-color: rgb(251 191 36 / 42%);
}
.map-view.map-dark .map-action--reset.arco-btn:hover {
  color: #fde68a;
  background: rgb(251 191 36 / 24%);
  border-color: rgb(251 191 36 / 58%);
}
.map-view.map-dark .map-action--center.arco-btn {
  color: #6ee7b7;
  background: rgb(52 211 153 / 16%);
  border-color: rgb(52 211 153 / 42%);
}
.map-view.map-dark .map-action--center.arco-btn:hover {
  color: #a7f3d0;
  background: rgb(52 211 153 / 24%);
  border-color: rgb(52 211 153 / 58%);
}
.map-view.map-dark .map-action--fit.arco-btn {
  color: var(--uvp-solid-text);
  background: var(--uvp-solid-bg);
  border-color: var(--uvp-solid-bg);
}
.map-position-readout {
  position: absolute;
  right: 14px;
  bottom: 14px;
  z-index: 2;
  display: flex;
  gap: 10px;
  align-items: center;
  padding: 8px 10px;
  font:
    12px/1.2 ui-monospace,
    SFMono-Regular,
    Menlo,
    Monaco,
    Consolas,
    monospace;
  color: #4b5563;
  background: rgb(255 255 255 / 92%);
  border: 1px solid rgb(148 163 184 / 35%);
  border-radius: 7px;
  box-shadow: 0 3px 12px rgb(15 23 42 / 13%);
  backdrop-filter: blur(8px);
}
.map-position-readout strong {
  font-weight: 700;
  color: #2563eb;
}
.map-view.map-dark .map-position-readout {
  color: #b8c7d8;
  background: linear-gradient(105deg, rgb(12 30 48 / 94%), rgb(18 28 53 / 94%));
  border-color: rgb(78 211 226 / 46%);
  box-shadow:
    0 0 0 1px rgb(96 165 250 / 10%),
    0 4px 18px rgb(0 0 0 / 38%);
}
.map-view.map-dark .map-position-readout strong {
  color: #8ff7e5;
}
.map-state {
  position: absolute;
  inset: 0;
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: center;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-list-panel-bg);
}
.map-state-error {
  padding: 24px;
  color: var(--uvp-danger);
  text-align: center;
}

/* ── 地图覆盖物（聚合气泡 / 通道标记点）──────────────────────────────
 * ⛔ 这两个 class 必须走 :deep()。
 * 元素是 **JS 动态 createElement、再由 MapLibre 插入 `.map-container` 内部** 的，
 * 而 scoped 样式靠 `data-v-*` 属性匹配 —— 动态创建的元素拿不到这个属性，
 * 裸选择器**一条都不生效**。失效的表现不是"样式差一点"，而是整个退回浏览器
 * 默认 button：白底黑字的方块（历史上地图上那个 "1" 就是它）。
 * ⚠️ 别把这两条搬回裸选择器，`index.runtime.test.ts` 有一条守卫盯着。
 */

/* ── 地图通道卡片（Popup 内容）────────────────────────────────────────
 * ⛔ 这些元素由 document.createElement 生成、塞进 MapLibre 的 popup，
 *    不在本组件的模板树里 ⇒ 必须用 :deep 才吃得到样式。
 */
:deep(.maplibregl-popup-content) {
  padding: 0;
  overflow: hidden;
  border-radius: 12px;
  box-shadow: 0 10px 28px rgb(15 23 42 / 18%);
}

/* popup 自带的关闭按钮默认钉死在 (top:0,right:0)，会正好压在卡片圆角上：
 * `overflow:hidden` + `border-radius:12px` 把它的左上角切掉一块，看起来像"被遮住了"。
 * ⇒ 内缩成 22px 圆形，并给标题行让出右侧空间（.map-channel-card__head 的 padding-right）。 */
:deep(.maplibregl-popup-close-button) {
  top: 6px;
  right: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  padding: 0;
  font-size: 15px;
  line-height: 1;
  color: #86909c;
  border-radius: 50%;
  transition:
    background-color 0.15s ease,
    color 0.15s ease;
}

:deep(.maplibregl-popup-close-button:hover) {
  color: #1d2129;
  background: #f2f3f5;
}

:deep(.map-channel-card) {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 220px;
  padding: 12px;
  background: #ffffff;
}

:deep(.map-channel-card__head) {
  display: flex;
  gap: 6px;
  align-items: center;
  min-width: 0;

  /* 给右上角那颗关闭按钮让位，否则标题一长就钻到 × 底下 */
  padding-right: 20px;
}

:deep(.map-channel-card__dot) {
  flex: 0 0 auto;
  width: 8px;
  height: 8px;
  background: #a9aeb8;
  border-radius: 50%;
}

:deep(.map-channel-card__dot.online) {
  background: #00b42a;
  box-shadow: 0 0 0 3px rgb(0 180 42 / 16%);
}

:deep(.map-channel-card__name) {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 13px;
  font-weight: 600;
  color: #1d2129;
  white-space: nowrap;
}

:deep(.map-channel-card__meta) {
  font-size: 11px;
  line-height: 1.5;
  color: #86909c;
  word-break: break-all;
}

:deep(.map-channel-card__actions) {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px;
  margin-top: 2px;
}

:deep(.map-channel-card__action) {
  height: 28px;
  font-size: 12px;
  color: #1d2129;
  cursor: pointer;
  background: #f8fbff;
  border: 1px solid #dce6f2;
  border-radius: 6px;
  transition: background-color 0.15s ease;
}

:deep(.map-channel-card__action:hover:not(:disabled)) {
  background: #eaf2ff;
  border-color: #b7d3ff;
}

:deep(.map-channel-card__action:disabled) {
  color: #c9cdd4;
  cursor: not-allowed;
  background: #f7f8fa;
}

/* 这四颗是**原生 `<button>`**，焦点环走浏览器默认的 `-webkit-focus-ring-color`
 * （Chrome 下是 1px `rgb(0 95 204)`），压在深色卡片上像一圈没清干净的描边。
 * 统一成项目里其它按钮那套「2px 品牌色 + 1px 外扩」，键盘可达性不掉。
 * ⚠️ `:focus` 与 `:focus-visible` 同优先级，后者必须写在后面才能盖掉前者。 */
:deep(.map-channel-card__action:focus) {
  outline: none;
}

:deep(.map-channel-card__action:focus-visible) {
  outline: 2px solid var(--uvp-brand, #2563eb);
  outline-offset: 1px;
}

/* ── 卡片动作按钮：四个动作四种颜色 ────────────────────────────────────
 * 原来只有「播放视频」是实心蓝，另外三个都是同一套白底描边，看着像三个等价按钮，
 * 实际语义差很远：播放=主行动、轨迹=查看历史、设置位置=**写数据**、详情=纯导航。
 * 配色沿用工具条那套语义：
 *   播放 = 实心主色（唯一主行动，保持焦点）
 *   轨迹 = 紫（历史/回放语义）
 *   设置位置 = 绿（写配置，和「设为默认中心」同一族）
 *   通道详情 = 中性灰（只是跳转，不改变任何状态）
 * ⛔ 浅底一律「同色文字 + 低透明同色底 + 同色描边」：四颗实心饱和色并排像调色板，
 *    而且白字压紫/绿只有 4:1 上下。 */
:deep(.map-channel-card__action--play) {
  color: #ffffff;
  background: var(--uvp-solid-bg, #2563eb);
  border-color: var(--uvp-solid-bg, #2563eb);
}

:deep(.map-channel-card__action--play:hover:not(:disabled)) {
  background: var(--uvp-solid-hover-bg, #1d4ed8);
  border-color: var(--uvp-solid-hover-bg, #1d4ed8);
}

:deep(.map-channel-card__action--play:disabled) {
  color: #ffffff;
  background: #c9cdd4;
  border-color: #c9cdd4;
}

:deep(.map-channel-card__action--trajectory) {
  color: #6d28d9;
  background: rgb(124 58 237 / 8%);
  border-color: rgb(124 58 237 / 30%);
}

:deep(.map-channel-card__action--trajectory:hover:not(:disabled)) {
  color: #5b21b6;
  background: rgb(124 58 237 / 16%);
  border-color: rgb(124 58 237 / 46%);
}

:deep(.map-channel-card__action--location) {
  color: #047857;
  background: rgb(5 150 105 / 9%);
  border-color: rgb(5 150 105 / 30%);
}

:deep(.map-channel-card__action--location:hover:not(:disabled)) {
  color: #065f46;
  background: rgb(5 150 105 / 17%);
  border-color: rgb(5 150 105 / 46%);
}

:deep(.map-channel-card__action--detail) {
  color: #475569;
  background: rgb(100 116 139 / 8%);
  border-color: rgb(100 116 139 / 26%);
}

:deep(.map-channel-card__action--detail:hover:not(:disabled)) {
  color: #334155;
  background: rgb(100 116 139 / 15%);
  border-color: rgb(100 116 139 / 40%);
}

/* ⛔ 按类定的色优先级高于上面那条通用的 `.map-channel-card__action:disabled`，
 *    禁用态必须逐个显式补，否则无权限时按钮还是彩色的（看着像能点）。 */
:deep(.map-channel-card__action--trajectory:disabled),
:deep(.map-channel-card__action--location:disabled),
:deep(.map-channel-card__action--detail:disabled) {
  color: #c9cdd4;
  background: #f7f8fa;
  border-color: #e5e6eb;
}

/* 深色底图下的卡片 */
:deep(.map-dark .map-channel-card) {
  background: #1f2733;
}

:deep(.map-dark .map-channel-card__name) {
  color: #e5e6eb;
}

:deep(.map-dark .map-channel-card__action) {
  color: #e5e6eb;
  background: #2a3442;
  border-color: #3a4657;
}

:deep(.map-dark .map-channel-card__action:hover:not(:disabled)) {
  background: #33404f;
}

/* 深色卡片上，通用规则会把四颗按钮压成同一种深灰 ⇒ 必须逐个再提亮一遍。
 * ⚠️ 这几条要放在上面两条之后：选择器都是 (0,2,0)，靠顺序取胜。 */
:deep(.map-dark .maplibregl-popup-close-button) {
  color: #86909c;
}

:deep(.map-dark .maplibregl-popup-close-button:hover) {
  color: #e5e6eb;
  background: #2a3442;
}

:deep(.map-dark .map-channel-card__action--play) {
  color: var(--uvp-solid-text, #ffffff);
  background: var(--uvp-solid-bg, #2563eb);
  border-color: var(--uvp-solid-bg, #2563eb);
}

:deep(.map-dark .map-channel-card__action--play:hover:not(:disabled)) {
  background: var(--uvp-solid-hover-bg, #1d4ed8);
  border-color: var(--uvp-solid-hover-bg, #1d4ed8);
}

:deep(.map-dark .map-channel-card__action--trajectory) {
  color: #c4b5fd;
  background: rgb(139 92 246 / 18%);
  border-color: rgb(196 181 253 / 34%);
}

:deep(.map-dark .map-channel-card__action--trajectory:hover:not(:disabled)) {
  color: #ddd6fe;
  background: rgb(139 92 246 / 28%);
  border-color: rgb(196 181 253 / 52%);
}

:deep(.map-dark .map-channel-card__action--location) {
  color: #6ee7b7;
  background: rgb(16 185 129 / 16%);
  border-color: rgb(110 231 183 / 34%);
}

:deep(.map-dark .map-channel-card__action--location:hover:not(:disabled)) {
  color: #a7f3d0;
  background: rgb(16 185 129 / 26%);
  border-color: rgb(110 231 183 / 52%);
}

:deep(.map-dark .map-channel-card__action--detail) {
  color: #cbd5e1;
  background: rgb(148 163 184 / 14%);
  border-color: rgb(148 163 184 / 30%);
}

:deep(.map-dark .map-channel-card__action--detail:hover:not(:disabled)) {
  color: #e2e8f0;
  background: rgb(148 163 184 / 24%);
  border-color: rgb(148 163 184 / 46%);
}

/* 禁用态同样要压回去（否则深色下无权限的按钮仍是彩色的） */
:deep(.map-dark .map-channel-card__action--trajectory:disabled),
:deep(.map-dark .map-channel-card__action--location:disabled),
:deep(.map-dark .map-channel-card__action--detail:disabled),
:deep(.map-dark .map-channel-card__action--stop:disabled) {
  color: #5c6675;
  background: #252f3c;
  border-color: #333d4c;
}

/* ⛔ 实心那两颗（播放视频 / 播放控制台）必须单独补深色禁用态：
 *    `:deep(.map-dark .x--play)` 与 `:deep(.x--play:disabled)` 是**同优先级**（都是 3 个
 *    类级选择器），深色块写在后面 ⇒ 深色实心蓝会把浅色的禁用灰盖掉，
 *    表现为"没权限/离线时按钮还是亮蓝色、看着能点"。 */
:deep(.map-dark .map-channel-card__action--play:disabled),
:deep(.map-dark .map-channel-card__action--console:disabled) {
  color: #5c6675;
  background: #252f3c;
  border-color: #333d4c;
}

/* 深色卡片底下的小三角默认是**白的**（maplibre 把 `#fff` 写死在按锚点分的规则里），
 * 配深色卡片像贴了个白标签。
 * ⛔ 三角是「10px transparent 边框 + 只染一条边」做出来的，
 *    所以只能改**对应锚点方向的那一条边**；四条一起改会把它变成实心方块。 */
:deep(.map-dark .maplibregl-popup-anchor-bottom .maplibregl-popup-tip),
:deep(.map-dark .maplibregl-popup-anchor-bottom-left .maplibregl-popup-tip),
:deep(.map-dark .maplibregl-popup-anchor-bottom-right .maplibregl-popup-tip) {
  border-top-color: #1f2733;
}

:deep(.map-dark .maplibregl-popup-anchor-top .maplibregl-popup-tip),
:deep(.map-dark .maplibregl-popup-anchor-top-left .maplibregl-popup-tip),
:deep(.map-dark .maplibregl-popup-anchor-top-right .maplibregl-popup-tip) {
  border-bottom-color: #1f2733;
}

:deep(.map-dark .maplibregl-popup-anchor-left .maplibregl-popup-tip) {
  border-right-color: #1f2733;
}

:deep(.map-dark .maplibregl-popup-anchor-right .maplibregl-popup-tip) {
  border-left-color: #1f2733;
}

:deep(.map-cluster-marker),
:deep(.map-channel-marker) {
  box-sizing: border-box;
  display: grid;
  place-items: center;
  padding: 0;
  font: inherit;
  cursor: pointer;
  border: 0;

  /* transform 由 MapLibre 每帧定位，禁止过渡或缩放此节点。 */
  transition:
    box-shadow 140ms ease,
    border-color 140ms ease;
}

/* 深绿玻璃：保留底图路网，同时让聚合点在复杂底图上有足够存在感。 */
:deep(.map-cluster-marker) {
  /* ⛔ 兜底不能省：百分比一旦非法（变量没拿到 / 写成 67 而不是 67%），
   *    整条 `background` 声明会作废 ⇒ 气泡变成一颗看不见字的空壳。 */
  --cluster-rate: 0%;
  --cluster-tint-offline: rgb(12 98 66 / 58%);
  --cluster-tint-online: rgb(0 151 92 / 64%);

  width: var(--cluster-size, 40px);
  height: var(--cluster-size, 40px);
  font-size: var(--cluster-font-size, 13px);
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  line-height: 1;
  color: #ffffff;

  /* 数字保持不透明，阴影加强底图路网重叠时的可读性。 */
  text-shadow: 0 1px 2px rgb(0 30 20 / 68%);
  background:
    radial-gradient(120% 120% at 50% 6%, rgb(255 255 255 / 9%) 0%, rgb(255 255 255 / 1%) 30%, rgb(255 255 255 / 0%) 74%),
    color-mix(in srgb, var(--cluster-tint-online) var(--cluster-rate), var(--cluster-tint-offline));
  border: 1.5px solid rgb(150 255 207 / 88%);
  border-radius: 50%;
  box-shadow:
    0 0 0 1px rgb(5 68 43 / 58%),
    0 3px 9px rgb(4 30 20 / 32%),
    inset 0 1px 1px rgb(255 255 255 / 35%),
    inset 0 -5px 9px rgb(5 76 49 / 28%);
  backdrop-filter: blur(0.6px) saturate(145%);
}

/* 暗色底图：浅色数字和深青填充保证辨识度。
 * ⚠️ 这里按实际底图选择器匹配，而不是跟随系统主题；用户可在浅色系统主题中选择深色底图。
 *    markers 是 MapLibre 插进 `.map-container` 的，选择器必须从 `.map-view` 开始。 */
.map-view[data-basemap="dark"] :deep(.map-cluster-marker) {
  --cluster-tint-offline: rgb(4 72 50 / 70%);
  --cluster-tint-online: rgb(0 126 80 / 74%);

  color: #ffffff;
  text-shadow: 0 1px 2px rgb(0 0 0 / 48%);
  background:
    radial-gradient(120% 120% at 50% 6%, rgb(255 255 255 / 7%) 0%, rgb(255 255 255 / 0%) 42%, rgb(255 255 255 / 0%) 74%),
    color-mix(in srgb, var(--cluster-tint-online) var(--cluster-rate), var(--cluster-tint-offline));
  border-color: rgb(142 255 198 / 92%);
  box-shadow:
    0 0 0 1px rgb(8 104 68 / 58%),
    0 0 12px color-mix(in srgb, rgb(0 170 108 / 38%) var(--cluster-rate), rgb(16 94 66 / 30%)),
    0 4px 12px rgb(0 4 12 / 38%),
    inset 0 1px 1px rgb(215 255 237 / 28%),
    inset 0 -5px 9px rgb(0 44 30 / 34%);
}

:deep(.map-cluster-marker:hover) {
  border-color: rgb(255 255 255 / 78%);
  box-shadow:
    0 0 0 1px rgb(20 100 66 / 26%),
    0 5px 12px rgb(4 30 20 / 22%),
    inset 0 1px 1px rgb(255 255 255 / 56%),
    inset 0 -5px 9px rgb(12 90 60 / 14%);
}

/* 暗色 hover 保留绿色辉光（上面那条 hover 会把辉光整条覆盖掉）。 */
.map-view[data-basemap="dark"] :deep(.map-cluster-marker:hover) {
  box-shadow:
    0 0 0 1px rgb(10 138 89 / 70%),
    0 0 16px color-mix(in srgb, rgb(0 190 122 / 48%) var(--cluster-rate), rgb(20 116 78 / 38%)),
    0 5px 14px rgb(0 4 12 / 42%),
    inset 0 1px 1px rgb(215 255 237 / 38%),
    inset 0 -5px 9px rgb(0 44 30 / 36%);
}

:deep(.map-cluster-marker:focus-visible),
:deep(.map-channel-marker:focus-visible) {
  outline: 2px solid var(--uvp-brand-cyan);
  outline-offset: 2px;
}

/* 通道标记点：圆形摄像头图标，避免水滴形状与视频通道语义不匹配。
 * 在线用青绿色，离线用深灰蓝；亮色描边和阴影保证两套底图上都能看清。 */
:deep(.map-channel-marker) {
  width: 28px;
  height: 28px;
  background: #475569;
  border: 2px solid rgb(255 255 255 / 86%);
  border-radius: 50%;
  box-shadow:
    0 0 0 1px rgb(4 23 35 / 35%),
    0 2px 5px rgb(0 0 0 / 34%),
    inset 0 1px 1px rgb(255 255 255 / 24%);
}

:deep(.map-channel-marker:hover) {
  border-color: rgb(255 255 255 / 100%);
}

:deep(.map-channel-marker.online) {
  color: #ffffff;
  background: rgb(8 132 117 / 94%);
  border-color: rgb(178 255 238 / 92%);
}

:deep(.map-channel-pip) {
  display: grid;
  place-items: center;
  width: 16px;
  height: 16px;
  color: rgb(255 255 255 / 92%);
}

:deep(.map-channel-pip svg) {
  display: block;
}
:deep(.maplibregl-ctrl-bottom-left) {
  right: 0;
  bottom: 14px;
  left: 0;
  display: flex;
  justify-content: center;
  pointer-events: none;
}
:deep(.maplibregl-ctrl-scale) {
  position: static;
  margin: 0;
  color: #4b5563;
  text-align: center;
  pointer-events: auto;
  background: rgb(255 255 255 / 88%);
  border-color: #6b7280;
  border-radius: 3px;
  box-shadow: 0 2px 8px rgb(15 23 42 / 12%);
}
.map-view.map-dark :deep(.maplibregl-ctrl-scale) {
  color: #b8c7d8;
  background: rgb(12 30 48 / 94%);
  border-color: #59c7d3;
  box-shadow:
    0 0 10px rgb(45 212 191 / 18%),
    0 3px 12px rgb(0 0 0 / 32%);
}

/* The map occupies the full content area; overlays are DOM markers. */
.map-view > .map-container {
  inset: 0;
}
.drawer-body {
  display: grid;
  gap: 14px;
  padding-bottom: 8px;
}
.drawer-headline {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  gap: 10px;
  align-items: center;
  padding: 14px 16px;
  background: linear-gradient(180deg, var(--uvp-list-panel-bg) 0%, var(--uvp-panel-bg) 100%);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 12px;
}
.drawer-headline.warning {
  border-color: var(--uvp-warning-border);
}
.drawer-headline h3 {
  margin: 0;
  color: var(--uvp-text-primary);
}
.drawer-headline p {
  margin: 2px 0 0;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.kv-grid {
  display: grid;
  grid-template-columns: 92px minmax(0, 1fr);
  gap: 10px 12px;
}
.kv-grid span {
  color: var(--uvp-text-tertiary);
}
.kv-grid strong {
  color: var(--uvp-text-primary);
  overflow-wrap: anywhere;
}
.mount-list {
  display: grid;
  gap: 8px;
}
.mount-item {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 10px;
  align-items: center;
  padding: 10px;
  background: var(--uvp-list-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 10px;
}
.mount-item span {
  display: block;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.timeline-strip {
  display: grid;
  grid-template-columns: repeat(48, minmax(3px, 1fr));
  gap: 3px;
}
.timeline-strip span {
  height: 24px;
  background: var(--uvp-panel-border);
  border-radius: 999px;
}
.timeline-strip span.online {
  background: var(--uvp-brand-cyan);
}

/* ============ 详情抽屉 · 增强(2026-07-18) ============ */

/* 注：.drawer-body 的 padding-bottom: 8px 已并入上方主定义（同名选择器不允许重复）。 */

/* ============ 设备详情抽屉 · 只读概览 ============
   设备详情只回答设备当前状态和接入信息；通道级控制、配置和设备维护从这里分离。 */

/* 档案条只承载设备身份与在线状态，操作入口位于设备卡片或通道操作抽屉。 */
.device-idbar {
  display: grid;
  grid-template-columns: 36px minmax(0, 1fr) auto;
  gap: 12px;
  align-items: start;
  padding: 10px 12px;
  margin-bottom: 12px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 10px;
}
.device-idbar .idbar-icon {
  display: grid;
  place-items: center;
  width: 36px;
  height: 36px;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-radius: 10px;
}
.device-idbar .idbar-main {
  min-width: 0;
}
.device-idbar .idbar-main h3 {
  margin: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 17px;
  font-weight: 650;
  line-height: 1.35;
  color: var(--uvp-text-primary);
  white-space: nowrap;
}
.device-idbar .idbar-alt {
  margin: 2px 0 0;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.device-idbar .idbar-code {
  display: flex;
  gap: 4px;
  align-items: center;
  margin: 2px 0 0;
  font-size: 13px;
  color: var(--uvp-text-tertiary);
}
.device-idbar .idbar-sum {
  margin: 3px 0 0;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 13px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}

/* 概览随抽屉滚动，内容不再另设滚动区。 */
.device-seg {
  display: grid;
  gap: 18px;
}
.device-readonly {
  gap: 18px;
}
.channel-operation-entry {
  display: grid;
  gap: 14px;
}
.channel-operation-context {
  display: grid;
  gap: 3px;
  padding: 10px 12px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
}
.channel-operation-context strong {
  color: var(--uvp-text-primary);
}
.channel-operation-context span {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}
.channel-operation-tabs {
  width: fit-content;
  max-width: 100%;
}

/* 概览:分区标题 + 细分隔线,**不画卡片框** —— 原「设备信息」页是 5 个等权重
   `.info-group` 白盒子,没有视觉层级(P1-3)。 */
.seg-sect + .seg-sect {
  padding-top: 12px;
  border-top: 1px solid var(--uvp-border-subtle);
}
.seg-head {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 6px;
}
.seg-head b {
  font-size: 13px;
  font-weight: 650;
  color: var(--uvp-text-secondary);
  letter-spacing: 0.02em;
}
.seg-head .seg-right {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  margin-left: auto;
}

/* `.seg-diag` 现在是 `a-badge`(`:status` + `:text`):圆点颜色与文字色都由框架的语义色给
   (在线=success / 离线=normal),"已注册/未注册"的文案也由它渲染。
   ⛔ 不再自己画 `i` 圆点 —— 那样离线态的点色要自己维护,还会跟别处的 `a-badge` 不一致。
   这里只把字号降到本段的小字规格。 */
.seg-head .seg-cnt {
  font-size: 12px;
  color: var(--uvp-text-secondary);
}
.seg-head .seg-diag {
  font-size: 12px;
}
.seg-rows {
  display: grid;
  grid-template-columns: 62px minmax(0, 1fr);
  gap: 6px 10px;
  align-items: baseline;
}
.seg-rows .k {
  font-size: 13px;
  font-weight: 550;
  color: var(--uvp-text-secondary);
}
.seg-rows .v {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: baseline;
  font-size: 14px;
  color: var(--uvp-text-primary);
}
.seg-rows .sub {
  font-size: 13px;
  color: var(--uvp-text-tertiary);
}

/* 在线率条 = `a-progress`(`size="small"` + `:show-text="false"`),轨道与填充色都是框架的,
   这里只限宽、并让它跟右侧百分号**居中对齐** ——
   进度条没有文字基线,跟着 `.v` 的 `align-items: baseline` 会被抬到字上方。 */
.seg-rate {
  display: inline-block;
  width: 160px;
  max-width: min(160px, 42vw);
}
.seg-rate :deep(.arco-progress-line) {
  height: 6px;
  background: var(--uvp-border-subtle);
}
.seg-rate :deep(.arco-progress-line-bar) {
  background: var(--uvp-success);
}
.seg-rows .seg-rate-row {
  align-items: center;
}

/* 注：.drawer-headline 的 padding / background 增强值已并入上方主定义（同名选择器不允许重复）。 */
.drawer-headline .drawer-icon {
  width: 36px;
  height: 36px;
  border-radius: 10px;
}
.drawer-headline .headline-title {
  min-width: 0;
}
.drawer-headline .headline-title h3 {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 15px;
  font-weight: 650;
  line-height: 1.35;
  white-space: nowrap;
}
.drawer-headline .headline-title p {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.drawer-headline .headline-title .headline-alt {
  display: block;
  margin-top: 2px;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.drawer-headline .headline-title .headline-alt .muted {
  margin-right: 2px;
  color: var(--uvp-text-tertiary);
  opacity: 0.7;
}

/* 注：.status-pill 的 gap / min-width / padding 增强值已并入上方主定义（同名选择器不允许重复）。 */
.status-pill .status-dot {
  width: 6px;
  height: 6px;
  background: currentColor;
  border-radius: 999px;
  opacity: 0.75;
}
.status-pill.online .status-dot {
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--uvp-brand-cyan) 18%, transparent);
  opacity: 1;
}
.status-event-current-status {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
  border-color: var(--uvp-danger-border);
}
.status-event-current-status.online {
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 12%, transparent);
  border-color: color-mix(in srgb, var(--uvp-brand-cyan) 28%, transparent);
}
.status-event-current-status .status-dot {
  position: relative;
  box-shadow: 0 0 0 3px color-mix(in srgb, currentColor 14%, transparent);
  opacity: 1;
}
.status-event-current-status .status-dot::after {
  position: absolute;
  inset: -1px;
  content: "";
  border: 1px solid currentColor;
  border-radius: 50%;
  animation: status-event-ripple 1.8s cubic-bezier(0.2, 0.7, 0.3, 1) infinite;
}

@keyframes status-event-ripple {
  0% {
    opacity: 0.55;
    transform: scale(0.8);
  }
  72%,
  100% {
    opacity: 0;
    transform: scale(3.2);
  }
}

@media (prefers-reduced-motion: reduce) {
  .status-event-current-status .status-dot::after {
    animation: none;
  }
}
.status-event-body {
  display: grid;
  gap: 12px;
}
.status-event-summary {
  display: flex;
  gap: 16px;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 10px;
}
.status-event-device {
  display: flex;
  gap: 10px;
  align-items: center;
  min-width: 0;
}
.status-event-device > div {
  display: grid;
  gap: 2px;
  min-width: 0;
}
.status-event-device strong,
.status-event-device span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.status-event-device strong {
  color: var(--uvp-text-primary);
}
.status-event-device span {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.status-event-device .runtime-eyebrow {
  font-size: 11px;
  font-weight: 650;
  color: var(--uvp-brand);
  letter-spacing: 0.04em;
}
.summary-icon {
  display: inline-grid;
  flex: 0 0 32px;
  place-items: center;
  width: 32px;
  height: 32px;
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 12%, transparent);
  border-radius: 8px;
}
.runtime-overview {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
}
.runtime-overview-card {
  display: grid;
  gap: 6px;
  min-width: 0;
  padding: 11px 12px;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 9px;
}
.runtime-overview-card span {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.runtime-overview-card strong {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 13px;
  font-weight: 650;
  color: var(--uvp-text-primary);
  white-space: nowrap;
}
.runtime-overview-card strong.online {
  color: var(--uvp-brand-cyan);
}
.runtime-tabs {
  min-width: 0;
}
.runtime-tabs :deep(.arco-tabs-content) {
  padding-top: 2px;
}
.runtime-tab-title {
  display: inline-flex;
  gap: 6px;
  align-items: center;
}
.status-event-section-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 50px;
  padding: 4px 10px 8px;
  border-bottom: 1px solid var(--uvp-panel-border);
}
.status-event-section-head > div {
  display: grid;
  gap: 2px;
}
.status-event-section-head strong {
  font-size: 13px;
  color: var(--uvp-text-primary);
}
.status-event-section-head span {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.runtime-filter {
  display: grid;
  grid-template-columns: 72px minmax(220px, 440px);
  gap: 12px;
  align-items: start;
  min-height: 66px;
  padding: 8px 0;
  border-bottom: 1px solid var(--uvp-panel-border);
}
.runtime-filter > span {
  padding-top: 8px;
  font-size: 12px;
  font-weight: 600;
  color: var(--uvp-text-secondary);
}
.runtime-filter-control {
  display: grid;
  gap: 4px;
}
.runtime-filter-control small {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.runtime-channel-state {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  min-height: 40px;
  padding: 7px 10px;
  font-size: 12px;
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
  border: 1px solid var(--uvp-warning-border);
  border-radius: 8px;
}
.runtime-channel-state.error {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
  border-color: var(--uvp-danger-border);
}
.status-event-scroll {
  min-height: 180px;
  max-height: clamp(180px, calc(100vh - 350px), 440px);
  padding-right: 6px;
  overflow-y: auto;
  scrollbar-gutter: stable;
  overscroll-behavior: contain;
}

@media (width <= 768px) {
  .runtime-filter {
    grid-template-columns: 1fr;
    gap: 4px;
    padding: 8px 0;
  }
  .runtime-filter > span {
    padding-top: 0;
  }
  .runtime-overview {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
.status-event-content {
  display: block;
  min-height: 180px;
}
.status-event-timeline {
  padding: 10px 12px 0;
}
.status-event-timeline :deep(.arco-timeline-item) {
  min-height: 64px;
  padding-left: 8px;
}
.status-event-timeline :deep(.arco-timeline-item-dot-wrapper .arco-timeline-item-dot-content) {
  width: 10px;
}
.status-event-timeline :deep(.arco-timeline-item-dot) {
  width: 10px;
  height: 10px;
  border-width: 2px;
}
.status-event-timeline :deep(.arco-timeline-item-dot-line) {
  left: 5px;
  border-color: var(--uvp-panel-border);
  border-left-width: 1px;
}
.status-event-timeline :deep(.arco-timeline-item-content-wrapper) {
  margin-left: 22px;
}
.status-event-timeline :deep(.arco-timeline-item-content) {
  margin-bottom: 2px;
  line-height: 20px;
}
.status-event-timeline :deep(.arco-timeline-item-label) {
  font-size: 12px;
  line-height: 18px;
  color: color-mix(in srgb, var(--uvp-text-tertiary) 82%, var(--uvp-brand));
}
.status-event-item {
  min-width: 0;
  font-size: 13px;
  font-weight: 600;
  line-height: 20px;
  color: var(--uvp-text-primary);
  cursor: help;
}
.status-event-load-more {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 38px;
  padding: 8px 12px;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
  text-align: center;
  border-top: 1px solid var(--uvp-panel-border);
}
.status-event-load-more > span {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  justify-content: center;
}
.status-event-load-more .error {
  color: var(--uvp-danger);
}
.status-event-load-more button {
  padding: 0;
  font: inherit;
  color: var(--uvp-brand);
  cursor: pointer;
  background: transparent;
  border: 0;
}
.status-event-load-more button:hover {
  color: var(--uvp-brand-strong);
}
.status-event-state {
  display: grid;
  gap: 8px;
  place-items: center;
  min-height: 180px;
  color: var(--uvp-text-tertiary);
  text-align: center;
}
.status-event-state.error strong {
  color: var(--uvp-danger);
}

@media (width <= 720px) {
  .device-idbar {
    grid-template-columns: 36px minmax(0, 1fr);
  }
  .status-event-summary {
    align-items: flex-start;
  }
  .runtime-overview {
    grid-template-columns: 1fr;
  }
  .status-event-scroll {
    max-height: clamp(180px, calc(100vh - 470px), 360px);
  }
}

.info-group {
  display: grid;
  gap: 10px;
  padding: 12px 14px 14px;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 12px;
}
.info-group.meta-group {
  background: var(--uvp-list-toolbar-bg);
}
.info-group .group-label {
  font-size: 11px;
  font-weight: 620;
  color: var(--uvp-text-tertiary);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.field-grid {
  display: grid;
  grid-template-columns: 92px minmax(0, 1fr);
  gap: 10px 14px;
  align-items: center;
  font-size: 13px;
}
.field-grid .k {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.field-grid .v {
  color: var(--uvp-text-primary);
  overflow-wrap: anywhere;
}
.field-grid .v.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, "PingFang SC", monospace;
}
.field-grid .v .muted {
  margin-left: 4px;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.field-grid .v .mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}

/* 设备上报属性区块:把「未上报」与「值为 0」在视觉上区分开(未上报用弱化斜体),
   并把版本独有属性的归属(仅 2016 / 仅 2022)标出来,便于一眼分辨设备报的是哪一版形态。 */
.catalog-attr-group .group-label {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: space-between;
}
.catalog-attr-group .attr-shape {
  padding: 1px 7px;
  font-size: 10px;
  font-weight: 500;
  color: var(--uvp-text-secondary);
  text-transform: none;
  letter-spacing: 0;
  background: var(--uvp-shell-muted);
  border-radius: 999px;
}
.field-grid .k .attr-scope {
  margin-left: 4px;
  font-size: 10px;
  font-style: normal;
  color: var(--uvp-text-tertiary);
  opacity: 0.8;
}
.field-grid .v.unreported {
  font-style: italic;
  color: var(--uvp-text-tertiary);
}
.inline-dot {
  display: inline-block;
  width: 6px;
  height: 6px;
  margin-right: 6px;
  vertical-align: middle;
  background: var(--uvp-text-tertiary);
  border-radius: 999px;
}
.inline-dot.online {
  background: var(--uvp-brand-cyan);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--uvp-brand-cyan) 18%, transparent);
}

/* 「复制编码」= 框架的 `a-button type="text" size="mini"`。
   ⛔ 只把它收成 20×20 的**纯图标**按钮:mini 尺寸仍带 `padding: 0 8px` / `height: 24px`,
      放进档案条那行 12px 的编码里会把行高撑开、还多出一截空白。 */
.copy-btn {
  width: 20px;
  height: 20px;
  padding: 0;
  margin-left: 2px;
  color: var(--uvp-text-tertiary);
}
.copy-btn:hover {
  color: var(--uvp-brand);
}
.subscription-summary {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.subscription-chip {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  padding: 3px 10px;
  font-size: 12px;
  font-weight: 600;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 999px;
}
.subscription-chip strong {
  font-weight: 620;
}
.subscription-chip.status-active {
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent);
  border-color: color-mix(in srgb, var(--uvp-brand-cyan) 26%, transparent);
}
.subscription-chip.status-pending,
.subscription-chip.status-expired {
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
  border-color: var(--uvp-warning-border);
}
.subscription-chip.status-degraded {
  color: var(--uvp-danger);
  background: color-mix(in srgb, var(--uvp-danger) 8%, transparent);
  border-color: color-mix(in srgb, var(--uvp-danger) 24%, transparent);
}
.channel-summary {
  display: grid;
  gap: 10px;
}
.channel-stats {
  display: grid;
  grid-template-columns: 1fr auto 1fr auto 1fr;
  align-items: center;
  padding: 10px 4px;
  background: var(--uvp-list-toolbar-bg);
  border-radius: 10px;
}
.channel-stats .stat-item {
  display: grid;
  gap: 2px;
  place-items: center;
}
.channel-stats .stat-num {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 22px;
  font-weight: 650;
  line-height: 1.1;
  color: var(--uvp-text-primary);
}
.channel-stats .stat-label {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.channel-stats .stat-divider {
  width: 1px;
  height: 24px;
  background: var(--uvp-panel-border);
}
.online-progress .progress-track {
  height: 6px;
  overflow: hidden;
  background: var(--uvp-panel-border);
  border-radius: 999px;
}
.online-progress .progress-fill {
  height: 100%;
  background: linear-gradient(90deg, var(--uvp-brand) 0%, var(--uvp-brand-cyan) 100%);
  border-radius: 999px;
  transition: width 0.4s ease-out;
}

/* ⛔ `flex-wrap: wrap` 不是排版偏好，是**防横向滚动条**的必需项，别删。
   抽屉里这块是 `.arco-drawer-body`(overflow:auto) > `a-spin`(`.arco-spin` 是 `display:inline-block`
   —— 宽度按**收缩到适应**算,下限就是内容的 min-content)。一整排不换行的按钮,min-content
   等于"所有按钮宽度之和",设备详情最多 6 个按钮(固件升级/维护记录/重启设备/管理订阅/刷新目录/复制编码,
   每个还带 14px 图标)约需 700px,而 640 宽的抽屉扣掉内边距只剩 608px ⇒ 整个 `.arco-spin`
   被撑到 700px,抽屉就长出横向滚动条。
   实测(无头 Chrome,640 宽抽屉):6 按钮 = 700px 超宽 92px;5 按钮 = 582px 放得下;
   加上本行后 6 按钮回落到一行放不下就换行,横向溢出归零。 */
.drawer-foot {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding: 12px 0 4px;
  margin-top: 4px;
  border-top: 1px solid var(--uvp-panel-border);
}
.drawer-foot .arco-btn {
  flex: 0 0 auto;
}
.drawer-foot .arco-btn-primary {
  flex: 1;
}

.input-counter {
  display: inline-flex;
  align-items: center;
  min-height: 22px;
  padding: 2px 8px;
  margin-right: -4px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  font-weight: 600;
  line-height: 1;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 6px;
}
.input-counter.done {
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 12%, transparent);
  border-color: color-mix(in srgb, var(--uvp-brand-cyan) 28%, transparent);
}
.form-hint {
  font-size: 12px;
  line-height: 1.4;
  color: var(--uvp-text-tertiary);
}
.form-hint-error {
  color: var(--uvp-danger);
}

/* 通道坐标三路来源的标注（目录 / 实时 / 人工），列表与详情卡共用 */
.coord-inputs {
  display: grid;

  /* 经度 / 纬度 / 「地图选点」按钮一行放下：按钮单独一行会让"选完点还得往下找保存"更远。 */
  grid-template-columns: repeat(2, minmax(0, 1fr)) auto;
  gap: 8px;
}
.coord-cell {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  min-width: 0;
}
.coord-source {
  flex: 0 0 auto;
  padding: 0 5px;
  font-size: 11px;
  font-style: normal;
  line-height: 16px;
  color: var(--uvp-brand-cyan);
  white-space: nowrap;
  background: color-mix(in srgb, var(--uvp-brand-cyan) 12%, transparent);
  border: 1px solid color-mix(in srgb, var(--uvp-brand-cyan) 28%, transparent);
  border-radius: 4px;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@media (width <= 1080px) {
  .workspace {
    grid-template-rows: auto minmax(0, 1fr);
    grid-template-columns: 260px minmax(0, 1fr);
  }
  .toolbar-actions {
    flex-wrap: wrap;
  }
}

@media (width <= 768px) {
  .workspace {
    grid-template-rows: auto 310px minmax(440px, 1fr);
    grid-template-columns: minmax(0, 1fr);
    overflow: auto;
  }
  .catalog-pane {
    border-right: 0;
    border-bottom: 1px solid var(--uvp-panel-border);
  }
  .content-pane {
    padding: 12px 8px;
  }
  .workspace-toolbar-row {
    flex-wrap: wrap;
  }
  .toolbar-actions {
    flex-basis: 100%;
  }
  .cmdk {
    flex-basis: 100%;
    order: -1;
    width: 100%;
    max-width: none;
  }
  .cmdk .kbd {
    display: none;
  }
}

/* 地图上的实时视频小窗（Popup 内容，Teleport 进来）。
   刻意与通道卡片同一族：同样的头部圆点+名称、同样的说明行、同样的动作按钮药丸。
   宽度按 300px 定死：够看清是"哪一路、人在不在"，又不会盖掉半块地图（旧模态是 960px）。 */
:deep(.map-video-card) {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 300px;
  padding: 12px;
  background: #ffffff;
}

:deep(.map-video-card__head) {
  display: flex;
  gap: 6px;
  align-items: center;
  min-width: 0;

  /* 同通道卡片：给右上角那颗关闭按钮让位 */
  padding-right: 20px;
}

/* ⛔ 高度必须定死：高度自适应会让 EasyPlayer 的首帧计算跳一次，画面先糊后清。
   16:9 由内层播放器自己 letterbox，这里只保证容器稳定。 */
:deep(.map-video-card__stage) {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  height: 156px;
  overflow: hidden;
  background: #0b0f14;
  border-radius: 8px;
}

:deep(.map-video-card__state) {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  font-size: 12px;
  color: #c9cdd4;
}

:deep(.map-video-card__meta) {
  display: flex;
  gap: 10px;
  align-items: center;
  overflow: hidden;
  font-size: 11px;
  line-height: 1.5;
  color: #86909c;
  white-space: nowrap;
}

:deep(.map-video-card__meta span) {
  overflow: hidden;
  text-overflow: ellipsis;
}

:deep(.map-video-card__actions) {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px;
}

/* 小窗的两颗按钮沿用卡片那套语义色，见上方「四个动作四种颜色」：
   播放控制台 = 主行动（实心主色）；停止播放 = 中性灰（只是收尾，不该抢焦点）。 */
:deep(.map-channel-card__action--console) {
  color: #ffffff;
  background: var(--uvp-solid-bg, #2563eb);
  border-color: var(--uvp-solid-bg, #2563eb);
}

:deep(.map-channel-card__action--console:hover:not(:disabled)) {
  background: var(--uvp-solid-hover-bg, #1d4ed8);
  border-color: var(--uvp-solid-hover-bg, #1d4ed8);
}

:deep(.map-channel-card__action--console:disabled) {
  color: #ffffff;
  background: #c9cdd4;
  border-color: #c9cdd4;
}

:deep(.map-channel-card__action--stop) {
  color: #475569;
  background: rgb(100 116 139 / 8%);
  border-color: rgb(100 116 139 / 26%);
}

:deep(.map-channel-card__action--stop:hover:not(:disabled)) {
  color: #334155;
  background: rgb(100 116 139 / 15%);
  border-color: rgb(100 116 139 / 40%);
}

/* 深色底图 */
:deep(.map-dark .map-video-card) {
  background: #1f2733;
}

:deep(.map-dark .map-channel-card__action--console) {
  color: var(--uvp-solid-text, #ffffff);
  background: var(--uvp-solid-bg, #2563eb);
  border-color: var(--uvp-solid-bg, #2563eb);
}

:deep(.map-dark .map-channel-card__action--console:hover:not(:disabled)) {
  background: var(--uvp-solid-hover-bg, #1d4ed8);
  border-color: var(--uvp-solid-hover-bg, #1d4ed8);
}

:deep(.map-dark .map-channel-card__action--stop) {
  color: #cbd5e1;
  background: rgb(148 163 184 / 14%);
  border-color: rgb(148 163 184 / 30%);
}

:deep(.map-dark .map-channel-card__action--stop:hover:not(:disabled)) {
  color: #e2e8f0;
  background: rgb(148 163 184 / 24%);
  border-color: rgb(148 163 184 / 46%);
}
</style>
