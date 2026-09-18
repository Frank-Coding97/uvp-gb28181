<template>
  <a-modal
    :visible="visible"
    :width="'min(1080px, 96vw)'"
    modal-class="uvp-system-dialog channel-assignment-dialog"
    align-center
    :modal-style="{
      height: 'min(760px, calc(100vh - 48px))',
      maxHeight: 'calc(100vh - 48px)',
      display: 'flex',
      flexDirection: 'column'
    }"
    :body-style="{ flex: '1 1 auto', minHeight: 0, maxHeight: 'none', overflowY: 'auto' }"
    :mask-closable="false"
    :esc-to-close="true"
    unmount-on-close
    @update:visible="emit('update:visible', $event)"
    @cancel="close"
  >
    <template #title>
      <div class="assignment-title">
        <span><Link2 :size="18" /></span>
        <div><strong>分配录像计划</strong><small>将计划批量应用到设备通道</small></div>
      </div>
    </template>

    <div class="assignment-content">
      <section v-if="selectedPlan" class="assignment-plan-section">
        <div class="field-label">当前录像计划</div>
        <div class="selected-plan-summary">
          <div><span>计划名称</span><strong>{{ selectedPlan.name }}</strong></div>
          <div><span>执行周期</span><strong>{{ selectedPlan.cycle }}</strong></div>
          <div><span>录像时段</span><strong>{{ selectedPlan.timeSummary }}</strong></div>
          <div><span>当前状态</span><a-tag :color="selectedPlan.enabled ? 'green' : 'gray'">{{ selectedPlan.enabled ? "已启用" : "已停用" }}</a-tag></div>
        </div>
      </section>
      <a-alert v-else type="error">当前录像计划不存在或已被删除，请关闭后刷新台账。</a-alert>
      <a-alert v-if="selectedPlan && !selectedPlan.enabled" type="warning" class="assignment-plan-warning">当前计划已停用，不能新增分配，请先启用计划。</a-alert>

      <section class="assignment-channel-section">
        <div class="assignment-section-heading">
          <div><h3>选择应用对象</h3><span>{{ selectionSummary }}</span></div>
        </div>

        <div class="assignment-scope-bar">
          <div class="segmented assignment-scope-switch" role="tablist" aria-label="分配范围">
            <button type="button" :class="{ active: selectionScope === 'device' }" :aria-pressed="selectionScope === 'device'" @click="switchScope('device')">按设备</button>
            <button type="button" :class="{ active: selectionScope === 'channel' }" :aria-pressed="selectionScope === 'channel'" @click="switchScope('channel')">按通道</button>
          </div>
          <span>{{ selectionScope === "device" ? "选中设备后，将应用到该设备下全部有权限通道" : "按通道精确选择，不影响同设备的其他通道" }}</span>
        </div>

        <div class="assignment-filters">
          <a-input v-model="keyword" allow-clear :placeholder="selectionScope === 'device' ? '输入设备名称或国标编码' : '输入通道名称、编码或所属设备'" @press-enter="applySearch">
            <template #prefix><Search :size="15" /></template>
          </a-input>
          <a-select v-model="onlineFilter" :options="onlineFilterOptions" />
          <a-button type="primary" @click="applySearch"><template #icon><Search :size="14" /></template>查询</a-button>
          <a-button @click="resetSearch">重置</a-button>
        </div>

        <div class="selection-policy">
          <span>全选仅作用于当前页，已选结果跨页保留。</span>
          <a-link v-if="selectedTargetCount" @click="clearSelection">清空已选</a-link>
        </div>

        <a-table
          v-if="selectionScope === 'device'"
          v-model:selected-keys="selectedDeviceKeys"
          class="uvp-data-table assignment-channel-table"
          row-key="id"
		  :data="assignmentOptions"
		  :loading="loading"
          :pagination="assignmentPagination"
          :bordered="false"
          :row-selection="{ type: 'checkbox', showCheckedAll: true }"
          :scroll="{ x: 860, y: 300 }"
          @page-change="handlePageChange"
          @page-size-change="handlePageSizeChange"
        >
          <template #columns>
            <a-table-column title="设备" :width="240">
              <template #cell="{ record }"><div class="entity-cell"><span>{{ record.name }}</span><small>{{ record.code }}</small></div></template>
            </a-table-column>
            <a-table-column title="在线状态" :width="100" align="center">
              <template #cell="{ record }"><a-badge :status="record.online ? 'success' : 'normal'" :text="record.online ? '在线' : '离线'" /></template>
            </a-table-column>
			<a-table-column title="分配说明"><template #cell>确认后应用到该设备下当前有权限的全部通道</template></a-table-column>
          </template>
        </a-table>

        <a-table
          v-else
          v-model:selected-keys="selectedChannelKeys"
          class="uvp-data-table assignment-channel-table"
          row-key="id"
		  :data="assignmentOptions"
		  :loading="loading"
          :pagination="assignmentPagination"
          :bordered="false"
          :row-selection="{ type: 'checkbox', showCheckedAll: true }"
          :scroll="{ x: 900, y: 300 }"
          @page-change="handlePageChange"
          @page-size-change="handlePageSizeChange"
        >
          <template #columns>
            <a-table-column title="通道" :width="210"><template #cell="{ record }"><div class="entity-cell"><span>{{ record.name }}</span><small>{{ record.code }}</small></div></template></a-table-column>
			<a-table-column title="所属设备" :width="200"><template #cell="{ record }"><div class="entity-cell"><span>{{ record.deviceCode || "—" }}</span></div></template></a-table-column>
            <a-table-column title="在线状态" :width="100" align="center"><template #cell="{ record }"><a-badge :status="record.online ? 'success' : 'normal'" :text="record.online ? '在线' : '离线'" /></template></a-table-column>
			<a-table-column title="分配状态"><template #cell="{ record }"><a-tag :color="record.bound ? 'orange' : 'gray'">{{ record.bound ? "已有录像计划" : "未分配" }}</a-tag></template></a-table-column>
          </template>
        </a-table>
      </section>

      <a-alert type="warning" class="assignment-impact">
        选择后，通道录像模式将切换为“按计划”；若当前正在持续录像，将按新计划立即重新计算执行状态。
      </a-alert>
    </div>

    <template #footer>
      <div class="assignment-footer">
        <span>{{ footerSummary }}</span>
        <a-space>
          <a-button @click="close">取消</a-button>
		  <a-button type="primary" :loading="submitting" :disabled="!selectedPlan?.enabled || !selectedTargetCount" @click="confirm">
            <template #icon><Check :size="15" /></template>
            确认分配
          </a-button>
        </a-space>
      </div>
    </template>
  </a-modal>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { Check, Link2, Search } from "@lucide/vue";
