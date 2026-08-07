<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { Camera, Cctv, ChevronRight, Folder, ListFilter, MapPin, RefreshCw, Search, X } from "lucide-vue-next";
import {
    listChannels,
    listDevices,
    listDirectoryTree,
    type ChannelVO,
    type DeviceVO,
    type DirectoryNode,
    type DirectoryView
} from "../device-mgmt/api";

type SourceView = "devices" | DirectoryView;
type SourceNodeKind = "directory" | "device" | "channel";

interface SourceTreeNode {
    key: string;
    name: string;
    kind: SourceNodeKind;
    depth: number;
    count: number;
    onlineCount: number;
    status?: number;
    deviceId?: string;
    device?: DeviceVO;
    channel?: ChannelVO;
    children: SourceTreeNode[];
    staticChildren: SourceTreeNode[];
    hasChildren: boolean;
    expanded: boolean;
    loaded: boolean;
    loading: boolean;
}

const props = defineProps<{
    usedChannelIds?: number[];
}>();

const emit = defineEmits<{
    select: [channel: ChannelVO];
}>();

const view = ref<SourceView>("devices");
const trees = ref<Record<SourceView, SourceTreeNode[]>>({ devices: [], national: [], custom: [] });
const loaded = ref<Record<SourceView, boolean>>({ devices: false, national: false, custom: false });
const loading = ref(false);
const error = ref("");
const onlineDeviceTotal = ref(0);
const offlineDeviceTotal = ref(0);
const deviceStatsLoaded = ref(false);
const devicePage = ref(1);
const devicePageSize = 50;
const deviceListTotal = ref(0);
const deviceKeyword = ref("");
const appliedDeviceKeyword = ref("");
const deviceStatusFilter = ref<"" | "online" | "offline">("");
const filtersOpen = ref(false);
const hasDeviceFilters = computed(() => Boolean(appliedDeviceKeyword.value || deviceStatusFilter.value));
let refreshTimer: number | null = null;
let deviceSearchTimer: number | null = null;
let refreshInFlight = false;
let unmounted = false;

const viewOptions: Array<{ value: SourceView; label: string }> = [
    { value: "devices", label: "设备树" },
    { value: "national", label: "国标目录" },
    { value: "custom", label: "自定义目录" }
];

const currentTree = computed(() => trees.value[view.value]);
const visibleRows = computed(() => {
    const rows: Array<{ node: SourceTreeNode; level: number }> = [];
    const walk = (nodes: SourceTreeNode[], level: number) => {
        nodes.forEach(node => {
            rows.push({ node, level });
            if (node.expanded) walk(node.children, level + 1);
        });
    };
    walk(currentTree.value, 0);
    return rows;
});

function isUsed(channel: ChannelVO) {
    return props.usedChannelIds?.includes(channel.id) || false;
}

function displayName(device: DeviceVO) {
    return device.name || device.alias || device.deviceId;
}

function displayChannelName(channel: ChannelVO) {
    return channel.name || channel.alias || channel.channelId;
}

function directoryNode(node: DirectoryNode, depth = node.depth): SourceTreeNode {
    const staticChildren = (node.children || []).map(child => directoryNode(child, depth + 1));
    return {
        key: node.key,
        name: node.name,
        kind: "directory",
        depth,
        count: node.count,
        onlineCount: node.onlineCount,
        children: staticChildren,
        staticChildren,
        hasChildren: staticChildren.length > 0 || node.count > 0,
        expanded: false,
        loaded: false,
        loading: false
    };
}

function deviceNode(device: DeviceVO, parentKey: string, depth: number): SourceTreeNode {
    return {
        key: `${parentKey}:device:${device.id}`,
        name: displayName(device),
        kind: "device",
        depth,
        count: device.channelCount,
        onlineCount: device.channelOnlineCount,
        status: device.status,
        deviceId: device.deviceId,
        device,
        children: [],
        staticChildren: [],
        hasChildren: device.channelCount > 0,
        expanded: false,
        loaded: false,
        loading: false
    };
}

