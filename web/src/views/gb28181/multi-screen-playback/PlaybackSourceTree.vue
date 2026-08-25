<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch, type Component } from "vue";
import { Camera, Cctv, ChevronLeft, ChevronRight, Folder, ListFilter, MapPin, Play, RefreshCw, Search, Star, Trash2, X } from "lucide-vue-next";
import {
    listChannels,
    listDevices,
    listDirectoryTree,
    type ChannelVO,
    type DeviceVO,
    type DirectoryNode,
} from "../device-mgmt/api";
import { appendChannelFavoriteGroup, createChannelFavoriteGroup, deleteChannelFavoriteGroup, listChannelFavoriteGroups, removeChannelFavoriteItem, type ChannelFavoriteGroup as ApiFavoriteGroup, type ChannelFavoriteInput } from "@/api/gb28181";

type SourceView = "devices" | "national" | "custom" | "favorites";
type SourceNodeKind = "directory" | "device" | "channel" | "favorite-group";

export interface FavoriteChannelGroup {
    id: number;
    name: string;
    channels: ChannelVO[];
    unavailableCount?: number;
}

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
    favoriteGroup?: FavoriteChannelGroup;
    favoriteGroupId?: number;
    children: SourceTreeNode[];
    staticChildren: SourceTreeNode[];
    hasChildren: boolean;
    expanded: boolean;
    loaded: boolean;
    loading: boolean;
}

const props = defineProps<{
    usedChannelIds?: number[];
    view?: SourceView;
}>();

const emit = defineEmits<{
    select: [channel: ChannelVO];
    "select-group": [group: FavoriteChannelGroup];
    "favorite-saved": [groupName: string, addedCount: number, skippedCount: number];
    "update:view": [view: SourceView];
}>();

const view = ref<SourceView>(props.view || "devices");
const trees = ref<Record<SourceView, SourceTreeNode[]>>({ devices: [], national: [], custom: [], favorites: [] });
const loaded = ref<Record<SourceView, boolean>>({ devices: false, national: false, custom: false, favorites: false });
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

const viewOptions: Array<{ value: SourceView; label: string; icon: Component }> = [
    { value: "devices", label: "设备树", icon: Cctv },
    { value: "national", label: "国标目录", icon: MapPin },
    { value: "custom", label: "自定义目录", icon: Folder },
    { value: "favorites", label: "我的收藏", icon: Star }
];

const favoriteDialogVisible = ref(false);
const favoriteDialogChannel = ref<ChannelVO | null>(null);
const favoriteDialogChannels = ref<ChannelVO[]>([]);
const favoriteDialogMode = ref<"existing" | "new">("new");
const favoriteDialogGroupId = ref("");
const favoriteGroupName = ref("");
const favoriteDialogError = ref("");
const viewTabsRef = ref<HTMLElement | null>(null);

const favoriteGroups = ref<FavoriteChannelGroup[]>([]);

function isChannelFavorite(channel: ChannelVO) {
    return favoriteGroups.value.some(group => group.channels.some(item => item.deviceId === channel.deviceId && item.channelId === channel.channelId));
}

function favoriteGroupNode(group: FavoriteChannelGroup, index: number): SourceTreeNode {
    const channelChildren = group.channels.map(channel => ({ ...channelNode(channel, `favorites:${index}:group:${group.id}`, 1), favoriteGroupId: group.id }));
    const staticChildren = channelChildren;
    return {
        key: `favorites:group:${group.id}`,
        name: group.name,
        kind: "favorite-group",
        depth: 0,
        count: staticChildren.length,
        onlineCount: group.channels.filter(channel => channel.status === 1).length,
        children: staticChildren,
        staticChildren,
        favoriteGroup: group,
        hasChildren: staticChildren.length > 0,
        expanded: false,
        loaded: false,
        loading: false
    };
}

async function removeFavoriteChannel(node: SourceTreeNode) {
    if (!node.channel || !node.favoriteGroupId) return;
    try { await removeChannelFavoriteItem(node.favoriteGroupId, { deviceCode: node.channel.deviceId, channelCode: node.channel.channelId }); await loadFavorites(); } catch { error.value = "移除收藏失败，请稍后重试"; }
}

