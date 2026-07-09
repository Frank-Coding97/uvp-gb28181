<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
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
    RadioTower,
    RefreshCcw,
    Search,
    Settings2,
    SlidersHorizontal,
    Video,
    Download,
    Plus
} from "@lucide/vue";
import {
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
    type AssetKind,
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
const pageSize = ref(6);
const total = ref(0);
const noCoordCount = ref(0);
const selectedRowKeys = ref<number[]>([]);
const mapZoom = ref(10);
const onlineDeviceTotal = ref(0);
const offlineDeviceTotal = ref(0);

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
function vendorText(item: { manufacturer?: string; model?: string }) { return [item.manufacturer, item.model].filter(Boolean).join(" / ") || "未上报"; }
function locationText(item: ChannelVO | MapMarker) { return !item.latitude || !item.longitude ? "无坐标" : `${item.longitude.toFixed(5)}, ${item.latitude.toFixed(5)}`; }
function canExpand(node: CatalogNode) { return node.nodeType !== "channel"; }
function shortCode(v?: string) { return !v ? "-" : v.length <= 14 ? v : `${v.slice(0, 6)}...${v.slice(-6)}`; }
function projectX(longitude: number) { const min = 73; const max = 136; return Math.min(96, Math.max(4, ((longitude - min) / (max - min)) * 100)); }
function projectY(latitude: number) { const min = 18; const max = 54; return Math.min(94, Math.max(6, 100 - ((latitude - min) / (max - min)) * 100)); }

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

onMounted(async () => {
    await Promise.all([loadTree(), refreshMainData(), refreshDeviceStats()]);
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
                                    <button class="btn-ghost" type="button" @click="refreshMainData"><RefreshCcw :size="14" /> 刷新</button>
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
                            <button class="btn-ghost" type="button">批量重新注册</button>
                            <button class="btn-ghost" type="button">批量删除</button>
                        </div>
                    </div>

                    <div v-if="viewMode === 'list'" class="view-body table-view">
                        <a-table
                            v-if="assetKind === 'channel'"
                            v-model:selected-keys="selectedRowKeys"
                            :data="channels"
                            :loading="rowsLoading"
                            :pagination="false"
                            row-key="id"
                            :row-selection="{ type: 'checkbox', showCheckedAll: true }"
                            class="dm-table"
                        >
                            <template #columns>
                                <a-table-column title="缩略图" :width="90">
                                    <template #cell="{ record }"><div class="thumb"><div class="placeholder">{{ (record.manufacturer || 'UV').slice(0, 2).toUpperCase() }}</div></div></template>
                                </a-table-column>
                                <a-table-column title="设备名 / 编码" :width="320">
                                    <template #cell="{ record }"><div class="device-name"><span class="pri">{{ displayName(record) }}<span v-if="record.ptzType > 0" class="mount-badge">PTZ</span></span><span class="sec">{{ record.channelId }}</span></div></template>
                                </a-table-column>
                                <a-table-column title="厂商 / 型号" :width="180"><template #cell="{ record }"><div class="vendor-cell"><span class="v">{{ record.manufacturer || '-' }}</span><span class="m">{{ record.model || '-' }}</span></div></template></a-table-column>
                                <a-table-column title="设备编号" :width="180"><template #cell="{ record }">{{ shortCode(record.deviceId) }}</template></a-table-column>
                                <a-table-column title="类型" :width="120"><template #cell="{ record }"><span class="tag">{{ record.ptzType > 0 ? '球机 / PTZ' : '枪机' }}</span></template></a-table-column>
                                <a-table-column title="状态" :width="100"><template #cell="{ record }"><span class="status-pill" :class="{ online: record.status === 1 }">{{ record.status === 1 ? '在线' : '离线' }}</span></template></a-table-column>
                                <a-table-column title="最近心跳" :width="140"><template #cell="{ record }"><span class="relative" :class="{ warn: record.status === 0 }">{{ relTime(record.updatedAt) }}</span></template></a-table-column>
                                <a-table-column title="操作" :width="120" fixed="right"><template #cell="{ record }"><button class="play-cta" type="button" @click="openChannel(record)"><Play :size="12" /> 点播</button></template></a-table-column>
                            </template>
                        </a-table>

                        <a-table
                            v-else
                            v-model:selected-keys="selectedRowKeys"
                            :data="devices"
                            :loading="rowsLoading"
                            :pagination="false"
                            row-key="id"
                            :row-selection="{ type: 'checkbox', showCheckedAll: true }"
                            class="dm-table"
                        >
                            <template #columns>
                                <a-table-column title="缩略图" :width="90"><template #cell="{ record }"><div class="thumb"><div class="placeholder">{{ (record.manufacturer || 'UV').slice(0, 2).toUpperCase() }}</div></div></template></a-table-column>
                                <a-table-column title="设备名 / 编码" :width="320"><template #cell="{ record }"><div class="device-name"><span class="pri">{{ displayName(record) }}</span><span class="sec">{{ record.deviceId }}</span></div></template></a-table-column>
                                <a-table-column title="厂商 / 型号" :width="180"><template #cell="{ record }"><div class="vendor-cell"><span class="v">{{ record.manufacturer || '-' }}</span><span class="m">{{ record.model || '-' }}</span></div></template></a-table-column>
                                <a-table-column title="IP : 端口" :width="180"><template #cell="{ record }">{{ record.ip || '-' }}{{ record.port ? `:${record.port}` : '' }}</template></a-table-column>
                                <a-table-column title="通道" :width="140"><template #cell="{ record }"><span class="tag">{{ record.channelOnlineCount }}/{{ record.channelCount }}</span></template></a-table-column>
                                <a-table-column title="状态" :width="100"><template #cell="{ record }"><span class="status-pill" :class="{ online: record.online }">{{ record.online ? '在线' : '离线' }}</span></template></a-table-column>
                                <a-table-column title="最近心跳" :width="140"><template #cell="{ record }"><span class="relative" :class="{ warn: !record.online }">{{ relTime(record.keepaliveTime) }}</span></template></a-table-column>
                                <a-table-column title="操作" :width="120" fixed="right"><template #cell="{ record }"><button class="play-cta" type="button" @click="openDevice(record)">详情</button></template></a-table-column>
                            </template>
                        </a-table>

                        <div class="pagination-bar">
                            <span>共 {{ total }} 项</span>
                            <a-pagination :current="page" :page-size="pageSize" :total="total" show-total show-page-size @change="onPageChange" @page-size-change="onPageSizeChange" />
                        </div>
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
                            <span>网络地址</span><strong>{{ deviceDetail.ip || '-' }}{{ deviceDetail.port ? `:${deviceDetail.port}` : '' }}</strong>
                            <span>传输协议</span><strong>{{ deviceDetail.transport || '-' }}</strong>
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
.placeholder { color: #fff; font-size: 10px; font-weight: 700; }
.device-name { display: grid; gap: 2px; }
.pri { display: inline-flex; align-items: center; gap: 6px; color: var(--uvp-text-primary); font-weight: 600; }
.sec { color: var(--uvp-text-tertiary); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; }
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
.play-cta {
    color: var(--uvp-brand);
    background: var(--uvp-brand-soft);
    border: 1px solid color-mix(in srgb, var(--uvp-brand) 24%, transparent);
    font-size: 12px;
}
.pagination-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    color: var(--uvp-text-tertiary);
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
