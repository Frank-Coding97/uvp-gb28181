<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type { OpenAPICapability, OpenAPICapabilityGroup } from "@/api/gb28181-openapi";

type RiskFilter = "all" | "read" | "media" | "control";
type GrantFilter = "all" | "enabled" | "disabled";

const props = defineProps<{
  groups: OpenAPICapabilityGroup[];
  enabledScopes: string[];
  editable: boolean;
  saving: boolean;
}>();

const emit = defineEmits<{
  save: [scopes: string[]];
  dirtyChange: [dirty: boolean];
}>();

const query = ref("");
const activeGroup = ref("all");
const riskFilter = ref<RiskFilter>("all");
const grantFilter = ref<GrantFilter>("all");
const selectedScopes = ref<string[]>([]);

const capabilityDescriptions: Record<string, string> = {
  "device:list": "查询数据范围内可见的设备列表。",
  "device:detail": "读取指定设备的基础信息。",
  "device:status": "读取指定设备的在线和状态信息。",
  "channel:list": "查询指定设备下的数据通道。",
  "channel:detail": "读取指定通道的基础信息。",
  "channel:status": "读取指定通道的设备状态。",
  "ptz:preset:list": "读取指定通道已经配置的预置位。",
  "ptz:preset:save": "新增或更新指定通道的预置位。",
  "ptz:preset:call": "让指定通道转到已有预置位。",
  "ptz:preset:delete": "删除指定通道的预置位。",
  "ptz:operation:read": "查询云台控制操作的执行结果。"
};

const allCapabilities = computed(() =>
  props.groups.flatMap(group =>
    group.capabilities.map(capability => ({ ...capability, groupCode: group.code, groupName: group.name }))
  )
);
const baseline = computed(() => new Set(props.enabledScopes));
const selected = computed(() => new Set(selectedScopes.value));
const dirty = computed(() => {
  if (selected.value.size !== baseline.value.size) return true;
  for (const scope of selected.value) if (!baseline.value.has(scope)) return true;
  return false;
});
const addedScopes = computed(() => [...selected.value].filter(scope => !baseline.value.has(scope)).sort());
const removedScopes = computed(() => [...baseline.value].filter(scope => !selected.value.has(scope)).sort());
const changedCapabilities = computed(() =>
  allCapabilities.value.filter(
    capability => addedScopes.value.includes(capability.scope) || removedScopes.value.includes(capability.scope)
  )
);
const highRiskChanges = computed(() =>
  changedCapabilities.value.filter(capability => ["media", "control"].includes(capability.risk))
);

const groupSummaries = computed(() => [
  {
    code: "all",
    name: "全部能力",
    enabled: allCapabilities.value.filter(capability => selected.value.has(capability.scope)).length,
    total: allCapabilities.value.length
  },
  ...props.groups.map(group => ({
    code: group.code,
    name: group.name,
    enabled: group.capabilities.filter(capability => selected.value.has(capability.scope)).length,
    total: group.capabilities.length
  }))
]);

const filteredCapabilities = computed(() => {
  const keyword = query.value.trim().toLowerCase();
  return allCapabilities.value.filter(capability => {
    if (activeGroup.value !== "all" && capability.groupCode !== activeGroup.value) return false;
    if (riskFilter.value !== "all" && capability.risk !== riskFilter.value) return false;
    const enabled = selected.value.has(capability.scope);
    if (grantFilter.value === "enabled" && !enabled) return false;
    if (grantFilter.value === "disabled" && enabled) return false;
    if (!keyword) return true;
    return [capability.name, capability.scope, capability.externalPath, capability.method]
      .join(" ")
      .toLowerCase()
      .includes(keyword);
  });
});

watch(
  () => props.enabledScopes,
  scopes => {
    selectedScopes.value = [...new Set(scopes)].sort();
  },
  { immediate: true, deep: true }
);
watch(dirty, value => emit("dirtyChange", value), { immediate: true });

