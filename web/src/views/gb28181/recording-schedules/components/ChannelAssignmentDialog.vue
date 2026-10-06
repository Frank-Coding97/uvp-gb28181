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
    :body-style="{ flex: '1 1 auto', minHeight: 0, maxHeight: 'none', overflow: 'hidden' }"
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
          <div>
            <span>计划名称</span><strong>{{ selectedPlan.name }}</strong>
          </div>
          <div>
            <span>执行周期</span><strong>{{ selectedPlan.cycle }}</strong>
          </div>
          <div>
            <span>录像时段</span><strong>{{ selectedPlan.timeSummary }}</strong>
          </div>
          <div>
            <span>当前状态</span
            ><a-tag :color="selectedPlan.enabled ? 'green' : 'gray'">{{ selectedPlan.enabled ? "已启用" : "已停用" }}</a-tag>
          </div>
        </div>
      </section>
      <a-alert v-else type="error">当前录像计划不存在或已被删除，请关闭后刷新台账。</a-alert>
      <a-alert v-if="selectedPlan && !selectedPlan.enabled" type="warning" class="assignment-plan-warning"
        >当前计划已停用，不能新增分配，请先启用计划。</a-alert
      >

      <section class="assignment-channel-section">
        <div class="assignment-section-heading">
          <div>
            <h3>选择应用对象</h3>
            <span>{{ selectionSummary }}</span>
          </div>
        </div>

        <div class="assignment-scope-bar">
          <div class="segmented assignment-scope-switch" role="tablist" aria-label="分配范围">
            <button
              type="button"
              :class="{ active: selectionScope === 'device' }"
              :aria-pressed="selectionScope === 'device'"
              @click="switchScope('device')"
            >
              按设备
            </button>
            <button
              type="button"
              :class="{ active: selectionScope === 'channel' }"
              :aria-pressed="selectionScope === 'channel'"
              @click="switchScope('channel')"
            >
              按通道
            </button>
          </div>
          <span>{{
            selectionScope === "device" ? "选中设备后，将应用到该设备下全部有权限通道" : "按通道精确选择，不影响同设备的其他通道"
          }}</span>
        </div>

        <div class="assignment-filters">
          <a-input
            v-model="keyword"
            allow-clear
            :placeholder="selectionScope === 'device' ? '输入设备名称或国标编码' : '输入通道名称、编码或所属设备'"
            @press-enter="applySearch"
          >
            <template #prefix><Search :size="15" /></template>
          </a-input>
          <a-select v-model="onlineFilter" :options="onlineFilterOptions" />
          <a-button type="primary" @click="applySearch"
            ><template #icon><Search :size="14" /></template>查询</a-button
          >
          <a-button @click="resetSearch">重置</a-button>
        </div>

        <!-- 已选清空入口：仅在有勾选时出现，右侧对齐。 -->
        <div v-if="selectedTargetCount" class="selection-actions">
          <a-link @click="clearSelection">清空已选（{{ selectedTargetCount }}）</a-link>
        </div>

        <a-table
          v-if="selectionScope === 'device'"
          v-model:selected-keys="selectedDeviceKeys"
          class="uvp-data-table assignment-channel-table"
          row-key="id"
          :data="assignmentRows"
          :loading="loading"
          :pagination="assignmentPagination"
          :bordered="false"
          :row-selection="deviceRowSelection"
          :row-class="rowClass"
          :scroll="{ x: 860, y: 1 }"
          @page-change="handlePageChange"
          @page-size-change="handlePageSizeChange"
        >
          <template #columns>
            <a-table-column title="设备名称" :width="220">
              <template #cell="{ record }"
                ><div class="entity-cell">
                  <span>{{ record.name || "—" }}</span>
                  <!-- 已占用：名称后跟一个弱化标签，配合整行置灰与禁用勾选 -->
                  <small v-if="record.bound" class="bound-hint">已被录像计划占用</small>
                </div></template
              >
            </a-table-column>
            <a-table-column title="设备 ID" :width="220">
              <template #cell="{ record }"
                ><div class="entity-cell">
                  <small class="entity-code">{{ record.code }}</small>
                </div></template
              >
            </a-table-column>
            <a-table-column title="在线状态" :width="100" align="center">
              <template #cell="{ record }"
                ><a-badge :status="record.online ? 'success' : 'normal'" :text="deviceStatusLabel(!!record.online)"
              /></template>
            </a-table-column>
          </template>
        </a-table>

        <a-table
          v-else
          v-model:selected-keys="selectedChannelKeys"
          class="uvp-data-table assignment-channel-table"
          row-key="id"
          :data="assignmentRows"
          :loading="loading"
          :pagination="assignmentPagination"
          :bordered="false"
          :row-selection="channelRowSelection"
          :row-class="rowClass"
          :scroll="{ x: 900, y: 1 }"
          @page-change="handlePageChange"
          @page-size-change="handlePageSizeChange"
        >
          <template #columns>
            <a-table-column title="通道名称" :width="200">
              <template #cell="{ record }"
                ><div class="entity-cell">
                  <span>{{ record.name || "—" }}</span>
                  <small v-if="record.bound" class="bound-hint">已被录像计划占用</small>
                </div></template
              >
            </a-table-column>
            <a-table-column title="通道 ID" :width="200">
              <template #cell="{ record }"
                ><div class="entity-cell">
                  <small class="entity-code">{{ record.code }}</small>
                </div></template
              >
            </a-table-column>
            <a-table-column title="所属设备" :width="200"
              ><template #cell="{ record }"
                ><div class="entity-cell">
                  <span>{{ record.deviceCode || "—" }}</span>
                </div></template
              ></a-table-column
            >
            <a-table-column title="在线状态" :width="100" align="center"
              ><template #cell="{ record }"
                ><a-badge :status="record.online ? 'success' : 'normal'" :text="deviceStatusLabel(!!record.online)" /></template
            ></a-table-column>
          </template>
        </a-table>
      </section>
    </div>

    <template #footer>
      <div class="assignment-footer">
        <span>{{ footerSummary }}</span>
        <a-space>
          <a-button @click="close">取消</a-button>
          <a-button
            type="primary"
            :loading="submitting"
            :disabled="!selectedPlan?.enabled || !selectedTargetCount"
            @click="confirm"
          >
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
import {
  assignRecordingPlan,
  listRecordingPlanChannels,
  listRecordingPlanDevices,
  type AssignmentOption
} from "@/api/gb28181-recording-plan";
import type { RecordingSchedule } from "../types";
import { useDeviceStatusLabel } from "../../useDeviceStatusDict";

