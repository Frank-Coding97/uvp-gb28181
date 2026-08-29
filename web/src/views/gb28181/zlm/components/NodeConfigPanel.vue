<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import {
  getZLMNodeConfig,
  testZLMNodeConnection,
  updateZLMNodeConfig,
  type ConfigGroup,
  type ConfigItem,
  type UpdateConfigResp
} from "@/api/gb28181-zlm";
import {
  buildHotReloadChanges,
  configModePresentation,
  isConfigEditable,
  updateResultRows,
  type ConfigUpdateResultRow
} from "../nodeConfigState";

const props = withDefaults(defineProps<{ nodeId: number; editable?: boolean }>(), { editable: true });
const emit = defineEmits<{
  dirtyChange: [dirty: boolean];
  pollingSkipped: [];
  saved: [result: UpdateConfigResp];
}>();

const groups = ref<ConfigGroup[]>([]);
const loading = ref(false);
const saving = ref(false);
const testing = ref(false);
const dirty = ref<Record<string, string>>({});
const searchKey = ref("");
const activeGroup = ref("");
const saveResults = ref<ConfigUpdateResultRow[]>([]);
const loadError = ref("");

const dirtyCount = computed(() => Object.keys(dirty.value).length);
const allItems = computed(() => groups.value.flatMap(group => group.items));
const filteredGroups = computed<ConfigGroup[]>(() => {
  const query = searchKey.value.trim().toLowerCase();
  if (!query) return groups.value;
  return groups.value
    .map(group => ({
      ...group,
      items: group.items.filter(item => item.key.toLowerCase().includes(query) || item.comment?.toLowerCase().includes(query))
    }))
    .filter(group => group.items.length > 0);
});
const currentGroup = computed(() => filteredGroups.value.find(group => group.name === activeGroup.value) ?? filteredGroups.value[0] ?? null);

function dirtyCountInGroup(group: ConfigGroup) {
  return group.items.filter(item => dirty.value[item.key] !== undefined).length;
}

async function refresh(options: { force?: boolean } = {}) {
  if (dirtyCount.value > 0 && !options.force) {
    emit("pollingSkipped");
    return false;
  }
  if (!Number.isSafeInteger(props.nodeId) || props.nodeId <= 0) return false;
  loading.value = true;
  loadError.value = "";
  try {
    const response = await getZLMNodeConfig(props.nodeId);
    if (response.code !== 0) throw new Error(response.message || "节点配置加载失败");
    groups.value = response.data?.groups ?? [];
    dirty.value = {};
    saveResults.value = [];
    if (!groups.value.some(group => group.name === activeGroup.value)) activeGroup.value = groups.value[0]?.name ?? "";
    return true;
  } catch (error) {
    loadError.value = error instanceof Error ? error.message : "节点配置加载失败";
    return false;
  } finally {
    loading.value = false;
  }
}

function onChange(item: ConfigItem, value: string) {
  if (!props.editable || !isConfigEditable(item)) return;
  const next = { ...dirty.value };
  if (value === item.value) delete next[item.key];
  else next[item.key] = value;
  dirty.value = next;
  saveResults.value = saveResults.value.filter(row => row.key !== item.key);
}

function resetItem(key: string) {
  const next = { ...dirty.value };
  delete next[key];
  dirty.value = next;
  saveResults.value = saveResults.value.filter(row => row.key !== key);
}

function discardDrafts() {
  dirty.value = {};
  saveResults.value = [];
  Message.info("未保存草稿已放弃");
}

function applyConfirmedValues(rows: ConfigUpdateResultRow[]) {
  const confirmed = new Map(rows.filter(row => row.tone === "success").map(row => [row.key, row.actualValue]));
  if (confirmed.size === 0) return;
  groups.value = groups.value.map(group => ({
    ...group,
    items: group.items.map(item => confirmed.has(item.key) ? { ...item, value: confirmed.get(item.key) ?? item.value } : item)
  }));
  const next = { ...dirty.value };
  for (const key of confirmed.keys()) delete next[key];
  dirty.value = next;
}

async function handleSave() {
  if (!props.editable || saving.value) return;
  const changes = buildHotReloadChanges(allItems.value, dirty.value);
  if (Object.keys(changes).length === 0) {
    Message.info("没有可提交的热更新改动");
    return;
  }
  saving.value = true;
  try {
    const response = await updateZLMNodeConfig(props.nodeId, changes);
    if (response.code !== 0) throw new Error(response.message || "配置保存失败");
    const result = response.data;
    const rows = updateResultRows(allItems.value, changes, result);
    saveResults.value = rows;
    applyConfirmedValues(rows);
    emit("saved", result);
    if (rows.some(row => row.tone === "danger")) Message.warning("配置命令已返回，但存在回读不一致或未确认项");
    else Message.success("配置已下发并完成实际值回读");
  } catch (error) {
    Message.error(error instanceof Error ? error.message : "配置保存失败");
  } finally {
    saving.value = false;
  }
}