function channelNode(channel: ChannelVO, parentKey: string, depth: number): SourceTreeNode {
    return {
        key: `${parentKey}:channel:${channel.id}`,
        name: displayChannelName(channel),
        kind: "channel",
        depth,
        count: 0,
        onlineCount: channel.status === 1 ? 1 : 0,
        status: channel.status,
        deviceId: channel.deviceId,
        channel,
        children: [],
        staticChildren: [],
        hasChildren: false,
        expanded: false,
        loaded: true,
        loading: false
    };
}

function responseList<T>(response: any): T[] {
    if (response?.code !== 0) throw new Error(response?.message || "设备树加载失败");
    return response.data?.list || [];
}

function deviceListParams(page = devicePage.value) {
    const q = appliedDeviceKeyword.value || undefined;
    const status = deviceStatusFilter.value || undefined;
    return { ...(q ? { q } : {}), ...(status ? { status } : {}), page, pageSize: devicePageSize };
}

async function updateDeviceTotals(response: any) {
    if (typeof response.data?.onlineTotal === "number" && typeof response.data?.offlineTotal === "number") {
        onlineDeviceTotal.value = response.data.onlineTotal;
        offlineDeviceTotal.value = response.data.offlineTotal;
        deviceStatsLoaded.value = true;
        return;
    }
    try {
        const q = appliedDeviceKeyword.value || undefined;
        const [onlineResponse, offlineResponse] = await Promise.all([
            listDevices({ ...(q ? { q } : {}), status: "online", page: 1, pageSize: 1 }),
            listDevices({ ...(q ? { q } : {}), status: "offline", page: 1, pageSize: 1 })
        ]);
        if (onlineResponse.code !== 0 || offlineResponse.code !== 0) return;
        onlineDeviceTotal.value = onlineResponse.data?.total || 0;
        offlineDeviceTotal.value = offlineResponse.data?.total || 0;
        deviceStatsLoaded.value = true;
    } catch {
        return;
    }
}

function preserveNodeState(nextNodes: SourceTreeNode[], previousNodes: SourceTreeNode[]) {
    const previousByKey = new Map(previousNodes.map(node => [node.key, node]));
    return nextNodes.map(node => {
        const previous = previousByKey.get(node.key);
        if (!previous || !node.hasChildren) return node;
        node.expanded = previous.expanded;
        node.loaded = previous.loaded;
        if (node.kind === "device" && previous.loaded) {
            node.children = previous.children;
        } else if (node.kind === "directory") {
            node.staticChildren = preserveNodeState(node.staticChildren, previous.children);
            const staticKeys = new Set(node.staticChildren.map(child => child.key));
            const loadedDevices = previous.children.filter(child => child.kind === "device" && !staticKeys.has(child.key));
            node.children = previous.loaded ? [...node.staticChildren, ...loadedDevices] : node.staticChildren;
        }
        return node;
    });
}

async function loadRoot(nextView: SourceView = view.value, force = false, silent = false) {
    if (loaded.value[nextView] && !force) return;
    if (!silent) loading.value = true;
    error.value = "";
    try {
        if (nextView === "devices") {
            const response = await listDevices(deviceListParams());
            const devices = responseList<DeviceVO>(response);
            deviceListTotal.value = response.data?.total || 0;
            await updateDeviceTotals(response);
            const maxPage = Math.max(1, Math.ceil(deviceListTotal.value / devicePageSize));
            if (devicePage.value > maxPage) {
                devicePage.value = maxPage;
                await loadRoot(nextView, true, silent);
                return;
            }
            const nextNodes = devices.map(device => deviceNode(device, "root", 0));
            trees.value.devices = preserveNodeState(nextNodes, trees.value.devices);
        } else {
            const directories = responseList<DirectoryNode>(await listDirectoryTree(nextView));
            const nextNodes = directories.map(node => directoryNode(node));
            trees.value[nextView] = preserveNodeState(nextNodes, trees.value[nextView]);
        }
        loaded.value[nextView] = true;
    } catch (reason: any) {
        error.value = reason?.message || "设备树加载失败";
        if (!loaded.value[nextView]) trees.value[nextView] = [];
    } finally {
        if (!silent) loading.value = false;
    }
}