type AssignmentScope = "device" | "channel";
type OnlineFilter = "all" | "online";

const props = defineProps<{ visible: boolean; plans: RecordingSchedule[]; planId?: string }>();
const emit = defineEmits<{
  (event: "update:visible", value: boolean): void;
  (event: "confirm", payload: { planId: string; scope: AssignmentScope; targetIds: number[]; channelCount: number }): void;
}>();

/** 通道在线状态文案（`device_status` 字典）。 */
const deviceStatusLabel = useDeviceStatusLabel();

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
const onlineFilterOptions = computed(() =>
  selectionScope.value === "device"
    ? [
        { label: "全部设备", value: "all" },
        { label: "仅显示在线设备", value: "online" }
      ]
    : [
        { label: "全部通道", value: "all" },
        { label: "仅显示在线通道", value: "online" }
      ]
);
const selectedTargetCount = computed(() =>
  selectionScope.value === "device" ? selectedDeviceKeys.value.length : selectedChannelKeys.value.length
);
const selectionSummary = computed(() =>
  selectionScope.value === "device"
    ? `已选择 ${selectedDeviceKeys.value.length} 台设备，确认后按服务端当前通道清单展开`
    : `已选择 ${selectedChannelKeys.value.length} 个通道`
);

/**
 * 已被录像计划占用的候选**不可勾选**（老板选方案B：提交前就告知结果）。
 *
 * ⛔⛔ Arco 表格的「按行禁用」**只认数据行上的 `record.disabled`**：
 * 实测 `rowSelection.disabled`（哪怕写成按行返回的函数）**完全不生效** ——
 * `table-operation-td.js` 里是 `"disabled": Boolean(props.record.disabled)`。
 * 所以这里给候选行**注入** disabled 字段，而不是配 rowSelection。
 * （rowSelection 只负责 type/showCheckedAll。）
 */