async function handleTest() {
  if (testing.value) return;
  testing.value = true;
  try {
    const response = await testZLMNodeConnection(props.nodeId);
    if (response.code !== 0) throw new Error(response.message || "连通性探测失败");
    if (response.data.online) Message.success(`节点在线，HTTP 端口 ${response.data.httpPort || "未返回"}`);
    else Message.error(`节点不可达：${response.data.error || "未知原因"}`);
  } catch (error) {
    Message.error(error instanceof Error ? error.message : "连通性探测失败");
  } finally {
    testing.value = false;
  }
}

watch(dirtyCount, () => emit("dirtyChange", dirtyCount.value > 0), { immediate: true });
watch(filteredGroups, next => {
  if (!next.some(group => group.name === activeGroup.value)) activeGroup.value = next[0]?.name ?? "";
});
watch(() => props.nodeId, () => {
  if (dirtyCount.value > 0) {
    emit("pollingSkipped");
    return;
  }
  void refresh({ force: true });
});

onMounted(() => refresh({ force: true }));
defineExpose({ refresh, discardDrafts });
</script>

<template>
  <section class="node-config-panel">
    <s-layout-search class="config-search-panel">
      <template #fields>
        <a-input-search v-model="searchKey" placeholder="搜索配置 key 或说明" allow-clear class="config-search" />
        <span v-if="dirtyCount" class="draft-badge"><i />{{ dirtyCount }} 项未保存 · 轮询已暂停</span>
      </template>
      <template #actions>
        <a-button :loading="testing" @click="handleTest">测试连通性</a-button>
        <a-button class="uvp-refresh-btn" :loading="loading" :disabled="dirtyCount > 0" @click="refresh()"><template #icon><icon-refresh /></template>刷新</a-button>
        <a-button v-if="dirtyCount" @click="discardDrafts">放弃草稿</a-button>
        <a-button v-if="editable" type="primary" :loading="saving" :disabled="dirtyCount === 0" @click="handleSave">保存热更新</a-button>
      </template>
    </s-layout-search>

    <a-alert v-if="loadError" type="error" class="config-alert">{{ loadError }}</a-alert>
    <a-alert v-else-if="!editable" type="info" class="config-alert">当前账号可查看配置，但没有热更新权限。</a-alert>

    <div v-if="saveResults.length" class="readback-panel">
      <header><strong>最近一次保存与回读</strong><span>命令返回不等于实际生效；以下四列分别保留事实。</span></header>
      <a-table :data="saveResults" :pagination="false" size="small" row-key="key">
        <template #columns>
          <a-table-column title="配置项" data-index="key" :width="260" />
          <a-table-column title="旧值" data-index="oldValue" />
          <a-table-column title="新值" data-index="newValue" />
          <a-table-column title="命令结果" :width="150"><template #cell="{ record }"><span :class="`result-${record.tone}`">{{ record.commandResult }}</span></template></a-table-column>
          <a-table-column title="实际值" data-index="actualValue" />
        </template>
      </a-table>
    </div>

    <a-spin :loading="loading && groups.length === 0" class="config-spin">
      <div class="config-body">
        <aside class="category-tree" aria-label="配置分类">
          <div class="tree-title">配置分类</div>
          <button v-for="group in filteredGroups" :key="group.name" type="button" :class="['tree-switch', { active: group.name === currentGroup?.name }]" @click="activeGroup = group.name">
            <span>{{ group.name }}</span><span><b v-if="dirtyCountInGroup(group)">{{ dirtyCountInGroup(group) }}</b>{{ group.items.length }}</span>
          </button>
          <a-empty v-if="filteredGroups.length === 0" description="未匹配到配置" />
        </aside>

        <section class="category-detail">
          <header v-if="currentGroup" class="detail-header"><div><h2>{{ currentGroup.name }}</h2><p>{{ currentGroup.items.length }} 项 · 仅“可热更新”项允许编辑</p></div></header>
          <a-table v-if="currentGroup" :data="currentGroup.items" :pagination="false" row-key="key" class="uvp-data-table config-table">
            <template #columns>
              <a-table-column title="配置项" :width="310"><template #cell="{ record }"><div class="config-key"><strong>{{ record.key }}</strong><span>{{ record.comment || "暂无说明" }}</span></div></template></a-table-column>
              <a-table-column title="当前 / 草稿值"><template #cell="{ record }">
                <template v-if="isConfigEditable(record)">
                  <div class="config-input"><a-input :model-value="dirty[record.key] ?? record.value" :disabled="!editable" :class="{ dirty: dirty[record.key] !== undefined }" @input="(value: string) => onChange(record, value)" /><a-button v-if="dirty[record.key] !== undefined" size="mini" type="text" @click="resetItem(record.key)">还原</a-button></div>
                </template>
                <code v-else class="readonly-value">{{ record.value || "—" }}</code>
              </template></a-table-column>
              <a-table-column title="默认值" :width="140"><template #cell="{ record }"><code>{{ record.default || "—" }}</code></template></a-table-column>
              <a-table-column title="编辑模式" :width="230"><template #cell="{ record }"><div class="mode-cell"><span :class="`mode-${configModePresentation(record.mode).tone}`">{{ configModePresentation(record.mode).label }}</span><small>{{ configModePresentation(record.mode).reason }}</small></div></template></a-table-column>
            </template>
          </a-table>
        </section>
      </div>
    </a-spin>
  </section>
