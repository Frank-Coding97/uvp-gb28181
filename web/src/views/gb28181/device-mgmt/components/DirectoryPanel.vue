<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { ChevronRight, Folder, FolderPlus, FolderTree, MapPin, MoreHorizontal, RefreshCcw, Search } from "@lucide/vue";
import { Message } from "@arco-design/web-vue";
import { listDirectoryTree, type DirectoryNode, type DirectoryView } from "../api";
import { selectDirectory, switchDirectoryView, type DirectoryState } from "../directoryState";

const props = defineProps<{
    modelValue: DirectoryState;
    canManage: boolean;
}>();

const emit = defineEmits<{
    "update:modelValue": [value: DirectoryState];
    select: [node: DirectoryNode];
    viewChange: [view: DirectoryView];
    create: [parent: DirectoryNode | null];
    rename: [node: DirectoryNode];
    move: [node: DirectoryNode];
    delete: [node: DirectoryNode];
    treeLoaded: [view: DirectoryView, tree: DirectoryNode[]];
}>();

const loading = ref(false);
const trees = ref<Record<DirectoryView, DirectoryNode[]>>({ national: [], custom: [] });
const loaded = ref<Record<DirectoryView, boolean>>({ national: false, custom: false });
const requestTokens: Record<DirectoryView, number> = { national: 0, custom: 0 };
let loadingToken = 0;
let activeView = props.modelValue.view;

watch(() => props.modelValue.view, (view) => { activeView = view; });

const currentTree = computed(() => trees.value[props.modelValue.view]);
const selectedKey = computed(() => props.modelValue.selectedKey[props.modelValue.view]);
const expandedKeys = computed(() => props.modelValue.expandedKeys[props.modelValue.view]);
const normalizedKeyword = computed(() => props.modelValue.treeKeyword.trim().toLocaleLowerCase());

function nodeMatches(node: DirectoryNode, keyword: string) {
    return node.name.toLocaleLowerCase().includes(keyword) || (node.code || "").toLocaleLowerCase().includes(keyword);
}

function filterTree(nodes: DirectoryNode[], keyword: string): DirectoryNode[] {
    if (!keyword) return nodes;
    return nodes.flatMap((node) => {
        const children = filterTree(node.children || [], keyword);
        if (!nodeMatches(node, keyword) && children.length === 0) return [];
        return [{ ...node, children }];
    });
}

const visibleRows = computed(() => {
    const rows: Array<{ node: DirectoryNode; level: number }> = [];
    const keyword = normalizedKeyword.value;
    const walk = (nodes: DirectoryNode[], level: number) => {
        nodes.forEach((node) => {
            rows.push({ node, level });
            if (keyword || expandedKeys.value.includes(node.key)) walk(node.children || [], level + 1);
        });
    };
    walk(filterTree(currentTree.value, keyword), 0);
    return rows;
});

