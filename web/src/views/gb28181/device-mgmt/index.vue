<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch, onUnmounted } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import maplibregl, { LngLatBounds, Marker as MapLibreMarker, type Map as MapLibreMap } from "maplibre-gl";
import "maplibre-gl/dist/maplibre-gl.css";
import {
    Building2,
    Camera,
    ChevronRight,
    Copy,
    Eye,
    Folder,
    FolderTree,
    Grid2X2,
    History,
    Info,
    Layers,
    List,
    Loader2,
    Map as MapIcon,
    Monitor,
    MoreHorizontal,
    Play,
    Pencil,
    RadioTower,
    RefreshCcw,
    Search,
    Settings2,
    SlidersHorizontal,
    Trash2,
    Video,
    X,
    Download,
    Plus
} from "@lucide/vue";
import {
    batchDeleteChannels,
    batchDeleteDevices,
    createDevice,
    deleteChannel,
    deleteDevice,
    getChannel,
    getChannelTimeline,
    getDevice,
    getNoCoordCount,
    listCatalogChildren,
    listCatalogRoots,
    listChannelMounts,
    listChannels,
    listDevices,
    listDeviceStatusEvents,
    listMapClusters,
    listMapMarkers,
    refreshDeviceCatalog,
    updateChannelStreamTransport,
    updateChannel,
    updateDevice,
    type AssetKind,
    type BatchDeleteResult,
    type CatalogNode,
    type ChannelMount,
    type ChannelVO,
    type CreateDeviceDTO,
    type DeviceVO,
    type DeviceStatusEvent,
    type MapCluster,
    type MapMarker,
    type OnlineStatus,
    type TimelineSlot
} from "./api";
import { getDictItemsByDictCodeAPI, type SystemDictItem } from "@/api/dictionary";
import { useThemeConfig } from "@/store/modules/theme-config";
import { storeToRefs } from "pinia";
import ControlConsole from "../components/ControlConsole.vue";

type ViewMode = "list" | "card" | "map";
type DrawerTarget =
    | { type: "channel"; id: number }
    | { type: "device"; id: number }
    | { type: "node"; node: CatalogNode };

interface TreeRow {
    node: CatalogNode;
    level: number;
}

const viewModeStorageKey = "uvp.gb28181.device-mgmt.view-mode";
const autoRefreshStorageKey = "uvp.gb28181.device-mgmt.auto-refresh";
function initialViewMode(): ViewMode {
    if (typeof window === "undefined") return "list";
    try {
        const stored = window.localStorage.getItem(viewModeStorageKey);
        return stored === "card" || stored === "map" ? stored : "list";
    } catch {
        return "list";
    }
}
function initialAutoRefresh() {
    if (typeof window === "undefined") return true;
    try {
        return window.localStorage.getItem(autoRefreshStorageKey) !== "false";
    } catch {
        return true;
    }
}

const viewMode = ref<ViewMode>(initialViewMode());
const assetKind = ref<AssetKind>("device");
const keyword = ref("");
const keywordInput = ref<HTMLInputElement | null>(null);
const deviceIdFilter = ref("");
const statusFilter = ref<OnlineStatus | undefined>();
const selectedNode = ref<CatalogNode | null>(null);
const drawerVisible = ref(false);
const drawerTarget = ref<DrawerTarget | null>(null);
const drawerLoading = ref(false);
const rootLoading = ref(false);
const rowsLoading = ref(false);
const mapLoading = ref(false);
const page = ref(1);
const listPageSize = ref(10);
const cardPageSize = ref(12);
const pageSize = computed({
    get: () => viewMode.value === "card" ? cardPageSize.value : listPageSize.value,
    set: (value: number) => {
        if (viewMode.value === "card") cardPageSize.value = value;
        else listPageSize.value = value;
    }
});
const total = ref(0);
const noCoordCount = ref(0);
const selectedRowKeys = ref<number[]>([]);
const mapZoom = ref(10);
const mapMinZoom = 5;
const mapMaxZoom = 22;
const mapContainer = ref<HTMLElement | null>(null);
const mapReady = ref(false);
const mapError = ref("");
const themeStore = useThemeConfig();
const { darkMode } = storeToRefs(themeStore);
const mapStyleUrls = {
    light: (import.meta.env.VITE_MAP_STYLE_LIGHT_URL as string | undefined) || "https://tiles.openfreemap.org/styles/bright",
    dark: (import.meta.env.VITE_MAP_STYLE_DARK_URL as string | undefined) || "https://tiles.openfreemap.org/styles/dark"
};
const onlineDeviceTotal = ref(0);
const offlineDeviceTotal = ref(0);
const autoRefresh = ref(initialAutoRefresh());
const refreshInterval = ref<number | null>(null);
const isMacPlatform = computed(() => typeof navigator !== "undefined" && /Mac|iPhone|iPad|iPod/.test(navigator.platform));

let keywordSearchTimer: ReturnType<typeof setTimeout> | null = null;
let suppressKeywordSearch = false;
let mapInstance: MapLibreMap | null = null;
let mapMoveHandler: (() => void) | null = null;
let mapAutoFitPending = true;
const mapMarkers = new Map<number, MapLibreMarker>();
const mapClusters = new Map<string, MapLibreMarker>();

const roots = ref<CatalogNode[]>([]);
const childrenMap = reactive<Record<number, CatalogNode[]>>({});
const expandedKeys = ref<number[]>([]);
const loadingChildren = reactive<Record<number, boolean>>({});
const channels = ref<ChannelVO[]>([]);
const devices = ref<DeviceVO[]>([]);
const markers = ref<MapMarker[]>([]);
const clusters = ref<MapCluster[]>([]);
const channelDetail = ref<ChannelVO | null>(null);
const deviceDetail = ref<DeviceVO | null>(null);
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
const channelMounts = ref<ChannelMount[]>([]);
const timeline = ref<TimelineSlot[]>([]);
const editDeviceVisible = ref(false);
const editDeviceForm = ref({ deviceId: "", alias: "", name: "", manufacturer: "", model: "", firmware: "" });
const editingDevice = ref(false);
const editingDeviceId = ref("");
const editChannelVisible = ref(false);
const editChannelForm = ref({ channelId: "", deviceId: "", alias: "", name: "", manufacturer: "", model: "", ptzType: 0, streamTransport: "UDP" });
const editingChannel = ref(false);
const editingChannelId = ref(0);
const ptzTypeOptions = ref<SystemDictItem[]>([]);
const controlConsoleVisible = ref(false);
const controlConsoleChannel = ref<ChannelVO | null>(null);

const viewOptions: Array<{ label: string; value: ViewMode; icon: any }> = [
    { label: "列表", value: "list", icon: List },
    { label: "卡片", value: "card", icon: Grid2X2 },
    { label: "地图", value: "map", icon: MapIcon }
];
const nodeTypeMeta: Record<string, { className: string }> = {
    civil_code: { className: "civil" },
    biz_group: { className: "biz" },
    virtual_org: { className: "virtual" },
    device: { className: "device" },
    channel: { className: "channel" }
};

const flatTree = computed<TreeRow[]>(() => {
    const rows: TreeRow[] = [];
    const walk = (nodes: CatalogNode[], level: number) => {
        nodes.forEach((node) => {
            rows.push({ node, level });
            if (expandedKeys.value.includes(node.id)) walk(childrenMap[node.id] || [], level + 1);
        });
    };
    walk(roots.value, 0);
    return rows;
});

const hasFilters = computed(() => Boolean(keyword.value || deviceIdFilter.value || statusFilter.value || selectedNode.value));
const selectedCount = computed(() => selectedRowKeys.value.length);
const tablePagination = computed(() => ({
    current: page.value,
    pageSize: pageSize.value,
    total: total.value,
    showPageSize: true,
    showTotal: true,
    showJumper: true
}));