async function removeFavoriteGroup(node: SourceTreeNode) {
    if (!node.favoriteGroup) return;
    try { await deleteChannelFavoriteGroup(node.favoriteGroup.id); await loadFavorites(); } catch { error.value = "删除收藏组失败，请稍后重试"; }
}

function openFavoriteDialog(node: SourceTreeNode) {
    if (!node.channel) return;
    openFavoriteDialogForChannels([node.channel]);
}

function openFavoriteDialogForChannels(channels: ChannelVO[]) {
    const uniqueChannels = channels.filter((channel, index, list) => channel && list.findIndex(item => item.id === channel.id) === index);
    if (!uniqueChannels.length) return;
    favoriteDialogChannels.value = uniqueChannels;
    favoriteDialogChannel.value = uniqueChannels[0];
    favoriteDialogMode.value = favoriteGroups.value.length ? "existing" : "new";
    favoriteDialogGroupId.value = "";
    favoriteGroupName.value = "";
    favoriteDialogError.value = "";
    favoriteDialogVisible.value = true;
}

function closeFavoriteDialog() {
    favoriteDialogVisible.value = false;
    favoriteDialogChannel.value = null;
    favoriteDialogChannels.value = [];
    favoriteDialogGroupId.value = "";
    favoriteGroupName.value = "";
    favoriteDialogError.value = "";
}

async function saveFavoriteGroup() {
    const channels = favoriteDialogChannels.value;
    if (!channels.length) return;
    let group: FavoriteChannelGroup | undefined;
    const inputs: ChannelFavoriteInput[] = channels.map(channel => ({ deviceCode: channel.deviceId, channelCode: channel.channelId }));
    if (favoriteDialogMode.value === "existing") {
        group = favoriteGroups.value.find(item => String(item.id) === String(favoriteDialogGroupId.value));
        if (!group) {
            favoriteDialogError.value = "请选择要追加的收藏组";
            return;
        }
    } else {
        const name = favoriteGroupName.value.trim();
        if (!name) {
            favoriteDialogError.value = "请输入收藏组名称";
            return;
        }
        try {
            await createChannelFavoriteGroup(name, inputs);
            emit("favorite-saved", name, channels.length, 0);
            await loadFavorites();
            closeFavoriteDialog();
        } catch (error: any) {
            favoriteDialogError.value = error?.response?.data?.data?.errorCode === "CHANNEL_FAVORITE_GROUP_NAME_CONFLICT" ? "已存在同名收藏组，请切换“选择已有组”进行追加" : "收藏组保存失败，请稍后重试";
        }
        return;
    }
    try {
        const response = await appendChannelFavoriteGroup(group.id, inputs);
        const result = response.data;
        if (!result) throw new Error("收藏组追加失败");
        if (result.addedCount === 0 && result.skippedCount > 0) {
            favoriteDialogError.value = `当前通道已在“${group.name}”中，无需重复收藏`;
            return;
        }
        emit("favorite-saved", group.name, result.addedCount, result.skippedCount);
        await loadFavorites();
        closeFavoriteDialog();
    } catch { favoriteDialogError.value = "收藏组保存失败，请稍后重试"; }
}

async function loadFavorites() {
    try {
        const response = await listChannelFavoriteGroups();
        const groups = response.data?.list || [];
        favoriteGroups.value = groups.map((group: ApiFavoriteGroup) => ({ id: group.id, name: group.name, unavailableCount: group.unavailableCount, channels: (group.items || []).map(item => item.channel).filter((channel): channel is ChannelVO => Boolean(channel)) }));
    } catch { favoriteGroups.value = []; }
    const nextNodes = favoriteGroups.value.map(favoriteGroupNode);
    trees.value.favorites = preserveNodeState(nextNodes, trees.value.favorites);
    loaded.value.favorites = true;
}

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
        if (nextView === "favorites") {
            await loadFavorites();
        } else if (nextView === "devices") {
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
                directoryView: view.value === "national" || view.value === "custom" ? view.value : undefined,
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
    emit("update:view", nextView);
    await loadRoot(nextView);
}

