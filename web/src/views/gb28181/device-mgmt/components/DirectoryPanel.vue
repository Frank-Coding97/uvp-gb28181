<script setup lang="ts">
import { computed, nextTick, onMounted, onBeforeUnmount, ref, watch } from "vue";
import { Camera, ChevronRight, Circle, Folder, FolderTree, MapPin, RefreshCcw, Search } from "@lucide/vue";
import { Message } from "@arco-design/web-vue";
import { listDirectoryTree, type DirectoryNode, type DirectoryResourceFilter } from "../api";
import {
  persistDirectoryViewPreference,
  selectDirectory,
  switchDirectoryView,
  type DirectoryState,
  type DirectoryView
} from "../directoryState";

import { mergeDirectorySync, type DirectorySyncResult } from "../directorySync";

const props = defineProps<{
  modelValue: DirectoryState;
  selectedDeviceId: number | null;
  resourceFilter?: DirectoryResourceFilter;
}>();

const emit = defineEmits<{
  "update:modelValue": [value: DirectoryState];
  select: [node: DirectoryNode];
  deviceSelect: [device: NonNullable<DirectoryNode["device"]>];
  viewChange: [view: DirectoryView];
  treeLoaded: [view: DirectoryView, tree: DirectoryNode[]];
}>();

const loading = ref(false);
const trees = ref<Record<DirectoryView, DirectoryNode[]>>({ administrative: [], department: [], business: [] });
const loaded = ref<Record<DirectoryView, boolean>>({ administrative: false, department: false, business: false });
const requestTokens: Record<DirectoryView, number> = { administrative: 0, department: 0, business: 0 };
let loadingToken = 0;
let activeView = props.modelValue.view;
const treesInitialized = new Set<DirectoryView>();
const revisions: Partial<Record<DirectoryView, string>> = {};
const pending = new Set<DirectoryView>();
let filterTimer: ReturnType<typeof setTimeout> | undefined;
let disposed = false;
watch(() => JSON.stringify(props.resourceFilter || {}), () => {
  for (const view of Object.keys(requestTokens) as DirectoryView[]) {
    ++requestTokens[view];
    loaded.value[view] = false;
    delete revisions[view];
  }
  pending.clear();
  clearTimeout(filterTimer);
  filterTimer = setTimeout(() => { filterTimer = undefined; void load(props.modelValue.view, true); }, 300);
}, { flush: "sync" });
onBeforeUnmount(() => {
  disposed = true;
  clearTimeout(filterTimer);
});

watch(
  () => props.modelValue.view,
  view => {
    activeView = view;
  }
);

const currentTree = computed(() => trees.value[props.modelValue.view]);
const selectedKey = computed(() => props.modelValue.selectedKey[props.modelValue.view]);
const expandedKeys = computed(() => props.modelValue.expandedKeys[props.modelValue.view]);
const normalizedKeyword = computed(() => props.modelValue.treeKeyword.trim().toLocaleLowerCase());

// ⛔ 业务分组视图只展示设备**实际上报**的业务分组（国标附录 J），没上报的设备不属于本视图，
// 所以「空」是正常状态而非故障 —— 文案必须说清楚，否则会被当成加载失败。
const emptyHint = computed(() => {
  if (normalizedKeyword.value || props.resourceFilter?.q || props.resourceFilter?.status) return "暂无匹配目录或设备";
  if (props.modelValue.view === "business") return "当前设备未上报业务分组";
  return "暂无目录数据";
});

function nodeMatches(node: DirectoryNode, keyword: string) {
  return node.name.toLocaleLowerCase().includes(keyword) || (node.code || "").toLocaleLowerCase().includes(keyword);
}

function filterTree(nodes: DirectoryNode[], keyword: string): DirectoryNode[] {
  if (!keyword) return nodes;
  return nodes.flatMap(node => {
    const children = filterTree(node.children || [], keyword);
    if (!nodeMatches(node, keyword) && children.length === 0) return [];
    return [{ ...node, children }];
  });
}

const visibleRows = computed(() => {
  const rows: Array<{ node: DirectoryNode; level: number }> = [];
  const keyword = normalizedKeyword.value;
  const walk = (nodes: DirectoryNode[], level: number) => {
    nodes.forEach(node => {
      rows.push({ node, level });
      if (keyword || expandedKeys.value.includes(node.key)) walk(node.children || [], level + 1);
    });
  };
  walk(filterTree(currentTree.value, keyword), 0);
  return rows;
});

function collectExpandableKeys(nodes: DirectoryNode[]): string[] {
  return nodes.flatMap(node => (node.children?.length ? [node.key, ...collectExpandableKeys(node.children)] : []));
}

function updateState(state: DirectoryState) {
  emit("update:modelValue", state);
}

