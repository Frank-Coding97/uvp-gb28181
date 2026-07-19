<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch, onUnmounted } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
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
    Map,
    MapPin,
    Monitor,
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

const viewMode = ref<ViewMode>("list");
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
const onlineDeviceTotal = ref(0);
const offlineDeviceTotal = ref(0);
const autoRefresh = ref(true);
const refreshInterval = ref<number | null>(null);
const isMacPlatform = computed(() => typeof navigator !== "undefined" && /Mac|iPhone|iPad|iPod/.test(navigator.platform));

let keywordSearchTimer: ReturnType<typeof setTimeout> | null = null;
let suppressKeywordSearch = false;

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
const statusEventPageSize = 50;
const statusEventLoading = ref(false);
const statusEventError = ref("");
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
    { label: "地图", value: "map", icon: Map }
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

watch([viewMode, assetKind], () => {
    selectedRowKeys.value = [];
    page.value = 1;
    refreshMainData();
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
function projectX(longitude: number) { const min = 73; const max = 136; return Math.min(96, Math.max(4, ((longitude - min) / (max - min)) * 100)); }
function projectY(latitude: number) { const min = 18; const max = 54; return Math.min(94, Math.max(6, 100 - ((latitude - min) / (max - min)) * 100)); }
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
function setViewMode(mode: ViewMode) { viewMode.value = mode; if (mode === "map") assetKind.value = "channel"; }
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
    mapLoading.value = true;
    try {
        const [markerRes, clusterRes, noCoordRes] = await Promise.all([
            listMapMarkers({ limit: 800 }),
            listMapClusters({ zoom: mapZoom.value }),
            getNoCoordCount()
        ]);
        if (markerRes.code === 0) markers.value = markerRes.data?.list || [];
        if (clusterRes.code === 0) clusters.value = clusterRes.data?.clusters || [];
        if (noCoordRes.code === 0) noCoordCount.value = noCoordRes.data?.count || 0;
        total.value = markerRes.data?.total || 0;
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

async function loadStatusEvents() {
    if (!statusEventDevice.value) return;
    statusEventLoading.value = true;
    statusEventError.value = "";
    try {
        const res = await listDeviceStatusEvents(statusEventDevice.value.id, {
            page: statusEventPage.value,
            pageSize: statusEventPageSize
        });
        if (res.code !== 0) throw new Error(res.message || "状态轨迹加载失败");
        statusEventList.value = res.data?.list || [];
        statusEventTotal.value = res.data?.total || 0;
    } catch (error: any) {
        statusEventError.value = error?.message || "状态轨迹加载失败";
        statusEventList.value = [];
        statusEventTotal.value = 0;
    } finally {
        statusEventLoading.value = false;
    }
}

function openStatusEvents(record: DeviceVO) {
    statusEventDevice.value = record;
    statusEventPage.value = 1;
    statusEventVisible.value = true;
    loadStatusEvents();
}

function changeStatusEventPage(next: number) {
    statusEventPage.value = next;
    loadStatusEvents();
}

function closeStatusEvents() {
    statusEventDevice.value = null;
    statusEventList.value = [];
    statusEventError.value = "";
    statusEventTotal.value = 0;
}

function eventStatusText(status?: number | null) {
    if (status === null || status === undefined) return "未知";
    return status === 1 ? "在线" : "离线";
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
    await Promise.all([loadTree(), refreshMainData(), refreshDeviceStats(), loadPtzTypeDict()]);
    if (autoRefresh.value) {
        startAutoRefresh();
    }
});

onUnmounted(() => {
    window.removeEventListener("keydown", focusKeyword);
    cancelKeywordSearch();
    stopAutoRefresh();
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
                            :scroll="{ x: 1480 }"
                            :row-selection="{ type: 'checkbox', showCheckedAll: true }"
                            class="dm-table"
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
                                <a-table-column title="操作" :width="180" fixed="right">
                                    <template #cell="{ record }">
                                        <div class="table-actions">
                                            <a-tooltip content="播放" position="top">
                                                <button class="icon-btn small framed primary" type="button" @click="playChannel(record)"><Play :size="13" /></button>
                                            </a-tooltip>
                                            <a-tooltip content="查看详情" position="top">
                                                <button class="icon-btn small framed" type="button" @click="openChannel(record)"><Eye :size="13" /></button>
                                            </a-tooltip>
                                            <a-tooltip content="编辑通道" position="top">
                                                <button class="icon-btn small framed warning" type="button" @click="openEditChannelModal(record)"><Pencil :size="13" /></button>
                                            </a-tooltip>
                                            <a-tooltip content="删除通道" position="top">
                                                <button class="icon-btn small framed danger" type="button" :disabled="deleting" @click="handleDeleteChannel(record)"><Trash2 :size="13" /></button>
                                            </a-tooltip>
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
                            :scroll="{ x: 1600 }"
                            :row-selection="{ type: 'checkbox', showCheckedAll: true }"
                            class="dm-table"
                            @page-change="onPageChange"
                            @page-size-change="onPageSizeChange"
                        >
                            <template #columns>
                                <a-table-column title="设备名称" :width="130">
                                    <template #cell="{ record }">
                                        <a-tooltip :content="deviceNameText(record)" position="top">
                                            <div class="device-name text-ellipsis">
                                                <span class="pri">{{ deviceNameText(record) }}</span>
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
                                <a-table-column title="状态" :width="92">
                                    <template #cell="{ record }">
                                        <button class="status-inline status-trigger" :class="{ online: record.online }" type="button" title="查看状态轨迹" @click.stop="openStatusEvents(record)">
                                            <span class="status-dot"></span>
                                            <span>{{ record.online ? '在线' : '离线' }}</span>
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
                                <a-table-column title="操作" :width="180" fixed="right">
                                    <template #cell="{ record }">
                                        <div class="table-actions">
                                            <a-tooltip content="查看通道" position="top">
                                                <button class="icon-btn small framed primary" type="button" @click="showDeviceChannels(record)"><Camera :size="13" /></button>
                                            </a-tooltip>
                                            <a-tooltip content="查看详情" position="top">
                                                <button class="icon-btn small framed" type="button" @click="openDevice(record)"><Eye :size="13" /></button>
                                            </a-tooltip>
                                            <a-tooltip :content="record.online ? '刷新通道目录' : '设备离线,无法刷新'" position="top">
                                                <button
                                                    class="icon-btn small framed info"
                                                    :class="{ loading: refreshingCatalog[record.id] }"
                                                    type="button"
                                                    :disabled="refreshingCatalog[record.id] || !record.online"
                                                    @click="handleRefreshDeviceCatalog(record)"
                                                >
                                                    <Loader2 v-if="refreshingCatalog[record.id]" :size="13" class="spin" />
                                                    <RefreshCcw v-else :size="13" />
                                                </button>
                                            </a-tooltip>
                                            <a-tooltip content="编辑设备" position="top">
                                                <button class="icon-btn small framed warning" type="button" @click="openEditDeviceModal(record)"><Pencil :size="13" /></button>
                                            </a-tooltip>
                                            <a-tooltip content="删除设备" position="top">
                                                <button class="icon-btn small framed danger" type="button" :disabled="deleting" @click="handleDeleteDevice(record)"><Trash2 :size="13" /></button>
                                            </a-tooltip>
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
                                <span class="device-card-icon"><RadioTower :size="18" /></span>
                                <div class="device-card-title">
                                    <a-tooltip :content="deviceNameText(item)" position="top">
                                        <strong class="text-ellipsis">{{ deviceNameText(item) }}</strong>
                                    </a-tooltip>
                                    <a-tooltip :content="item.deviceId" position="top">
                                        <span class="code text-ellipsis">{{ item.deviceId }}</span>
                                    </a-tooltip>
                                </div>
                                <span class="status-pill" :class="{ online: item.online }">{{ item.online ? '在线' : '离线' }}</span>
                            </div>
                            <div class="device-card-info">
                                <div><span>设备 ID</span><a-tooltip :content="item.deviceId" position="top"><strong class="mono text-ellipsis">{{ item.deviceId }}</strong></a-tooltip></div>
                                <div><span>厂商</span><strong class="text-ellipsis">{{ item.manufacturer || '未上报' }}</strong></div>
                                <div><span>型号</span><strong class="text-ellipsis">{{ item.model || '未上报' }}</strong></div>
                                <div><span>地址</span><a-tooltip :content="endpointText(item)" position="top"><strong class="mono text-ellipsis">{{ endpointText(item) }}</strong></a-tooltip></div>
                                <div><span>通道数量</span><strong>{{ item.channelCount }} 路 · 在线 {{ item.channelOnlineCount }} 路</strong></div>
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
                        <div v-if="noCoordCount" class="map-banner"><Info :size="14" /> 有 {{ noCoordCount }} 路通道缺少坐标，暂未显示在地图中。</div>
                        <div class="map-toolbar">
                            <span>Zoom {{ mapZoom }}</span>
                            <a-slider v-model="mapZoom" :min="5" :max="16" :style="{ width: '180px' }" @change="loadMapData" />
                            <button class="btn-ghost" type="button" @click="loadMapData">刷新地图</button>
                        </div>
                        <div class="map-canvas">
                            <div class="map-grid"></div>
                            <button v-for="cluster in clusters" :key="`${cluster.centerLat}-${cluster.centerLng}-${cluster.count}`" class="cluster-dot" type="button" :style="{ left: `${projectX(cluster.centerLng)}%`, top: `${projectY(cluster.centerLat)}%` }">{{ cluster.count }}</button>
                            <button v-for="marker in markers" :key="marker.id" class="marker-dot" :class="{ online: marker.status === 1 }" type="button" :style="{ left: `${projectX(marker.longitude)}%`, top: `${projectY(marker.latitude)}%` }" @click="openChannel(marker)"><MapPin :size="14" /></button>
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
                        <span class="status-pill" :class="{ online: statusEventDevice.online }">
                            <span class="status-dot"></span>
                            {{ statusEventDevice.online ? '在线' : '离线' }}
                        </span>
                    </header>
                    <div class="status-event-facts">
                        <div><span>最近注册</span><strong>{{ dateTime(statusEventDevice.registerTime) }}</strong></div>
                        <div><span>最近心跳</span><strong>{{ dateTime(statusEventDevice.keepaliveTime) }}</strong></div>
                        <div><span>来源地址</span><strong class="mono">{{ endpointText(statusEventDevice) }}</strong></div>
                    </div>

                    <a-spin :loading="statusEventLoading" class="status-event-content">
                        <div v-if="statusEventError" class="status-event-state error">
                            <strong>状态轨迹加载失败</strong>
                            <span>{{ statusEventError }}</span>
                            <a-button size="small" @click="loadStatusEvents">重试</a-button>
                        </div>
                        <a-empty v-else-if="!statusEventLoading && statusEventList.length === 0" description="暂无状态事件" />
                        <a-timeline v-else class="status-event-timeline">
                            <a-timeline-item v-for="event in statusEventList" :key="event.id" :label="dateTime(event.occurredAt)">
                                <div class="status-event-item" :data-event="event.eventType">
                                    <div class="event-title">
                                        <strong>{{ event.eventName }}</strong>
                                        <span>{{ eventStatusText(event.fromStatus) }} → {{ eventStatusText(event.toStatus) }}</span>
                                    </div>
                                    <p>{{ eventMetaText(event) }}</p>
                                </div>
                            </a-timeline-item>
                        </a-timeline>
                    </a-spin>

                    <a-pagination
                        v-if="statusEventTotal > statusEventPageSize"
                        :current="statusEventPage"
                        :page-size="statusEventPageSize"
                        :total="statusEventTotal"
                        simple
                        @change="changeStatusEventPage"
                    />
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
.dm-table { border-radius: 14px; overflow: hidden; }
.dm-table :deep(.arco-table-container) { border-radius: 14px; overflow: hidden; }
.dm-table :deep(.arco-table-th) { background: var(--uvp-table-header-bg); color: var(--uvp-text-secondary); }
.dm-table :deep(.arco-table-td),
.dm-table :deep(.arco-table-th) { border-color: var(--uvp-panel-border); }
.dm-table :deep(.arco-table-tr:hover .arco-table-td) { background: var(--uvp-table-row-hover-bg); }
.dm-table :deep(.arco-table-tr-selected .arco-table-td) { background: var(--uvp-table-row-checked-bg); }
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
.status-trigger {
    padding: 0;
    border: 0;
    background: transparent;
    cursor: pointer;
    text-align: left;
}
.status-trigger:focus-visible {
    outline: 2px solid var(--uvp-brand-cyan);
    outline-offset: 3px;
    border-radius: 4px;
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
.table-actions {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    white-space: nowrap;
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
    grid-template-columns: auto minmax(0, 1fr) auto;
    gap: 8px;
    align-items: center;
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
.device-card-info {
    display: grid;
    grid-template-columns: 68px minmax(0, 1fr);
    gap: 5px 8px;
    font-size: 12px;
}
.device-card-info > div { display: contents; }
.device-card-info span { color: var(--uvp-text-tertiary); }
.device-card-info strong { min-width: 0; color: var(--uvp-text-primary); font-weight: 500; }
.device-card-info .mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
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
.map-toolbar {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
    color: var(--uvp-text-tertiary);
}
.map-canvas {
    position: relative;
    min-height: 460px;
    overflow: hidden;
    background: linear-gradient(180deg, var(--uvp-search-panel-bg), var(--uvp-list-panel-bg));
    border: 1px solid var(--uvp-panel-border);
    border-radius: 12px;
}
.map-grid {
    position: absolute;
    inset: 0;
    background-image:
        linear-gradient(color-mix(in srgb, var(--uvp-panel-border) 70%, transparent) 1px, transparent 1px),
        linear-gradient(90deg, color-mix(in srgb, var(--uvp-panel-border) 70%, transparent) 1px, transparent 1px);
    background-size: 42px 42px;
    opacity: 0.55;
}
.cluster-dot,
.marker-dot {
    position: absolute;
    transform: translate(-50%, -50%);
    display: inline-grid;
    place-items: center;
    border: 0;
    cursor: pointer;
}
.cluster-dot {
    min-width: 38px;
    height: 38px;
    padding: 0 8px;
    color: #fff;
    background: color-mix(in srgb, var(--uvp-brand) 82%, #111827);
    border-radius: 999px;
    font-weight: 700;
}
.marker-dot {
    width: 28px;
    height: 28px;
    color: var(--uvp-text-tertiary);
    background: var(--uvp-panel-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 999px;
}
.marker-dot.online { color: #fff; background: var(--uvp-brand-cyan); }
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
.status-event-content { min-height: 180px; }
.status-event-timeline { padding: 8px 12px 0; }
.status-event-item { display: grid; gap: 4px; padding-bottom: 6px; }
.status-event-item .event-title { display: flex; align-items: baseline; gap: 10px; flex-wrap: wrap; }
.status-event-item .event-title strong { color: var(--uvp-text-primary); font-size: 13px; }
.status-event-item .event-title span,
.status-event-item p { color: var(--uvp-text-tertiary); font-size: 12px; }
.status-event-item p { margin: 0; }
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
