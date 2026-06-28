<script setup lang="ts">
import { ref, computed, onMounted, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import {
    getZLMNodeConfig,
    updateZLMNodeConfig,
    testZLMNodeConnection,
    type ConfigGroup,
    type UpdateConfigResp
} from "@/api/gb28181-zlm";

const props = defineProps<{ nodeId: number }>();

const groups = ref<ConfigGroup[]>([]);
const loading = ref(false);
const saving = ref(false);
const testing = ref(false);
const dirty = ref<Record<string, string>>({});
const searchKey = ref("");
const activeGroup = ref<string>("");

const dirtyCount = computed(() => Object.keys(dirty.value).length);

// 当前分组每项 dirty 计数(给左侧菜单显示徽章)
function dirtyCountInGroup(group: ConfigGroup): number {
    let n = 0;
    for (const item of group.items) {
        if (dirty.value[item.key] !== undefined) n++;
    }
    return n;
}

const filteredGroups = computed<ConfigGroup[]>(() => {
    const q = searchKey.value.trim().toLowerCase();
    if (!q) return groups.value;
    return groups.value
        .map((g) => ({
            ...g,
            items: g.items.filter(
                (it) =>
                    it.key.toLowerCase().includes(q) ||
                    (it.comment || "").toLowerCase().includes(q)
            )
        }))
        .filter((g) => g.items.length > 0);
});

// 当前选中分组的内容
const currentGroup = computed<ConfigGroup | null>(() => {
    if (!activeGroup.value) return null;
    return filteredGroups.value.find((g) => g.name === activeGroup.value) || null;
});

async function refresh() {
    loading.value = true;
    try {
        const res = await getZLMNodeConfig(props.nodeId);
        if (res.code === 0) {
            groups.value = res.data.groups || [];
            dirty.value = {};
            // 默认选中第一个分组
            if (groups.value.length && !activeGroup.value) {
                activeGroup.value = groups.value[0].name;
            }
        }
    } catch (e: any) {
        Message.error(e?.message || "加载失败");
    } finally {
        loading.value = false;
    }
}

function onChange(key: string, value: string) {
    dirty.value[key] = value;
}

function resetItem(key: string) {
    delete dirty.value[key];
    dirty.value = { ...dirty.value };
}

async function handleSave() {
    if (dirtyCount.value === 0) {
        Message.info("没有改动");
        return;
    }
    saving.value = true;
    try {
        const res = await updateZLMNodeConfig(props.nodeId, dirty.value);
        if (res.code === 0) {
            const d: UpdateConfigResp = res.data;
            const msgs: string[] = [];
            if (d.applied.length) msgs.push(`已生效: ${d.applied.join(", ")}`);
            if (d.requiresRestart.length)
                msgs.push(`需重启 ZLM 才生效: ${d.requiresRestart.join(", ")}`);
            if (d.unknown.length) msgs.push(`未知 key (已下发): ${d.unknown.join(", ")}`);
            Message.success(msgs.join("\n") || "已保存");
            refresh();
        }
    } catch (e: any) {
        Message.error(e?.message || "保存失败");
    } finally {
        saving.value = false;
    }
}

async function handleTest() {
    testing.value = true;
    try {
        const res = await testZLMNodeConnection(props.nodeId);
        if (res.code === 0) {
            if (res.data.online) {
                Message.success(`节点在线,http.port=${res.data.httpPort || "?"}`);
            } else {
                Message.error(`节点不可达: ${res.data.error || "未知"}`);
            }
        }
    } catch (e: any) {
        Message.error(e?.message || "探测失败");
    } finally {
        testing.value = false;
    }
}

// 搜索时若当前分组被过滤掉了,自动切到第一个有匹配的分组
watch(filteredGroups, (groups) => {
    if (!searchKey.value.trim()) return;
    if (!groups.find((g) => g.name === activeGroup.value)) {
        activeGroup.value = groups[0]?.name || "";
    }
});

onMounted(refresh);
</script>

<template>
    <div class="node-config">
        <!-- toolbar -->
        <div class="config-toolbar">
            <a-input-search
                v-model="searchKey"
                placeholder="搜索配置 key 或说明"
                allow-clear
                class="search"
            />
            <div class="toolbar-right">
                <span v-if="dirtyCount > 0" class="dirty-pill">
                    <span class="dot" />
                    <span>{{ dirtyCount }} 项待保存</span>
                </span>
                <a-button :loading="testing" @click="handleTest">测试连通性</a-button>
                <a-button @click="refresh" :loading="loading">刷新</a-button>
                <a-button type="primary" :loading="saving" :disabled="dirtyCount === 0" @click="handleSave">
                    保存改动
                </a-button>
            </div>
        </div>

        <a-spin :loading="loading">
            <div class="config-body">
                <!-- 左侧分类树 -->
                <aside class="category-tree">
                    <div class="tree-title">配置分类</div>
                    <ul class="tree-list">
                        <li
                            v-for="g in filteredGroups"
                            :key="g.name"
                            class="tree-item"
                            :class="{ active: g.name === activeGroup }"
                            @click="activeGroup = g.name"
                        >
                            <span class="tree-name">{{ g.name }}</span>
                            <span class="tree-meta">
                                <span
                                    v-if="dirtyCountInGroup(g) > 0"
                                    class="tree-dirty-badge"
                                >
                                    {{ dirtyCountInGroup(g) }}
                                </span>
                                <span class="tree-count">{{ g.items.length }}</span>
                            </span>
                        </li>
                        <li v-if="filteredGroups.length === 0" class="tree-empty">
                            未匹配到分类
                        </li>
                    </ul>
                </aside>

                <!-- 右侧详情 -->
                <section class="category-detail">
                    <div v-if="currentGroup" class="detail-card">
                        <header class="detail-header">
                            <div>
                                <h2 class="detail-title">{{ currentGroup.name }}</h2>
                                <p class="detail-subtitle">
                                    {{ currentGroup.items.length }} 项配置 ·
                                    <span v-if="dirtyCountInGroup(currentGroup) > 0" class="dim">
                                        本组 {{ dirtyCountInGroup(currentGroup) }} 项待保存
                                    </span>
                                    <span v-else class="dim">无未保存改动</span>
                                </p>
                            </div>
                        </header>
                        <div class="detail-table-wrap">
                            <a-table
                                :data="currentGroup.items"
                                :pagination="false"
                                row-key="key"
                                class="config-table"
                            >
                                <template #columns>
                                    <a-table-column title="配置项" :width="320">
                                        <template #cell="{ record }">
                                            <div class="cell-key">
                                                <div class="key-name mono">{{ record.key }}</div>
                                                <div class="key-comment">{{ record.comment }}</div>
                                            </div>
                                        </template>
                                    </a-table-column>
                                    <a-table-column title="当前值">
                                        <template #cell="{ record }">
                                            <div class="cell-input">
                                                <a-input
                                                    :model-value="dirty[record.key] ?? record.value"
                                                    size="small"
                                                    :class="{ 'input-dirty': dirty[record.key] !== undefined }"
                                                    @input="(v: string) => onChange(record.key, v)"
                                                />
                                                <a-button
                                                    v-if="dirty[record.key] !== undefined"
                                                    size="mini"
                                                    type="text"
                                                    @click="resetItem(record.key)"
                                                >
                                                    还原
                                                </a-button>
                                            </div>
                                        </template>
                                    </a-table-column>
                                    <a-table-column title="默认" :width="140">
                                        <template #cell="{ record }">
                                            <span class="default-val mono">{{ record.default || "—" }}</span>
                                        </template>
                                    </a-table-column>
                                    <a-table-column title="生效方式" :width="120" align="center">
                                        <template #cell="{ record }">
                                            <span
                                                v-if="record.hotReloadable"
                                                class="hot-tag hot-tag-hot"
                                            >
                                                热改
                                            </span>
                                            <span v-else class="hot-tag hot-tag-restart">需重启</span>
                                        </template>
                                    </a-table-column>
                                </template>
                            </a-table>
                        </div>
                    </div>
                    <div v-else-if="!loading" class="detail-empty">
                        <icon-search class="empty-icon" />
                        <div class="empty-text">请在左侧选择分类</div>
                    </div>
                </section>
            </div>
        </a-spin>
    </div>
</template>

<style scoped>
.node-config {
    font-family: var(--zlm-font-body);
}

.config-toolbar {
    display: flex;
    align-items: center;
    gap: var(--zlm-space-3);
    margin-bottom: var(--zlm-space-4);
    padding: var(--zlm-space-3) var(--zlm-space-4);
    background: var(--zlm-card);
    border-radius: var(--zlm-radius-lg);
    border: 1px solid var(--zlm-border);
}

.search {
    width: 320px;
    flex-shrink: 0;
}

.toolbar-right {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: var(--zlm-space-2);
}

.dirty-pill {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 4px 10px;
    background: var(--zlm-warn-50);
    color: var(--zlm-warn-600);
    border: 1px solid var(--zlm-warn-500);
    border-radius: var(--zlm-radius-full);
    font-size: var(--zlm-fs-caption);
    font-weight: var(--zlm-fw-medium);
    margin-right: var(--zlm-space-2);
}

.dirty-pill .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--zlm-warn-500);
    animation: dirty-pulse 1.4s ease-in-out infinite;
}