function scrollViewTabs(direction: -1 | 1) {
    const tabs = viewTabsRef.value;
    if (!tabs) return;
    tabs.scrollLeft += direction * Math.max(80, Math.floor(tabs.clientWidth * 0.75));
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
    if (node.kind === "favorite-group") return Star;
    return node.key.startsWith("national:") ? MapPin : Folder;
}

function nodeStatus(node: SourceTreeNode) {
    if (node.kind === "channel") return node.status === 1 ? "在线" : "离线";
    if (node.kind === "device") return node.status === 1 ? "在线" : "离线";
    if (node.kind === "favorite-group") return node.favoriteGroup?.unavailableCount ? `${node.count} 个通道，${node.favoriteGroup.unavailableCount} 个不可用` : `${node.count} 个通道`;
    return node.count ? `${node.onlineCount}/${node.count}` : "空";
}

onMounted(async () => {
    await loadFavorites();
    await loadRoot();
    if (unmounted) return;
    refreshTimer = window.setInterval(() => {
        if (loading.value || visibleRows.value.some(({ node }) => node.loading)) return;
        void refresh(true);
    }, 10_000);
});

watch(() => props.view, nextView => {
    if (!nextView || nextView === view.value) return;
    view.value = nextView;
    void loadRoot(nextView);
});

onBeforeUnmount(() => {
    unmounted = true;
    if (refreshTimer) window.clearInterval(refreshTimer);
    cancelDeviceSearch();
});