import { assignRecordingPlan, listRecordingPlanChannels, listRecordingPlanDevices, type AssignmentOption } from "@/api/gb28181-recording-plan";
import type { RecordingSchedule } from "../types";

type AssignmentScope = "device" | "channel";
type OnlineFilter = "all" | "online";

const props = defineProps<{ visible: boolean; plans: RecordingSchedule[]; planId?: string }>();
const emit = defineEmits<{
  (event: "update:visible", value: boolean): void;
  (event: "confirm", payload: { planId: string; scope: AssignmentScope; targetIds: number[]; channelCount: number }): void;
}>();

const selectionScope = ref<AssignmentScope>("device");
const selectedDeviceKeys = ref<number[]>([]);
const selectedChannelKeys = ref<number[]>([]);
const keyword = ref("");
const appliedKeyword = ref("");
const onlineFilter = ref<OnlineFilter>("all");
const paginationState = reactive({ current: 1, pageSize: 10 });
const selectedPlan = computed(() => props.plans.find(item => item.id === props.planId));

const assignmentOptions = ref<AssignmentOption[]>([]);
const total = ref(0);
const loading = ref(false);
const submitting = ref(false);
const onlineFilterOptions = computed(() => selectionScope.value === "device"
  ? [{ label: "全部设备", value: "all" }, { label: "仅显示在线设备", value: "online" }]
  : [{ label: "全部通道", value: "all" }, { label: "仅显示在线通道", value: "online" }]);