@keyframes dirty-pulse {
    0%,
    100% {
        opacity: 1;
    }
    50% {
        opacity: 0.4;
    }
}

/* === 主体两栏 === */
.config-body {
    display: grid;
    grid-template-columns: 220px 1fr;
    gap: var(--zlm-space-4);
    align-items: flex-start;
}

/* === 左侧分类树 === */
.category-tree {
    background: var(--zlm-card);
    border-radius: var(--zlm-radius-lg);
    border: 1px solid var(--zlm-border);
    padding: var(--zlm-space-3) 0;
    position: sticky;
    top: var(--zlm-space-4);
}

.tree-title {
    padding: 0 var(--zlm-space-4) var(--zlm-space-3);
    font-size: var(--zlm-fs-caption);
    color: var(--zlm-text-3);
    font-weight: var(--zlm-fw-medium);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    border-bottom: 1px solid var(--zlm-divider);
    margin-bottom: var(--zlm-space-2);
}

.tree-list {
    list-style: none;
    margin: 0;
    padding: 0;
}

.tree-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--zlm-space-2);
    padding: 10px var(--zlm-space-4);
    cursor: pointer;
    font-size: var(--zlm-fs-body);
    color: var(--zlm-text-2);
    transition: all var(--zlm-dur-fast) var(--zlm-ease-out);
    border-left: 3px solid transparent;
    position: relative;
}

