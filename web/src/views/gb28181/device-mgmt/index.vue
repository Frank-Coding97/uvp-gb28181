<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch, onUnmounted } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import {
    Building2,
    Camera,
    ChevronRight,
    Folder,
    FolderTree,
    Grid2X2,
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
    Download,
    Plus
} from "@lucide/vue";
import {
    batchDeleteChannels,
    batchDeleteDevices,
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
    listMapClusters,
    listMapMarkers,
    refreshDeviceCatalog,
    type AssetKind,
    type BatchDeleteResult,
    type CatalogNode,
    type ChannelMount,
    type ChannelVO,
    type DeviceVO,
    type MapCluster,
    type MapMarker,
    type OnlineStatus,
    type TimelineSlot
} from "./api";

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
const statusFilter = ref<OnlineStatus | undefined>();
const selectedNode = ref<CatalogNode | null>(null);
const drawerVisible = ref(false);
const drawerTarget = ref<DrawerTarget | null>(null);
const drawerLoading = ref(false);
const rootLoading = ref(false);
const rowsLoading = ref(false);
const mapLoading = ref(false);
const page = ref(1);
const pageSize = ref(10);
const total = ref(0);
const noCoordCount = ref(0);
const selectedRowKeys = ref<number[]>([]);
const mapZoom = ref(10);
const onlineDeviceTotal = ref(0);
const offlineDeviceTotal = ref(0);
const autoRefresh = ref(true);
const refreshInterval = ref<number | null>(null);

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
const channelMounts = ref<ChannelMount[]>([]);
const timeline = ref<TimelineSlot[]>([]);

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

const hasFilters = computed(() => Boolean(keyword.value || statusFilter.value || selectedNode.value));
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
function displayName(item: { name?: string; channelId?: string; deviceId?: string }) { return item.name || item.channelId || item.deviceId || "未命名"; }
function deviceNameText(item: { name?: string | null }) { return item.name?.trim() || "-"; }
function manufacturerAbbr(value?: string | null) {
    const text = value?.trim();
    if (!text) return "-";
    const chars = Array.from(text.replace(/\s+/g, ""));
    return (chars.length <= 2 ? chars : chars.slice(0, 2)).join("").toUpperCase();
}
function vendorText(item: { manufacturer?: string; model?: string }) { return [item.manufacturer, item.model].filter(Boolean).join(" / ") || "未上报"; }
function locationText(item: ChannelVO | MapMarker) { return !item.latitude || !item.longitude ? "无坐标" : `${item.longitude.toFixed(5)}, ${item.latitude.toFixed(5)}`; }
function dateTime(value?: string | null) { if (!value || value.startsWith("0001-01-01")) return "-"; const d = new Date(value); if (Number.isNaN(d.getTime())) return "-"; return d.toLocaleString("zh-CN", { hour12: false }); }
function endpointText(item: { ip?: string; port?: number }) { if (!item.ip) return "-"; return item.port ? `${item.ip}:${item.port}` : item.ip; }
function transportText(value?: string | null) { return value ? value.toUpperCase() : "-"; }
function cameraTypeText(ptzType?: number) { return ptzType && ptzType > 0 ? "球机 / PTZ" : "枪机"; }
function modelVersionText(item: { model?: string; firmware?: string }) {
    return [item.model, item.firmware].filter(Boolean).join(" / ") || "-";
}
function channelUniqueId(record: ChannelVO) { return `${record.deviceId}_${record.channelId}`; }
function canExpand(node: CatalogNode) { return node.nodeType !== "channel"; }
function shortCode(v?: string) { return !v ? "-" : v.length <= 14 ? v : `${v.slice(0, 6)}...${v.slice(-6)}`; }
function projectX(longitude: number) { const min = 73; const max = 136; return Math.min(96, Math.max(4, ((longitude - min) / (max - min)) * 100)); }
function projectY(latitude: number) { const min = 18; const max = 54; return Math.min(94, Math.max(6, 100 - ((latitude - min) / (max - min)) * 100)); }
function showDeviceChannels(record: DeviceVO) {
    keyword.value = record.deviceId;
    selectedNode.value = null;
    statusFilter.value = undefined;
    assetKind.value = "channel";
    page.value = 1;
    refreshMainData();
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
function setViewMode(mode: ViewMode) { viewMode.value = mode; if (mode !== "list") assetKind.value = "channel"; }
function setAssetKind(kind: AssetKind) { assetKind.value = kind; }
function onSearch() { page.value = 1; refreshMainData(); }
function resetFilters() { keyword.value = ""; statusFilter.value = undefined; clearNode(); }
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

function openNode(node: CatalogNode) {
    drawerTarget.value = { type: "node", node };
    drawerVisible.value = true;
}

function playChannel(record: ChannelVO | MapMarker) {
    Message.info(`准备点播 ${record.channelId}`);
}

const deleting = ref(false);
const refreshingCatalog = reactive<Record<number, boolean>>({});

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
    } else {
        stopAutoRefresh();
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
    await Promise.all([loadTree(), refreshMainData(), refreshDeviceStats()]);
    if (autoRefresh.value) {
        startAutoRefresh();
    }
});