type AssignmentRow = AssignmentOption & { disabled?: boolean };
function rowDisabled(record: AssignmentOption): boolean {
  return !!record.bound;
}
/** 已占用行的行class，供置灰样式命中（Arco 不会自动加）。 */
function rowClass(record: AssignmentOption): string {
  return record.bound ? "row-bounded" : "";
}
const deviceRowSelection = computed(() => ({
  type: "checkbox" as const,
  showCheckedAll: true
}));
const channelRowSelection = computed(() => ({
  type: "checkbox" as const,
  showCheckedAll: true
}));
/** 候选数据：注入按行 disabled（Arco 只认 record.disabled）。 */
const assignmentRows = computed<AssignmentRow[]>(() =>
  assignmentOptions.value.map(item => ({ ...item, disabled: rowDisabled(item) }))
);
const footerSummary = computed(() => {
  if (!selectedPlan.value) return "未找到当前录像计划";
  if (selectionScope.value === "device")
    return `将为 ${selectedDeviceKeys.value.length} 台设备下当前有权限的通道分配“${selectedPlan.value.name}”`;
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
    const response = await assignRecordingPlan(Number(selectedPlan.value.id), {
      type: selectionScope.value,
      ids: [...targetIds]
    });
    const conflicts = response.data.items.filter(item => item.status !== "assigned").length;
    // ⚠️ 必须把「已被其他计划占用」单独挑出来说：笼统的"M 项未分配"让用户不知道
    // 该换哪几个（此前正是这个缺口）。这里按 status 分类计数。
    const takenCount = response.data.items.filter(item => item.status === "conflict").length;
    if (conflicts) {
      const assigned = response.data.assignedCount;
      if (takenCount && takenCount === conflicts) {
        Message.warning(`成功分配 ${assigned} 个通道，${takenCount} 个已被其他录像计划占用，未分配`);
      } else {
        Message.warning(
          `成功分配 ${assigned} 个通道，${conflicts} 项未分配${takenCount ? `（其中 ${takenCount} 个已被其他录像计划占用）` : ""}`
        );
      }
    }
    emit("confirm", {
      planId: selectedPlan.value.id,
      scope: selectionScope.value,
      targetIds: [...targetIds],
      channelCount: response.data.assignedCount
    });
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
    const params = {
      keyword: appliedKeyword.value,
      online: onlineFilter.value,
      page: paginationState.current,
      pageSize: paginationState.pageSize
    };
    const response =
      selectionScope.value === "device"
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
watch(onlineFilter, () => {
  paginationState.current = 1;
  void loadOptions();
});
</script>

<style scoped lang="scss">
.assignment-title {
  display: flex;
  gap: 10px;
  align-items: center;
  min-width: 0;
}
.assignment-title > span {
  display: grid;
  flex: 0 0 auto;
  place-items: center;
  width: 34px;
  height: 34px;
  color: #16845f;
  background: rgb(22 132 95 / 10%);
  border-radius: 8px;
}
.assignment-title > div {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.assignment-title strong {
  font-size: 15px;
}
.assignment-title small {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}

/* ⛔⛔ 滚动条只能出现在**表格表体**上，弹窗自身与全屏遮罩都不许有。
   实测（1440x900 / 1280x720 / 1280x577 三档）：修复前有两条多余竖向滚动条 ——
     ① `.arco-modal-wrapper`：Arco 的 `align-center` 靠 `white-space:nowrap` +
        高度 100% 的 `::after` 做垂直居中，`inline-block` 的弹窗把 wrapper 的
        scrollHeight 撑到 1108px（视口仅 577px）⇒ 全屏右侧一条大滚动条；
     ② `.arco-modal-body`：`:body-style` 的 `overflowY:auto` 让 777px 内容
        挤在 406px 可用高度里滚动 ⇒ 弹窗内一条大滚动条，表格被压到只剩表头。
   修法：wrapper 禁滚（同 RecordScheduleDrawer 的既有做法）+ body 改成
   「固定块 + 弹性表格」的 flex 纵向布局，滚动下沉到 `.arco-table-body`。 */

/* ⛔ `.arco-modal-body` 必须自己变成 flex 容器：本仓全局给弹窗 body 的 padding 是
   `22px 24px` 且 `box-sizing: content-box`（实测），所以 body 作为 flex item 时
   `clientHeight` 含 padding，而它若还是 `display:block`，内部 `.assignment-content`
   就只能按内容高度收缩（实测 637px 的 body 里只分到 486px，表格可用高度被压成 0）。
   这里用 scoped `:deep()` 接管：body 不滚、内部纵向 flex、滚动只交给表体。 */
.channel-assignment-dialog :deep(.arco-modal-body) {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}

.assignment-content {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  color: var(--uvp-text-primary);
}
.assignment-plan-section {
  flex: 0 0 auto;
  padding: 0 0 18px;
  border-bottom: 1px solid var(--uvp-border-subtle);
}
.field-label {
  margin-bottom: 8px;
  font-size: 13px;
  font-weight: 620;
  color: var(--uvp-text-secondary);
}
.selected-plan-summary {
  display: grid;
  grid-template-columns: 1.2fr 1fr 1.5fr 100px;
  padding: 12px 14px;
  background: var(--uvp-search-panel-bg);
  border: 1px solid var(--uvp-list-panel-border);
  border-radius: 10px;
}
.selected-plan-summary > div {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
  padding: 0 14px;
  border-right: 1px solid var(--uvp-border-subtle);
}
.selected-plan-summary > div:first-child {
  padding-left: 0;
}
.selected-plan-summary > div:last-child {
  border-right: 0;
}
.selected-plan-summary span {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.selected-plan-summary strong {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 13px;
  white-space: nowrap;
}
.assignment-plan-warning {
  margin-top: 10px;
}

/* 「选择应用对象」整段参与弹性伸缩，内部筛选/说明固定，只有表格吃掉剩余高度 */
.assignment-channel-section {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  min-height: 0;
  padding: 18px 0 14px;
}
.assignment-section-heading {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}
.assignment-section-heading h3 {
  margin: 0;
  font-size: 14px;
}
.assignment-section-heading span {
  display: block;
  margin-top: 3px;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.assignment-scope-bar {
  display: flex;
  gap: 14px;
  align-items: center;
  justify-content: space-between;
  padding: 8px 10px;
  margin-bottom: 12px;
  background: var(--uvp-table-header-bg);
  border: 1px solid var(--uvp-list-panel-border);
  border-radius: 8px;
}
.assignment-scope-switch {
  flex: 0 0 auto;
}
.assignment-scope-switch button {
  min-width: 82px;
}
.assignment-scope-bar > span {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
  text-align: right;
}
.assignment-filters {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 180px auto auto;
  gap: 8px;
  margin-bottom: 8px;
}
.assignment-filters :deep(.arco-input-wrapper),
.assignment-filters :deep(.arco-select-view) {
  min-height: 38px;
  border-radius: 8px;
}
.assignment-filters :deep(.arco-btn) {
  min-height: 38px;
  border-radius: 8px;
}

/* 已选清空入口：右对齐的轻量操作行。 */
.selection-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  min-height: 26px;
  font-size: 12px;
}

/* 表格区：表头/筛选等按内容固定，表格本体 `flex:1` 吃满剩余高度。
   原来的 `min-height: 340px` 会在矮视口下反向撑破 `.arco-modal-body`
   （实测 577px 视口时表格被腰斩到 48px），故改为纯 flex 分配。
   ⛔ 伸缩链必须一路穿过 Arco 的 `.arco-scrollbar` 包装层：它们是
   `display:block; flex:0 1 auto`（实测），不接管的话 `.arco-table-container`
   只按内容高度收缩，表格会被压回 140px（表头+一行+分页的自然高度）。
   ⛔ `height` 要压过 Arco 写在 `.arco-table` 上的内联 `height:100%`
   （`scroll.y` 传字符串时落的），否则 flex 分配同样失效。 */

/* ⛔⛔ `scroll.y` 这里给的是占位值 1，不是「表体高度 1px」，更不是随手写的：
   Arco 只有在 `scroll.y` 为**非空值**时才会拆分表头/表体（`splitTable`），拆开后
   表头才独立固定、只有表体滚动 —— 这正是「滚动条只出现在表格上」的前提。
   但写死正常数字（如 300）会把表体高度钉死、写字符串则被 Arco 落成内联
   `height:100%`，两者都会压掉下面的 flex 分配（实测表格被压回 140px，
   即「表头 + 一行 + 分页」的自然高度）。所以真正的表体高度交给 flex 链 +
   `max-height: none`，`scroll.y` 只负责「触发拆分」。 */
.assignment-channel-table {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  height: auto !important;
  min-height: 0;
}
.assignment-channel-table :deep(.arco-spin),
.assignment-channel-table :deep(.arco-table-container),
.assignment-channel-table :deep(.arco-table-content),
.assignment-channel-table :deep(.arco-table-container > .arco-scrollbar) {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  min-height: 0;
}
.assignment-channel-table :deep(.arco-table-header) {
  flex: 0 0 auto;
}

/* 唯一允许出现竖向滚动条的地方：表体。
   ⛔ `max-height` 必须 `!important`：Arco 会把 `scroll.y` 的值落成表体的**内联**
   `max-height`（实测 `style="max-height: 1px"`），内联优先级高于普通 class 规则，
   不加 `!important` 就会把表体钉死在占位值（实测只剩 1px，只露出表头）。 */
.assignment-channel-table :deep(.arco-table-body) {
  flex: 1 1 auto;
  min-height: 0;
  max-height: none !important;
}
.entity-cell {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.entity-cell span,
.entity-cell small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.entity-cell small {
  margin-top: 2px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
}

/* 设备 ID 独占一列后不再挤在名称下方，字号提到与名称同级（12px）才读得清；
   仍保留等宽字体 + 省略号，方便肉眼比对 20 位国标编码。 */
.entity-cell small.entity-code {
  margin-top: 0;
  font-size: 12px;
  color: var(--uvp-text-secondary);
}

/* 已占用标记：与名称同行的弱化小标签（不新增表格列，避免改变已定下的列结构）。 */
.bound-hint {
  margin-top: 2px;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}

/* ⛔ 已占用的候选整行置灰且禁用勾选。
   用「灰字+ 浅底」而不是 opacity：opacity 会连复选框一起变淡，看起来像"可点"，反而误导。 */
.assignment-channel-table :deep(tr.arco-table-tr.row-bounded) {
  cursor: not-allowed;
}
.assignment-channel-table :deep(tr.arco-table-tr.row-bounded > td) {
  color: var(--uvp-text-tertiary);
  background: var(--uvp-search-panel-bg);
}

.assignment-footer {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  width: 100%;
}
.assignment-footer > span {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}

@media (width <= 640px) {
  .selected-plan-summary {
    grid-template-columns: 1fr;
    gap: 10px;
  }
  .selected-plan-summary > div {
    padding: 0;
    border-right: 0;
  }
  .assignment-scope-bar {
    flex-direction: column;
    align-items: flex-start;
  }
  .assignment-scope-bar > span {
    text-align: left;
  }
  .assignment-filters {
    grid-template-columns: 1fr;
  }
  .assignment-footer > span {
    display: none;
  }
  .assignment-footer {
    justify-content: flex-end;
  }
}
</style>

<style lang="scss">
/* ⛔ 必须是非 scoped 的全局样式：`.arco-modal-wrapper` 是 Arco 在 body 下另起的
   容器，不在本组件作用域内。`modal-class` 落在 `.arco-modal` 面板上（不是
   wrapper），所以只能用 `:has(> ...)` 选中「装着本弹窗的那一个 wrapper」。

   这里做两件事，缺一不可：

   ① `overflow: hidden` —— Arco 的 `align-center` 靠「`white-space:nowrap` +
      高度 100% 的 `::after`」这套 inline-block 技巧做垂直居中，会把 wrapper 的
      scrollHeight 撑到远超视口（实测 1280x577 下撑到 1108px）⇒ 全屏一条大滚动条。
      禁掉它，滚动只由表格表体承担（同 RecordScheduleDrawer 的既有做法）。

   ② 换成真正的 flex 居中 —— ①禁滚后 `align-center` 那套技巧会退化成
      「贴顶对齐」（实测 top=0、下方却空 138px，上下不对称，看着很别扭）。
      Arco 官方的 align-center 就是这么实现的，**详情弹窗同样贴顶**，
      不是本组件独有。改用 flex 后实测上下各留 69px，真正居中。
      `white-space: normal` 必须一起改：nowrap 会让 flex 容器里的 inline-block
      仍按 nowrap 对齐。
   ⚠️ 不要再改回 `align-center` 属性配套的样式，否则又会贴顶。 */
.arco-modal-wrapper:has(> .channel-assignment-dialog) {
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  white-space: normal;
}
</style>