.tree-item:hover {
    background: var(--zlm-bg);
    color: var(--zlm-text-1);
}

.tree-item.active {
    background: var(--zlm-brand-50);
    color: var(--zlm-brand-600);
    font-weight: var(--zlm-fw-medium);
    border-left-color: var(--zlm-brand-500);
}

.tree-name {
    flex: 1;
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}

.tree-meta {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
}

.tree-count {
    font-size: var(--zlm-fs-caption);
    color: var(--zlm-text-4);
    font-variant-numeric: tabular-nums;
    min-width: 18px;
    text-align: right;
}

.tree-item.active .tree-count {
    color: var(--zlm-brand-500);
}

.tree-dirty-badge {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 18px;
    height: 18px;
    padding: 0 5px;
    background: var(--zlm-warn-500);
    color: #fff;
    border-radius: var(--zlm-radius-full);
    font-size: 11px;
    font-weight: var(--zlm-fw-semibold);
    line-height: 1;
}

.tree-empty {
    padding: var(--zlm-space-4);
    color: var(--zlm-text-4);
    font-size: var(--zlm-fs-caption);
    text-align: center;
}

/* === 右侧详情 === */
.category-detail {
    min-width: 0;
    width: 100%;
}

.detail-card {
    background: var(--zlm-card);
    border-radius: var(--zlm-radius-lg);
    border: 1px solid var(--zlm-border);
    overflow: hidden;
    width: 100%;
}