function setKeyword(value: string) {
  updateState({ ...props.modelValue, treeKeyword: value });
}

function setExpanded(keys: string[]) {
  updateState({
    ...props.modelValue,
    expandedKeys: { ...props.modelValue.expandedKeys, [props.modelValue.view]: keys }
  });
}

async function load(view: DirectoryView, force = false, background = false) {
  if (disposed || (background && (pending.has(view) || filterTimer))) return;
  if (loaded.value[view] && !force) return;
  const requestToken = ++requestTokens[view];
  const currentLoadingToken = ++loadingToken;
  const firstLoad = !loaded.value[view];
  if (!background || firstLoad) loading.value = true;
  pending.add(view);
  try {
    const response = await listDirectoryTree(view, { ...props.resourceFilter, sync: 1, since: revisions[view] });
    if (disposed || requestToken !== requestTokens[view]) return;
    if (response.code !== 0) throw new Error(response.message || "目录加载失败");
    const data = response.data;
    const list = data?.revision
      ? mergeDirectorySync(trees.value[view], data as DirectorySyncResult)
      : data?.list || [];
    revisions[view] = data?.revision;
    trees.value = { ...trees.value, [view]: list };
    loaded.value = { ...loaded.value, [view]: true };
    emit("treeLoaded", view, list);
    if (firstLoad && !treesInitialized.has(view) && props.modelValue.expandedKeys[view].length === 0) {
      updateState({
        ...props.modelValue,
        view: activeView,
        expandedKeys: { ...props.modelValue.expandedKeys, [view]: collectExpandableKeys(list) }
      });
    }
    await nextTick();
    if (activeView === view && view === "business" && !props.modelValue.selectedKey.business) {
      const firstResourceGroup = list.find(node => node.count > 0 || node.children?.some(child => child.count > 0));
      if (firstResourceGroup) {
        const nextState = selectDirectory({
          ...props.modelValue, view: "business",
          expandedKeys: { ...props.modelValue.expandedKeys, business: props.modelValue.expandedKeys.business.length ? props.modelValue.expandedKeys.business : collectExpandableKeys(list) }
        }, firstResourceGroup.key);
        updateState(nextState);
        emit("select", firstResourceGroup);
      }
    }
    treesInitialized.add(view);
  } catch (error: any) {
    if (!disposed && requestToken === requestTokens[view]) {
      delete revisions[view];
      if (!background) Message.error(error?.message || "目录加载失败");
    }
  } finally {
    if (requestToken === requestTokens[view]) pending.delete(view);
    if (currentLoadingToken === loadingToken) loading.value = false;
  }
}

async function changeView(view: DirectoryView) {
  if (view === props.modelValue.view) return;
  activeView = view;
  persistDirectoryViewPreference(view);
  updateState(switchDirectoryView(props.modelValue, view));
  emit("viewChange", view);
  await load(view, true);
}

function toggle(node: DirectoryNode) {
  if (!node.children?.length) return;
  if (expandedKeys.value.includes(node.key)) setExpanded(expandedKeys.value.filter(key => key !== node.key));
  else setExpanded([...expandedKeys.value, node.key]);
}

function choose(node: DirectoryNode) {
  if (node.device) {
    emit("deviceSelect", node.device);
    return;
  }
  updateState(selectDirectory(props.modelValue, node.key));
  emit("select", node);
}

function nodeTitle(node: DirectoryNode) {
  const detail = node.code ? `${node.name} (${node.code})` : node.name;
  return node.depth >= 3 ? `${detail}。目录层级较深，建议后续整理。` : detail;
}

async function refresh(view: DirectoryView = props.modelValue.view) {
  await load(view, true);
}

async function sync() { await load(props.modelValue.view, true, true); }

defineExpose({ refresh, sync });
onMounted(() => load(props.modelValue.view));
</script>