const selectedTargetCount = computed(() => selectionScope.value === "device" ? selectedDeviceKeys.value.length : selectedChannelKeys.value.length);
const selectionSummary = computed(() => selectionScope.value === "device"
  ? `已选择 ${selectedDeviceKeys.value.length} 台设备，确认后按服务端当前通道清单展开`
  : `已选择 ${selectedChannelKeys.value.length} 个通道`);
const footerSummary = computed(() => {
  if (!selectedPlan.value) return "未找到当前录像计划";
  if (selectionScope.value === "device") return `将为 ${selectedDeviceKeys.value.length} 台设备下当前有权限的通道分配“${selectedPlan.value.name}”`;
  return `将为 ${selectedChannelKeys.value.length} 个通道分配“${selectedPlan.value.name}”`;
});
const assignmentPagination = computed(() => ({
  current: paginationState.current,
  pageSize: paginationState.pageSize,
  total: total.value,
  showTotal: true,
  showJumper: true,
  showPageSize: true,
  pageSizeOptions: [10, 20, 50, 100]
}));

function close() {
  emit("update:visible", false);
}

async function confirm() {
  if (!selectedPlan.value || !selectedTargetCount.value) return;
  const targetIds = selectionScope.value === "device" ? selectedDeviceKeys.value : selectedChannelKeys.value;
  submitting.value = true;
  try {
    const response = await assignRecordingPlan(Number(selectedPlan.value.id), { type: selectionScope.value, ids: [...targetIds] });
    const conflicts = response.data.items.filter(item => item.status !== "assigned").length;
    if (conflicts) Message.warning(`成功分配 ${response.data.assignedCount} 个通道，${conflicts} 项未分配`);
    emit("confirm", { planId: selectedPlan.value.id, scope: selectionScope.value, targetIds: [...targetIds], channelCount: response.data.assignedCount });
  } finally {
    submitting.value = false;
  }
}

function switchScope(scope: AssignmentScope) {
  selectionScope.value = scope;
  keyword.value = "";
  appliedKeyword.value = "";
  onlineFilter.value = "all";
  paginationState.current = 1;
  void loadOptions();
}

function applySearch() {
  appliedKeyword.value = keyword.value;
  paginationState.current = 1;
  void loadOptions();
}

function resetSearch() {
  keyword.value = "";
  appliedKeyword.value = "";
  onlineFilter.value = "all";
  paginationState.current = 1;
  void loadOptions();
}

function clearSelection() {
  if (selectionScope.value === "device") selectedDeviceKeys.value = [];
  else selectedChannelKeys.value = [];
}

function handlePageChange(current: number) {
  paginationState.current = current;
  void loadOptions();
}

function handlePageSizeChange(pageSize: number) {
  paginationState.pageSize = pageSize;
  paginationState.current = 1;
  void loadOptions();
}

async function loadOptions() {
  if (!selectedPlan.value) return;
  loading.value = true;
  try {
    const params = { keyword: appliedKeyword.value, online: onlineFilter.value, page: paginationState.current, pageSize: paginationState.pageSize };
    const response = selectionScope.value === "device"
      ? await listRecordingPlanDevices(Number(selectedPlan.value.id), params)
      : await listRecordingPlanChannels(Number(selectedPlan.value.id), params);
    assignmentOptions.value = response.data.list;
    total.value = response.data.total;
  } finally {
    loading.value = false;
  }
}

watch(
  () => props.visible,
  visible => {
    if (!visible) return;
    selectionScope.value = "device";
    selectedDeviceKeys.value = [];
    selectedChannelKeys.value = [];
    keyword.value = "";
    appliedKeyword.value = "";
    onlineFilter.value = "all";
    paginationState.current = 1;
    void loadOptions();
  },
  { immediate: true }
);
watch(onlineFilter, () => { paginationState.current = 1; void loadOptions(); });
</script>