defineExpose({ openFavoriteDialogForChannels });
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
                <button class="tree-icon-button tree-refresh uvp-refresh-btn" type="button" aria-label="刷新设备树" title="刷新设备树" @click="refresh()"><RefreshCw :size="14" :class="{ spin: loading }" /></button>
            </div>
        </header>

        <div class="tree-views">
            <button class="tree-view-arrow" data-test="source-view-scroll-left" type="button" aria-label="显示前面的设备树视图" title="显示前面的视图" @click="scrollViewTabs(-1)"><ChevronLeft :size="14" aria-hidden="true" /></button>
            <div ref="viewTabsRef" class="tree-view-scroll" role="tablist" aria-label="设备树视图">
                <button v-for="option in viewOptions" :key="option.value" :data-test="`source-view-${option.value}`" type="button" role="tab" :aria-selected="view === option.value" :class="{ active: view === option.value }" @click="changeView(option.value)"><component :is="option.icon" :size="13" aria-hidden="true" />{{ option.label }}</button>
            </div>
            <button class="tree-view-arrow" data-test="source-view-scroll-right" type="button" aria-label="显示后面的设备树视图" title="显示后面的视图" @click="scrollViewTabs(1)"><ChevronRight :size="14" aria-hidden="true" /></button>
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
                <div v-for="{ node, level } in visibleRows" :key="node.key" class="tree-row" :class="{ used: node.kind === 'channel' && node.channel && isUsed(node.channel), offline: (node.kind === 'device' || node.kind === 'channel') && node.status !== 1 }" :style="{ paddingLeft: `${8 + level * 14}px` }" :data-node-key="node.key" role="treeitem" :aria-expanded="node.hasChildren ? node.expanded : undefined">
                    <button class="twist-button" :class="{ hidden: !node.hasChildren, expanded: node.expanded }" type="button" :disabled="!node.hasChildren || node.loading" aria-label="展开或收起" @click.stop="toggle(node)"><ChevronRight :size="12" /></button>
                    <button class="tree-node" type="button" :disabled="node.kind === 'channel' && (node.status !== 1 || Boolean(node.channel && isUsed(node.channel)))" @click="choose(node)">
                        <component :is="nodeIcon(node)" :size="13" aria-hidden="true" />
                        <span class="node-name">{{ node.name }}</span>
                        <span v-if="node.kind === 'directory' || node.kind === 'favorite-group'" class="node-status">{{ nodeStatus(node) }}</span>
                        <span v-else class="node-status-dot" :class="node.status === 1 ? 'online' : 'offline'" role="img" :aria-label="nodeStatus(node)" :title="nodeStatus(node)" />
                    </button>
                    <button v-if="node.kind === 'channel' && node.channel && !isChannelFavorite(node.channel)" class="favorite-toggle" type="button" aria-label="收藏通道" title="收藏通道" @click.stop="openFavoriteDialog(node)">
                        <Star :size="14" aria-hidden="true" />
                    </button>
                    <a-popconfirm v-if="node.kind === 'channel' && node.favoriteGroupId" :content="`确认移除“${node.channel ? displayChannelName(node.channel) : ''}”吗？`" type="warning" @ok="removeFavoriteChannel(node)">
                        <button class="favorite-remove" type="button" aria-label="移除收藏通道" title="移除收藏通道" @click.stop><Trash2 :size="13" aria-hidden="true" /></button>
                    </a-popconfirm>
                    <button v-if="node.kind === 'favorite-group' && node.favoriteGroup" class="favorite-play" type="button" :data-test="`favorite-group-play-${node.favoriteGroup.id}`" aria-label="播放收藏组" title="播放收藏组" @click.stop="emit('select-group', node.favoriteGroup)">
                        <Play :size="14" aria-hidden="true" />
                    </button>
                    <a-popconfirm v-if="node.kind === 'favorite-group' && node.favoriteGroup" :content="`确认删除收藏组“${node.favoriteGroup.name}”吗？`" type="warning" @ok="removeFavoriteGroup(node)">
                        <button class="favorite-remove" type="button" aria-label="删除收藏组" title="删除收藏组" @click.stop><Trash2 :size="13" aria-hidden="true" /></button>
                    </a-popconfirm>
                </div>
                <div v-if="!loading && !visibleRows.length" class="tree-empty">{{ view === "devices" && hasDeviceFilters ? "当前筛选条件下暂无设备" : view === "favorites" ? "暂无收藏组" : "暂无设备或通道" }}</div>
            </div>
        </a-spin>
        <div v-if="view === 'devices' && deviceStatsLoaded" class="tree-pagination" :aria-label="`共 ${deviceListTotal} 台设备`">
            <span class="device-total">共 {{ deviceListTotal }} 台</span>
            <a-pagination v-if="deviceListTotal > devicePageSize" simple size="mini" :current="devicePage" :page-size="devicePageSize" :total="deviceListTotal" @change="changeDevicePage" />
        </div>
        <div v-if="favoriteDialogVisible" class="favorite-dialog-backdrop" @click.self="closeFavoriteDialog">
            <section class="favorite-dialog" role="dialog" aria-modal="true" aria-labelledby="favorite-dialog-title">
                <header><strong id="favorite-dialog-title">收藏通道</strong><button type="button" aria-label="关闭收藏组弹窗" title="关闭" @click="closeFavoriteDialog"><X :size="16" aria-hidden="true" /></button></header>
                <p>{{ favoriteDialogChannels.length > 1 ? `将当前 ${favoriteDialogChannels.length} 个播放通道加入收藏组` : `将“${favoriteDialogChannel ? displayChannelName(favoriteDialogChannel) : ''}”加入收藏组` }}</p>
                <div class="favorite-dialog-modes" role="tablist" aria-label="收藏方式">
                    <button type="button" role="tab" :aria-selected="favoriteDialogMode === 'existing'" :class="{ active: favoriteDialogMode === 'existing' }" @click="favoriteDialogMode = 'existing'; favoriteDialogError = ''">选择已有组</button>
                    <button type="button" role="tab" :aria-selected="favoriteDialogMode === 'new'" :class="{ active: favoriteDialogMode === 'new' }" @click="favoriteDialogMode = 'new'; favoriteDialogError = ''">新建组</button>
                </div>
                <div v-if="favoriteDialogMode === 'existing'" class="favorite-dialog-field">
                    <label for="favorite-group-select">收藏组</label>
                    <a-select id="favorite-group-select" v-model="favoriteDialogGroupId" data-test="favorite-group-select" aria-label="选择已有收藏组" placeholder="请选择收藏组" allow-clear size="small" style="width: 100%">
                        <a-option v-for="group in favoriteGroups" :key="group.id" :value="String(group.id)">{{ group.name }}（{{ group.channels.length }} 个）</a-option>
                    </a-select>
                    <span v-if="!favoriteGroups.length" class="favorite-dialog-hint">暂无已有组，请切换到“新建组”</span>
                    <span v-else class="favorite-dialog-hint">已在组内的设备不会重复添加</span>
                </div>
                <div v-else class="favorite-dialog-field">
                    <label for="favorite-group-name">新建组名称</label>
                    <input id="favorite-group-name" v-model="favoriteGroupName" data-test="favorite-group-name" type="text" maxlength="64" autofocus placeholder="输入组名，例如：园区重点通道" aria-label="新建收藏组名称" @keyup.enter="saveFavoriteGroup" />
                    <span class="favorite-dialog-hint">组名不能与已有组重复</span>
                </div>
                <p v-if="favoriteDialogError" class="favorite-dialog-error" role="alert">{{ favoriteDialogError }}</p>
                <footer><button type="button" class="dialog-secondary" @click="closeFavoriteDialog">取消</button><button type="button" class="dialog-primary" data-test="save-favorite-group" @click="saveFavoriteGroup">保存收藏</button></footer>
            </section>
        </div>
    </aside>