<template>
  <aside class="directory-panel" aria-label="设备目录">
    <div class="directory-head">
      <div class="directory-title"><FolderTree :size="14" /><span>设备目录</span></div>
      <div class="directory-actions">
        <a-tooltip content="刷新目录">
          <button class="icon-button uvp-refresh-btn" type="button" aria-label="刷新目录" @click="refresh()">
            <RefreshCcw :size="13" :class="{ spin: loading }" />
          </button>
        </a-tooltip>
      </div>
    </div>

    <div class="directory-switch" role="tablist" aria-label="目录类型">
      <button
        data-view="administrative"
        type="button"
        role="tab"
        :aria-selected="modelValue.view === 'administrative'"
        :class="{ active: modelValue.view === 'administrative' }"
        @click="changeView('administrative')"
      >
        <MapPin :size="13" aria-hidden="true" />
        <span>行政区划</span>
      </button>
      <button
        data-view="department"
        type="button"
        role="tab"
        :aria-selected="modelValue.view === 'department'"
        :class="{ active: modelValue.view === 'department' }"
        @click="changeView('department')"
      >
        <Folder :size="13" aria-hidden="true" />
        <span>部门目录</span>
      </button>
      <button
        data-view="business"
        type="button"
        role="tab"
        :aria-selected="modelValue.view === 'business'"
        :class="{ active: modelValue.view === 'business' }"
        @click="changeView('business')"
      >
        <FolderTree :size="13" aria-hidden="true" />
        <span>业务分组</span>
      </button>
    </div>

    <label class="directory-search">
      <Search :size="13" />
      <input
        data-testid="directory-search"
        :value="modelValue.treeKeyword"
        type="search"
        aria-label="搜索目录"
        placeholder="搜索目录"
        @input="setKeyword(($event.target as HTMLInputElement).value)"
      />
    </label>

    <a-spin :loading="loading" class="directory-tree-wrap">
      <div class="directory-tree" role="tree">
        <div
          v-for="{ node, level } in visibleRows"
          :key="node.key"
          :data-node-key="node.key"
          class="directory-row"
          :class="{ active: selectedKey === node.key || selectedDeviceId === node.device?.id, 'directory-row-device': Boolean(node.device) }"
          :style="{ paddingLeft: `${8 + level * 14}px` }"
          :title="nodeTitle(node)"
          role="treeitem"
          :aria-selected="selectedKey === node.key || selectedDeviceId === node.device?.id"
        >
          <button
            class="twist-button"
            :class="{ hidden: !node.children?.length, expanded: expandedKeys.includes(node.key) || normalizedKeyword }"
            type="button"
            :disabled="!node.children?.length"
            aria-label="展开或收起"
            @click.stop="toggle(node)"
          >
            <ChevronRight :size="12" />
          </button>
          <button class="directory-node" :class="{ 'directory-device-node': node.device }" type="button" @click="choose(node)">
            <template v-if="node.device">
              <span class="device-node-icon" :class="{ online: node.device.online }"><Circle :size="8" fill="currentColor" /></span>
              <span class="device-node-copy">
                <span class="node-name">{{ node.name }}</span>
                <span class="device-node-code">{{ node.device.deviceId }}</span>
              </span>
              <span class="device-node-status" :class="{ online: node.device.online }">{{ node.device.online ? "在线" : "离线" }}</span>
            </template>
            <template v-else>
              <MapPin v-if="node.type === 'area' || node.type === 'unknown'" :size="13" />
              <Camera v-else-if="node.type === 'channel'" :size="13" class="channel-node-icon" />
              <FolderTree v-else-if="node.type === 'biz_group'" :size="13" />
              <Folder v-else :size="13" />
              <span class="node-name">{{ node.name }}</span>
              <span v-if="node.meta?.sourceName && node.type !== 'channel'" class="node-source">{{ node.meta.sourceName }}</span>
              <span v-if="node.type === 'channel'" class="channel-status" :class="{ online: node.onlineCount > 0 }"><Circle :size="8" fill="currentColor" />{{ node.onlineCount > 0 ? "在线" : "离线" }}</span>
              <span
                v-else
                class="node-count"
                :aria-label="modelValue.view === 'business' ? `在线通道 ${node.onlineCount} 个，共 ${node.count} 个` : `在线设备 ${node.onlineCount} 台，共 ${node.count} 台`"
                :title="`在线 ${node.onlineCount} / 总数 ${node.count}`"
              >
                <span class="node-online-count">{{ node.onlineCount }}</span
                ><span aria-hidden="true">/</span><span>{{ node.count }}</span>
              </span>
            </template>
          </button>
        </div>
        <div v-if="!loading && visibleRows.length === 0" class="directory-empty">{{ emptyHint }}</div>
      </div>
    </a-spin>
  </aside>
</template>