<style scoped lang="scss">
.assignment-title { display: flex; align-items: center; gap: 10px; min-width: 0; }
.assignment-title > span { display: grid; width: 34px; height: 34px; flex: 0 0 auto; color: #16845f; background: rgb(22 132 95 / 10%); border-radius: 8px; place-items: center; }
.assignment-title > div { display: flex; min-width: 0; flex-direction: column; }
.assignment-title strong { font-size: 15px; }
.assignment-title small { color: var(--uvp-text-tertiary); font-size: 12px; }
.assignment-content { color: var(--uvp-text-primary); }
.assignment-plan-section { padding: 0 0 18px; border-bottom: 1px solid var(--uvp-border-subtle); }
.field-label { margin-bottom: 8px; color: var(--uvp-text-secondary); font-size: 13px; font-weight: 620; }
.selected-plan-summary { display: grid; grid-template-columns: 1.2fr 1fr 1.5fr 100px; padding: 12px 14px; background: var(--uvp-search-panel-bg); border: 1px solid var(--uvp-list-panel-border); border-radius: 10px; }
.selected-plan-summary > div { display: flex; min-width: 0; flex-direction: column; gap: 4px; padding: 0 14px; border-right: 1px solid var(--uvp-border-subtle); }
.selected-plan-summary > div:first-child { padding-left: 0; }
.selected-plan-summary > div:last-child { border-right: 0; }
.selected-plan-summary span { color: var(--uvp-text-tertiary); font-size: 11px; }
.selected-plan-summary strong { overflow: hidden; font-size: 13px; text-overflow: ellipsis; white-space: nowrap; }
.assignment-plan-warning { margin-top: 10px; }
.assignment-channel-section { padding: 18px 0 14px; }
.assignment-section-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 12px; }
.assignment-section-heading h3 { margin: 0; font-size: 14px; }
.assignment-section-heading span { display: block; margin-top: 3px; color: var(--uvp-text-tertiary); font-size: 12px; }
.assignment-scope-bar { display: flex; align-items: center; justify-content: space-between; gap: 14px; margin-bottom: 12px; padding: 8px 10px; background: var(--uvp-table-header-bg); border: 1px solid var(--uvp-list-panel-border); border-radius: 8px; }
.assignment-scope-switch { flex: 0 0 auto; }
.assignment-scope-switch button { min-width: 82px; }
.assignment-scope-bar > span { color: var(--uvp-text-tertiary); font-size: 12px; text-align: right; }
.assignment-filters { display: grid; grid-template-columns: minmax(0, 1fr) 180px auto auto; gap: 8px; margin-bottom: 8px; }
.assignment-filters :deep(.arco-input-wrapper),
.assignment-filters :deep(.arco-select-view) { min-height: 38px; border-radius: 8px; }
.assignment-filters :deep(.arco-btn) { min-height: 38px; border-radius: 8px; }
.selection-policy { display: flex; min-height: 28px; align-items: flex-start; justify-content: space-between; gap: 12px; color: var(--uvp-text-tertiary); font-size: 11px; }
.assignment-channel-table { min-height: 340px; }
.entity-cell { display: flex; min-width: 0; flex-direction: column; }
.entity-cell span,
.entity-cell small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.entity-cell small { margin-top: 2px; color: var(--uvp-text-tertiary); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 10px; }
.assignment-impact { margin-top: 4px; }
.assignment-footer { display: flex; width: 100%; align-items: center; justify-content: space-between; gap: 12px; }
.assignment-footer > span { overflow: hidden; color: var(--uvp-text-tertiary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
@media (max-width: 640px) {
  .selected-plan-summary { grid-template-columns: 1fr; gap: 10px; }
  .selected-plan-summary > div { padding: 0; border-right: 0; }
  .assignment-scope-bar { align-items: flex-start; flex-direction: column; }
  .assignment-scope-bar > span { text-align: left; }
  .assignment-filters { grid-template-columns: 1fr; }
  .assignment-footer > span { display: none; }
  .assignment-footer { justify-content: flex-end; }
}
</style>