async function loadNodeChildren(node: SourceTreeNode) {
    if (node.loaded || node.loading || !node.hasChildren) return;
    node.loading = true;
    error.value = "";
    try {
        if (node.kind === "device" && node.deviceId) {
            const channels = responseList<ChannelVO>(await listChannels({ deviceId: node.deviceId, page: 1, pageSize: 200 }));
            node.children = channels.map(channel => channelNode(channel, node.key, node.depth + 1));
        } else if (node.kind === "directory") {
            const devices = responseList<DeviceVO>(await listDevices({
                directoryView: view.value === "devices" ? undefined : view.value,
                directoryKey: node.key,
                page: 1,
                pageSize: 200
            }));
            const existingKeys = new Set(node.staticChildren.map(child => child.key));
            node.children = [
                ...node.staticChildren,
                ...devices.filter(device => !existingKeys.has(`${node.key}:device:${device.id}`)).map(device => deviceNode(device, node.key, node.depth + 1))
            ];
        }
        node.loaded = true;
    } catch (reason: any) {
        error.value = reason?.message || "设备树节点加载失败";
    } finally {
        node.loading = false;
    }
}

async function toggle(node: SourceTreeNode) {
    if (!node.hasChildren) return;
    if (!node.expanded) await loadNodeChildren(node);
    node.expanded = !node.expanded;
}

async function choose(node: SourceTreeNode) {
    if (node.kind === "channel") {
        if (node.channel && node.status === 1 && !isUsed(node.channel)) emit("select", node.channel);
        return;
    }
    await toggle(node);
}

async function changeView(nextView: SourceView) {
    if (nextView === view.value) return;
    view.value = nextView;
    await loadRoot(nextView);
}

async function changeDevicePage(page: number) {
    if (page === devicePage.value) return;
    devicePage.value = page;
    await loadRoot("devices", true);
}

function cancelDeviceSearch() {
    if (deviceSearchTimer === null) return;
    window.clearTimeout(deviceSearchTimer);
    deviceSearchTimer = null;
}

async function applyDeviceSearch() {
    cancelDeviceSearch();
    const keyword = deviceKeyword.value.trim();
    deviceKeyword.value = keyword;
    appliedDeviceKeyword.value = keyword;
    devicePage.value = 1;
    await loadRoot("devices", true);
}

function scheduleDeviceSearch() {
    cancelDeviceSearch();
    deviceSearchTimer = window.setTimeout(() => {
        deviceSearchTimer = null;
        void applyDeviceSearch();
    }, 300);
}

async function clearDeviceSearch() {
    deviceKeyword.value = "";
    await applyDeviceSearch();
}

async function changeDeviceStatus(status: "" | "online" | "offline") {
    if (status === deviceStatusFilter.value) return;
    deviceStatusFilter.value = status;
    devicePage.value = 1;
    await loadRoot("devices", true);
}

async function refresh(silent = false) {
    if (refreshInFlight) return;
    refreshInFlight = true;
    try {
        await loadRoot(view.value, true, silent);
    } finally {
        refreshInFlight = false;
    }
}

function nodeIcon(node: SourceTreeNode) {
    if (node.kind === "channel") return Camera;
    if (node.kind === "device") return Cctv;
    return node.key.startsWith("national:") ? MapPin : Folder;
}

function nodeStatus(node: SourceTreeNode) {
    if (node.kind === "channel") return node.status === 1 ? "在线" : "离线";
    if (node.kind === "device") return node.status === 1 ? "在线" : "离线";
    return node.count ? `${node.onlineCount}/${node.count}` : "空";
}

onMounted(async () => {
    await loadRoot();
    if (unmounted) return;
    refreshTimer = window.setInterval(() => {
        if (loading.value || visibleRows.value.some(({ node }) => node.loading)) return;
        void refresh(true);
    }, 10_000);
});

onBeforeUnmount(() => {
    unmounted = true;
    if (refreshTimer) window.clearInterval(refreshTimer);
    cancelDeviceSearch();
});
</script>