function isEnabled(scope: string) {
  return selected.value.has(scope);
}

function toggleScope(scope: string, enabled: boolean) {
  if (!props.editable || props.saving) return;
  const next = new Set(selectedScopes.value);
  if (enabled) next.add(scope);
  else next.delete(scope);
  selectedScopes.value = [...next].sort();
}

function toggleGroup(code: string, enabled: boolean) {
  if (!props.editable || props.saving) return;
  const scopes =
    code === "all"
      ? allCapabilities.value.map(capability => capability.scope)
      : props.groups.find(group => group.code === code)?.capabilities.map(capability => capability.scope) || [];
  const next = new Set(selectedScopes.value);
  for (const scope of scopes) {
    if (enabled) next.add(scope);
    else next.delete(scope);
  }
  selectedScopes.value = [...next].sort();
}

function groupChecked(code: string) {
  const summary = groupSummaries.value.find(group => group.code === code);
  return !!summary && summary.total > 0 && summary.enabled === summary.total;
}

function groupIndeterminate(code: string) {
  const summary = groupSummaries.value.find(group => group.code === code);
  return !!summary && summary.enabled > 0 && summary.enabled < summary.total;
}

function selectGroup(code: string) {
  activeGroup.value = code;
}

function riskLabel(risk: string) {
  return risk === "control" ? "控制" : risk === "media" ? "媒体" : "只读";
}

function riskColor(risk: string) {
  return risk === "control" ? "red" : risk === "media" ? "orange" : "green";
}

function capabilityDescription(capability: OpenAPICapability) {
  return capabilityDescriptions[capability.scope] || "允许接入方调用该项 OpenAPI 能力。";
}

function resetDraft() {
  selectedScopes.value = [...new Set(props.enabledScopes)].sort();
}

function setDraft(scopes: string[]) {
  selectedScopes.value = [...new Set(scopes)].sort();
}

function save() {
  if (!props.editable || props.saving || !dirty.value) return;
  emit("save", [...selectedScopes.value].sort());
}

defineExpose({
  selectedScopes,
  addedScopes,
  removedScopes,
  filteredCapabilities,
  dirty,
  toggleScope,
  toggleGroup,
  resetDraft,
  setDraft,
  save
});
</script>