.detail-header {
    padding: var(--zlm-space-4) var(--zlm-space-6);
    border-bottom: 1px solid var(--zlm-divider);
    background: linear-gradient(to right, var(--zlm-brand-50) 0%, var(--zlm-card) 40%);
    position: relative;
}

.detail-header::before {
    content: '';
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
    width: 3px;
    background: var(--zlm-brand-500);
}

.detail-title {
    font-size: var(--zlm-fs-h2);
    font-weight: var(--zlm-fw-semibold);
    color: var(--zlm-text-1);
    margin: 0;
    letter-spacing: -0.01em;
}

.detail-subtitle {
    font-size: var(--zlm-fs-caption);
    color: var(--zlm-text-3);
    margin: 4px 0 0;
}

.detail-subtitle .dim {
    color: var(--zlm-text-3);
}

.detail-table-wrap {
    padding: 0;
}

.config-table {
    width: 100%;
}

.config-table :deep(.arco-table) {
    width: 100% !important;
}

.config-table :deep(.arco-table-th) {
    background: var(--zlm-bg);
    font-weight: var(--zlm-fw-medium);
    font-size: var(--zlm-fs-caption);
    color: var(--zlm-text-3);
}

.config-table :deep(.arco-table-td) {
    padding: 12px 16px !important;
    vertical-align: top;
}

.cell-key {
    line-height: 1.4;
}

.key-name {
    font-size: var(--zlm-fs-body);
    color: var(--zlm-text-1);
    font-weight: var(--zlm-fw-medium);
}

.key-comment {
    font-size: var(--zlm-fs-caption);
    color: var(--zlm-text-3);
    margin-top: 2px;
}

.cell-input {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
}

.cell-input :deep(.arco-input-wrapper) {
    transition: border-color var(--zlm-dur-fast) var(--zlm-ease-out);
    flex: 1;
    min-width: 0;
}

.cell-input :deep(.arco-input) {
    width: 100%;
}

.input-dirty :deep(.arco-input-wrapper) {
    border-color: var(--zlm-warn-500) !important;
    background: var(--zlm-warn-50);
}

.default-val {
    font-size: var(--zlm-fs-caption);
    color: var(--zlm-text-3);
}

.mono {
    font-family: var(--zlm-font-mono);
}

.hot-tag {
    display: inline-flex;
    align-items: center;
    padding: 2px 10px;
    border-radius: var(--zlm-radius-full);
    font-size: var(--zlm-fs-caption);
    font-weight: var(--zlm-fw-medium);
    border: 1px solid transparent;
    white-space: nowrap;
    line-height: 1.4;
}

.hot-tag-hot {
    background: var(--zlm-success-50);
    color: var(--zlm-success-600);
    border-color: var(--zlm-success-500);
}

.hot-tag-restart {
    background: var(--zlm-warn-50);
    color: var(--zlm-warn-600);
    border-color: var(--zlm-warn-500);
}

.detail-empty {
    padding: var(--zlm-space-12);
    text-align: center;
    color: var(--zlm-text-3);
    background: var(--zlm-card);
    border-radius: var(--zlm-radius-lg);
    border: 1px solid var(--zlm-border);
}

.empty-icon {
    font-size: 32px;
    color: var(--zlm-text-4);
    margin-bottom: var(--zlm-space-2);
}

.empty-text {
    font-size: var(--zlm-fs-body);
}
</style>