</template>

<style scoped>
.playback-source-tree { position: relative; display: flex; min-width: 0; min-height: 0; flex: 1 1 auto; flex-direction: column; color: var(--uvp-text-secondary); background: var(--zlm-card); border: 1px solid var(--zlm-border); }
.tree-head, .tree-head-actions, .tree-summary, .tree-summary span { display: flex; align-items: center; }
.tree-head { min-height: 44px; justify-content: space-between; padding: 7px 10px 7px 14px; border-bottom: 1px solid var(--uvp-panel-border); }
.device-total { color: var(--uvp-text-tertiary); font-size: 10px; font-weight: 500; }
.tree-summary { gap: 12px; color: var(--uvp-text-tertiary); font-size: 10px; }
.tree-summary span { gap: 5px; }
.summary-dot { display: inline-block; width: 8px; height: 8px; flex: 0 0 8px; border-radius: 50%; }
.summary-dot.online { background: #10B981; box-shadow: 0 0 0 2px rgb(16 185 129 / 12%); }
.summary-dot.offline { background: #EF4444; box-shadow: 0 0 0 2px rgb(239 68 68 / 10%); }
.tree-head-actions { gap: 2px; }
.tree-icon-button { position: relative; display: inline-grid; width: 26px; height: 26px; padding: 0; color: var(--uvp-text-tertiary); background: transparent; border: 0; border-radius: 5px; place-items: center; cursor: pointer; }
.tree-icon-button:hover, .tree-filter-toggle[aria-expanded="true"], .tree-filter-toggle.active { color: var(--uvp-brand); background: var(--uvp-sidebar-active-bg); }
.tree-filter-toggle.active::after { position: absolute; top: 4px; right: 4px; width: 5px; height: 5px; background: var(--uvp-brand); border: 1px solid var(--zlm-card); border-radius: 50%; content: ""; }
.tree-views { display: flex; min-width: 0; gap: 2px; align-items: center; margin: 10px 12px 8px; padding: 2px; background: var(--uvp-list-toolbar-bg); border: 1px solid var(--uvp-panel-border); border-radius: 6px; }
.tree-view-scroll { display: flex; min-width: 0; flex: 1; gap: 2px; overflow-x: auto; scrollbar-width: none; }
.tree-view-scroll::-webkit-scrollbar { display: none; }
.tree-views button { height: 28px; padding: 0 8px; color: var(--uvp-text-tertiary); white-space: nowrap; background: transparent; border: 0; border-radius: 4px; cursor: pointer; font-size: 12px; }
.tree-view-scroll button { flex: 0 0 auto; }
.tree-view-scroll [role="tab"] { position: relative; display: inline-flex; align-items: center; gap: 4px; border: 1px solid transparent; transition: color 0.15s ease, background 0.15s ease, border-color 0.15s ease, box-shadow 0.15s ease; }
.tree-view-scroll [role="tab"] > svg { flex: 0 0 auto; }
.tree-view-scroll [role="tab"]:hover:not(.active) { color: var(--uvp-text-primary); background: var(--uvp-sidebar-active-bg); }
.tree-view-scroll [role="tab"]:focus-visible { outline: 2px solid var(--uvp-brand); outline-offset: 1px; }
.tree-view-scroll [role="tab"].active { color: var(--uvp-brand-strong, var(--uvp-brand)); font-weight: 650; }
.tree-view-arrow { display: inline-grid; width: 24px; flex: 0 0 24px; padding: 0; place-items: center; }
.tree-view-arrow:hover { color: var(--uvp-text-primary); background: var(--uvp-panel-bg); }
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
.tree-loading { min-width: 0; min-height: 0; flex: 1; overflow: hidden; }
.tree-content { box-sizing: border-box; width: 100%; height: 100%; min-width: 0; min-height: 120px; padding: 2px 6px 10px; overflow: auto; scrollbar-gutter: stable; }
.tree-row { display: flex; width: 100%; height: 32px; min-width: 0; align-items: center; border-radius: 5px; }
.tree-row:hover { background: var(--uvp-sidebar-active-bg); }
.tree-row.used { background: color-mix(in srgb, var(--uvp-brand) 8%, transparent); }
.twist-button { display: inline-grid; width: 22px; height: 22px; flex: 0 0 22px; padding: 0; color: var(--uvp-text-tertiary); background: transparent; border: 0; border-radius: 5px; cursor: pointer; place-items: center; transition: transform 0.15s ease; }
.twist-button:hover:not(:disabled) { color: var(--uvp-brand); background: var(--uvp-sidebar-active-bg); }
.twist-button.expanded { transform: rotate(90deg); }
.twist-button.hidden { visibility: hidden; }
.tree-node { display: flex; min-width: 0; height: 100%; flex: 1; align-items: center; gap: 8px; padding: 0 8px 0 6px; color: var(--uvp-text-secondary); text-align: left; background: transparent; border: 0; cursor: pointer; font: inherit; }
.tree-node:disabled { cursor: not-allowed; }
.tree-node > svg { flex: 0 0 auto; color: var(--uvp-brand); }
.favorite-toggle { display: inline-grid; width: 24px; height: 24px; flex: 0 0 24px; margin-right: 4px; padding: 0; color: var(--uvp-text-tertiary); background: transparent; border: 0; border-radius: 4px; cursor: pointer; place-items: center; }
.favorite-toggle:hover, .favorite-toggle.active { color: var(--zlm-warn-500); background: var(--uvp-sidebar-active-bg); }
.favorite-remove { display: inline-grid; width: 20px; height: 22px; flex: 0 0 20px; margin-right: 0; padding: 0; color: var(--uvp-danger); background: transparent; border: 0; border-radius: 4px; cursor: pointer; place-items: center; }
.favorite-remove:hover { color: var(--uvp-danger); background: var(--uvp-sidebar-active-bg); }
.favorite-play { display: inline-grid; width: 22px; height: 22px; flex: 0 0 22px; margin-right: 0; padding: 0; color: var(--uvp-brand); background: transparent; border: 1px solid var(--uvp-panel-border); border-radius: 4px; cursor: pointer; place-items: center; }
.favorite-play:hover { background: var(--uvp-sidebar-active-bg); border-color: var(--uvp-brand); }
.tree-row.offline .tree-node > svg { color: var(--uvp-text-tertiary); }
.tree-row.offline .node-name { color: var(--uvp-text-secondary); }
.node-name { min-width: 0; flex: 1; overflow: hidden; color: var(--uvp-text-primary); text-overflow: ellipsis; white-space: nowrap; font-size: 12px; }
.node-status { flex: 0 0 auto; color: var(--uvp-text-tertiary); font-size: 10px; }
.node-status-dot { position: relative; width: 18px; height: 100%; flex: 0 0 18px; margin-left: 8px; }
.node-status-dot::after { position: absolute; top: 50%; left: 50%; width: 6px; height: 6px; border-radius: 50%; content: ""; transform: translate(-50%, -50%); }
.node-status-dot.online::after { background: #10B981; }
.node-status-dot.offline::after { background: #EF4444; }
.tree-error { display: flex; gap: 8px; align-items: center; padding: 10px 12px; color: var(--uvp-danger); font-size: 12px; }
.tree-error button { padding: 0; color: var(--uvp-brand); background: transparent; border: 0; cursor: pointer; font: inherit; }
.tree-empty { display: grid; min-height: 120px; color: var(--uvp-text-tertiary); place-items: center; font-size: 12px; }
.favorite-dialog-backdrop { position: fixed; z-index: 1000; inset: 0; display: grid; padding: 16px; background: rgb(15 23 42 / 20%); place-items: center; }
.favorite-dialog { width: min(320px, 100%); padding: 16px; color: var(--uvp-text-primary); background: var(--zlm-card); border: 1px solid var(--uvp-panel-border); border-radius: 8px; box-shadow: 0 12px 30px rgb(15 23 42 / 18%); }
.favorite-dialog header, .favorite-dialog footer { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.favorite-dialog header { padding-bottom: 10px; border-bottom: 1px solid var(--uvp-panel-border); }
.favorite-dialog header button { display: inline-grid; width: 26px; height: 26px; padding: 0; color: var(--uvp-text-tertiary); background: transparent; border: 0; border-radius: 4px; cursor: pointer; place-items: center; }
.favorite-dialog header button:hover { color: var(--uvp-text-primary); background: var(--uvp-sidebar-active-bg); }
.favorite-dialog > p { margin: 12px 0 8px; font-size: 12px; }
.favorite-dialog-modes { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 2px; margin: 14px 0 12px; padding: 2px; background: var(--uvp-list-toolbar-bg); border: 1px solid var(--uvp-panel-border); border-radius: 5px; }
.favorite-dialog-modes button { height: 28px; color: var(--uvp-text-tertiary); background: transparent; border: 0; border-radius: 4px; cursor: pointer; font: inherit; font-size: 11px; }
.favorite-dialog-modes button:hover { color: var(--uvp-text-primary); background: var(--uvp-sidebar-active-bg); }
.favorite-dialog-modes button:disabled { color: var(--uvp-text-tertiary); cursor: not-allowed; opacity: 0.55; }
.favorite-dialog-modes button.active { color: var(--uvp-brand); font-weight: 600; background: var(--uvp-panel-bg); box-shadow: 0 0 0 1px var(--uvp-panel-border); }
.favorite-dialog-field { display: grid; gap: 6px; }
.favorite-dialog-field label { color: var(--uvp-text-secondary); font-size: 11px; }
.favorite-dialog select { box-sizing: border-box; width: 100%; height: 34px; padding: 0 9px; color: var(--uvp-text-primary); background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: 5px; outline: 0; font: inherit; font-size: 12px; }
.favorite-dialog select:focus { border-color: var(--uvp-brand); }
.favorite-dialog input { box-sizing: border-box; width: 100%; height: 34px; padding: 0 9px; color: var(--uvp-text-primary); background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: 5px; outline: 0; font: inherit; font-size: 12px; }
.favorite-dialog input:focus { border-color: var(--uvp-brand); }
.favorite-dialog-hint { display: block; margin-top: 7px; color: var(--uvp-text-tertiary); font-size: 10px; }
.favorite-dialog-error { color: var(--uvp-danger); font-size: 11px; }
.favorite-dialog footer { justify-content: flex-end; margin-top: 16px; }
.favorite-dialog .dialog-secondary, .favorite-dialog .dialog-primary { height: 30px; padding: 0 11px; border-radius: 5px; cursor: pointer; font: inherit; font-size: 11px; }
.favorite-dialog .dialog-secondary { color: var(--uvp-text-secondary); background: transparent; border: 1px solid var(--uvp-panel-border); }
.favorite-dialog .dialog-primary { color: #fff; background: var(--uvp-brand); border: 1px solid var(--uvp-brand); }
.tree-pagination { display: flex; min-height: 38px; flex: 0 0 38px; align-items: center; justify-content: center; gap: 12px; padding: 4px 8px; border-top: 1px solid var(--uvp-panel-border); }
.spin { animation: source-tree-spin 0.9s linear infinite; }
@keyframes source-tree-spin { to { transform: rotate(360deg); } }
</style>