</template>

<style scoped>
.node-config-panel { display: flex; min-width: 0; flex-direction: column; gap: 12px; color: var(--zlm-text-2); }.config-search { width: 300px; }.draft-badge { display: inline-flex; align-items: center; gap: 6px; color: var(--zlm-warn-600); font-size: var(--zlm-fs-caption); }.draft-badge i { width: 7px; height: 7px; background: var(--zlm-warn-500); border-radius: 50%; }.config-alert { flex: none; }.readback-panel { overflow: hidden; background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); }.readback-panel header { display: flex; align-items: baseline; gap: 10px; padding: 12px 14px; border-bottom: 1px solid var(--zlm-border); }.readback-panel header strong { color: var(--zlm-text-1); }.readback-panel header span { color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.result-success { color: var(--zlm-success-600); }.result-warning { color: var(--zlm-warn-600); }.result-danger { color: var(--zlm-danger-600); }.config-spin { min-height: 320px; }.config-body { display: grid; grid-template-columns: 220px minmax(0, 1fr); min-height: 420px; overflow: hidden; background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); }.category-tree { display: flex; flex-direction: column; gap: 5px; padding: 14px 10px; background: var(--zlm-fill-1); border-right: 1px solid var(--zlm-border); }.tree-title { padding: 0 8px 8px; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); font-weight: var(--zlm-fw-medium); }.tree-switch { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 9px 10px; color: var(--zlm-text-2); text-align: left; background: transparent; border: 0; border-radius: var(--zlm-radius-md); cursor: pointer; }.tree-switch:hover { background: var(--zlm-fill-2); }.tree-switch.active { color: var(--zlm-brand-600); background: var(--zlm-brand-50); }.tree-switch > span:last-child { color: var(--zlm-text-4); font-size: var(--zlm-fs-caption); }.tree-switch b { margin-right: 7px; color: var(--zlm-warn-600); }.category-detail { min-width: 0; overflow: auto; }.detail-header { padding: 15px 18px; border-bottom: 1px solid var(--zlm-border); }.detail-header h2 { margin: 0; color: var(--zlm-text-1); font-size: 16px; }.detail-header p { margin: 4px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.config-table { min-width: 900px; }.config-key { display: flex; min-width: 0; flex-direction: column; }.config-key strong, code { font-family: var(--zlm-font-mono); }.config-key strong { overflow-wrap: anywhere; color: var(--zlm-text-1); }.config-key span { margin-top: 3px; color: var(--zlm-text-3); font-size: 11px; }.config-input { display: flex; align-items: center; gap: 5px; }.config-input :deep(.dirty) { border-color: var(--zlm-warn-500); }.readonly-value { color: var(--zlm-text-3); overflow-wrap: anywhere; }.mode-cell { display: flex; flex-direction: column; gap: 4px; }.mode-cell > span { width: fit-content; padding: 2px 7px; border-radius: 999px; font-size: 11px; }.mode-cell small { color: var(--zlm-text-3); line-height: 1.45; }.mode-success { color: var(--zlm-success-600); background: var(--zlm-success-50); }.mode-warning { color: var(--zlm-warn-600); background: var(--zlm-warn-50); }.mode-danger { color: var(--zlm-danger-600); background: var(--zlm-danger-50); }.mode-neutral { color: var(--zlm-text-3); background: var(--zlm-fill-2); }
@media (max-width: 820px) { .config-search { width: 100%; }.readback-panel { overflow-x: auto; }.config-body { grid-template-columns: 1fr; }.category-tree { flex-direction: row; overflow-x: auto; border-right: 0; border-bottom: 1px solid var(--zlm-border); }.tree-title { display: none; }.tree-switch { flex: 0 0 auto; }.category-detail { min-height: 420px; } }
</style>