<template>
    <aside class="playback-source-tree" aria-label="设备树">
        <header class="tree-head">
            <div class="tree-summary" :aria-label="deviceStatsLoaded ? `设备状态：在线 ${onlineDeviceTotal} 台，离线 ${offlineDeviceTotal} 台` : '设备状态加载中'">
                <template v-if="deviceStatsLoaded">
                    <span><i class="summary-dot online" aria-hidden="true" />在线 {{ onlineDeviceTotal }}</span>
                    <span><i class="summary-dot offline" aria-hidden="true" />离线 {{ offlineDeviceTotal }}</span>
                </template>
            </div>
            <div class="tree-head-actions">
                <button
                    v-if="view === 'devices'"
                    class="tree-icon-button tree-filter-toggle"
                    :class="{ active: hasDeviceFilters }"
                    data-test="device-filter-toggle"
                    type="button"
                    :aria-label="hasDeviceFilters ? '筛选设备（已启用）' : '筛选设备'"
                    :title="hasDeviceFilters ? '筛选设备（已启用）' : '筛选设备'"
                    :aria-expanded="filtersOpen"
                    aria-controls="device-tree-filters"
                    @click="filtersOpen = !filtersOpen"
                ><ListFilter :size="14" /></button>
                <button class="tree-icon-button tree-refresh" type="button" aria-label="刷新设备树" title="刷新设备树" @click="refresh()"><RefreshCw :size="14" :class="{ spin: loading }" /></button>
            </div>
        </header>

        <div class="tree-views" role="tablist" aria-label="设备树视图">
            <button v-for="option in viewOptions" :key="option.value" :data-test="`source-view-${option.value}`" type="button" role="tab" :aria-selected="view === option.value" :class="{ active: view === option.value }" @click="changeView(option.value)">{{ option.label }}</button>
        </div>

        <div v-if="view === 'devices' && filtersOpen" id="device-tree-filters" class="tree-filters" aria-label="设备筛选条件">
            <div class="device-status-filter" role="group" aria-label="设备状态筛选">
                <button data-test="device-status-all" type="button" :aria-pressed="deviceStatusFilter === ''" :class="{ active: deviceStatusFilter === '' }" @click="changeDeviceStatus('')">全部</button>
                <button data-test="device-status-online" type="button" :aria-pressed="deviceStatusFilter === 'online'" :class="{ active: deviceStatusFilter === 'online' }" @click="changeDeviceStatus('online')"><i class="filter-status-dot online" aria-hidden="true" />在线</button>
                <button data-test="device-status-offline" type="button" :aria-pressed="deviceStatusFilter === 'offline'" :class="{ active: deviceStatusFilter === 'offline' }" @click="changeDeviceStatus('offline')"><i class="filter-status-dot offline" aria-hidden="true" />离线</button>
            </div>
            <div class="tree-search">
                <Search :size="13" aria-hidden="true" />
                <input v-model="deviceKeyword" data-test="device-search" type="text" aria-label="搜索设备名称或国标编号" placeholder="搜索设备名称或国标编号" @input="scheduleDeviceSearch" @keydown.enter.prevent="applyDeviceSearch" />
                <button v-if="deviceKeyword" data-test="clear-device-search" type="button" aria-label="清空设备搜索" title="清空设备搜索" @click="clearDeviceSearch"><X :size="13" /></button>
            </div>
        </div>

        <div v-if="error" class="tree-error" role="alert">{{ error }}<button type="button" @click="refresh()">重试</button></div>
        <a-spin :loading="loading" class="tree-loading">
            <div class="tree-content" role="tree">
                <div v-for="{ node, level } in visibleRows" :key="node.key" class="tree-row" :class="{ used: node.kind === 'channel' && node.channel && isUsed(node.channel), offline: node.kind !== 'directory' && node.status !== 1 }" :style="{ paddingLeft: `${8 + level * 14}px` }" :data-node-key="node.key" role="treeitem" :aria-expanded="node.hasChildren ? node.expanded : undefined">
                    <button class="twist-button" :class="{ hidden: !node.hasChildren, expanded: node.expanded }" type="button" :disabled="!node.hasChildren || node.loading" aria-label="展开或收起" @click.stop="toggle(node)"><ChevronRight :size="12" /></button>
                    <button class="tree-node" type="button" :disabled="node.kind === 'channel' && (node.status !== 1 || Boolean(node.channel && isUsed(node.channel)))" @click="choose(node)">
                        <component :is="nodeIcon(node)" :size="13" aria-hidden="true" />
                        <span class="node-name">{{ node.name }}</span>
                        <span v-if="node.kind === 'directory'" class="node-status">{{ nodeStatus(node) }}</span>
                        <span v-else class="node-status-dot" :class="node.status === 1 ? 'online' : 'offline'" role="img" :aria-label="nodeStatus(node)" :title="nodeStatus(node)" />
                    </button>
                </div>
                <div v-if="!loading && !visibleRows.length" class="tree-empty">{{ view === "devices" && hasDeviceFilters ? "当前筛选条件下暂无设备" : "暂无设备或通道" }}</div>
            </div>
        </a-spin>
        <div v-if="view === 'devices' && deviceStatsLoaded" class="tree-pagination" :aria-label="`共 ${deviceListTotal} 台设备`">
            <span class="device-total">共 {{ deviceListTotal }} 台</span>
            <a-pagination v-if="deviceListTotal > devicePageSize" simple size="mini" :current="devicePage" :page-size="devicePageSize" :total="deviceListTotal" @change="changeDevicePage" />
        </div>
    </aside>