onUnmounted(() => {
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
                    <label class="cmdk">
                        <Search :size="14" />
                        <input v-model="keyword" type="text" placeholder="搜索设备 / 通道 / 编码 ..." @keydown.enter.prevent="onSearch" />
                        <span class="kbd"><kbd>⌘</kbd><kbd>K</kbd></span>
                    </label>
                    <div class="view-switch">
                        <button v-for="item in viewOptions" :key="item.value" type="button" :class="{ active: viewMode === item.value }" @click="setViewMode(item.value)">
                            <component :is="item.icon" :size="14" />
                        </button>
                    </div>
                    <button class="icon-btn" type="button" @click="resetFilters"><SlidersHorizontal :size="16" /></button>
                    <button class="btn-primary" type="button" @click="Message.info('设备录入表单待接入')"><Plus :size="14" /> 新建设备</button>
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
                                    <button class="btn-ghost" type="button" @click="refreshMainData"><RefreshCcw :size="14" /> 刷新</button>
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
                                    <div v-if="viewMode === 'list'" class="segmented">
                                        <button type="button" :class="{ active: assetKind === 'device' }" @click="setAssetKind('device')">设备</button>
                                        <button type="button" :class="{ active: assetKind === 'channel' }" @click="setAssetKind('channel')">通道</button>
                                    </div>
                                </div>
                            </template>
                        </s-layout-search>
                    </div>

                    <div class="filter-chips">
                        <span v-if="statusFilter" class="filter-chip">状态: {{ statusFilter === 'online' ? '在线' : '离线' }} <button class="close" @click="statusFilter = undefined">×</button></span>
                        <span v-if="selectedNode" class="filter-chip">目录: {{ selectedNode.name }} <button class="close" @click="clearNode">×</button></span>
                        <button v-if="hasFilters" class="clear-all" type="button" @click="resetFilters">清除筛选</button>
                    </div>

                    <div v-if="selectedCount" class="batch-bar">
                        <div class="batch-info"><strong>{{ selectedCount }}</strong> 项已选</div>
                        <div class="batch-ops">
                            <button class="btn-ghost" type="button" :disabled="deleting" @click="handleBatchDelete">批量删除</button>
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
                                <a-table-column title="通道编号" :width="210">
                                    <template #cell="{ record }">
                                        <div class="code-cell">
                                            <span class="code-main">{{ record.channelId }}</span>
                                            <span class="code-sub">{{ channelUniqueId(record) }}</span>
                                        </div>
                                    </template>
                                </a-table-column>
                                <a-table-column title="通道名称" :width="180">
                                    <template #cell="{ record }">
                                        <div class="device-name">
                                            <span class="pri">{{ displayName(record) }}</span>
                                            <span class="sec">{{ shortCode(record.deviceId) }}</span>
                                        </div>
                                    </template>
                                </a-table-column>
                                <a-table-column title="快照" :width="92" align="center">
                                    <template #cell="{ record }">
                                        <div class="thumb small"><div class="placeholder">{{ (record.manufacturer || 'UV').slice(0, 2).toUpperCase() }}</div></div>
                                    </template>
                                </a-table-column>
                                <a-table-column title="状态" :width="92">
                                    <template #cell="{ record }"><span class="status-pill" :class="{ online: record.status === 1 }">{{ record.status === 1 ? '在线' : '离线' }}</span></template>
                                </a-table-column>
                                <a-table-column title="摄像头类型" :width="120">
                                    <template #cell="{ record }"><span class="tag">{{ cameraTypeText(record.ptzType) }}</span></template>
                                </a-table-column>
                                <a-table-column title="媒体传输" :width="110">
                                    <template #cell="{ record }"><span class="tag muted">{{ transportText(record.transport) }}</span></template>
                                </a-table-column>
                                <a-table-column title="厂商 / 型号" :width="170"><template #cell="{ record }"><div class="vendor-cell"><span class="v">{{ record.manufacturer || '-' }}</span><span class="m">{{ record.model || '-' }}</span></div></template></a-table-column>
                                <a-table-column title="位置信息" :width="180"><template #cell="{ record }"><span class="relative">{{ locationText(record) }}</span></template></a-table-column>
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
                                            <button class="play-cta" type="button" @click="openChannel(record)"><Play :size="12" /> 播放</button>
                                            <button class="icon-btn small framed" type="button" @click="Message.info('通道编辑待接入')"><Pencil :size="13" /></button>
                                            <button class="icon-btn small framed danger" type="button" :disabled="deleting" @click="handleDeleteChannel(record)"><Trash2 :size="13" /></button>
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
                            :scroll="{ x: 1820 }"
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
                                        <span class="status-inline" :class="{ online: record.online }">
                                            <span class="status-dot"></span>
                                            <span>{{ record.online ? '在线' : '离线' }}</span>
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
                                <a-table-column title="传输模式" :width="110">
                                    <template #cell="{ record }"><span class="tag">{{ transportText(record.transport) }}</span></template>
                                </a-table-column>
                                <a-table-column title="来源地址" :width="180">
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
                                            <button class="play-cta" type="button" @click="showDeviceChannels(record)"><Folder :size="12" /> 通道</button>
                                            <button class="btn-ghost compact" type="button" @click="openDevice(record)">详情</button>
                                        </div>
                                    </template>
                                </a-table-column>
                                <a-table-column title="设备控制" :width="150" fixed="right">
                                    <template #cell="{ record }">
                                        <div class="table-actions">
                                            <button
                                                class="icon-btn small framed"
                                                type="button"
                                                :disabled="refreshingCatalog[record.id] || !record.online"
                                                :title="record.online ? '刷新通道目录' : '设备离线,无法刷新'"
                                                @click="handleRefreshDeviceCatalog(record)"
                                            >
                                                <Loader2 v-if="refreshingCatalog[record.id]" :size="13" class="spin" />
                                                <RefreshCcw v-else :size="13" />
                                            </button>
                                            <button class="icon-btn small framed" type="button" @click="Message.info('设备编辑待接入')"><Pencil :size="13" /></button>
                                            <button class="icon-btn small framed danger" type="button" :disabled="deleting" @click="handleDeleteDevice(record)"><Trash2 :size="13" /></button>
                                        </div>
                                    </template>
                                </a-table-column>
                            </template>
                        </a-table>

                    </div>

                    <div v-else-if="viewMode === 'card'" class="view-body card-grid">
                        <article v-for="item in channels" :key="item.id" class="device-card" @dblclick="openChannel(item)">
                            <div class="card-snap" :class="{ offline: item.status !== 1 }">
                                <div class="snap-type"><Video :size="12" /> {{ item.ptzType > 0 ? '球机' : '枪机' }}</div>
                                <div class="snap-corner">
                                    <div v-if="item.ptzType > 0" class="corner-badge"><Camera :size="12" /></div>
                                    <div v-if="item.streamId" class="corner-badge alarm"><Play :size="12" /></div>
                                </div>
                                <div class="snap-overlay">
                                    <span class="status-dot" :class="{ online: item.status === 1 }"></span>
                                    <span class="status-text">{{ item.status === 1 ? '在线' : '离线' }}</span>
                                    <span class="heart-text">{{ relTime(item.updatedAt) }}</span>
                                </div>
                            </div>
                            <div class="card-info">
                                <span class="title">{{ displayName(item) }}</span>
                                <span class="code">{{ item.channelId }}</span>
                                <span class="meta">{{ vendorText(item) }}</span>
                                <div class="card-actions">
                                    <button class="icon-btn small" type="button"><Layers :size="14" /></button>
                                    <button class="play-cta" type="button" @click.stop="playChannel(item)"><Play :size="12" /> 点播</button>
                                </div>
                            </div>
                        </article>
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

            <a-drawer v-model:visible="drawerVisible" :width="520" :footer="false" unmount-on-close>
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
                    </div>
                    <div v-else-if="drawerTarget?.type === 'device' && deviceDetail" class="drawer-body">
                        <div class="drawer-headline">
                            <span class="drawer-icon"><RadioTower :size="18" /></span>
                            <div><h3>{{ displayName(deviceDetail) }}</h3><p>{{ deviceDetail.deviceId }}</p></div>
                            <span class="status-pill" :class="{ online: deviceDetail.online }">{{ deviceDetail.online ? '在线' : '离线' }}</span>
                        </div>
                        <div class="kv-grid">
                            <span>厂商型号</span><strong>{{ vendorText(deviceDetail) }}</strong>
                            <span>来源地址</span><strong>{{ endpointText(deviceDetail) }}</strong>
                            <span>通道在线</span><strong>{{ deviceDetail.channelOnlineCount }}/{{ deviceDetail.channelCount }}</strong>
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
.btn-ghost {
    color: var(--uvp-text-secondary);
    background: var(--uvp-search-secondary-btn-bg);
    border: 1px solid var(--uvp-search-secondary-btn-border);
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
.icon-btn.framed {
    color: var(--uvp-text-secondary);
    background: var(--uvp-search-secondary-btn-bg);
    border: 1px solid var(--uvp-search-secondary-btn-border);
}
.icon-btn.framed:hover {
    color: var(--uvp-brand);
    border-color: color-mix(in srgb, var(--uvp-brand) 28%, var(--uvp-search-secondary-btn-border));
}
.icon-btn.framed.danger:hover {
    color: var(--uvp-danger, #ef4444);
    background: color-mix(in srgb, var(--uvp-danger, #ef4444) 8%, transparent);
    border-color: color-mix(in srgb, var(--uvp-danger, #ef4444) 30%, var(--uvp-search-secondary-btn-border));
}
.card-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 12px;
    overflow: auto;
    padding-right: 4px;
}
.device-card {
    overflow: hidden;
    background: var(--uvp-panel-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 12px;
}
.card-snap {
    position: relative;
    aspect-ratio: 16 / 9;
    background: linear-gradient(135deg, rgb(37 99 235 / 72%), rgb(15 170 166 / 62%));
}
.card-snap.offline { filter: grayscale(0.7) brightness(0.58); }
.snap-type,
.snap-corner,
.snap-overlay { position: absolute; }
.snap-type {
    top: 8px;
    left: 8px;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    height: 20px;
    padding: 0 8px;
    border-radius: 8px;
    background: rgb(15 23 42 / 70%);
    color: #fff;
    font-size: 11px;
}
.snap-corner { top: 8px; right: 8px; display: flex; gap: 4px; }
.corner-badge {
    width: 22px;
    height: 22px;
    display: grid;
    place-items: center;
    border-radius: 8px;
    background: rgb(15 23 42 / 70%);
    color: #fff;
}
.corner-badge.alarm { color: var(--uvp-warning); }
.snap-overlay {
    left: 0;
    right: 0;
    bottom: 0;
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 10px;
    background: linear-gradient(180deg, transparent, rgb(2 6 23 / 70%));
    color: #fff;
    font-size: 12px;
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
.heart-text { margin-left: auto; color: rgb(255 255 255 / 86%); }
.card-info { display: grid; gap: 4px; padding: 10px 11px; }
.card-info .title { color: var(--uvp-text-primary); font-weight: 600; }
.card-info .code,
.card-info .meta { color: var(--uvp-text-tertiary); font-size: 12px; }
.card-actions { display: flex; align-items: center; gap: 8px; }
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