<style scoped>
.node-source { max-width: 90px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 11px; color: var(--color-text-3); }
.channel-status { display: inline-flex; align-items: center; gap: 4px; margin-left: auto; font-size: 11px; color: #f53f3f; white-space: nowrap; }
.channel-status.online { color: #00b42a; }

.directory-panel {
  display: flex;
  flex-direction: column;
  width: 240px;
  min-width: 240px;
  min-height: 0;
  overflow: hidden;
  color: var(--uvp-text-secondary);
  background: var(--uvp-panel-bg);
  border-right: 1px solid var(--uvp-panel-border);
}
.directory-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 42px;
  min-height: 42px;
  padding: 0 10px 0 14px;
  border-bottom: 1px solid var(--uvp-panel-border);
}
.directory-title,
.directory-actions,
.directory-node {
  display: flex;
  align-items: center;
}
.directory-title {
  gap: 7px;
  font-size: 13px;
  font-weight: 620;
  color: var(--uvp-text-primary);
}
.directory-actions {
  gap: 2px;
}
.icon-button,
.twist-button {
  display: inline-grid;
  place-items: center;
  width: 26px;
  height: 26px;
  padding: 0;
  color: var(--uvp-text-tertiary);
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: 5px;
}
.icon-button:hover,
.twist-button:hover {
  color: var(--uvp-brand);
  background: var(--uvp-sidebar-active-bg);
}
.directory-switch {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 2px;
  padding: 2px;
  margin: 10px 12px 8px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 6px;
}
.directory-switch button {
  position: relative;
  display: inline-flex;
  gap: 3px;
  align-items: center;
  justify-content: center;
  min-width: 0;
  height: 28px;
  padding: 0 3px;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  cursor: pointer;
  background: transparent;
  border: 1px solid transparent;
  border-radius: 4px;
  transition:
    color 0.15s ease,
    background 0.15s ease,
    border-color 0.15s ease,
    box-shadow 0.15s ease;
}
.directory-switch button > svg {
  flex: 0 0 auto;
}
.directory-switch button:hover:not(.active) {
  color: var(--uvp-text-primary);
  background: var(--uvp-sidebar-active-bg);
}
.directory-switch button:focus-visible {
  outline: 2px solid var(--uvp-brand);
  outline-offset: 1px;
}
.directory-switch button.active {
  font-weight: 650;
  color: var(--uvp-brand-strong, var(--uvp-brand));
}
.directory-search {
  display: flex;
  gap: 7px;
  align-items: center;
  height: 32px;
  min-height: 32px;
  padding: 0 9px;
  margin: 0 12px 8px;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-page-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 6px;
}
.directory-search:focus-within {
  border-color: var(--uvp-brand);
}
.directory-search input {
  width: 100%;
  min-width: 0;
  font-size: 12px;
  color: var(--uvp-text-primary);
  outline: none;
  background: transparent;
  border: 0;
}
.directory-tree-wrap {
  flex: 1;
  min-height: 0;
}
:global(body[arco-theme="dark"] .directory-tree-wrap.arco-spin .arco-spin-mask) {
  background-color: rgb(16 25 35 / 78%) !important;
}
.directory-tree {
  box-sizing: border-box;
  height: 100%;
  padding: 2px 6px 10px;
  overflow: auto;
  scrollbar-gutter: stable;
}
.directory-row {
  display: flex;
  align-items: center;
  min-width: 0;
  height: 32px;
  border-radius: 5px;
}
.directory-row-device {
  height: 42px;
}
.directory-row:hover,
.directory-row.active {
  color: var(--uvp-text-primary);
  background: var(--uvp-sidebar-active-bg);
}
.twist-button {
  flex: 0 0 22px;
  width: 22px;
  height: 22px;
  transition: transform 0.15s ease;
}
.twist-button.expanded {
  transform: rotate(90deg);
}
.twist-button.hidden {
  visibility: hidden;
}
.directory-node {
  flex: 1;
  gap: 7px;
  min-width: 0;
  height: 100%;
  padding: 0 4px 0 2px;
  color: inherit;
  text-align: left;
  cursor: pointer;
  background: transparent;
  border: 0;
}
.directory-node > svg {
  flex: 0 0 auto;
  color: var(--uvp-brand);
}
.directory-device-node {
  gap: 7px;
}
.device-node-icon {
  display: inline-grid;
  flex: 0 0 16px;
  place-items: center;
  color: var(--uvp-danger);
}
.device-node-icon.online {
  color: #158052;
}
:global(body[arco-theme="dark"] .directory-panel .device-node-icon.online) {
  color: #86efac;
}
.device-node-copy {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  line-height: 1.15;
}
.device-node-code {
  overflow: hidden;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.device-node-status {
  flex: 0 0 auto;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
}
.device-node-status.online {
  color: #158052;
}
:global(body[arco-theme="dark"] .directory-panel .device-node-status.online) {
  color: #86efac;
}
.node-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.node-count {
  display: inline-flex;
  justify-content: flex-end;
  min-width: 32px;
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  color: var(--uvp-text-tertiary);
  text-align: right;
}
.node-online-count {
  font-weight: 600;
  color: #158052;
}
:global(body[arco-theme="dark"] .directory-panel .node-online-count) {
  color: #86efac;
}
.directory-empty {
  display: grid;
  place-items: center;
  min-height: 120px;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.spin {
  animation: directory-spin 0.9s linear infinite;
}

@keyframes directory-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (width <= 900px) {
  .directory-panel {
    width: 100%;
    min-width: 0;
    max-height: 320px;
    border-right: 0;
    border-bottom: 1px solid var(--uvp-panel-border);
  }
  .directory-tree-wrap {
    min-height: 160px;
  }
}
</style>