<template>
  <section class="capability-workbench">
    <s-layout-search class="capability-workbench__toolbar">
      <template #fields>
        <a-input
          v-model="query"
          class="capability-workbench__search"
          style="width: 320px"
          allow-clear
          placeholder="搜索能力名称、scope 或接口路径"
        />
        <a-select v-model="riskFilter" class="capability-workbench__filter" style="width: 150px" aria-label="风险筛选">
          <a-option value="all">全部风险</a-option>
          <a-option value="read">只读</a-option>
          <a-option value="media">媒体</a-option>
          <a-option value="control">控制</a-option>
        </a-select>
        <a-checkbox
          :model-value="grantFilter === 'enabled'"
          @change="(value: boolean) => (grantFilter = value ? 'enabled' : 'all')"
        >
          仅看已授权
        </a-checkbox>
      </template>
    </s-layout-search>

    <div class="capability-workbench__body">
      <aside class="capability-workbench__groups" aria-label="能力分类">
        <div class="capability-workbench__groups-title">能力分类</div>
        <a-menu :selected-keys="[activeGroup]" @menu-item-click="selectGroup">
          <a-menu-item v-for="group in groupSummaries" :key="group.code">
            <span class="capability-workbench__group-name">{{ group.name }}</span>
            <span class="capability-workbench__group-count">{{ group.enabled }}/{{ group.total }}</span>
          </a-menu-item>
        </a-menu>
      </aside>

      <main class="capability-workbench__list">
        <div class="capability-workbench__list-head">
          <div>
            <h2>{{ groupSummaries.find(group => group.code === activeGroup)?.name || "全部能力" }}</h2>
            <p>能力授权决定客户端可以调用哪些外部 API，实际数据仍受归属部门和数据范围约束。</p>
          </div>
          <a-checkbox
            :model-value="groupChecked(activeGroup)"
            :indeterminate="groupIndeterminate(activeGroup)"
            :disabled="!editable || saving"
            @change="(value: boolean) => toggleGroup(activeGroup, Boolean(value))"
          >
            本分类全选
          </a-checkbox>
        </div>

        <a-table
          class="uvp-data-table capability-workbench__table"
          :data="filteredCapabilities"
          :pagination="false"
          :bordered="false"
          row-key="scope"
          :scroll="{ x: 900, y: 440 }"
        >
          <template #empty><a-empty description="没有符合当前筛选条件的能力" /></template>
          <template #columns>
            <a-table-column title="能力" :width="260">
              <template #cell="{ record }">
                <div class="capability-workbench__capability-name">{{ record.name }}</div>
                <div class="capability-workbench__description">{{ capabilityDescription(record) }}</div>
              </template>
            </a-table-column>
            <a-table-column title="Scope" :width="170">
              <template #cell="{ record }"
                ><code>{{ record.scope }}</code></template
              >
            </a-table-column>
            <a-table-column title="接口" :width="310">
              <template #cell="{ record }">
                <div class="capability-workbench__endpoint">
                  <a-tag size="small">{{ record.method }}</a-tag>
                  <code>{{ record.externalPath }}</code>
                </div>
              </template>
            </a-table-column>
            <a-table-column title="风险" :width="100" align="center">
              <template #cell="{ record }">
                <a-tag size="small" :color="riskColor(record.risk)">{{ riskLabel(record.risk) }}</a-tag>
                <div v-if="record.idempotencyRequired" class="capability-workbench__idempotency">要求幂等键</div>
              </template>
            </a-table-column>
            <a-table-column title="授权" :width="90" align="center" fixed="right">
              <template #cell="{ record }">
                <a-switch
                  :model-value="isEnabled(record.scope)"
                  :disabled="!editable || saving"
                  :aria-label="`${record.name}授权`"
                  @change="(value: boolean) => toggleScope(record.scope, Boolean(value))"
                />
              </template>
            </a-table-column>
          </template>
        </a-table>
      </main>

      <a-card class="uvp-system-panel capability-workbench__changes" :bordered="false" title="本次变更">
        <template #extra>
          <span class="capability-workbench__current">已授权 {{ selectedScopes.length }} / {{ allCapabilities.length }} 项</span>
        </template>
        <div class="capability-workbench__change-scroll">
          <div class="capability-change capability-change--added">
            <strong>新增 {{ addedScopes.length }} 项</strong>
            <span v-for="scope in addedScopes" :key="scope">+ {{ scope }}</span>
            <small v-if="!addedScopes.length">无新增能力</small>
          </div>
          <div class="capability-change capability-change--removed">
            <strong>移除 {{ removedScopes.length }} 项</strong>
            <span v-for="scope in removedScopes" :key="scope">- {{ scope }}</span>
            <small v-if="!removedScopes.length">无移除能力</small>
          </div>
          <a-alert v-if="highRiskChanges.length" type="warning">
            包含 {{ highRiskChanges.length }} 项媒体或控制能力变更，请确认接入方影响。
          </a-alert>
          <a-alert v-if="!editable" type="info">当前客户端状态或账号权限不允许修改能力。</a-alert>
        </div>
        <div class="capability-workbench__actions">
          <a-button :disabled="!dirty || saving" @click="resetDraft">撤销修改</a-button>
          <a-button type="primary" :loading="saving" :disabled="!editable || !dirty || saving" @click="save">保存授权</a-button>
        </div>
      </a-card>
    </div>
  </section>
</template>

<style scoped>
.capability-workbench {
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-width: 0;
}