watch([viewMode, assetKind], async () => {
    selectedRowKeys.value = [];
    page.value = 1;
    if (viewMode.value === "map") await nextTick(ensureMap);
    refreshMainData();
});
watch(viewMode, (mode) => {
    try {
        window.localStorage.setItem(viewModeStorageKey, mode);
    } catch {
        // Storage can be unavailable in privacy-restricted browser contexts.
    }
});
watch(autoRefresh, (enabled) => {
    try {
        window.localStorage.setItem(autoRefreshStorageKey, String(enabled));
    } catch {
        // Storage can be unavailable in privacy-restricted browser contexts.
    }
});
watch(darkMode, () => {
    if (!mapInstance) return;
    mapInstance.setStyle(currentMapStyleUrl());
    mapReady.value = false;
    mapInstance.once("style.load", () => {
        mapReady.value = true;
        renderMapOverlays();
    });
});
watch(statusFilter, () => {
    page.value = 1;
    refreshMainData();
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
function deviceNameText(item: { alias?: string | null; name?: string | null }) {
    return item.alias?.trim() || item.name?.trim() || "-";
}
function vendorText(item: { manufacturer?: string; model?: string }) { return [item.manufacturer, item.model].filter(Boolean).join(" / ") || "未上报"; }
function locationText(item: ChannelVO | MapMarker) { return !item.latitude || !item.longitude ? "无坐标" : `${item.longitude.toFixed(5)}, ${item.latitude.toFixed(5)}`; }
function dateTime(value?: string | null) { if (!value || value.startsWith("0001-01-01")) return "-"; const d = new Date(value); if (Number.isNaN(d.getTime())) return "-"; return d.toLocaleString("zh-CN", { hour12: false }); }
function endpointText(item: { ip?: string; port?: number; transport?: string }) {
    if (!item.ip) return "-";
    const addr = item.port ? `${item.ip}:${item.port}` : item.ip;
    const protocol = item.transport ? item.transport.toLowerCase() : "udp";
    return `${protocol}://${addr}`;
}
function transportText(value?: string | null) { return value ? value.toUpperCase() : "-"; }
function streamTransportText(value?: string | null) { return value ? value.toUpperCase() : "-"; }
function cameraTypeText(ptzType?: number | null) {
    if (ptzType === null || ptzType === undefined) return "未知";
    const item = ptzTypeOptions.value.find(opt => Number(opt.value) === ptzType);
    return item?.name || "未知";
}
function modelVersionText(item: { model?: string; firmware?: string }) {
    return [item.model, item.firmware].filter(Boolean).join(" / ") || "-";
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
function parseCapability(source?: string): string[] {
    if (!source) return [];
    return source
        .split(/[,;/\s]+/)
        .map(x => x.trim())
        .filter(Boolean);
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
function canExpand(node: CatalogNode) { return node.nodeType !== "channel"; }
function showDeviceChannels(record: DeviceVO) {
    setKeywordWithoutSearch("");
    deviceIdFilter.value = record.deviceId;
    selectedNode.value = null;
    statusFilter.value = undefined;
    assetKind.value = "channel";
    page.value = 1;
}

async function loadTree() {
    rootLoading.value = true;
    try {
        const res = await listCatalogRoots();
        if (res.code === 0) roots.value = res.data?.list || [];
    } catch (error: any) {
        Message.error(error?.message || "目录加载失败");
    } finally {
        rootLoading.value = false;
    }
}

async function loadPtzTypeDict() {
    try {
        const res = await getDictItemsByDictCodeAPI("ptz_type");
        if (res.code === 0) {
            ptzTypeOptions.value = res.data?.list || [];
        }
    } catch (error: any) {
        console.error("摄像头类型字典加载失败:", error);
    }
}

async function loadChildren(id: number) {
    loadingChildren[id] = true;
    try {
        const res = await listCatalogChildren(id);
        if (res.code === 0) childrenMap[id] = res.data?.list || [];
    } catch (error: any) {
        Message.error(error?.message || "子目录加载失败");
    } finally {
        loadingChildren[id] = false;
    }
}

async function toggleNode(node: CatalogNode) {
    if (!canExpand(node)) return selectNode(node);
    if (expandedKeys.value.includes(node.id)) {
        expandedKeys.value = expandedKeys.value.filter((id) => id !== node.id);
        return;
    }
    expandedKeys.value = [...expandedKeys.value, node.id];
    if (!childrenMap[node.id]) await loadChildren(node.id);
}

function selectNode(node: CatalogNode) {
    selectedNode.value = node;
    page.value = 1;
    refreshMainData();
}
function clearNode() { selectedNode.value = null; page.value = 1; refreshMainData(); }
function setViewMode(mode: ViewMode) {
    viewMode.value = mode;
    if (mode === "map") {
        assetKind.value = "channel";
        mapAutoFitPending = true;
    }
}
function setAssetKind(kind: AssetKind) {
    if (kind === "device") deviceIdFilter.value = "";
    assetKind.value = kind;
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
function onSearch() { cancelKeywordSearch(); page.value = 1; refreshMainData(); }
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
function clearDeviceFilter() { deviceIdFilter.value = ""; page.value = 1; refreshMainData(); }
function resetFilters() { setKeywordWithoutSearch(""); deviceIdFilter.value = ""; statusFilter.value = undefined; clearNode(); }
function onPageChange(next: number) { page.value = next; refreshMainData(); }
function onPageSizeChange(next: number) { pageSize.value = next; page.value = 1; refreshMainData(); }

async function refreshMainData() {
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
    return darkMode.value ? mapStyleUrls.dark : mapStyleUrls.light;
}

function removeMapMarkers() {
    mapMarkers.forEach(marker => marker.remove());
    mapMarkers.clear();
    mapClusters.forEach(marker => marker.remove());
    mapClusters.clear();
}

function createClusterElement(cluster: MapCluster) {
    const element = document.createElement("button");
    element.type = "button";
    element.className = "map-cluster-marker";
    element.textContent = String(cluster.count);
    element.title = `${cluster.count} 路通道 · 在线 ${cluster.onlineCount}`;
    element.style.setProperty("--cluster-rate", `${Math.round(cluster.onlineRate * 100)}%`);
    element.addEventListener("click", () => {
        mapInstance?.flyTo({
            center: [cluster.centerLng, cluster.centerLat],
            zoom: Math.min(16, Math.max(mapInstance.getZoom() + 2, 12)),
            essential: true
        });
    });
    return element;
}

function createMarkerElement(marker: MapMarker) {
    const element = document.createElement("button");
    element.type = "button";
    element.className = `map-channel-marker${marker.status === 1 ? " online" : ""}`;
    element.title = `${displayName(marker)} · ${marker.channelId}`;
    element.innerHTML = '<span class="map-channel-pip"></span>';
    element.addEventListener("click", () => openChannel(marker));
    return element;
}

function renderMapOverlays() {
    if (!mapInstance || !mapReady.value) return;
    removeMapMarkers();
    clusters.value.filter(cluster => cluster.count > 1).forEach((cluster) => {
        const key = `${cluster.centerLat}:${cluster.centerLng}:${cluster.count}`;
        mapClusters.set(key, new MapLibreMarker({ element: createClusterElement(cluster), anchor: "center" })
            .setLngLat([cluster.centerLng, cluster.centerLat])
            .addTo(mapInstance!));
    });
    if (mapZoom.value >= 14) {
        markers.value.forEach(marker => {
            mapMarkers.set(marker.id, new MapLibreMarker({ element: createMarkerElement(marker), anchor: "center" })
                .setLngLat([marker.longitude, marker.latitude])
                .addTo(mapInstance!));
        });
    }
}

function fitMapToData() {
    if (!mapInstance || !markers.value.length) return;
    const bounds = new LngLatBounds();
    markers.value.forEach(marker => bounds.extend([marker.longitude, marker.latitude]));
    mapInstance.fitBounds(bounds, { padding: 60, maxZoom: mapMaxZoom, duration: 500 });
}

function ensureMap() {
    if (mapInstance || !mapContainer.value) return;
    mapError.value = "";
    mapInstance = new maplibregl.Map({
        container: mapContainer.value,
        style: currentMapStyleUrl(),
        center: [116.3974, 39.9093],
        zoom: mapZoom.value,
        minZoom: mapMinZoom,
        maxZoom: mapMaxZoom,
        attributionControl: false,
        hash: false
    });
    mapInstance.addControl(new maplibregl.NavigationControl({ showCompass: false }), "top-right");
    mapInstance.addControl(new maplibregl.AttributionControl({ compact: true }), "bottom-right");
    mapInstance.once("load", () => {
        mapReady.value = true;
        mapInstance?.resize();
        loadMapData();
    });
    mapMoveHandler = () => {
        if (!mapInstance) return;
        mapZoom.value = Math.round(mapInstance.getZoom());
        loadMapData();
    };
    mapInstance.on("moveend", mapMoveHandler);
    mapInstance.on("error", () => {
        if (!mapReady.value) mapError.value = "底图加载失败，请检查网络或配置 VITE_MAP_STYLE_URL";
    });
}

async function loadChannelsData() {
    rowsLoading.value = true;
    try {
        const res = await listChannels({
            q: keyword.value.trim() || undefined,
            deviceId: deviceIdFilter.value || undefined,
            nodeId: selectedNode.value?.id,
            status: statusFilter.value,
            page: page.value,
            pageSize: pageSize.value
        });
        if (res.code === 0) {
            channels.value = res.data?.list || [];
            total.value = res.data?.total || 0;
        }
    } catch (error: any) {
        Message.error(error?.message || "通道列表加载失败");
    } finally {
        rowsLoading.value = false;
    }
}

async function loadDevicesData() {
    rowsLoading.value = true;
    try {
        const res = await listDevices({
            q: keyword.value.trim() || undefined,
            nodeId: selectedNode.value?.id,
            status: statusFilter.value,
            page: page.value,
            pageSize: pageSize.value,
            sort: "heartbeat:desc"
        });
        if (res.code === 0) {
            devices.value = res.data?.list || [];
            total.value = res.data?.total || 0;
        }
    } catch (error: any) {
        Message.error(error?.message || "设备列表加载失败");
    } finally {
        rowsLoading.value = false;
    }
}

async function loadMapData() {
    if (viewMode.value !== "map") return;
    mapLoading.value = true;
    try {
        const query = {
            ...mapBoundsParams(),
            q: keyword.value.trim() || undefined,
            nodeId: selectedNode.value?.id,
            status: statusFilter.value
        };
        const [markerRes, clusterRes, noCoordRes] = await Promise.all([
            listMapMarkers({ ...query, limit: 800 }),
            listMapClusters({ ...query, zoom: mapZoom.value }),
            getNoCoordCount(query)
        ]);
        if (markerRes.code === 0) markers.value = markerRes.data?.list || [];
        if (clusterRes.code === 0) clusters.value = clusterRes.data?.clusters || [];
        if (noCoordRes.code === 0) noCoordCount.value = noCoordRes.data?.count || 0;
        total.value = markerRes.data?.total || 0;
        renderMapOverlays();
        if (mapAutoFitPending && markers.value.length) {
            mapAutoFitPending = false;
            fitMapToData();
        }
    } catch (error: any) {
        Message.error(error?.message || "地图数据加载失败");
    } finally {
        mapLoading.value = false;
    }
}

async function refreshDeviceStats() {
    try {
        const [onlineRes, offlineRes] = await Promise.all([
            listDevices({ status: "online", page: 1, pageSize: 1 }),
            listDevices({ status: "offline", page: 1, pageSize: 1 })
        ]);
        if (onlineRes.code === 0) onlineDeviceTotal.value = onlineRes.data?.total || 0;
        if (offlineRes.code === 0) offlineDeviceTotal.value = offlineRes.data?.total || 0;
    } catch (error: any) {
        console.warn(error);
    }
}

async function openChannel(record: ChannelVO | MapMarker) {
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
    drawerTarget.value = { type: "device", id: record.id };
    drawerVisible.value = true;
    drawerLoading.value = true;
    try {
        const detailRes = await getDevice(record.id);
        if (detailRes.code === 0) deviceDetail.value = detailRes.data;
    } catch (error: any) {
        Message.error(error?.message || "详情加载失败");
    } finally {
        drawerLoading.value = false;
    }
}

async function loadStatusEvents(append = false) {
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

function openStatusEvents(record: DeviceVO) {
    statusEventRequestVersion += 1;
    statusEventDevice.value = record;
    statusEventPage.value = 1;
    statusEventList.value = [];
    statusEventTotal.value = 0;
    statusEventLoading.value = false;
    statusEventLoadingMore.value = false;
    statusEventVisible.value = true;
    loadStatusEvents();
    nextTick(() => {
        if (statusEventScroll.value) statusEventScroll.value.scrollTop = 0;
    });
}

function loadMoreStatusEvents() {
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
}

function statusEventTime(value?: string | null) {
    if (!value || value.startsWith("0001-01-01")) return "-";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "-";
    const pad = (part: number) => String(part).padStart(2, "0");
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

function eventColor(eventType: DeviceStatusEvent["eventType"]) {
    return ({
        register_online: "#10b981",
        register_renewed: "var(--uvp-brand)",
        heartbeat_recovered: "var(--uvp-brand-cyan)",
        heartbeat_timeout: "var(--uvp-danger)",
        unregister_offline: "var(--uvp-text-tertiary)"
    } as const)[eventType];
}

function eventSourceText(source: DeviceStatusEvent["source"]) {
    return ({
        register: "REGISTER",
        unregister: "REGISTER 注销",
        keepalive: "Keepalive",
        offline_scanner: "离线扫描器"
    } as const)[source] || source;
}

function eventMetaText(event: DeviceStatusEvent) {
    const parts: string[] = [eventSourceText(event.source)];
    if (event.ip) parts.push(`${event.transport || "UDP"} ${event.ip}${event.port ? `:${event.port}` : ""}`);
    if (event.registerExpires != null) parts.push(`有效期 ${event.registerExpires} 秒`);
    return parts.join(" · ");
}

function openNode(node: CatalogNode) {
    drawerTarget.value = { type: "node", node };
    drawerVisible.value = true;
}

function playChannel(record: ChannelVO) {
    controlConsoleChannel.value = record;
    controlConsoleVisible.value = true;
}

const deleting = ref(false);
const refreshingCatalog = reactive<Record<number, boolean>>({});
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
    refreshDeviceStats();
}

async function handleRefreshDeviceCatalog(record: DeviceVO) {
    if (refreshingCatalog[record.id]) return;
    if (!record.online) {
        Message.warning("设备离线,无法下发 Catalog 查询");
        return;
    }
    refreshingCatalog[record.id] = true;
    try {
        const res = await refreshDeviceCatalog(record.id);
        if (res.code === 0) {
            Message.success(`已下发 Catalog 查询 · ${record.deviceId}`);
            // 通道回执异步落库,延迟稍长一点再拉列表,避免空刷新
            setTimeout(() => refreshMainData(), 1500);
        } else {
            Message.error(res.message || "下发失败");
        }
    } catch (error: any) {
        Message.error(error?.message || "下发失败");
    } finally {
        refreshingCatalog[record.id] = false;
    }
}

function openCreateDeviceModal() {
    createDeviceForm.deviceId = "";
    createDeviceForm.name = "";
    createDeviceForm.password = "";
    createDeviceVisible.value = true;
    createDeviceFormRef.value?.clearValidate?.();
}

async function handleCreateDevice() {
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
            refreshMainData();
            refreshDeviceStats();
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
    editDeviceForm.value = {
        deviceId: record.deviceId,
        alias: record.alias || "",
        name: record.name || "",
        manufacturer: record.manufacturer || "",
        model: record.model || "",
        firmware: record.firmware || ""
    };
    editingDeviceId.value = record.deviceId;
    editDeviceVisible.value = true;
}

function openEditChannelModal(record: ChannelVO) {
    editChannelForm.value = {
        channelId: record.channelId,
        deviceId: record.deviceId,
        alias: record.alias || "",
        name: record.name || "",
        manufacturer: record.manufacturer || "",
        model: record.model || "",
        ptzType: record.ptzType || 0,
        streamTransport: record.streamTransport || "UDP"
    };
    editingChannelId.value = record.id;
    editChannelVisible.value = true;
}

function cancelEditChannel() {
    editChannelVisible.value = false;
    editingChannelId.value = 0;
    editChannelForm.value = { channelId: "", deviceId: "", alias: "", name: "", manufacturer: "", model: "", ptzType: 0, streamTransport: "UDP" };
}

async function handleEditChannel() {
    if (!editingChannelId.value) return;
    editingChannel.value = true;
    try {
        const [ptzRes, transportRes] = await Promise.all([
            updateChannel(editingChannelId.value, { alias: editChannelForm.value.alias, ptzType: editChannelForm.value.ptzType }),
            updateChannelStreamTransport(editingChannelId.value, editChannelForm.value.streamTransport as any)
        ]);
        if (ptzRes.code !== 0 || transportRes.code !== 0) {
            Message.error(ptzRes.code !== 0 ? ptzRes.message || "摄像头类型更新失败" : transportRes.message || "流传输模式更新失败");
            return;
        }
        const item = channels.value.find(channel => channel.id === editingChannelId.value);
        if (item) {
            item.alias = editChannelForm.value.alias;
            item.ptzType = editChannelForm.value.ptzType;
            item.streamTransport = editChannelForm.value.streamTransport;
        }
        if (channelDetail.value?.id === editingChannelId.value) {
            channelDetail.value = {
                ...channelDetail.value,
                alias: editChannelForm.value.alias,
                ptzType: editChannelForm.value.ptzType,
                streamTransport: editChannelForm.value.streamTransport
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
    editDeviceForm.value = { deviceId: "", alias: "", name: "", manufacturer: "", model: "", firmware: "" };
    editingDeviceId.value = "";
}

async function handleEditDevice() {
    if (!editingDeviceId.value) return;
    editingDevice.value = true;
    try {
        const res = await updateDevice(editingDeviceId.value, {
            alias: editDeviceForm.value.alias,
            manufacturer: editDeviceForm.value.manufacturer,
            model: editDeviceForm.value.model,
            firmware: editDeviceForm.value.firmware
        });
        if (res.code === 0) {
            Message.success("设备信息已更新");
            editDeviceVisible.value = false;
            refreshMainData();
            // 如果抽屉打开着,同步更新抽屉内容
            if (drawerVisible.value && deviceDetail.value?.deviceId === editingDeviceId.value) {
                deviceDetail.value = { ...deviceDetail.value, ...editDeviceForm.value };
            }
        } else {
            Message.error(res.message || "更新失败");
        }
    } catch (error: any) {
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
    try {
        const res = await updateChannelStreamTransport(channelId, streamTransport as any);
        if (res.code === 0) {
            Message.success("流传输模式已更新");
            // 更新本地数据
            if (assetKind.value === "channel") {
                const item = channels.value.find(c => c.id === channelId);
                if (item) item.streamTransport = streamTransport;
            }
        } else {
            Message.error(res.message || "更新失败");
        }
    } catch (error: any) {
        Message.error(error?.message || "更新失败");
    }
}

async function handlePtzTypeChange(channelId: number, ptzTypeValue: string) {
    try {
        const ptzType = Number(ptzTypeValue);
        const res = await updateChannel(channelId, { ptzType });
        if (res.code === 0) {
            Message.success("摄像头类型已更新");
            // 更新本地数据
            if (assetKind.value === "channel") {
                const item = channels.value.find(c => c.id === channelId);
                if (item) item.ptzType = ptzType;
            }
        } else {
            Message.error(res.message || "更新失败");
        }
    } catch (error: any) {
        Message.error(error?.message || "更新失败");
    }
}

async function handleBatchDelete() {
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
                const call = assetKind.value === "device" ? batchDeleteDevices : batchDeleteChannels;
                const res = await call(ids);
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

function startAutoRefresh() {
    if (refreshInterval.value) return;
    refreshInterval.value = window.setInterval(() => {
        if (autoRefresh.value) {
            refreshMainData();
            refreshDeviceStats();
        }
    }, 10000); // 每10秒刷新一次
}

function stopAutoRefresh() {
    if (refreshInterval.value) {
        clearInterval(refreshInterval.value);
        refreshInterval.value = null;
    }
}

function toggleAutoRefresh(checked: boolean) {
    autoRefresh.value = checked;
    if (checked) {
        startAutoRefresh();
        Message.success("自动刷新已开启");
    } else {
        stopAutoRefresh();
        Message.info("自动刷新已关闭");
    }
}

function channelPercentage(record: DeviceVO): string {
    if (!record.channelCount || record.channelCount === 0) return '0%';
    const percentage = (record.channelOnlineCount / record.channelCount) * 100;
    return `${percentage.toFixed(0)}%`;
}

function channelStatusClass(record: DeviceVO): string {
    if (!record.channelCount || record.channelCount === 0) return 'empty';
    const percentage = (record.channelOnlineCount / record.channelCount) * 100;
    if (percentage === 0) return 'offline';
    if (percentage < 50) return 'warning';
    return 'healthy';
}

onMounted(async () => {
    window.addEventListener("keydown", focusKeyword);
    if (viewMode.value === "map") {
        await nextTick(ensureMap);
    }
    await Promise.all([
        loadTree(),
        viewMode.value === "map" ? Promise.resolve() : refreshMainData(),
        refreshDeviceStats(),
        loadPtzTypeDict()
    ]);
    if (autoRefresh.value) {
        startAutoRefresh();
    }
});

onUnmounted(() => {
    window.removeEventListener("keydown", focusKeyword);
    cancelKeywordSearch();
    stopAutoRefresh();
    if (mapInstance && mapMoveHandler) mapInstance.off("moveend", mapMoveHandler);
    removeMapMarkers();
    mapInstance?.remove();
    mapInstance = null;
});
</script>

<template>
    <div class="snow-fill">
        <div class="snow-fill-inner device-mgmt-page">
            <div class="content-topbar">
                <div class="device-stats">
                    <span class="stat online">在线 {{ onlineDeviceTotal }}</span>
                    <span class="stat offline">离线 {{ offlineDeviceTotal }}</span>
                </div>
                <div class="topbar-actions">
                    <div class="cmdk">
                        <Search :size="14" />
                        <input ref="keywordInput" v-model="keyword" type="text" aria-label="搜索设备、通道或编码" placeholder="搜索设备 / 通道 / 编码 ..." @keydown.enter.prevent="onSearch" />
                        <a-tooltip v-if="keyword" content="清空搜索" position="bottom">
                            <button class="cmdk-clear" type="button" aria-label="清空搜索" @click="clearKeyword"><X :size="14" /></button>
                        </a-tooltip>
                        <a-tooltip :content="`按 ${isMacPlatform ? 'Command' : 'Ctrl'} + K 聚焦搜索框`" position="bottom">
                            <span class="kbd"><kbd>{{ isMacPlatform ? '⌘' : 'Ctrl' }}</kbd><kbd>K</kbd></span>
                        </a-tooltip>
                    </div>
                    <div class="view-switch">
                        <button v-for="item in viewOptions" :key="item.value" type="button" :class="{ active: viewMode === item.value }" @click="setViewMode(item.value)">
                            <component :is="item.icon" :size="14" />
                        </button>
                    </div>
                    <button class="icon-btn" type="button" @click="resetFilters"><SlidersHorizontal :size="16" /></button>
                    <button class="btn-primary" type="button" @click="openCreateDeviceModal"><Plus :size="14" /> 新建设备</button>
                    <button class="icon-btn" type="button"><Settings2 :size="16" /></button>
                </div>
            </div>

            <div class="workspace">
                <aside class="catalog-pane">
                    <div class="pane-head">
                        <div class="head-title"><FolderTree :size="14" /> <span>目录</span></div>
                        <button class="icon-btn small" type="button" @click="loadTree"><RefreshCcw :size="12" /></button>
                    </div>
                    <div class="aside-search"><input v-model="keyword" placeholder="筛选节点 ..." @keydown.enter.prevent="onSearch" /></div>
                    <a-spin :loading="rootLoading" class="tree-wrap">
                        <div class="tree">
                            <div
                                v-for="{ node, level } in flatTree"
                                :key="node.id"
                                class="tree-row"
                                :class="{ active: selectedNode?.id === node.id, anomaly: node.anomaly }"
                                :style="{ paddingLeft: `${8 + level * 14}px` }"
                            >
                                <button class="twist" type="button" :class="{ hidden: !canExpand(node) }" :disabled="!canExpand(node)" @click.stop="toggleNode(node)">
                                    <Loader2 v-if="loadingChildren[node.id]" :size="12" class="spin" />
                                    <ChevronRight v-else :size="12" />
                                </button>
                                <button class="tree-node-btn" type="button" @click="selectNode(node)" @dblclick="openNode(node)">
                                    <span class="node-chip" :class="nodeTypeMeta[node.nodeType]?.className || 'civil'">
                                        <Building2 v-if="node.nodeType === 'civil_code'" :size="12" />
                                        <Folder v-else-if="node.nodeType === 'biz_group' || node.nodeType === 'virtual_org'" :size="12" />
                                        <Monitor v-else-if="node.nodeType === 'device'" :size="12" />
                                        <Camera v-else :size="12" />
                                    </span>
                                    <span class="label">{{ node.name }}</span>
                                    <span class="count">{{ node.mountCount ?? node.childCount ?? '' }}</span>
                                </button>
                            </div>
                        </div>
                    </a-spin>
                </aside>

                <main class="content-pane">
                    <div class="main-head">
                        <s-layout-search class="device-filter-panel">
                            <template #extra>
                                <div class="toolbar">
                                    <div v-if="viewMode === 'map' && noCoordCount" class="map-banner toolbar-map-banner">
                                        <Info :size="14" /> 有 {{ noCoordCount }} 路通道缺少坐标，暂未显示在地图中。
                                    </div>
                                    <div class="auto-refresh-control">
                                        <span class="refresh-label">自动刷新</span>
                                        <a-switch
                                            v-model="autoRefresh"
                                            @change="toggleAutoRefresh"
                                        >
                                            <template #checked>开启</template>
                                            <template #unchecked>关闭</template>
                                        </a-switch>
                                    </div>
                                    <button class="btn-ghost" type="button" @click="refreshMainData">
                                        <RefreshCcw :size="14" :class="{ spin: rowsLoading || mapLoading }" />
                                        刷新
                                    </button>
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
                                    <button class="icon-btn" type="button"><Download :size="14" /></button>
                                    <div v-if="viewMode !== 'map'" class="segmented">
                                        <button type="button" :class="{ active: assetKind === 'device' }" @click="setAssetKind('device')">设备</button>
                                        <button type="button" :class="{ active: assetKind === 'channel' }" @click="setAssetKind('channel')">通道</button>
                                    </div>
                                </div>
                            </template>
                        </s-layout-search>
                    </div>

                    <div class="filter-chips">
                        <span v-if="deviceIdFilter" class="filter-chip">所属设备: {{ deviceIdFilter }} <button class="close" @click="clearDeviceFilter">×</button></span>
                        <span v-if="statusFilter" class="filter-chip">状态: {{ statusFilter === 'online' ? '在线' : '离线' }} <button class="close" @click="statusFilter = undefined">×</button></span>
                        <span v-if="selectedNode" class="filter-chip">目录: {{ selectedNode.name }} <button class="close" @click="clearNode">×</button></span>
                        <button v-if="hasFilters" class="clear-all" type="button" @click="resetFilters">清除筛选</button>
                    </div>

                    <div v-if="selectedCount" class="batch-bar">
                        <div class="batch-info"><strong>{{ selectedCount }}</strong> 项已选</div>
                        <div class="batch-ops">
                            <button class="btn-danger" type="button" :disabled="deleting" @click="handleBatchDelete">批量删除</button>
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
                            :scroll="{ x: 1580, y: '85%' }"
                            :row-selection="{ type: 'checkbox', showCheckedAll: true }"
                            class="uvp-data-table"
                            @page-change="onPageChange"
                            @page-size-change="onPageSizeChange"
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
                                <a-table-column title="快照" :width="92" align="center">
                                    <template #cell="{ record }">
                                        <div class="thumb small"><div class="placeholder">{{ (record.manufacturer || 'UV').slice(0, 2).toUpperCase() }}</div></div>
                                    </template>
                                </a-table-column>
                                <a-table-column title="状态" :width="92">
                                    <template #cell="{ record }">
                                        <span class="status-inline" :class="{ online: record.status === 1 }">
                                            <span class="status-dot"></span>
                                            <span>{{ record.status === 1 ? '在线' : '离线' }}</span>
                                        </span>
                                    </template>
                                </a-table-column>
                                <a-table-column title="摄像头类型" :width="150">
                                    <template #cell="{ record }">
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
                                </a-table-column>
                                <a-table-column title="流传输模式" :width="150">
                                    <template #cell="{ record }">
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
                                </a-table-column>
                                <a-table-column title="位置信息" :width="180">
                                    <template #cell="{ record }">
                                        <a-tooltip :content="locationText(record)" position="top">
                                            <span class="relative text-ellipsis">{{ locationText(record) }}</span>
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
                                <a-table-column title="操作" :width="280" fixed="right">
                                    <template #cell="{ record }">
                                        <div class="uvp-table-actions">
                                            <a-link class="uvp-table-action uvp-table-action--preview" @click="playChannel(record)">
                                                <template #icon><Play :size="13" /></template>
                                                <span>播放</span>
                                            </a-link>
                                            <a-link class="uvp-table-action uvp-table-action--detail" @click="openChannel(record)">
                                                <template #icon><Eye :size="13" /></template>
                                                <span>详情</span>
                                            </a-link>
                                            <a-link class="uvp-table-action uvp-table-action--edit" @click="openEditChannelModal(record)">
                                                <template #icon><Pencil :size="13" /></template>
                                                <span>编辑</span>
                                            </a-link>
                                            <a-link class="uvp-table-action uvp-table-action--delete" :disabled="deleting" @click="handleDeleteChannel(record)">
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
                            :scroll="{ x: 1650, y: '85%' }"
                            :row-selection="{ type: 'checkbox', showCheckedAll: true }"
                            class="uvp-data-table device-data-table"
                            @page-change="onPageChange"
                            @page-size-change="onPageSizeChange"
                        >
                            <template #columns>
                                <a-table-column title="设备名称" :width="150">
                                    <template #cell="{ record }">
                                        <a-tooltip :content="deviceNameText(record)" position="top">
                                            <div class="device-name">
                                                <span class="pri text-ellipsis">{{ deviceNameText(record) }}</span>
                                            </div>
                                        </a-tooltip>
                                    </template>
                                </a-table-column>
                                <a-table-column title="设备编号" :width="190">
                                    <template #cell="{ record }">
                                        <a-tooltip :content="record.deviceId" position="top">
                                            <span class="code-main text-ellipsis">{{ record.deviceId }}</span>
                                        </a-tooltip>
                                    </template>
                                </a-table-column>
                                <a-table-column title="厂商" :width="120">
                                    <template #cell="{ record }">
                                        <a-tooltip :content="record.manufacturer || '-'" position="top">
                                            <span class="text-ellipsis">{{ record.manufacturer || '-' }}</span>
                                        </a-tooltip>
                                    </template>
                                </a-table-column>
                                <a-table-column title="状态" :width="100" align="center">
                                    <template #cell="{ record }">
                                        <button class="status-inline status-trigger" :class="{ online: record.online }" type="button" title="查看状态轨迹" :aria-label="`查看${record.online ? '在线' : '离线'}状态轨迹`" @click.stop="openStatusEvents(record)">
                                            <History class="status-trigger-icon" :size="13" aria-hidden="true" />
                                            <span class="status-trigger-label">{{ record.online ? '在线' : '离线' }}</span>
                                        </button>
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
                                <a-table-column title="来源地址" :width="220">
                                    <template #cell="{ record }">
                                        <a-tooltip :content="endpointText(record)" position="top">
                                            <span class="code-main mono text-ellipsis">{{ endpointText(record) }}</span>
                                        </a-tooltip>
                                    </template>
                                </a-table-column>
                                <a-table-column title="型号 / 版本" :width="160">
                                    <template #cell="{ record }">
                                        <a-tooltip :content="modelVersionText(record)" position="top">
                                            <span class="relative text-ellipsis">{{ modelVersionText(record) }}</span>
                                        </a-tooltip>
                                    </template>
                                </a-table-column>
                                <a-table-column title="注册时间" :width="170">
                                    <template #cell="{ record }">
                                        <span class="relative">{{ dateTime(record.registerTime) }}</span>
                                    </template>
                                </a-table-column>
                                <a-table-column title="心跳时间" :width="170">
                                    <template #cell="{ record }">
                                        <span class="relative" :class="{ warn: !record.online }">{{ dateTime(record.keepaliveTime) }}</span>
                                    </template>
                                </a-table-column>
                                <a-table-column title="操作" :width="230" fixed="right">
                                    <template #cell="{ record }">
                                        <div class="uvp-table-actions">
                                            <a-link class="uvp-table-action uvp-table-action--preview" @click="showDeviceChannels(record)">
                                                <template #icon><Camera :size="13" /></template>
                                                <span>通道</span>
                                            </a-link>
                                            <a-link
                                                class="uvp-table-action uvp-table-action--sync"
                                                :disabled="refreshingCatalog[record.id] || !record.online"
                                                :title="record.online ? '刷新通道目录' : '设备离线,无法刷新'"
                                                @click="handleRefreshDeviceCatalog(record)"
                                            >
                                                <template #icon><Loader2 v-if="refreshingCatalog[record.id]" :size="13" class="spin" /><RefreshCcw v-else :size="13" /></template>
                                                <span>刷新</span>
                                            </a-link>
                                            <a-dropdown trigger="click" position="br">
                                                <a-link class="uvp-table-action uvp-table-action--more">
                                                    <span>更多</span>
                                                    <MoreHorizontal :size="13" />
                                                </a-link>
                                                <template #content>
                                                    <a-doption class="device-action-menu-item" @click="openDevice(record)">
                                                        <Eye :size="14" />
                                                        <span>详情</span>
                                                    </a-doption>
                                                    <a-doption class="device-action-menu-item" @click="openEditDeviceModal(record)">
                                                        <Pencil :size="14" />
                                                        <span>编辑</span>
                                                    </a-doption>
                                                    <a-doption class="device-action-menu-item device-action-menu-item--danger" :disabled="deleting" @click="handleDeleteDevice(record)">
                                                        <Trash2 :size="14" />
                                                        <span>删除</span>
                                                    </a-doption>
                                                </template>
                                            </a-dropdown>
                                        </div>
                                    </template>
                                </a-table-column>
                            </template>
                        </a-table>

                    </div>

                    <div v-else-if="viewMode === 'card'" class="view-body card-view">
                        <div class="card-grid">
                            <template v-if="assetKind === 'device'">
                        <article v-for="item in devices" :key="item.id" class="device-card device-summary-card" @dblclick="openDevice(item)">
                            <div class="device-card-head">
                                <span class="device-card-icon"><Camera :size="18" /></span>
                                <div class="device-card-title">
                                    <a-tooltip :content="deviceNameText(item)" position="top">
                                        <strong class="text-ellipsis">{{ deviceNameText(item) }}</strong>
                                    </a-tooltip>
                                    <a-tooltip :content="item.deviceId" position="top">
                                        <span class="code text-ellipsis">{{ item.deviceId }}</span>
                                    </a-tooltip>
                                </div>
                            </div>
                            <a-tooltip content="查看状态轨迹" position="top">
                                <button
                                    class="device-status-ribbon"
                                    :class="{ online: item.online }"
                                    type="button"
                                    :aria-label="`查看${item.online ? '在线' : '离线'}状态轨迹`"
                                    @click.stop="openStatusEvents(item)"
                                >
                                    {{ item.online ? '在线' : '离线' }}
                                </button>
                            </a-tooltip>
                            <div class="device-card-info">
                                <div><span>设备 ID</span><a-tooltip :content="item.deviceId" position="top"><strong class="mono text-ellipsis">{{ item.deviceId }}</strong></a-tooltip></div>
                                <div><span>厂商</span><strong class="text-ellipsis">{{ item.manufacturer || '未上报' }}</strong></div>
                                <div><span>型号</span><strong class="text-ellipsis">{{ item.model || '未上报' }}</strong></div>
                                <div><span>地址</span><a-tooltip :content="endpointText(item)" position="top"><strong class="mono text-ellipsis">{{ endpointText(item) }}</strong></a-tooltip></div>
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
                                <div><span>最近心跳</span><strong :class="{ warn: !item.online }">{{ dateTime(item.keepaliveTime) }}</strong></div>
                                <div><span>注册时间</span><strong>{{ dateTime(item.registerTime) }}</strong></div>
                            </div>
                            <div class="card-actions device-card-actions">
                                <span class="device-transport">SIP / {{ transportText(item.transport) }}</span>
                                <a-tooltip content="查看通道" position="top">
                                    <button class="icon-btn small framed primary" type="button" @click.stop="showDeviceChannels(item)"><Camera :size="13" /></button>
                                </a-tooltip>
                                <a-tooltip content="查看详情" position="top">
                                    <button class="icon-btn small framed" type="button" @click.stop="openDevice(item)"><Eye :size="13" /></button>
                                </a-tooltip>
                                <a-tooltip :content="item.online ? '刷新通道目录' : '设备离线,无法刷新'" position="top">
                                    <button class="icon-btn small framed info" :class="{ loading: refreshingCatalog[item.id] }" type="button" :disabled="refreshingCatalog[item.id] || !item.online" @click.stop="handleRefreshDeviceCatalog(item)">
                                        <Loader2 v-if="refreshingCatalog[item.id]" :size="13" class="spin" />
                                        <RefreshCcw v-else :size="13" />
                                    </button>
                                </a-tooltip>
                                <a-tooltip content="编辑设备" position="top">
                                    <button class="icon-btn small framed warning" type="button" @click.stop="openEditDeviceModal(item)"><Pencil :size="13" /></button>
                                </a-tooltip>
                                <a-tooltip content="删除设备" position="top">
                                    <button class="icon-btn small framed danger" type="button" :disabled="deleting" @click.stop="handleDeleteDevice(item)"><Trash2 :size="13" /></button>
                                </a-tooltip>
                            </div>
                        </article>
                            </template>
                            <template v-else>
                        <article v-for="item in channels" :key="item.id" class="device-card channel-summary-card" @dblclick="openChannel(item)">
                            <div class="channel-snapshot" :class="{ offline: item.status !== 1 }">
                                <div class="snapshot-empty">
                                    <Video :size="30" />
                                    <span>暂无快照</span>
                                </div>
                                <a-tooltip content="查看详情" position="top">
                                    <button class="snapshot-detail" type="button" @click.stop="openChannel(item)"><Eye :size="15" /></button>
                                </a-tooltip>
                            </div>
                            <div class="channel-card-body">
                                <a-tooltip :content="displayName(item)" position="top"><strong class="channel-card-title text-ellipsis">{{ displayName(item) }}</strong></a-tooltip>
                                <div class="channel-card-info">
                                    <div><span>通道 ID</span><a-tooltip :content="item.channelId" position="top"><strong class="mono text-ellipsis">{{ item.channelId }}</strong></a-tooltip></div>
                                    <div><span>所属设备</span><a-tooltip :content="item.deviceId" position="top"><strong class="mono text-ellipsis">{{ item.deviceId }}</strong></a-tooltip></div>
                                    <div><span>厂商 / 型号</span><strong class="text-ellipsis">{{ vendorText(item) }}</strong></div>
                                    <div><span>位置</span><strong>{{ locationText(item) }}</strong></div>
                                    <div><span>摄像头类型</span><strong>{{ cameraTypeText(item.ptzType) }}</strong></div>
                                    <div><span>流传输模式</span><strong>{{ streamTransportText(item.streamTransport) }}</strong></div>
                                </div>
                                <div class="card-actions channel-card-actions">
                                    <span class="channel-card-status" :class="{ online: item.status === 1 }">{{ item.status === 1 ? '在线' : '离线' }}</span>
                                    <a-tooltip content="点播" position="top"><button class="icon-btn small framed primary" type="button" @click.stop="playChannel(item)"><Play :size="13" /></button></a-tooltip>
                                    <a-tooltip content="编辑通道" position="top"><button class="icon-btn small framed warning" type="button" @click.stop="openEditChannelModal(item)"><Pencil :size="13" /></button></a-tooltip>
                                    <a-tooltip content="删除通道" position="top"><button class="icon-btn small framed danger" type="button" :disabled="deleting" @click.stop="handleDeleteChannel(item)"><Trash2 :size="13" /></button></a-tooltip>
                                </div>
                            </div>
                        </article>
                            </template>
                        </div>
                        <a-pagination
                            v-if="total > 0"
                            class="card-pagination"
                            :current="page"
                            :page-size="pageSize"
                            :total="total"
                            :page-size-options="[12, 24, 48]"
                            show-page-size
                            show-total
                            show-jumper
                            @change="onPageChange"
                            @page-size-change="onPageSizeChange"
                        />
                    </div>

                    <div v-else class="view-body map-view">
                        <div class="map-toolbar">
                            <span>缩放 {{ mapZoom }}</span>
                            <a-slider :model-value="mapZoom" :min="mapMinZoom" :max="mapMaxZoom" :style="{ width: '180px' }" @change="(value: number) => mapInstance?.setZoom(value)" />
                            <button class="btn-ghost" type="button" @click="fitMapToData">定位点位</button>
                            <button class="btn-ghost" type="button" @click="loadMapData">刷新地图</button>
                        </div>
                        <div class="map-canvas">
                            <div ref="mapContainer" class="map-container"></div>
                            <div v-if="mapError" class="map-state map-state-error"><Info :size="16" /> {{ mapError }}</div>
                            <div v-else-if="!mapReady" class="map-state"><Loader2 :size="16" class="spin" /> 正在加载地图</div>
                        </div>
                    </div>

                </main>
            </div>

            <a-drawer v-model:visible="drawerVisible" :width="640" :footer="false" unmount-on-close>
                <template #title>
                    <span v-if="drawerTarget?.type === 'channel'">通道详情</span>
                    <span v-else-if="drawerTarget?.type === 'device'">设备详情</span>
                    <span v-else>目录节点</span>
                </template>
                <a-spin :loading="drawerLoading">
                    <div v-if="drawerTarget?.type === 'channel' && channelDetail" class="drawer-body">
                        <div class="drawer-headline">
                            <span class="drawer-icon"><Video :size="18" /></span>
                            <div><h3>{{ displayName(channelDetail) }}</h3><p>{{ channelDetail.channelId }}</p></div>
                            <span class="status-pill" :class="{ online: channelDetail.status === 1 }">{{ channelDetail.status === 1 ? '在线' : '离线' }}</span>
                        </div>
                        <div class="kv-grid">
                            <span>所属设备</span><strong>{{ channelDetail.deviceId }}</strong>
                            <span>厂商型号</span><strong>{{ vendorText(channelDetail) }}</strong>
                            <span>摄像头类型</span><strong>{{ cameraTypeText(channelDetail.ptzType) }}</strong>
                            <span>父级通道</span><strong>{{ channelDetail.parentId || '无' }}</strong>
                            <span>坐标</span><strong>{{ locationText(channelDetail) }}</strong>
                            <span>流传输模式</span><strong>{{ streamTransportText(channelDetail.streamTransport) }}</strong>
                            <span>当前流</span><strong>{{ channelDetail.streamId || '未播放' }}</strong>
                        </div>
                        <div v-if="channelMounts.length" class="mount-list">
                            <div v-for="mount in channelMounts" :key="mount.id" class="mount-item">
                                <Layers :size="14" />
                                <div>
                                    <strong>{{ mount.displayName || mount.parentName }}</strong>
                                    <span>{{ mount.parentPath || '未记录路径' }}</span>
                                </div>
                            </div>
                        </div>
                        <div v-if="timeline.length" class="timeline-strip">
                            <span v-for="slot in timeline" :key="slot.start" :class="{ online: slot.status === 'online' }"></span>
                        </div>
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
                            <a-button type="primary" @click="openEditChannelModal(channelDetail)">
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
                        <div class="drawer-headline">
                            <span class="drawer-icon"><RadioTower :size="20" /></span>
                            <div class="headline-title">
                                <h3>{{ displayName(deviceDetail) }}</h3>
                                <p v-if="deviceDetail.alias && deviceDetail.name && deviceDetail.alias !== deviceDetail.name" class="headline-alt">
                                    <span class="muted">设备上报:</span> {{ deviceDetail.name }}
                                </p>
                                <p class="mono">
                                    {{ deviceDetail.deviceId }}
                                    <button class="copy-btn" type="button" title="复制设备编码" @click="copyText(deviceDetail.deviceId)">
                                        <Copy :size="12" />
                                    </button>
                                </p>
                            </div>
                            <span class="status-pill" :class="{ online: deviceDetail.online }">
                                <span class="status-dot" />
                                {{ deviceDetail.online ? '在线' : '离线' }}
                            </span>
                        </div>

                        <section v-if="parseCapability(deviceDetail.subscribeCapability).length" class="info-group">
                            <div class="group-label">订阅能力</div>
                            <div class="capability-chips">
                                <span v-for="cap in parseCapability(deviceDetail.subscribeCapability)" :key="cap" class="cap-chip on">{{ cap }}</span>
                            </div>
                        </section>

                        <section class="info-group">
                            <div class="group-label">国标 GB/T 28181</div>
                            <div class="field-grid">
                                <span class="k">设备编码</span>
                                <span class="v mono">{{ deviceDetail.deviceId }}</span>
                                <span class="k">传输协议</span>
                                <span class="v">{{ transportText(deviceDetail.transport) }}</span>
                                <span class="k">注册状态</span>
                                <span class="v">
                                    <span class="inline-dot" :class="{ online: deviceDetail.online }"></span>
                                    {{ deviceDetail.online ? '已注册' : '未注册 / 已离线' }}
                                    <span v-if="deviceDetail.registerTime" class="muted mono">· {{ dateTime(deviceDetail.registerTime) }}</span>
                                </span>
                                <span v-if="deviceDetail.online" class="k">注册过期</span>
                                <span v-if="deviceDetail.online" class="v mono">{{ dateTime(deviceDetail.registerExpireAt) }}</span>
                                <span class="k">最近心跳</span>
                                <span class="v">
                                    {{ relTime(deviceDetail.keepaliveTime) }}
                                    <span v-if="deviceDetail.keepaliveTime" class="muted mono">· {{ dateTime(deviceDetail.keepaliveTime) }}</span>
                                </span>
                                <span class="k">心跳间隔</span>
                                <span class="v">{{ keepaliveIntervalText(deviceDetail.keepaliveInterval) }}</span>
                                <template v-if="!deviceDetail.online && deviceDetail.offlineAt">
                                    <span class="k">离线时间</span>
                                    <span class="v mono">{{ dateTime(deviceDetail.offlineAt) }} <span class="muted">({{ relTime(deviceDetail.offlineAt) }})</span></span>
                                </template>
                            </div>
                        </section>

                        <section class="info-group">
                            <div class="group-label">网络与厂商</div>
                            <div class="field-grid">
                                <span class="k">网络地址</span>
                                <span class="v mono">
                                    {{ deviceDetail.ip || '-' }}<template v-if="deviceDetail.port">:{{ deviceDetail.port }}</template>
                                    <span class="muted">SIP/{{ transportText(deviceDetail.transport) }}</span>
                                </span>
                                <span class="k">厂商 / 型号</span>
                                <span class="v">{{ vendorText(deviceDetail) }}</span>
                                <span class="k">固件版本</span>
                                <span class="v mono">{{ deviceDetail.firmware || '未上报' }}</span>
                            </div>
                        </section>

                        <section class="info-group">
                            <div class="group-label">通道概览</div>
                            <div class="channel-summary">
                                <div class="channel-stats">
                                    <div class="stat-item">
                                        <span class="stat-num">{{ deviceDetail.channelOnlineCount }}</span>
                                        <span class="stat-label">在线通道</span>
                                    </div>
                                    <div class="stat-divider"></div>
                                    <div class="stat-item">
                                        <span class="stat-num">{{ deviceDetail.channelCount }}</span>
                                        <span class="stat-label">总通道</span>
                                    </div>
                                    <div class="stat-divider"></div>
                                    <div class="stat-item">
                                        <span class="stat-num">{{ onlineRatePercent(deviceDetail.onlineRate) }}%</span>
                                        <span class="stat-label">在线率</span>
                                    </div>
                                </div>
                                <div class="online-progress">
                                    <div class="progress-track">
                                        <div class="progress-fill" :style="{ width: `${onlineRatePercent(deviceDetail.onlineRate)}%` }"></div>
                                    </div>
                                </div>
                            </div>
                        </section>

                        <section class="info-group meta-group">
                            <div class="group-label">元数据</div>
                            <div class="field-grid">
                                <span class="k">创建时间</span>
                                <span class="v mono">{{ dateTime(deviceDetail.createdAt) }}</span>
                                <span class="k">更新时间</span>
                                <span class="v mono">{{ dateTime(deviceDetail.updatedAt) }}</span>
                            </div>
                        </section>

                        <div class="drawer-foot">
                            <a-button type="primary" @click="handleRefreshDeviceCatalog(deviceDetail)">
                                <template #icon><RefreshCcw :size="14" /></template>
                                <template #default>刷新目录</template>
                            </a-button>
                            <a-button @click="copyText(deviceDetail.deviceId)">
                                <template #icon><Copy :size="14" /></template>
                                <template #default>复制编码</template>
                            </a-button>
                        </div>
                    </div>
                    <div v-else-if="drawerTarget?.type === 'node'" class="drawer-body">
                        <div class="drawer-headline">
                            <span class="drawer-icon"><FolderTree :size="18" /></span>
                            <div><h3>{{ drawerTarget.node.name }}</h3><p>{{ drawerTarget.node.path }}</p></div>
                        </div>
                        <div class="kv-grid">
                            <span>编码</span><strong>{{ drawerTarget.node.code || '-' }}</strong>
                            <span>行政区划</span><strong>{{ drawerTarget.node.civilCode || '-' }}</strong>
                            <span>来源</span><strong>{{ drawerTarget.node.source }}</strong>
                            <span>挂载数</span><strong>{{ drawerTarget.node.mountCount || 0 }}</strong>
                        </div>
                    </div>
                </a-spin>
            </a-drawer>

            <ControlConsole
                v-model:visible="controlConsoleVisible"
                :channel="controlConsoleChannel"
            />

            <a-modal
                v-model:visible="statusEventVisible"
                modal-class="uvp-system-dialog status-event-dialog"
                title="设备状态轨迹"
                :width="720"
                :footer="false"
                unmount-on-close
                @close="closeStatusEvents"
            >
                <div v-if="statusEventDevice" class="status-event-body">
                    <header class="status-event-summary">
                        <div class="status-event-device">
                            <span class="summary-icon"><History :size="18" /></span>
                            <div>
                                <strong>{{ displayName(statusEventDevice) }}</strong>
                                <span class="mono">{{ statusEventDevice.deviceId }}</span>
                            </div>
                        </div>
                        <span class="status-pill status-event-current-status" :class="{ online: statusEventDevice.online }">
                            <span class="status-dot"></span>
                            {{ statusEventDevice.online ? '在线' : '离线' }}
                        </span>
                    </header>
                    <div class="status-event-facts">
                        <div><span>最近注册</span><strong>{{ dateTime(statusEventDevice.registerTime) }}</strong></div>
                        <div><span>最近心跳</span><strong>{{ dateTime(statusEventDevice.keepaliveTime) }}</strong></div>
                        <div><span>来源地址</span><strong class="mono">{{ endpointText(statusEventDevice) }}</strong></div>
                    </div>

                    <div ref="statusEventScroll" class="status-event-scroll" @scroll.passive="onStatusEventScroll">
                        <a-spin :loading="statusEventLoading" class="status-event-content">
                            <div v-if="statusEventError" class="status-event-state error">
                                <strong>状态轨迹加载失败</strong>
                                <span>{{ statusEventError }}</span>
                                <a-button size="small" @click="loadStatusEvents()">重试</a-button>
                            </div>
                            <a-empty v-else-if="!statusEventLoading && statusEventList.length === 0" description="暂无状态事件" />
                            <a-timeline v-else class="status-event-timeline">
                                <a-timeline-item v-for="event in statusEventList" :key="event.id" :label="statusEventTime(event.occurredAt)" :dot-color="eventColor(event.eventType)" dot-type="hollow">
                                    <div class="status-event-item" :data-event="event.eventType" :title="eventMetaText(event)">
                                        <strong>{{ event.eventName }}</strong>
                                    </div>
                                </a-timeline-item>
                            </a-timeline>
                        </a-spin>
                        <div v-if="statusEventList.length" class="status-event-load-more">
                            <span v-if="statusEventLoadingMore"><Loader2 :size="14" class="spin" /> 正在加载更多</span>
                            <span v-else-if="statusEventLoadMoreError" class="error">
                                {{ statusEventLoadMoreError }}
                                <button type="button" @click="loadMoreStatusEvents">重试</button>
                            </span>
                            <span v-else-if="!statusEventHasMore">已展示全部 {{ statusEventTotal }} 条</span>
                            <span v-else>向下滚动加载更多 · 已加载 {{ statusEventList.length }}/{{ statusEventTotal }} 条</span>
                        </div>
                    </div>
                </div>
            </a-modal>

            <a-modal
                v-model:visible="createDeviceVisible"
                modal-class="uvp-system-dialog"
                title="新建设备"
                :width="480"
                :mask-closable="false"
                unmount-on-close
                @cancel="createDeviceVisible = false"
            >
                <a-form
                    ref="createDeviceFormRef"
                    :model="createDeviceForm"
                    :rules="createDeviceRules"
                    layout="vertical"
                >
                    <a-form-item field="deviceId" label="设备国标 ID" validate-trigger="blur">
                        <a-input
                            v-model="createDeviceForm.deviceId"
                            placeholder="请输入 20 位国标设备 ID"
                            :maxlength="20"
                            allow-clear
                        >
                            <template #suffix>
                                <span class="input-counter" :class="{ done: (createDeviceForm.deviceId?.length || 0) === 20 }">
                                    {{ createDeviceForm.deviceId?.length || 0 }} / 20
                                </span>
                            </template>
                        </a-input>
                    </a-form-item>
                    <a-form-item field="name" label="设备名称">
                        <a-input
                            v-model="createDeviceForm.name"
                            placeholder="选填,方便识别"
                            allow-clear
                        />
                    </a-form-item>
                    <a-form-item field="password" label="设备密码">
                        <a-input-password
                            v-model="createDeviceForm.password"
                            placeholder="选填,用于一设备一密码场景"
                            allow-clear
                        />
                    </a-form-item>
                </a-form>
                <template #footer>
                    <a-button @click="createDeviceVisible = false">取消</a-button>
                    <a-button type="primary" :loading="creatingDevice" @click="handleCreateDevice">创建</a-button>
                </template>
            </a-modal>

            <!-- 编辑设备 Modal -->
            <a-modal
                v-model:visible="editDeviceVisible"
                modal-class="uvp-system-dialog"
                title="编辑设备"
                :width="480"
                :mask-closable="false"
                unmount-on-close
                @cancel="cancelEditDevice"
            >
                <a-form
                    ref="editDeviceFormRef"
                    :model="editDeviceForm"
                    layout="vertical"
                >
                    <a-form-item label="设备国标 ID">
                        <a-input
                            :model-value="editDeviceForm.deviceId"
                            disabled
                            class="code-main mono"
                        />
                    </a-form-item>
                    <a-form-item label="设备上报名称">
                        <a-input
                            :model-value="editDeviceForm.name"
                            disabled
                            placeholder="设备未上报"
                        />
                        <template #extra>
                            <span class="form-hint">来自设备 DeviceInfo 应答,不可编辑</span>
                        </template>
                    </a-form-item>
                    <a-form-item field="alias" label="设备别名">
                        <a-input
                            v-model="editDeviceForm.alias"
                            placeholder="选填,给设备起一个好记的名字"
                            allow-clear
                        />
                        <template #extra>
                            <span class="form-hint">优先展示,不会被设备重新注册覆盖</span>
                        </template>
                    </a-form-item>
                    <a-form-item field="manufacturer" label="厂商">
                        <a-input
                            v-model="editDeviceForm.manufacturer"
                            placeholder="选填"
                            allow-clear
                        />
                    </a-form-item>
                    <a-form-item field="model" label="型号">
                        <a-input
                            v-model="editDeviceForm.model"
                            placeholder="选填"
                            allow-clear
                        />
                    </a-form-item>
                    <a-form-item field="firmware" label="固件版本">
                        <a-input
                            v-model="editDeviceForm.firmware"
                            placeholder="选填"
                            allow-clear
                        />
                    </a-form-item>
                </a-form>
                <template #footer>
                    <a-button @click="cancelEditDevice">取消</a-button>
                    <a-button type="primary" :loading="editingDevice" @click="handleEditDevice">保存</a-button>
                </template>
            </a-modal>

            <!-- 编辑通道 Modal -->
            <a-modal
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
                        <a-input :model-value="editChannelForm.channelId" disabled class="code-main mono" />
                    </a-form-item>
                    <a-form-item label="所属设备">
                        <a-input :model-value="editChannelForm.deviceId" disabled class="code-main mono" />
                    </a-form-item>
                    <a-form-item label="通道名称">
                        <a-input :model-value="editChannelForm.name" disabled placeholder="设备上报名称" />
                        <template #extra><span class="form-hint">来自设备 Catalog 应答,不可编辑</span></template>
                    </a-form-item>
                    <a-form-item label="通道别名">
                        <a-input v-model="editChannelForm.alias" placeholder="选填,给通道起一个好记的名称" allow-clear />
                        <template #extra><span class="form-hint">优先展示,不会被设备 Catalog 上报覆盖</span></template>
                    </a-form-item>
                    <a-form-item label="厂商 / 型号">
                        <a-input :model-value="vendorText(editChannelForm)" disabled />
                        <template #extra><span class="form-hint">来自设备 Catalog 应答,不可编辑</span></template>
                    </a-form-item>
                    <a-form-item label="摄像头类型">
                        <a-select v-model="editChannelForm.ptzType">
                            <a-option v-for="opt in ptzTypeOptions" :key="opt.value" :value="Number(opt.value)">{{ opt.name }}</a-option>
                        </a-select>
                    </a-form-item>
                    <a-form-item label="流传输模式">
                        <a-select v-model="editChannelForm.streamTransport">
                            <a-option value="UDP">UDP</a-option>
                            <a-option value="TCP-Active">TCP-Active</a-option>
                            <a-option value="TCP-Passive">TCP-Passive</a-option>
                        </a-select>
                    </a-form-item>
                </a-form>
                <template #footer>
                    <a-button @click="cancelEditChannel">取消</a-button>
                    <a-button type="primary" :loading="editingChannel" @click="handleEditChannel">保存</a-button>
                </template>
            </a-modal>
        </div>
    </div>
</template>

<style scoped lang="scss">
.device-mgmt-page {
    height: 100%;
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 0;
    background: var(--uvp-navigation-bg);
}
.content-topbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    min-height: 52px;
    padding: 0 4px;
}
.device-stats {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    flex: 0 0 auto;
}
.device-stats .stat {
    display: inline-flex;
    align-items: center;
    height: 30px;
    padding: 0 10px;
    border-radius: 10px;
    font-size: 13px;
    font-weight: 620;
    background: var(--uvp-panel-bg);
    border: 1px solid var(--uvp-panel-border);
}
.device-stats .online {
    color: var(--uvp-brand-cyan);
}
.device-stats .offline {
    color: var(--uvp-text-tertiary);
}
.topbar-actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 10px;
    flex: 1;
    min-width: 0;
}
.cmdk {
    display: flex;
    align-items: center;
    gap: 8px;
    width: min(520px, 100%);
    max-width: 42vw;
    height: 36px;
    padding: 0 12px;
    color: var(--uvp-text-tertiary);
    background: var(--uvp-search-control-bg);
    border: 1px solid var(--uvp-search-secondary-btn-border);
    border-radius: 10px;
}
.cmdk input {
    flex: 1;
    min-width: 0;
    color: var(--uvp-text-primary);
    background: transparent;
    border: 0;
    outline: none;
}
.cmdk-clear {
    width: 22px;
    height: 22px;
    display: inline-grid;
    flex: 0 0 auto;
    place-items: center;
    color: var(--uvp-text-tertiary);
    background: transparent;
    border: 0;
    border-radius: 5px;
}
.cmdk-clear:hover { color: var(--uvp-text-primary); background: var(--uvp-brand-soft); }
.cmdk .kbd { margin-left: auto; display: inline-flex; gap: 2px; font-size: 11px; }
.cmdk kbd { padding: 1px 5px; border: 1px solid var(--uvp-panel-border); border-radius: 4px; }
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
    width: 32px;
    height: 32px;
    display: inline-grid;
    place-items: center;
    color: var(--uvp-text-tertiary);
    background: transparent;
    border: 0;
    border-radius: 8px;
}
.view-switch button.active,
.icon-btn:hover { color: var(--uvp-brand); background: var(--uvp-brand-soft); }
.icon-btn.small { width: 24px; height: 24px; }
.btn-primary,
.btn-ghost,
.btn-danger,
.play-cta {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    height: 32px;
    padding: 0 12px;
    border-radius: 10px;
    white-space: nowrap;
}
.btn-primary {
    color: #fff;
    background: var(--uvp-brand);
    border: 0;
    font-weight: 600;
}
.btn-primary.long { width: 100%; }
.btn-danger {
    color: #fff;
    background: #ef4444;
    border: 0;
    font-weight: 600;
}
.btn-danger:hover {
    background: #dc2626;
}
.btn-danger:disabled {
    opacity: 0.5;
    cursor: not-allowed;
}
.btn-ghost {
    color: var(--uvp-text-secondary);
    background: var(--uvp-search-secondary-btn-bg);
    border: 1px solid var(--uvp-search-secondary-btn-border);
    cursor: pointer;
    transition: color 0.15s ease, background 0.15s ease, border-color 0.15s ease, transform 0.1s ease;
}
.btn-ghost:hover {
    color: var(--uvp-brand);
    background: var(--uvp-brand-soft);
    border-color: color-mix(in srgb, var(--uvp-brand) 32%, transparent);
}
.btn-ghost:active {
    transform: scale(0.96);
    background: color-mix(in srgb, var(--uvp-brand) 14%, var(--uvp-brand-soft));
}
.btn-ghost.active {
    color: var(--uvp-brand);
    background: var(--uvp-brand-soft);
    border-color: color-mix(in srgb, var(--uvp-brand) 28%, transparent);
}
.workspace {
    display: grid;
    grid-template-columns: 280px minmax(0, 1fr);
    gap: 16px;
    min-height: 0;
    flex: 1;
}
.catalog-pane,
.content-pane {
    min-width: 0;
    min-height: 0;
    background: var(--uvp-panel-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 14px;
    box-shadow: var(--uvp-panel-shadow);
}
.catalog-pane {
    display: flex;
    flex-direction: column;
    overflow: hidden;
}
.pane-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    min-height: 48px;
    padding: 0 14px;
    border-bottom: 1px solid var(--uvp-panel-border);
}
.head-title { display: inline-flex; align-items: center; gap: 8px; color: var(--uvp-text-primary); font-weight: 620; }
.aside-search { padding: 12px 14px 10px; }
.aside-search input {
    box-sizing: border-box;
    width: 100%;
    height: 32px;
    padding: 0 10px;
    color: var(--uvp-text-primary);
    background: var(--uvp-search-control-bg);
    border: 1px solid var(--uvp-search-secondary-btn-border);
    border-radius: 10px;
}
.tree-wrap { flex: 1; min-height: 0; }
.tree { padding: 0 8px 12px; }
.tree-row {
    display: flex;
    align-items: center;
    min-height: 34px;
    border-radius: 10px;
    color: var(--uvp-text-secondary);
}
.tree-row.active,
.tree-row:hover { background: var(--uvp-sidebar-active-bg); color: var(--uvp-text-primary); }
.tree-row.anomaly { box-shadow: inset 3px 0 0 var(--uvp-warning); }
.twist {
    width: 22px;
    height: 22px;
    display: inline-grid;
    place-items: center;
    color: var(--uvp-text-tertiary);
    background: transparent;
    border: 0;
}
.twist.hidden { visibility: hidden; }
.spin { animation: spin 0.8s linear infinite; }
.tree-node-btn {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: 1;
    min-width: 0;
    height: 30px;
    padding: 0 8px 0 0;
    text-align: left;
    color: inherit;
    background: transparent;
    border: 0;
}
.node-chip,
.drawer-icon {
    width: 22px;
    height: 22px;
    display: inline-grid;
    place-items: center;
    border-radius: 7px;
    color: var(--uvp-brand);
    background: var(--uvp-brand-soft);
    flex: 0 0 auto;
}
.node-chip.biz { color: var(--uvp-brand-cyan); }
.node-chip.virtual { color: var(--uvp-warning); background: var(--uvp-warning-soft); }
.label { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; }
.count { margin-left: auto; color: var(--uvp-text-tertiary); font-size: 12px; }
.content-pane {
    display: flex;
    flex-direction: column;
    padding: 14px 14px 12px;
}
.main-head {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 12px;
    min-height: 64px;
}
.device-filter-panel {
    width: 100%;
    margin-bottom: 0;
}
.toolbar {
    display: flex;
    align-items: center;
    flex-wrap: nowrap;
    gap: 10px;
    width: 100%;
    justify-content: flex-end;
    min-width: 0;
}
.auto-refresh-control {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 0 12px;
    height: 36px;
    background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 10px;
}
.refresh-label {
    font-size: 13px;
    font-weight: 500;
    color: var(--uvp-text-secondary);
    white-space: nowrap;
}
.segmented {
    display: inline-flex;
    gap: 2px;
    flex: 0 0 auto;
    order: 10;
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
    color: var(--uvp-text-secondary);
    background: transparent;
    border: 0;
    border-radius: 8px;
    font-size: 12px;
    line-height: 30px;
    white-space: nowrap;
}
.segmented button.active {
    color: var(--uvp-brand);
    background: var(--uvp-brand-soft);
    font-weight: 620;
}
.toolbar-select {
    flex: 0 0 auto;
}
.status-select {
    min-width: 126px;
}
.toolbar > .btn-ghost {
    flex: 0 0 auto;
    min-width: 84px;
    height: 40px;
    border-radius: 8px;
}
.toolbar > .icon-btn {
    flex: 0 0 auto;
    width: 40px;
    height: 40px;
    border-radius: 8px;
}
.filter-chips {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    padding-top: 12px;
    padding-bottom: 12px;
}
.filter-chip,
.clear-all {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 28px;
    padding: 0 10px;
    border-radius: 10px;
    font-size: 12px;
}
.filter-chip {
    color: var(--uvp-brand);
    background: var(--uvp-brand-soft);
    border: 1px solid color-mix(in srgb, var(--uvp-brand) 24%, transparent);
}
.filter-chip .close { background: transparent; border: 0; color: inherit; }
.clear-all { color: var(--uvp-text-secondary); background: transparent; border: 0; }
.batch-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    min-height: 54px;
    padding: 0 14px;
    margin-bottom: 10px;
    background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 12px;
}
.batch-info strong { color: var(--uvp-brand); }
.batch-ops { display: flex; gap: 8px; }
.view-body { min-width: 0; flex: 1; min-height: 0; }
.table-view { display: flex; flex-direction: column; gap: 10px; }
.uvp-data-table :deep(.arco-table-body.arco-scrollbar-container) { height: calc(100% - 15px); }
.thumb {
    width: 48px;
    height: 27px;
    display: grid;
    place-items: center;
    overflow: hidden;
    border-radius: 6px;
    background: linear-gradient(135deg, #0f172a, #1d4ed8);
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
.thumb.brand-thumb {
    width: 40px;
    height: 40px;
    border-radius: 10px;
    background: linear-gradient(135deg, color-mix(in srgb, var(--uvp-brand) 86%, #ffffff 14%), color-mix(in srgb, var(--uvp-brand-cyan) 82%, #0f172a 18%));
}
.placeholder { color: #fff; font-size: 10px; font-weight: 700; }
.brand-cell {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
}
.brand-name {
    overflow: hidden;
    color: var(--uvp-text-primary);
    font-size: 13px;
    font-weight: 600;
    text-overflow: ellipsis;
    white-space: nowrap;
}
.device-name { display: grid; gap: 2px; }
.device-name .pri { min-width: 0; }
.pri { display: inline-flex; align-items: center; gap: 6px; color: var(--uvp-text-primary); font-weight: 600; }
.pri.mono,
.sec,
.code-main,
.code-sub {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.sec { color: var(--uvp-text-tertiary); font-size: 12px; }
.code-cell,
.time-cell {
    display: grid;
    gap: 3px;
    min-width: 0;
}
.code-main {
    color: var(--uvp-text-primary);
    font-size: 12px;
    font-weight: 620;
    white-space: nowrap;
}
.code-main.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.code-sub {
    overflow: hidden;
    color: var(--uvp-text-tertiary);
    font-size: 11px;
    text-overflow: ellipsis;
    white-space: nowrap;
}
.time-cell span:first-child {
    color: var(--uvp-text-secondary);
    font-size: 12px;
}
.time-cell span:last-child {
    color: var(--uvp-text-tertiary);
    font-size: 12px;
}
.time-cell .warn {
    color: var(--uvp-warning);
}
.mount-badge {
    display: inline-flex;
    align-items: center;
    height: 18px;
    padding: 0 6px;
    border-radius: 999px;
    color: var(--uvp-warning);
    background: var(--uvp-warning-soft);
    font-size: 11px;
}
.vendor-cell { display: grid; gap: 2px; }
.vendor-cell .v { color: var(--uvp-text-primary); }
.vendor-cell .m { color: var(--uvp-text-tertiary); font-size: 12px; }
.tag {
    display: inline-flex;
    align-items: center;
    height: 24px;
    padding: 0 8px;
    border-radius: 999px;
    color: var(--uvp-brand);
    background: var(--uvp-brand-soft);
    font-size: 12px;
}
.tag.muted {
    color: var(--uvp-text-secondary);
    background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border);
}
.status-pill {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 52px;
    padding: 3px 8px;
    border-radius: 999px;
    color: var(--uvp-text-tertiary);
    background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border);
    font-size: 12px;
    font-weight: 650;
}
.status-pill.online {
    color: var(--uvp-brand-cyan);
    background: color-mix(in srgb, var(--uvp-brand-cyan) 12%, transparent);
    border-color: color-mix(in srgb, var(--uvp-brand-cyan) 28%, transparent);
}
.relative { color: var(--uvp-text-secondary); font-size: 12px; }
.relative.warn { color: var(--uvp-warning); }
.status-inline {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--uvp-text-secondary);
    font-size: 12px;
    font-weight: 600;
}
.status-inline .status-dot {
    width: 8px;
    height: 8px;
    box-shadow: none;
    background: #ef4444;
}
.status-inline.online {
    color: var(--uvp-text-primary);
}
.status-inline.online .status-dot {
    background: #10b981;
}
.status-trigger {
    justify-content: center;
    flex-wrap: nowrap;
    width: 64px;
    min-width: 64px;
    height: 24px;
    padding: 0 6px;
    line-height: 1;
    white-space: nowrap;
    color: var(--uvp-danger);
    background: var(--uvp-danger-soft);
    border: 1px solid var(--uvp-danger-border);
    border-radius: 6px;
    box-shadow: inset 0 1px 0 color-mix(in srgb, #ffffff 72%, transparent);
    cursor: pointer;
    transition: color 0.16s ease, background 0.16s ease, border-color 0.16s ease, box-shadow 0.16s ease, transform 0.16s ease;
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
    align-items: center;
    gap: 8px;
}
.progress-bar {
    flex: 1;
    height: 6px;
    background: var(--uvp-panel-border);
    border-radius: 3px;
    overflow: hidden;
    position: relative;
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
    font-size: 12px;
    font-weight: 500;
    color: var(--uvp-text-secondary);
    white-space: nowrap;
    min-width: 36px;
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
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 100%;
}
.device-name.text-ellipsis {
    max-width: 100%;
}
.play-cta {
    color: var(--uvp-brand);
    background: var(--uvp-brand-soft);
    border: 1px solid color-mix(in srgb, var(--uvp-brand) 24%, transparent);
    font-size: 12px;
}
.uvp-data-table :deep(.uvp-table-actions) { white-space: nowrap; }
.uvp-data-table :deep(.uvp-table-action) { flex: 0 0 auto; }
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--preview) { color: #2563eb; }
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--preview:hover) { color: #1d4ed8; background: rgb(37 99 235 / 8%); }
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--detail) { color: #0f7490; }
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--detail:hover) { color: #0e7490; background: rgb(14 116 144 / 8%); }
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--sync) { color: #0f766e; }
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--sync:hover) { color: #0f675f; background: rgb(15 118 110 / 8%); }
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--edit) { color: #b7791f; }
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--edit:hover) { color: #9a6b18; background: rgb(183 121 31 / 9%); }
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--more) { color: #6b4f9b; }
.device-mgmt-page :deep(.uvp-data-table .uvp-table-action--more:hover) { color: #5a3f89; background: rgb(107 79 155 / 8%); }
:global(.arco-dropdown:has(.device-action-menu-item)) {
    padding: 4px;
    background: var(--uvp-panel-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 8px;
    box-shadow: 0 12px 26px -16px rgb(15 23 42 / 40%);
}
:global(.arco-dropdown:has(.device-action-menu-item) .arco-dropdown-list) { padding: 0; }
:global(.arco-dropdown:has(.device-action-menu-item) .device-action-menu-item) {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 112px;
    height: 32px;
    padding: 0 9px;
    border-radius: 6px;
    color: var(--uvp-text-secondary);
    font-size: 13px;
}
:global(.arco-dropdown:has(.device-action-menu-item) .device-action-menu-item:hover) {
    color: var(--uvp-brand-strong);
    background: color-mix(in srgb, var(--uvp-brand) 7%, var(--uvp-list-toolbar-bg));
}
:global(.arco-dropdown:has(.device-action-menu-item) .device-action-menu-item--danger) {
    margin-top: 4px;
    border-top: 1px solid var(--uvp-panel-border);
    border-radius: 0 0 6px 6px;
    color: #d14343;
}
:global(.arco-dropdown:has(.device-action-menu-item) .device-action-menu-item--danger:hover) {
    color: #ba2f2f;
    background: rgb(209 67 67 / 8%);
}
.btn-ghost.compact {
    height: 28px;
    padding: 0 10px;
    border-radius: 8px;
    font-size: 12px;
}
.icon-btn.framed.primary {
    color: var(--uvp-brand);
    background: var(--uvp-brand-soft);
    border: 1px solid color-mix(in srgb, var(--uvp-brand) 24%, transparent);
}
.icon-btn.framed.primary:hover {
    color: #fff;
    background: var(--uvp-brand);
    border-color: var(--uvp-brand);
}
.icon-btn.framed.info {
    color: var(--uvp-brand-cyan);
    background: color-mix(in srgb, var(--uvp-brand-cyan) 8%, transparent);
    border: 1px solid color-mix(in srgb, var(--uvp-brand-cyan) 20%, var(--uvp-search-secondary-btn-border));
}
.icon-btn.framed.info:hover {
    color: #fff;
    background: var(--uvp-brand-cyan);
    border-color: var(--uvp-brand-cyan);
}
.icon-btn.framed.warning {
    color: #f59e0b;
    background: color-mix(in srgb, #f59e0b 8%, transparent);
    border: 1px solid color-mix(in srgb, #f59e0b 20%, var(--uvp-search-secondary-btn-border));
}
.icon-btn.framed.warning:hover {
    color: #fff;
    background: #f59e0b;
    border-color: #f59e0b;
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
    color: #fff;
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
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    grid-auto-rows: max-content;
    align-content: start;
    align-items: start;
    gap: 12px;
    min-height: 0;
    flex: 1;
    overflow-x: hidden;
    overflow-y: auto;
    padding-right: 4px;
}
.card-pagination {
    flex: 0 0 auto;
    display: flex;
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
    border-radius: 999px;
    background: var(--uvp-warning);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--uvp-warning) 15%, transparent);
}
.status-dot.online {
    background: #10b981;
    box-shadow: 0 0 0 3px rgb(16 185 129 / 14%);
}
.card-actions { display: flex; align-items: center; gap: 8px; }
.device-summary-card {
    position: relative;
    display: grid;
    gap: 10px;
    align-self: start;
    min-height: 250px;
    padding: 12px;
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
.device-card-title { min-width: 0; display: grid; gap: 3px; }
.device-card-title strong { color: var(--uvp-text-primary); }
.device-card-title .code { color: var(--uvp-text-tertiary); font-size: 12px; }
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
    color: #fff;
    background: #ef4444;
    transform: rotate(45deg);
    font-size: 12px;
    font-weight: 600;
    line-height: 1;
    border: 0;
    cursor: pointer;
    transition: filter 0.16s ease, box-shadow 0.16s ease;
}
.device-status-ribbon:hover {
    filter: brightness(0.94);
    box-shadow: 0 4px 10px -7px rgb(15 23 42 / 55%);
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
.device-card-info > div { display: contents; }
.device-card-info span { color: var(--uvp-text-tertiary); }
.device-card-info strong { min-width: 0; color: var(--uvp-text-primary); font-weight: 500; }
.device-card-info .mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.device-card-channel-value { min-width: 0; }
.device-card-channel-value .channel-progress { width: 100%; }
.device-card-channel-value .channel-text { font-weight: 500; }
.device-card-actions {
    gap: 7px;
    padding-top: 9px;
    border-top: 1px solid var(--uvp-panel-border);
}
.device-transport {
    margin-right: auto;
    padding: 4px 8px;
    color: var(--uvp-text-secondary);
    background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 6px;
    font-size: 11px;
    white-space: nowrap;
}
.channel-summary-card { min-width: 0; }
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
    background-position: 0 0, 0 8px, 8px -8px, -8px 0;
    background-size: 16px 16px;
    opacity: 0.38;
}
.channel-snapshot.offline { color: var(--uvp-text-tertiary); opacity: 0.72; }
.snapshot-empty {
    position: relative;
    z-index: 1;
    display: grid;
    justify-items: center;
    gap: 7px;
    font-size: 12px;
}
.snapshot-empty svg { opacity: 0.7; }
.snapshot-detail {
    position: absolute;
    z-index: 1;
    top: 9px;
    right: 9px;
    display: grid;
    place-items: center;
    width: 30px;
    height: 30px;
    padding: 0;
    color: #fff;
    background: rgb(15 23 42 / 72%);
    border: 0;
    border-radius: 50%;
    cursor: pointer;
    transition: background 0.15s, transform 0.15s;
}
.snapshot-detail:hover {
    background: var(--uvp-brand);
    transform: translateY(-1px);
}
.channel-card-body {
    display: grid;
    gap: 10px;
    padding: 11px 12px 12px;
}
.channel-card-title {
    min-width: 0;
    color: var(--uvp-text-primary);
    font-size: 14px;
    line-height: 20px;
}
.channel-card-info {
    display: grid;
    grid-template-columns: 68px minmax(0, 1fr);
    gap: 5px 8px;
    font-size: 12px;
    line-height: 18px;
}
.channel-card-info > div { display: contents; }
.channel-card-info span { color: var(--uvp-text-tertiary); }
.channel-card-info strong {
    min-width: 0;
    color: var(--uvp-text-primary);
    font-weight: 500;
}
.channel-card-info .mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.channel-card-actions {
    gap: 7px;
    padding-top: 9px;
    border-top: 1px solid var(--uvp-panel-border);
}
.channel-card-status {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    margin-right: auto;
    color: var(--uvp-text-tertiary);
    font-size: 12px;
}
.channel-card-status::before {
    width: 6px;
    height: 6px;
    content: "";
    background: var(--uvp-text-tertiary);
    border-radius: 50%;
}
.channel-card-status.online { color: var(--uvp-brand-cyan); }
.channel-card-status.online::before { background: var(--uvp-brand-cyan); }
.map-view { display: flex; flex-direction: column; gap: 10px; }
.map-banner {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 8px 10px;
    color: var(--uvp-warning);
    background: var(--uvp-warning-soft);
    border: 1px solid var(--uvp-warning-border);
    border-radius: 10px;
}
.toolbar-map-banner {
    flex: 1 1 auto;
    min-width: 0;
    overflow: hidden;
    padding: 0 10px;
    height: 36px;
    white-space: nowrap;
    text-overflow: ellipsis;
}
.toolbar-map-banner svg { flex: 0 0 auto; }
.map-toolbar {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
    color: var(--uvp-text-tertiary);
}
@media (max-width: 960px) {
    .toolbar-map-banner {
        max-width: 42%;
    }
}
.map-canvas {
    position: relative;
    min-height: 460px;
    overflow: hidden;
    border: 1px solid var(--uvp-panel-border);
    border-radius: 12px;
}
.map-container {
    position: absolute;
    inset: 0;
}
.map-state {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    color: var(--uvp-text-tertiary);
    background: var(--uvp-list-panel-bg);
}
.map-state-error {
    color: var(--uvp-danger);
    text-align: center;
    padding: 24px;
}
.map-cluster-marker,
.map-channel-marker {
    display: grid;
    place-items: center;
    border: 0;
    cursor: pointer;
    font: inherit;
}
.map-cluster-marker {
    width: 40px;
    height: 40px;
    color: #fff;
    background: radial-gradient(circle at center, color-mix(in srgb, var(--uvp-brand-cyan) var(--cluster-rate), var(--uvp-brand) var(--cluster-rate)), var(--uvp-brand));
    border: 3px solid color-mix(in srgb, #fff 75%, transparent);
    border-radius: 50%;
    box-shadow: 0 2px 10px rgb(0 0 0 / 35%);
    font-size: 12px;
    font-weight: 700;
}
.map-channel-marker {
    width: 18px;
    height: 18px;
    background: var(--uvp-text-tertiary);
    border: 3px solid rgb(255 255 255 / 85%);
    border-radius: 50% 50% 50% 0;
    transform: rotate(-45deg);
    box-shadow: 0 2px 8px rgb(0 0 0 / 35%);
}
.map-channel-marker.online { background: var(--uvp-brand-cyan); }
.map-channel-pip {
    width: 4px;
    height: 4px;
    background: #fff;
    border-radius: 50%;
}
.maplibregl-ctrl-group {
    overflow: hidden;
    border: 1px solid rgb(255 255 255 / 14%);
    background: rgb(13 19 28 / 88%);
    box-shadow: 0 2px 8px rgb(0 0 0 / 22%);
}
.maplibregl-ctrl-group button {
    filter: invert(1) brightness(1.8);
}
.maplibregl-ctrl-attrib {
    color: rgb(255 255 255 / 75%);
    background: rgb(13 19 28 / 70%);
}
.maplibregl-ctrl-attrib a { color: rgb(255 255 255 / 82%); }
/* Keep attribution readable on the dark basemap. */
.maplibregl-ctrl-attrib a { text-decoration: none; }
/* The map occupies the full content area; overlays are DOM markers. */
.map-canvas > .map-container {
    inset: 0;
}
.drawer-body { display: grid; gap: 14px; }
.drawer-headline {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    gap: 10px;
    align-items: center;
    padding: 12px;
    background: var(--uvp-list-panel-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 12px;
}
.drawer-headline.warning { border-color: var(--uvp-warning-border); }
.drawer-headline h3 { margin: 0; color: var(--uvp-text-primary); }
.drawer-headline p { margin: 2px 0 0; color: var(--uvp-text-tertiary); font-size: 12px; }
.kv-grid {
    display: grid;
    grid-template-columns: 92px minmax(0, 1fr);
    gap: 10px 12px;
}
.kv-grid span { color: var(--uvp-text-tertiary); }
.kv-grid strong { color: var(--uvp-text-primary); overflow-wrap: anywhere; }
.mount-list { display: grid; gap: 8px; }
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
.mount-item span { display: block; color: var(--uvp-text-tertiary); font-size: 12px; }
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
.timeline-strip span.online { background: var(--uvp-brand-cyan); }

/* ============ 详情抽屉 · 增强(2026-07-18) ============ */
.drawer-body { padding-bottom: 8px; }
.drawer-headline {
    padding: 14px 16px;
    background: linear-gradient(180deg, var(--uvp-list-panel-bg) 0%, var(--uvp-panel-bg) 100%);
}
.drawer-headline .drawer-icon {
    width: 36px;
    height: 36px;
    border-radius: 10px;
}
.drawer-headline .headline-title { min-width: 0; }
.drawer-headline .headline-title h3 {
    font-size: 15px;
    font-weight: 650;
    line-height: 1.35;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}
.drawer-headline .headline-title p {
    display: inline-flex;
    align-items: center;
    gap: 4px;
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
    color: var(--uvp-text-tertiary);
    opacity: 0.7;
    margin-right: 2px;
}
.status-pill {
    gap: 5px;
    padding: 4px 10px;
    min-width: 60px;
}
.status-pill .status-dot {
    width: 6px;
    height: 6px;
    border-radius: 999px;
    background: currentColor;
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
    border: 1px solid currentColor;
    border-radius: 50%;
    content: "";
    animation: status-event-ripple 1.8s cubic-bezier(0.2, 0.7, 0.3, 1) infinite;
}
@keyframes status-event-ripple {
    0% { opacity: 0.55; transform: scale(0.8); }
    72%, 100% { opacity: 0; transform: scale(3.2); }
}
@media (prefers-reduced-motion: reduce) {
    .status-event-current-status .status-dot::after { animation: none; }
}
.status-event-body {
    display: grid;
    gap: 16px;
}
.status-event-summary {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 12px 14px;
    border: 1px solid var(--uvp-panel-border);
    border-radius: 10px;
    background: var(--uvp-list-toolbar-bg);
}
.status-event-device {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
}
.status-event-device > div {
    display: grid;
    gap: 3px;
    min-width: 0;
}
.status-event-device strong,
.status-event-device span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}
.status-event-device strong { color: var(--uvp-text-primary); }
.status-event-device span { color: var(--uvp-text-tertiary); font-size: 12px; }
.summary-icon {
    display: inline-grid;
    place-items: center;
    width: 32px;
    height: 32px;
    flex: 0 0 32px;
    color: var(--uvp-brand-cyan);
    border-radius: 8px;
    background: color-mix(in srgb, var(--uvp-brand-cyan) 12%, transparent);
}
.status-event-facts {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 10px;
}
.status-event-facts > div {
    display: grid;
    gap: 4px;
    min-width: 0;
    padding: 10px 12px;
    border-bottom: 1px solid var(--uvp-panel-border);
}
.status-event-facts span { color: var(--uvp-text-tertiary); font-size: 12px; }
.status-event-facts strong {
    overflow: hidden;
    color: var(--uvp-text-secondary);
    font-size: 12px;
    text-overflow: ellipsis;
    white-space: nowrap;
}
.status-event-scroll {
    min-height: 180px;
    max-height: clamp(180px, calc(100vh - 350px), 440px);
    padding-right: 6px;
    overflow-y: auto;
    overscroll-behavior: contain;
    scrollbar-gutter: stable;
}
.status-event-content { display: block; min-height: 180px; }
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
    color: color-mix(in srgb, var(--uvp-text-tertiary) 82%, var(--uvp-brand));
    font-size: 12px;
    line-height: 18px;
}
.status-event-item {
    min-width: 0;
    color: var(--uvp-text-primary);
    font-size: 13px;
    font-weight: 600;
    line-height: 20px;
    cursor: help;
}
.status-event-load-more {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 38px;
    padding: 8px 12px;
    color: var(--uvp-text-tertiary);
    font-size: 12px;
    text-align: center;
    border-top: 1px solid var(--uvp-panel-border);
}
.status-event-load-more > span {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
}
.status-event-load-more .error { color: var(--uvp-danger); }
.status-event-load-more button {
    padding: 0;
    color: var(--uvp-brand);
    font: inherit;
    background: transparent;
    border: 0;
    cursor: pointer;
}
.status-event-load-more button:hover { color: var(--uvp-brand-strong); }
.status-event-state {
    display: grid;
    place-items: center;
    gap: 8px;
    min-height: 180px;
    color: var(--uvp-text-tertiary);
    text-align: center;
}
.status-event-state.error strong { color: var(--uvp-danger); }
@media (max-width: 720px) {
    .status-event-facts { grid-template-columns: 1fr; }
    .status-event-scroll { max-height: clamp(180px, calc(100vh - 470px), 360px); }
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
    letter-spacing: 0.04em;
    color: var(--uvp-text-tertiary);
    text-transform: uppercase;
}
.field-grid {
    display: grid;
    grid-template-columns: 92px minmax(0, 1fr);
    gap: 10px 14px;
    align-items: center;
    font-size: 13px;
}
.field-grid .k { color: var(--uvp-text-tertiary); font-size: 12px; }
.field-grid .v { color: var(--uvp-text-primary); overflow-wrap: anywhere; }
.field-grid .v.mono { font-family: ui-monospace, SFMono-Regular, Menlo, "PingFang SC", monospace; }
.field-grid .v .muted { color: var(--uvp-text-tertiary); font-size: 12px; margin-left: 4px; }
.field-grid .v .mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.inline-dot {
    display: inline-block;
    width: 6px;
    height: 6px;
    margin-right: 6px;
    border-radius: 999px;
    background: var(--uvp-text-tertiary);
    vertical-align: middle;
}
.inline-dot.online {
    background: var(--uvp-brand-cyan);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--uvp-brand-cyan) 18%, transparent);
}
.copy-btn {
    display: inline-grid;
    place-items: center;
    width: 20px;
    height: 20px;
    padding: 0;
    margin-left: 2px;
    background: transparent;
    color: var(--uvp-text-tertiary);
    border: 0;
    border-radius: 4px;
    cursor: pointer;
    transition: background 0.15s, color 0.15s;
}
.copy-btn:hover {
    color: var(--uvp-brand);
    background: color-mix(in srgb, var(--uvp-brand) 10%, transparent);
}
.capability-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
}
.cap-chip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 3px 10px;
    font-size: 12px;
    font-weight: 600;
    border-radius: 999px;
    color: var(--uvp-text-tertiary);
    background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border);
}
.cap-chip.on {
    color: var(--uvp-brand-cyan);
    background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent);
    border-color: color-mix(in srgb, var(--uvp-brand-cyan) 26%, transparent);
}
.channel-summary { display: grid; gap: 10px; }
.channel-stats {
    display: grid;
    grid-template-columns: 1fr auto 1fr auto 1fr;
    align-items: center;
    padding: 10px 4px;
    background: var(--uvp-list-toolbar-bg);
    border-radius: 10px;
}
.channel-stats .stat-item { display: grid; place-items: center; gap: 2px; }
.channel-stats .stat-num {
    font-size: 22px;
    font-weight: 650;
    color: var(--uvp-text-primary);
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    line-height: 1.1;
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
    background: var(--uvp-panel-border);
    border-radius: 999px;
    overflow: hidden;
}
.online-progress .progress-fill {
    height: 100%;
    background: linear-gradient(90deg, var(--uvp-brand) 0%, var(--uvp-brand-cyan) 100%);
    border-radius: 999px;
    transition: width 0.4s ease-out;
}
.drawer-foot {
    display: flex;
    gap: 8px;
    padding: 12px 0 4px;
    margin-top: 4px;
    border-top: 1px solid var(--uvp-panel-border);
}
.drawer-foot .arco-btn { flex: 0 0 auto; }
.drawer-foot .arco-btn-primary { flex: 1; }

.input-counter {
    display: inline-flex;
    align-items: center;
    padding: 2px 8px;
    margin-right: -4px;
    color: var(--uvp-text-tertiary);
    background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 6px;
    font-size: 12px;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-weight: 600;
    line-height: 1;
    min-height: 22px;
}
.input-counter.done {
    color: var(--uvp-brand-cyan);
    background: color-mix(in srgb, var(--uvp-brand-cyan) 12%, transparent);
    border-color: color-mix(in srgb, var(--uvp-brand-cyan) 28%, transparent);
}
.form-hint {
    font-size: 12px;
    color: var(--uvp-text-tertiary);
    line-height: 1.4;
}
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 1080px) {
    .workspace { grid-template-columns: 1fr; }
    .content-topbar,
    .main-head {
        align-items: stretch;
        flex-direction: column;
    }
    .topbar-actions,
    .toolbar {
        flex-wrap: wrap;
    }
}
</style>
