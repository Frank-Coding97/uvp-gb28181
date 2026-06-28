<script setup lang="ts">
import { ref, computed, onMounted } from "vue";
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
const activeGroups = ref<string[]>([]);
const searchKey = ref("");

const dirtyCount = computed(() => Object.keys(dirty.value).length);

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

async function refresh() {
    loading.value = true;
    try {
        const res = await getZLMNodeConfig(props.nodeId);
        if (res.code === 0) {
            groups.value = res.data.groups || [];
            dirty.value = {};
            // 默认全展开
            activeGroups.value = groups.value.map((g) => g.name);
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
            <a-collapse v-model:active-key="activeGroups" :bordered="false" class="config-collapse">
                <a-collapse-item v-for="g in filteredGroups" :key="g.name" :header="''">
                    <template #header>
                        <div class="group-header">
                            <span class="group-name">{{ g.name }}</span>
                            <span class="group-count">{{ g.items.length }} 项</span>
                        </div>
                    </template>
                    <div class="config-table-wrap">
                        <a-table
                            :data="g.items"
                            :pagination="false"
                            row-key="key"
                            :show-header="true"
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
                                <a-table-column title="生效方式" :width="100">
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
                </a-collapse-item>
            </a-collapse>
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

/* === 折叠面板 === */
.config-collapse {
    background: transparent;
}

.config-collapse :deep(.arco-collapse-item) {
    background: var(--zlm-card);
    border-radius: var(--zlm-radius-lg) !important;
    border: 1px solid var(--zlm-border) !important;
    margin-bottom: var(--zlm-space-3);
    overflow: hidden;
}

.config-collapse :deep(.arco-collapse-item-header) {
    background: var(--zlm-card);
    padding: 0;
}

.group-header {
    display: flex;
    align-items: center;
    gap: var(--zlm-space-3);
    padding: var(--zlm-space-3) var(--zlm-space-4);
}

.group-name {
    font-size: var(--zlm-fs-h2);
    font-weight: var(--zlm-fw-semibold);
    color: var(--zlm-text-1);
}

.group-count {
    font-size: var(--zlm-fs-caption);
    color: var(--zlm-text-3);
}

/* === 表格 === */
.config-table-wrap {
    padding: 0;
}

.config-table :deep(.arco-table-th) {
    background: var(--zlm-bg);
    font-weight: var(--zlm-fw-medium);
    font-size: var(--zlm-fs-caption);
    color: var(--zlm-text-3);
}

.config-table :deep(.arco-table-td) {
    padding: 10px 16px !important;
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
}

.cell-input :deep(.arco-input-wrapper) {
    transition: border-color var(--zlm-dur-fast) var(--zlm-ease-out);
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
    padding: 2px 8px;
    border-radius: var(--zlm-radius-full);
    font-size: var(--zlm-fs-caption);
    font-weight: var(--zlm-fw-medium);
    border: 1px solid transparent;
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
</style>