.capability-workbench__body {
  display: grid;
  grid-template-columns: 200px minmax(460px, 1fr) 280px;
  gap: 12px;
  height: clamp(480px, calc(100vh - 360px), 760px);
  min-height: 0;
  overflow: hidden;
}

.capability-workbench__groups {
  min-height: 0;
  padding: 14px 8px;
  overflow-y: auto;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 14px;
}

.capability-workbench__groups-title {
  padding: 0 10px 9px;
  font-size: 12px;
  color: var(--color-text-3);
}

.capability-workbench__groups :deep(.arco-menu) {
  background: transparent;
}

.capability-workbench__groups :deep(.arco-menu-item) {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: space-between;
}

.capability-workbench__group-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

.capability-workbench__group-count {
  flex: none;
  font-size: 12px;
  color: var(--color-text-3);
}

.capability-workbench__list {
  display: flex;
  flex-direction: column;
  min-width: 0;
  overflow: hidden;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 14px;
}

.capability-workbench__list-head {
  position: sticky;
  top: 0;
  z-index: 2;
  display: flex;
  gap: 16px;
  align-items: center;
  justify-content: space-between;
  padding: 16px 18px 12px;
  background: var(--uvp-panel-bg);
  border-bottom: 1px solid var(--color-border-2);
}

.capability-workbench h2 {
  margin: 0;
  font-size: 16px;
  color: var(--color-text-1);
}

.capability-workbench__list-head p {
  margin: 5px 0 0;
  font-size: 12px;
  line-height: 1.5;
  color: var(--color-text-3);
}

.capability-workbench__table {
  flex: 1;
  min-height: 0;
}

.capability-workbench__capability-name {
  font-weight: 600;
  color: var(--color-text-1);
}

.capability-workbench__description,
.capability-workbench__idempotency {
  margin-top: 4px;
  font-size: 12px;
  color: var(--color-text-2);
}

.capability-workbench__endpoint {
  display: flex;
  gap: 8px;
  align-items: center;
  min-width: 0;
}

.capability-workbench__endpoint code {
  min-width: 0;
  font-size: 12px;
  overflow-wrap: anywhere;
}

.capability-workbench__changes {
  min-height: 0;
}

.capability-workbench__changes :deep(.arco-card-body) {
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  height: calc(100% - 55px);
  min-height: 0;
  overflow: hidden;
}

.capability-workbench__change-scroll {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 16px;
  min-height: 0;
  padding-bottom: 8px;
  overflow-y: auto;
}

.capability-workbench__current {
  font-size: 12px;
  color: var(--color-text-3);
}

.capability-change {
  display: flex;
  flex-direction: column;
  gap: 5px;
  font-size: 12px;
  color: var(--color-text-2);
}

.capability-change span {
  font-family: var(--font-mono, monospace);
  overflow-wrap: anywhere;
}

.capability-change small {
  color: var(--color-text-3);
}

.capability-change--added strong {
  color: rgb(var(--success-6));
}

.capability-change--removed strong {
  color: rgb(var(--danger-6));
}

.capability-workbench__actions {
  display: flex;
  flex: 0 0 auto;
  gap: 8px;
  justify-content: flex-end;
  padding-top: 12px;
  margin-top: 8px;
  background: var(--uvp-panel-bg);
  border-top: 1px solid var(--color-border-2);
}

@media (width <= 1180px) {
  .capability-workbench__body {
    grid-template-rows: minmax(0, 1fr) 180px;
    grid-template-columns: 170px minmax(420px, 1fr);
    height: clamp(480px, calc(100vh - 360px), 760px);
    min-height: 0;
  }

  .capability-workbench__changes {
    grid-column: 1 / -1;
  }
}

@media (width <= 760px) {
  .capability-workbench__body {
    grid-template-rows: auto auto;
    grid-template-columns: 1fr;
    height: auto;
    min-height: 0;
  }

  .capability-workbench__search {
    max-width: 100%;
  }

  .capability-workbench__list-head {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>