function collectExpandableKeys(nodes: DirectoryNode[]): string[] {
    return nodes.flatMap((node) => (node.children?.length ? [node.key, ...collectExpandableKeys(node.children)] : []));
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

async function load(view: DirectoryView, force = false) {
    if (loaded.value[view] && !force) return;
    const requestToken = ++requestTokens[view];
    const currentLoadingToken = ++loadingToken;
    loading.value = true;
    try {
        const response = await listDirectoryTree(view);
        if (requestToken !== requestTokens[view]) return;
        if (response.code !== 0) throw new Error(response.message || "目录加载失败");
        const list = response.data?.list || [];
        trees.value = { ...trees.value, [view]: list };
        loaded.value = { ...loaded.value, [view]: true };
        emit("treeLoaded", view, list);
        if (props.modelValue.expandedKeys[view].length === 0) {
            updateState({
                ...props.modelValue,
                view: activeView,
                expandedKeys: { ...props.modelValue.expandedKeys, [view]: collectExpandableKeys(list) }
            });
        }
    } catch (error: any) {
        if (requestToken === requestTokens[view]) Message.error(error?.message || "目录加载失败");
    } finally {
        if (currentLoadingToken === loadingToken) loading.value = false;
    }
}

async function changeView(view: DirectoryView) {
    if (view === props.modelValue.view) return;
    activeView = view;
    updateState(switchDirectoryView(props.modelValue, view));
    emit("viewChange", view);
    await load(view);
}

function toggle(node: DirectoryNode) {
    if (!node.children?.length) return;
    if (expandedKeys.value.includes(node.key)) setExpanded(expandedKeys.value.filter((key) => key !== node.key));
    else setExpanded([...expandedKeys.value, node.key]);
}

function choose(node: DirectoryNode) {
    updateState(selectDirectory(props.modelValue, node.key));
    emit("select", node);
}

function actionAllowed(node: DirectoryNode) {
    return props.canManage && props.modelValue.view === "custom" && node.type === "group" && !node.readOnly;
}

function nodeTitle(node: DirectoryNode) {
    const detail = node.code ? `${node.name} (${node.code})` : node.name;
    return node.depth >= 3 ? `${detail}。目录层级较深，建议后续整理。` : detail;
}

async function refresh(view: DirectoryView = props.modelValue.view) {
    await load(view, true);
}

defineExpose({ refresh });
onMounted(() => load(props.modelValue.view));
</script>

<template>
    <aside class="directory-panel" aria-label="设备目录">
        <div class="directory-head">
            <div class="directory-title"><FolderTree :size="14" /><span>设备目录</span></div>
            <div class="directory-actions">
                <a-tooltip v-if="canManage && modelValue.view === 'custom'" content="新建根分组">
                    <button data-action="create-root" class="icon-button" type="button" aria-label="新建根分组" @click="emit('create', null)">
                        <FolderPlus :size="14" />
                    </button>
                </a-tooltip>
                <a-tooltip content="刷新目录">
                    <button class="icon-button" type="button" aria-label="刷新目录" @click="refresh()">
                        <RefreshCcw :size="13" :class="{ spin: loading }" />
                    </button>
                </a-tooltip>
            </div>
        </div>

        <div class="directory-switch" role="tablist" aria-label="目录类型">
            <button data-view="national" type="button" :class="{ active: modelValue.view === 'national' }" @click="changeView('national')">国标目录</button>
            <button data-view="custom" type="button" :class="{ active: modelValue.view === 'custom' }" @click="changeView('custom')">自定义分组</button>
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
                    :class="{ active: selectedKey === node.key }"
                    :style="{ paddingLeft: `${8 + level * 14}px` }"
                    :title="nodeTitle(node)"
                    role="treeitem"
                    :aria-selected="selectedKey === node.key"
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
                    <button class="directory-node" type="button" @click="choose(node)">
                        <MapPin v-if="node.type === 'area' || node.type === 'unknown'" :size="13" />
                        <Folder v-else :size="13" />
                        <span class="node-name">{{ node.name }}</span>
                        <span class="node-count">{{ node.count }}</span>
                    </button>
                    <a-dropdown v-if="actionAllowed(node)" trigger="click" position="br">
                        <button class="more-button" type="button" aria-label="分组操作"><MoreHorizontal :size="14" /></button>
                        <template #content>
                            <a-menu>
                                <a-menu-item @click="emit('create', node)">新建子分组</a-menu-item>
                                <a-menu-item data-action="rename" @click="emit('rename', node)">重命名</a-menu-item>
                                <a-menu-item @click="emit('move', node)">移动</a-menu-item>
                                <a-menu-item @click="emit('delete', node)">删除</a-menu-item>
                            </a-menu>
                        </template>
                    </a-dropdown>
                </div>
                <div v-if="!loading && visibleRows.length === 0" class="directory-empty">暂无匹配目录</div>
            </div>
        </a-spin>
    </aside>
</template>

<style scoped>
.directory-panel {
    display: flex;
    width: 240px;
    min-width: 240px;
    min-height: 0;
    flex-direction: column;
    overflow: hidden;
    color: var(--uvp-text-secondary);
    background: var(--uvp-panel-bg);
    border-right: 1px solid var(--uvp-panel-border);
}
.directory-head {
    display: flex;
    height: 42px;
    min-height: 42px;
    align-items: center;
    justify-content: space-between;
    padding: 0 10px 0 14px;
    border-bottom: 1px solid var(--uvp-panel-border);
}
.directory-title,
.directory-actions,
.directory-node {
    display: flex;
    align-items: center;
}
.directory-title { gap: 7px; color: var(--uvp-text-primary); font-size: 13px; font-weight: 620; }
.directory-actions { gap: 2px; }
.icon-button,
.more-button,
.twist-button {
    display: inline-grid;
    width: 26px;
    height: 26px;
    padding: 0;
    place-items: center;
    color: var(--uvp-text-tertiary);
    background: transparent;
    border: 0;
    border-radius: 5px;
    cursor: pointer;
}
.icon-button:hover,
.more-button:hover,
.twist-button:hover { color: var(--uvp-brand); background: var(--uvp-sidebar-active-bg); }
.directory-switch {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 2px;
    margin: 10px 12px 8px;
    padding: 2px;
    background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 6px;
}
.directory-switch button {
    min-width: 0;
    height: 28px;
    padding: 0 6px;
    color: var(--uvp-text-tertiary);
    font-size: 12px;
    background: transparent;
    border: 0;
    border-radius: 4px;
    cursor: pointer;
}
.directory-switch button.active { color: var(--uvp-text-primary); font-weight: 620; background: var(--uvp-panel-bg); box-shadow: 0 0 0 1px var(--uvp-panel-border); }
.directory-search {
    display: flex;
    height: 32px;
    min-height: 32px;
    align-items: center;
    gap: 7px;
    margin: 0 12px 8px;
    padding: 0 9px;
    color: var(--uvp-text-tertiary);
    background: var(--uvp-page-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 6px;
}
.directory-search:focus-within { border-color: var(--uvp-brand); }
.directory-search input { width: 100%; min-width: 0; color: var(--uvp-text-primary); font-size: 12px; outline: none; background: transparent; border: 0; }
.directory-tree-wrap { min-height: 0; flex: 1; }
.directory-tree { box-sizing: border-box; height: 100%; padding: 2px 6px 10px; overflow: auto; scrollbar-gutter: stable; }
.directory-row { display: flex; height: 32px; min-width: 0; align-items: center; border-radius: 5px; }
.directory-row:hover,
.directory-row.active { color: var(--uvp-text-primary); background: var(--uvp-sidebar-active-bg); }
.twist-button { width: 22px; height: 22px; flex: 0 0 22px; transition: transform 0.15s ease; }
.twist-button.expanded { transform: rotate(90deg); }
.twist-button.hidden { visibility: hidden; }
.directory-node { min-width: 0; height: 100%; flex: 1; gap: 7px; padding: 0 4px 0 2px; color: inherit; text-align: left; background: transparent; border: 0; cursor: pointer; }
.directory-node > svg { flex: 0 0 auto; color: var(--uvp-brand); }
.node-name { min-width: 0; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.node-count { min-width: 20px; color: var(--uvp-text-tertiary); font-size: 11px; text-align: right; }
.more-button { width: 24px; height: 24px; margin-right: 3px; flex: 0 0 24px; }
.directory-empty { display: grid; min-height: 120px; place-items: center; color: var(--uvp-text-tertiary); font-size: 12px; }
.spin { animation: directory-spin 0.9s linear infinite; }
@keyframes directory-spin { to { transform: rotate(360deg); } }
@media (max-width: 900px) {
    .directory-panel { width: 100%; min-width: 0; max-height: 320px; border-right: 0; border-bottom: 1px solid var(--uvp-panel-border); }
    .directory-tree-wrap { min-height: 160px; }
}
</style>