</template>

<style scoped>
.playback-source-tree { display: flex; min-height: 0; flex: 1 1 auto; flex-direction: column; color: var(--uvp-text-secondary); background: var(--zlm-card); border: 1px solid var(--zlm-border); }
.tree-head, .tree-head-actions, .tree-summary, .tree-summary span { display: flex; align-items: center; }
.tree-head { min-height: 44px; justify-content: space-between; padding: 7px 10px 7px 14px; border-bottom: 1px solid var(--uvp-panel-border); }
.device-total { color: var(--uvp-text-tertiary); font-size: 10px; font-weight: 500; }
.tree-summary { gap: 12px; color: var(--uvp-text-tertiary); font-size: 10px; }
.tree-summary span { gap: 5px; }
.summary-dot, .node-status-dot { display: inline-block; width: 8px; height: 8px; flex: 0 0 8px; border-radius: 50%; }
.summary-dot.online, .node-status-dot.online { background: #10B981; box-shadow: 0 0 0 2px rgb(16 185 129 / 12%); }
.summary-dot.offline, .node-status-dot.offline { background: #EF4444; box-shadow: 0 0 0 2px rgb(239 68 68 / 10%); }
.tree-head-actions { gap: 2px; }
.tree-icon-button { position: relative; display: inline-grid; width: 26px; height: 26px; padding: 0; color: var(--uvp-text-tertiary); background: transparent; border: 0; border-radius: 5px; place-items: center; cursor: pointer; }
.tree-icon-button:hover, .tree-filter-toggle[aria-expanded="true"], .tree-filter-toggle.active { color: var(--uvp-brand); background: var(--uvp-sidebar-active-bg); }
.tree-filter-toggle.active::after { position: absolute; top: 4px; right: 4px; width: 5px; height: 5px; background: var(--uvp-brand); border: 1px solid var(--zlm-card); border-radius: 50%; content: ""; }
.tree-views { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 2px; margin: 10px 12px 8px; padding: 2px; background: var(--uvp-list-toolbar-bg); border: 1px solid var(--uvp-panel-border); border-radius: 6px; }
.tree-views button { min-width: 0; height: 28px; padding: 0 4px; color: var(--uvp-text-tertiary); white-space: nowrap; background: transparent; border: 0; border-radius: 4px; cursor: pointer; font-size: 12px; }
.tree-views button.active { color: var(--uvp-text-primary); font-weight: 620; background: var(--uvp-panel-bg); box-shadow: 0 0 0 1px var(--uvp-panel-border); }
.tree-filters { flex: 0 0 auto; margin: 0 12px 8px; padding: 6px; background: var(--uvp-list-toolbar-bg); border: 1px solid var(--uvp-panel-border); border-radius: 6px; }
.device-status-filter { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 2px; margin-bottom: 6px; }
.device-status-filter button { display: flex; min-width: 0; height: 26px; align-items: center; justify-content: center; gap: 5px; padding: 0 4px; color: var(--uvp-text-tertiary); background: transparent; border: 0; border-radius: 4px; cursor: pointer; font: inherit; font-size: 11px; }
.device-status-filter button:hover { color: var(--uvp-text-primary); background: var(--uvp-sidebar-active-bg); }
.device-status-filter button.active { color: var(--uvp-text-primary); font-weight: 600; background: var(--uvp-panel-bg); box-shadow: 0 0 0 1px var(--uvp-panel-border); }
.filter-status-dot { width: 6px; height: 6px; flex: 0 0 6px; border-radius: 50%; }
.filter-status-dot.online { background: #10B981; }
.filter-status-dot.offline { background: #EF4444; }
.tree-search { display: flex; height: 30px; flex: 0 0 30px; align-items: center; gap: 7px; padding: 0 8px; color: var(--uvp-text-tertiary); background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: 5px; }
.tree-search:focus-within { color: var(--uvp-brand); border-color: var(--uvp-brand); }
.tree-search input { min-width: 0; height: 100%; flex: 1; padding: 0; color: var(--uvp-text-primary); background: transparent; border: 0; outline: 0; font: inherit; font-size: 12px; }
.tree-search input::placeholder { color: var(--uvp-text-tertiary); }
.tree-search button { display: inline-grid; width: 22px; height: 22px; flex: 0 0 22px; padding: 0; color: var(--uvp-text-tertiary); background: transparent; border: 0; border-radius: 4px; cursor: pointer; place-items: center; }
.tree-search button:hover { color: var(--uvp-text-primary); background: var(--uvp-sidebar-active-bg); }
.tree-loading { min-height: 0; flex: 1; overflow: hidden; }
.tree-content { height: 100%; min-height: 120px; padding: 2px 6px 10px; overflow: auto; scrollbar-gutter: stable; }
.tree-row { display: flex; height: 32px; min-width: 0; align-items: center; border-radius: 5px; }
.tree-row:hover { background: var(--uvp-sidebar-active-bg); }
.tree-row.used { background: color-mix(in srgb, var(--uvp-brand) 8%, transparent); }
.twist-button { display: inline-grid; width: 22px; height: 22px; flex: 0 0 22px; padding: 0; color: var(--uvp-text-tertiary); background: transparent; border: 0; border-radius: 5px; cursor: pointer; place-items: center; transition: transform 0.15s ease; }
.twist-button:hover:not(:disabled) { color: var(--uvp-brand); background: var(--uvp-sidebar-active-bg); }
.twist-button.expanded { transform: rotate(90deg); }
.twist-button.hidden { visibility: hidden; }
.tree-node { display: flex; min-width: 0; height: 100%; flex: 1; align-items: center; gap: 7px; padding: 0 4px 0 2px; color: var(--uvp-text-secondary); text-align: left; background: transparent; border: 0; cursor: pointer; font: inherit; }
.tree-node:disabled { cursor: not-allowed; }
.tree-node > svg { flex: 0 0 auto; color: var(--uvp-brand); }
.tree-row.offline .tree-node > svg { color: var(--uvp-text-tertiary); }
.tree-row.offline .node-name { color: var(--uvp-text-secondary); }
.node-name { min-width: 0; flex: 1; overflow: hidden; color: var(--uvp-text-primary); text-overflow: ellipsis; white-space: nowrap; font-size: 12px; }
.node-status { flex: 0 0 auto; color: var(--uvp-text-tertiary); font-size: 10px; }
.tree-error { display: flex; gap: 8px; align-items: center; padding: 10px 12px; color: var(--uvp-danger); font-size: 12px; }
.tree-error button { padding: 0; color: var(--uvp-brand); background: transparent; border: 0; cursor: pointer; font: inherit; }
.tree-empty { display: grid; min-height: 120px; color: var(--uvp-text-tertiary); place-items: center; font-size: 12px; }
.tree-pagination { display: flex; min-height: 38px; flex: 0 0 38px; align-items: center; justify-content: center; gap: 12px; padding: 4px 8px; border-top: 1px solid var(--uvp-panel-border); }
.spin { animation: source-tree-spin 0.9s linear infinite; }
@keyframes source-tree-spin { to { transform: rotate(360deg); } }
</style>
