<template>
  <div class="snow-fill recording-schedules-page">
    <div class="snow-fill-inner uvp-page-shell-flat recording-schedules-shell">
      <div class="recording-schedules-toolbar">
        <div class="segmented schedule-view-switch" role="tablist" aria-label="录像计划视图">
          <button type="button" :class="{ active: activeView === 'plans' }" :aria-pressed="activeView === 'plans'" @click="activeView = 'plans'">
			<CalendarClock :size="14" />计划管理<span>{{ planTotal }}</span>
          </button>
          <button type="button" :class="{ active: activeView === 'status' }" :aria-pressed="activeView === 'status'" @click="activeView = 'status'">
            <Activity :size="14" />通道执行状态<span>{{ plannedExecutionChannelCount }}</span>
          </button>
        </div>
        <div class="toolbar-actions">
          <a-button class="uvp-refresh-btn" @click="refresh"><template #icon><RefreshCw :size="15" /></template>刷新</a-button>
          <a-button v-if="activeView === 'plans'" type="primary" @click="openCreate"><template #icon><Plus :size="15" /></template>新建计划</a-button>
        </div>
      </div>

      <template v-if="activeView === 'plans'">
        <s-layout-search>
          <template #fields>
            <a-input v-model="planFilters.keyword" allow-clear placeholder="计划名称 / 说明" style="width: 260px" @press-enter="searchPlans">
              <template #prefix><Search :size="15" /></template>
            </a-input>
            <a-select v-model="planFilters.status" allow-clear placeholder="计划状态" style="width: 150px">
              <a-option value="enabled">已启用</a-option>
              <a-option value="disabled">已停用</a-option>
            </a-select>
          </template>
          <template #actions>
            <a-button type="primary" @click="searchPlans"><template #icon><Search :size="15" /></template>查询</a-button>
            <a-button @click="resetPlanFilters"><template #icon><RotateCcw :size="15" /></template>重置</a-button>
          </template>
        </s-layout-search>

        <div class="schedule-table-wrap">
		  <a-table class="uvp-data-table schedule-data-table" row-key="id" :data="plans" :loading="plansLoading" :bordered="false" :pagination="planPagination" :scroll="planTableScroll" @page-change="handlePlanPageChange" @page-size-change="handlePlanPageSizeChange">
            <template #columns>
              <a-table-column title="计划名称" :width="226">
                <template #cell="{ record }">
                  <div class="schedule-name-cell">
                    <span class="schedule-icon"><CalendarDays :size="16" /></span>
                    <div><strong>{{ record.name }}</strong><small>{{ record.description }}</small></div>
                  </div>
                </template>
              </a-table-column>
              <a-table-column title="执行周期" :width="132" data-index="cycle" />
              <a-table-column title="录像时段" :width="220">
                <template #cell="{ record }"><span class="schedule-time"><Clock3 :size="14" />{{ record.timeSummary }}</span></template>
              </a-table-column>
              <a-table-column title="已应用通道" :width="126" align="center">
                <template #cell="{ record }">
                  <a-link class="channel-count-link" :title="`查看并管理 ${record.channelCount} 个已应用通道`" @click="openAssign(record)">{{ record.channelCount }} 个</a-link>
                </template>
              </a-table-column>
              <a-table-column title="状态" :width="110" align="center">
                <template #cell="{ record }">
                  <a-switch v-model="record.enabled" :aria-label="`${record.name}${record.enabled ? '停用' : '启用'}`" @change="notifyPlanStatusChange(record)" />
                </template>
              </a-table-column>
              <a-table-column title="最近更新" :width="172" data-index="updatedAt" />
			  <a-table-column title="操作" :width="260" align="center" fixed="right">
                <template #cell="{ record }">
                  <div class="uvp-table-actions schedule-actions">
                    <a-link class="uvp-table-action uvp-table-action--detail" @click="openDetail(record)"><template #icon><Eye :size="13" /></template>查看</a-link>
                    <a-link class="uvp-table-action uvp-table-action--edit" @click="openEdit(record)"><template #icon><Pencil :size="13" /></template>编辑</a-link>
                    <a-link class="uvp-table-action uvp-table-action--assign" @click="openAssign(record)"><template #icon><Link2 :size="13" /></template>分配通道</a-link>
					<a-popconfirm content="确认删除该录像计划？已分配通道的计划将拒绝删除。" @ok="removePlan(record)"><a-link class="uvp-table-action uvp-table-action--danger"><template #icon><Trash2 :size="13" /></template>删除</a-link></a-popconfirm>
                  </div>
                </template>
              </a-table-column>
            </template>
            <template #empty><a-empty description="当前筛选条件下暂无录像计划" /></template>
          </a-table>
        </div>
      </template>

      <template v-else>
        <div class="execution-workspace">
          <aside class="execution-plan-panel">
            <div class="execution-plan-heading">
              <div><strong>录像计划</strong><span>选择计划查看执行情况</span></div>
              <em>{{ plans.length }}</em>
            </div>
            <s-layout-search class="execution-plan-search">
              <template #fields>
                <a-input v-model="executionPlanKeyword" allow-clear placeholder="搜索计划名称" @input="resetExecutionPlanPaging">
                  <template #prefix><Search :size="14" /></template>
                </a-input>
              </template>
            </s-layout-search>
            <div class="execution-plan-list" @scroll.passive="handleExecutionPlanScroll">
              <button
                v-for="plan in visibleExecutionPlans"
                :key="plan.id"
                type="button"
                :class="['execution-plan-item', { active: selectedExecutionPlanId === plan.id }]"
                @click="selectExecutionPlan(plan.id)"
              >
                <span class="execution-plan-icon"><CalendarDays :size="16" /></span>
                <span class="execution-plan-main">
                  <span class="execution-plan-name"><strong>{{ plan.name }}</strong><i :class="{ enabled: plan.enabled }">{{ plan.enabled ? "启用" : "停用" }}</i></span>
                  <small>{{ plan.cycle }} · {{ plan.timeSummary }}</small>
                  <span class="execution-plan-meta"><b>{{ plan.channelCount }}</b> 个关联通道</span>
                </span>
              </button>
              <a-empty v-if="visibleExecutionPlans.length === 0" description="未找到匹配的录像计划" />
              <div v-else class="execution-plan-list-status">
                <span v-if="hasMoreExecutionPlans">继续滚动加载</span>
				<span v-else>已加载 {{ visibleExecutionPlans.length }} / 共 {{ executionPlanTotal }} 条</span>
              </div>
            </div>
          </aside>

          <section class="execution-channel-panel">
            <header class="execution-channel-header">
              <div class="execution-channel-title">
                <span><Activity :size="18" /></span>
                <div><strong>{{ selectedExecutionPlan?.name || "请选择录像计划" }}</strong><small>当前计划关联通道</small></div>
              </div>
              <div v-if="selectedExecutionPlan" class="execution-plan-overview">
                <span><small>计划状态</small><b :class="{ enabled: selectedExecutionPlan.enabled }">{{ selectedExecutionPlan.enabled ? "已启用" : "已停用" }}</b></span>
                <span><small>当前时段</small><b>{{ selectedExecutionPlan.current }}</b></span>
                <span><small>下次切换</small><b>{{ selectedExecutionPlan.nextChange }}</b></span>
              </div>
            </header>

            <s-layout-search class="execution-channel-search">
              <template #fields>
                <a-input v-model="statusFilters.keyword" allow-clear placeholder="设备名称 / 通道名称或编码" style="width: 280px" @press-enter="searchStatus">
                  <template #prefix><Search :size="15" /></template>
                </a-input>
                <a-select v-model="statusFilters.online" allow-clear placeholder="在线状态" style="width: 130px">
                  <a-option value="online">在线</a-option><a-option value="offline">离线</a-option>
                </a-select>
                <a-select v-model="statusFilters.state" allow-clear placeholder="执行状态" style="width: 160px">
				  <a-option value="recording">录像中</a-option><a-option value="waiting_device">等待设备上线</a-option><a-option value="outside_schedule">时段外</a-option><a-option value="idle">已关闭</a-option>
                </a-select>
              </template>
              <template #actions>
                <a-button type="primary" @click="searchStatus"><template #icon><Search :size="15" /></template>查询</a-button>
                <a-button @click="resetStatusFilters"><template #icon><RotateCcw :size="15" /></template>重置</a-button>
              </template>
              <template #extra>
                <div class="execution-legend">
                  <span><i class="dot success" />录像中 {{ executionStats.recording }}</span>
                  <span><i class="dot warning" />异常 {{ executionStats.abnormal }}</span>
                  <span><i class="dot neutral" />未执行 {{ executionStats.idle }}</span>
                </div>
              </template>
            </s-layout-search>

            <div class="execution-channel-table-wrap">
			  <a-table class="uvp-data-table schedule-data-table execution-data-table" row-key="id" :data="filteredExecutionChannels" :loading="executionChannelsLoading" :bordered="false" :pagination="executionPagination" :scroll="executionTableScroll" @page-change="handleExecutionPageChange" @page-size-change="handleExecutionPageSizeChange">
                <template #columns>
                  <a-table-column title="通道" :width="218"><template #cell="{ record }"><div class="entity-cell"><span>{{ record.name }}</span><small>{{ record.code }}</small></div></template></a-table-column>
                  <a-table-column title="所属设备" :width="200"><template #cell="{ record }"><div class="entity-cell"><span>{{ record.device }}</span><small>{{ record.deviceCode }}</small></div></template></a-table-column>
                  <a-table-column title="在线状态" :width="104" align="center"><template #cell="{ record }"><a-badge :status="record.online ? 'success' : 'normal'" :text="record.online ? '在线' : '离线'" /></template></a-table-column>
                  <a-table-column title="计划命中" :width="104" align="center"><template #cell="{ record }"><span :class="['match-state', { matched: record.matched === '是' }]">{{ record.matched }}</span></template></a-table-column>
                  <a-table-column title="实际状态" :width="144"><template #cell="{ record }"><span :class="['execution-state', stateTone(record.state)]"><i />{{ record.state }}</span></template></a-table-column>
                  <a-table-column title="最近错误" :width="150"><template #cell="{ record }"><span :class="{ 'error-text': record.error !== '—' }">{{ record.error }}</span></template></a-table-column>
                  <a-table-column title="下次切换" :width="152" data-index="next" />
                </template>
                <template #empty><a-empty description="当前计划下暂无符合条件的通道" /></template>
              </a-table>
            </div>
          </section>
        </div>
      </template>
    </div>
  </div>

	<RecordingScheduleDrawer v-model:visible="detailVisible" mode="detail" :plan="activePlan" :assigned-channels="detailChannels" :channels-loading="detailChannelsLoading" :channel-total="detailChannelTotal" :channel-page="detailChannelPage" @channel-page-change="detailChannelPage = $event; loadDetailChannels()" />
  <RecordingScheduleEditorDialog v-model:visible="editorVisible" :mode="editorMode" :plan="activePlan" @save="savePlan" />
  <ChannelAssignmentDialog v-model:visible="assignmentVisible" :plans="plans" :plan-id="activePlan?.id" @confirm="confirmAssignment" />
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import dayjs from "dayjs";
import { Activity, CalendarClock, CalendarDays, Clock3, Eye, Link2, Pencil, Plus, RefreshCw, RotateCcw, Search, Trash2 } from "@lucide/vue";
import {
  createRecordingPlan, deleteRecordingPlan, getRecordingPlan, listRecordingPlanExecutionChannels,
  listRecordingPlans, setRecordingPlanEnabled, updateRecordingPlan,
	type PlanChannelStatus, type RecordingPlanInput, type RecordingPlanPeriod
} from "@/api/gb28181-recording-plan";
import RecordingScheduleDrawer from "./components/RecordingScheduleDrawer.vue";
import RecordingScheduleEditorDialog from "./components/RecordingScheduleEditorDialog.vue";
import ChannelAssignmentDialog from "./components/ChannelAssignmentDialog.vue";
import type { RecordingSchedule, ScheduleChannel, ScheduleDay } from "./types";

const dayNames = ["周一", "周二", "周三", "周四", "周五", "周六", "周日"];
const plans = ref<RecordingSchedule[]>([]);
const executionPlans = ref<RecordingSchedule[]>([]);
const executionChannels = ref<ScheduleChannel[]>([]);
const planTotal = ref(0);
const executionPlanTotal = ref(0);
const executionChannelTotal = ref(0);
const executionStatusCounts = ref<Record<string, number>>({});
const plansLoading = ref(false);
const executionPlansLoading = ref(false);
const executionChannelsLoading = ref(false);
const detailChannels = ref<ScheduleChannel[]>([]);
const detailChannelTotal = ref(0);
const detailChannelPage = ref(1);
const detailChannelsLoading = ref(false);

const activeView = ref<"plans" | "status">("plans");
const detailVisible = ref(false);
const editorVisible = ref(false);
const assignmentVisible = ref(false);
const editorMode = ref<"create" | "edit">("create");
const activePlan = ref<RecordingSchedule | null>(null);
const selectedExecutionPlanId = ref("");
const executionPlanKeyword = ref("");
const executionPlanPageSize = 30;
const executionPlanPage = ref(1);
const planFilters = reactive({ keyword: "", status: "" });
const statusFilters = reactive({ keyword: "", online: "", state: "" });
const planPageState = reactive({ current: 1, pageSize: 10 });
const executionPageState = reactive({ current: 1, pageSize: 10 });
const planTableScroll = { x: "100%", minWidth: 1200 };
const executionTableScroll = { x: "100%", minWidth: 1072 };

const visibleExecutionPlans = computed(() => executionPlans.value);
const hasMoreExecutionPlans = computed(() => executionPlans.value.length < executionPlanTotal.value);
const plannedExecutionChannelCount = computed(() => executionPlans.value.reduce((total, plan) => total + plan.channelCount, 0));
const selectedExecutionPlan = computed(() => executionPlans.value.find(plan => plan.id === selectedExecutionPlanId.value) || null);
const filteredExecutionChannels = computed(() => executionChannels.value);
const executionStats = computed(() => ({
	recording: executionStatusCounts.value.recording || 0,
	abnormal: Object.entries(executionStatusCounts.value).filter(([state]) => !["recording", "idle", "outside_schedule"].includes(state)).reduce((sum, [, count]) => sum + count, 0),
	idle: (executionStatusCounts.value.idle || 0) + (executionStatusCounts.value.outside_schedule || 0)
}));

const planPagination = computed(() => ({
  current: planPageState.current,
  pageSize: planPageState.pageSize,
	total: planTotal.value,
  showTotal: true,
  showJumper: true,
  showPageSize: true,
  pageSizeOptions: [10, 20, 50, 100]
}));
const executionPagination = computed(() => ({
  current: executionPageState.current,
  pageSize: executionPageState.pageSize,
	total: executionChannelTotal.value,
  showTotal: true,
  showJumper: true,
  showPageSize: true,
  pageSizeOptions: [10, 20, 50, 100]
}));

function searchPlans() { planPageState.current = 1; void loadPlans(); }
function searchStatus() { executionPageState.current = 1; void loadExecutionChannels(); }
function resetPlanFilters() { Object.assign(planFilters, { keyword: "", status: "" }); searchPlans(); }
function resetStatusFilters() { Object.assign(statusFilters, { keyword: "", online: "", state: "" }); searchStatus(); }
function selectExecutionPlan(planId: string) { selectedExecutionPlanId.value = planId; executionPageState.current = 1; void loadExecutionChannels(); }
function resetExecutionPlanPaging() { executionPlanPage.value = 1; executionPlans.value = []; void loadExecutionPlans(false); }
function loadMoreExecutionPlans() {
	if (!hasMoreExecutionPlans.value || executionPlansLoading.value) return;
	executionPlanPage.value += 1;
	void loadExecutionPlans(true);
}
function handleExecutionPlanScroll(event: Event) {
  const target = event.currentTarget as HTMLElement;
  const nearVerticalEnd = target.scrollHeight - target.scrollTop - target.clientHeight <= 48;
  const nearHorizontalEnd = target.scrollWidth - target.scrollLeft - target.clientWidth <= 48;
  if (nearVerticalEnd || nearHorizontalEnd) loadMoreExecutionPlans();
}
function handlePlanPageChange(current: number) { planPageState.current = current; void loadPlans(); }
function handlePlanPageSizeChange(pageSize: number) { planPageState.pageSize = pageSize; planPageState.current = 1; void loadPlans(); }
function handleExecutionPageChange(current: number) { executionPageState.current = current; void loadExecutionChannels(); }
function handleExecutionPageSizeChange(pageSize: number) { executionPageState.pageSize = pageSize; executionPageState.current = 1; void loadExecutionChannels(); }
function refresh() { void Promise.all([loadPlans(), loadExecutionPlans(false)]).then(() => Message.success("录像计划数据已刷新")); }
async function notifyPlanStatusChange(plan: RecordingSchedule) {
	const next = plan.enabled;
	try {
		await setRecordingPlanEnabled(Number(plan.id), next);
		Message.success(`录像计划“${plan.name}”已${next ? "启用" : "停用"}`);
	} catch {
		plan.enabled = !next;
	}
}
async function openDetail(plan: RecordingSchedule) {
	const response = await getRecordingPlan(Number(plan.id));
	activePlan.value = toSchedule(response.data);
	detailChannelPage.value = 1;
	detailVisible.value = true;
	void loadDetailChannels();
}
function openCreate() { activePlan.value = null; editorMode.value = "create"; editorVisible.value = true; }
async function openEdit(plan: RecordingSchedule | null) {
	if (!plan) return;
	const response = await getRecordingPlan(Number(plan.id));
	detailVisible.value = false;
	activePlan.value = toSchedule(response.data);
	editorMode.value = "edit";
	editorVisible.value = true;
}
function openAssign(plan?: RecordingSchedule | null) { activePlan.value = plan || plans.value.find(item => item.enabled) || null; assignmentVisible.value = true; }
async function savePlan(plan: RecordingSchedule) {
	const editing = editorMode.value === "edit" && Boolean(activePlan.value?.id);
	const response = editing
		? await updateRecordingPlan(Number(activePlan.value?.id), toPlanInput(plan))
		: await createRecordingPlan(toPlanInput(plan));
	editorVisible.value = false;
	activePlan.value = toSchedule(response.data);
	Message.success(editing ? "计划已更新" : "计划已创建");
	await loadPlans();
}
function confirmAssignment(payload: { planId: string; scope: "device" | "channel"; targetIds: number[]; channelCount: number }) {
  const plan = plans.value.find(item => item.id === payload.planId);
	if (plan) plan.channelCount += payload.channelCount;
  assignmentVisible.value = false;
  Message.success(payload.scope === "device"
    ? `已按设备为 ${payload.channelCount} 个通道分配录像计划`
    : `已为 ${payload.targetIds.length} 个通道分配录像计划`);
}
async function removePlan(plan: RecordingSchedule) {
	await deleteRecordingPlan(Number(plan.id));
	Message.success(`录像计划“${plan.name}”已删除`);
	await loadPlans();
}
function stateTone(state: ScheduleChannel["state"]) { if (state === "录像中") return "success"; if (state === "等待设备上线") return "warning"; return "neutral"; }

async function loadPlans() {
	plansLoading.value = true;
	try {
		const response = await listRecordingPlans({ keyword: planFilters.keyword, status: planFilters.status, page: planPageState.current, pageSize: planPageState.pageSize });
		plans.value = response.data.list.map(toSchedule);
		planTotal.value = response.data.total;
	} finally {
		plansLoading.value = false;
	}
}

async function loadExecutionPlans(append: boolean) {
	executionPlansLoading.value = true;
	try {
		if (!append) executionPlanPage.value = 1;
		const response = await listRecordingPlans({ keyword: executionPlanKeyword.value, page: executionPlanPage.value, pageSize: executionPlanPageSize });
		const next = response.data.list.map(toSchedule);
		executionPlans.value = append ? [...executionPlans.value, ...next.filter(item => !executionPlans.value.some(existing => existing.id === item.id))] : next;
		executionPlanTotal.value = response.data.total;
		if (!selectedExecutionPlanId.value && executionPlans.value[0]) {
			selectedExecutionPlanId.value = executionPlans.value[0].id;
			void loadExecutionChannels();
		}
	} finally {
		executionPlansLoading.value = false;
	}
}

async function loadExecutionChannels() {
	if (!selectedExecutionPlanId.value) {
		executionChannels.value = [];
		return;
	}
	executionChannelsLoading.value = true;
	try {
		const response = await listRecordingPlanExecutionChannels(Number(selectedExecutionPlanId.value), {
			keyword: statusFilters.keyword, online: statusFilters.online, actualState: statusFilters.state, page: executionPageState.current, pageSize: executionPageState.pageSize
		});
		executionChannels.value = response.data.list.map(toScheduleChannel);
		executionChannelTotal.value = response.data.total;
		executionStatusCounts.value = response.data.statusCounts;
	} finally {
		executionChannelsLoading.value = false;
	}
}

async function loadDetailChannels() {
	if (!activePlan.value) return;
	detailChannelsLoading.value = true;
	try {
		const response = await listRecordingPlanExecutionChannels(Number(activePlan.value.id), { page: detailChannelPage.value, pageSize: 10 });
		detailChannels.value = response.data.list.map(toScheduleChannel);
		detailChannelTotal.value = response.data.total;
	} finally {
		detailChannelsLoading.value = false;
	}
}

function toSchedule(plan: { id: number; name: string; description: string; enabled: boolean; channelCount: number; periods: RecordingPlanPeriod[]; updatedAt: string }): RecordingSchedule {
	const days = periodsToDays(plan.periods || []);
	return {
		id: String(plan.id), name: plan.name, description: plan.description, enabled: plan.enabled,
		cycle: cycleLabel(days), timeSummary: timeSummary(days), channelCount: plan.channelCount,
		updatedAt: dayjs(plan.updatedAt).format("YYYY-MM-DD HH:mm"), current: plan.enabled ? "由执行状态实时计算" : "计划已停用",
		nextChange: plan.enabled ? "由调度实时计算" : "—", days
	};
}

function periodsToDays(periods: RecordingPlanPeriod[]): ScheduleDay[] {
	return dayNames.map((name, index) => {
		const dayPeriods = periods.filter(period => period.weekday === index + 1).map(period => ({ start: slotToTime(period.startSlot), end: slotToTime(period.endSlot) }));
		return { name, enabled: dayPeriods.length > 0, periods: dayPeriods };
	});
}

function toPlanInput(plan: RecordingSchedule): RecordingPlanInput {
	const periods = plan.days.flatMap((day, dayIndex) => day.periods.map(period => ({ weekday: dayIndex + 1, startSlot: timeToSlot(period.start), endSlot: timeToSlot(period.end) })));
	return { name: plan.name, description: plan.description, enabled: plan.enabled, periods };
}

function toScheduleChannel(channel: PlanChannelStatus): ScheduleChannel {
	return {
		id: String(channel.channelId), name: channel.channelName, code: channel.channelCode, device: channel.deviceName || channel.deviceCode,
		deviceCode: channel.deviceCode, online: channel.online, mode: channel.recordingMode === "continuous" ? "持续录像" : channel.recordingMode === "scheduled" ? "按计划" : "关闭",
		plan: selectedExecutionPlan.value?.name || activePlan.value?.name || "—", matched: channel.desiredState === "recording" ? "是" : "否",
		state: channel.actualState === "recording" ? "录像中" : channel.actualState === "waiting_device" || channel.actualState === "recovering" ? "等待设备上线" : channel.actualState === "outside_schedule" ? "时段外" : "已关闭",
		error: channel.reasonMessage || "—", next: channel.nextTransitionAt ? dayjs(channel.nextTransitionAt).format("MM-DD HH:mm") : "—"
	};
}

function cycleLabel(days: ScheduleDay[]) {
	const enabled = days.filter(day => day.enabled).map(day => day.name);
	if (enabled.length === 7) return "每天";
	if (enabled.join("") === "周一周二周三周四周五") return "周一至周五";
	if (enabled.join("") === "周六周日") return "周末";
	return enabled.join("、") || "未配置";
}
function timeSummary(days: ScheduleDay[]) { return days.find(day => day.enabled)?.periods.map(period => `${period.start}-${period.end}`).join("，") || "—"; }
function slotToTime(slot: number) { return slot === 48 ? "24:00" : `${String(Math.floor(slot / 2)).padStart(2, "0")}:${slot % 2 ? "30" : "00"}`; }
function timeToSlot(value: string) { const [hour, minute] = value.split(":").map(Number); return hour * 2 + (minute >= 30 ? 1 : 0); }

onMounted(() => { void loadPlans(); void loadExecutionPlans(false); });
</script>

<style scoped lang="scss">
.recording-schedules-page { box-sizing: border-box; width: 100%; height: 100%; max-width: 100vw; min-width: 0; min-height: 0; overflow: hidden; contain: inline-size; color: var(--uvp-text-primary); }
.recording-schedules-shell { box-sizing: border-box; display: flex; width: 100%; height: 100%; max-width: 100%; min-width: 0; min-height: 0; flex-direction: column; overflow: hidden; }
.recording-schedules-toolbar { display: flex; flex: 0 0 auto; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 10px; }
.schedule-view-switch button { display: inline-flex; align-items: center; gap: 6px; }
.schedule-view-switch button > span { color: var(--uvp-text-tertiary); font-variant-numeric: tabular-nums; }
.schedule-view-switch button.active > span { color: currentColor; }
.toolbar-actions { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.schedule-table-wrap { flex: 1; max-width: 100%; min-width: 0; min-height: 0; overflow: hidden; contain: inline-size; border-radius: 6px; }
.schedule-table-wrap :deep(.uvp-data-table) { height: 100%; min-height: 0; }
.recording-schedules-page :deep(.uvp-search-panel .arco-input-wrapper),
.recording-schedules-page :deep(.uvp-search-panel .arco-select-view) { box-sizing: border-box; background: var(--uvp-search-control-bg) !important; border: 1px solid var(--uvp-search-secondary-btn-border) !important; border-radius: 10px !important; box-shadow: var(--uvp-search-control-shadow) !important; }
.recording-schedules-page :deep(.uvp-search-panel .arco-input-wrapper:focus-within),
.recording-schedules-page :deep(.uvp-search-panel .arco-select-view-focus) { border-color: var(--uvp-brand) !important; box-shadow: var(--uvp-search-control-focus-shadow) !important; }
.recording-schedules-page :deep(.uvp-search-panel .arco-btn),
.recording-schedules-page :deep(.toolbar-actions .arco-btn) { box-sizing: border-box; border-radius: 10px; }
.recording-schedules-page :deep(.uvp-data-table .arco-table-cell) { font-size: 14px; line-height: 22px; }
.schedule-name-cell { display: flex; min-width: 0; align-items: center; gap: 10px; }
.schedule-icon { display: grid; width: 32px; height: 32px; flex: 0 0 auto; color: var(--uvp-brand-strong); background: var(--uvp-brand-soft); border-radius: 8px; place-items: center; }
.schedule-name-cell > div { display: flex; min-width: 0; flex-direction: column; }
.schedule-name-cell strong,
.schedule-name-cell small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.schedule-name-cell strong { font-size: 13px; font-weight: 650; }
.schedule-name-cell small { margin-top: 2px; color: var(--uvp-text-tertiary); font-size: 11px; }
.schedule-time { display: inline-flex; align-items: center; gap: 6px; color: var(--uvp-text-secondary); }
.schedule-time svg { color: var(--uvp-text-tertiary); }
.channel-count-link {
  display: inline-flex;
  padding: 2px 7px;
  color: var(--uvp-brand-strong) !important;
  cursor: pointer;
  font-weight: 650;
  text-decoration: underline;
  text-decoration-style: dotted;
  text-underline-offset: 3px;
  border-radius: 6px;
}
.channel-count-link:hover { background: var(--uvp-brand-soft); text-decoration-style: solid; }
.schedule-actions { flex-wrap: nowrap; white-space: nowrap; }
.execution-workspace { display: flex; flex: 1; min-width: 0; min-height: 0; gap: 12px; }
.execution-plan-panel { box-sizing: border-box; display: flex; width: 276px; min-width: 276px; min-height: 0; flex-direction: column; padding: 16px 14px; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: 12px; }
.execution-plan-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 14px; }
.execution-plan-heading > div { display: flex; min-width: 0; flex-direction: column; }
.execution-plan-heading strong { font-size: 14px; font-weight: 680; }
.execution-plan-heading span { margin-top: 3px; color: var(--uvp-text-tertiary); font-size: 11px; }
.execution-plan-heading em { display: grid; width: 28px; height: 24px; flex: 0 0 auto; color: var(--uvp-brand-strong); font-size: 12px; font-style: normal; font-weight: 680; background: var(--uvp-brand-soft); border-radius: 7px; place-items: center; }
.execution-plan-search { flex: 0 0 auto; margin-bottom: 12px; }
.execution-plan-search :deep(.uvp-search-panel__surface) { display: block; padding: 0; background: transparent; border: 0; border-radius: 0; box-shadow: none; }
.execution-plan-search :deep(.uvp-search-panel__main),
.execution-plan-search :deep(.uvp-search-panel__fields),
.execution-plan-search :deep(.arco-input-wrapper) { width: 100%; }
.execution-plan-list { display: flex; min-height: 0; flex: 1; flex-direction: column; gap: 8px; overflow-y: auto; padding-right: 3px; }
.execution-plan-list-status { flex: 0 0 auto; padding: 8px 4px 2px; color: var(--uvp-text-tertiary); font-size: 11px; text-align: center; }
.execution-plan-item { display: flex; width: 100%; min-width: 0; align-items: flex-start; gap: 10px; padding: 12px; color: var(--uvp-text-primary); text-align: left; background: transparent; border: 1px solid var(--uvp-border-subtle); border-radius: 10px; cursor: pointer; transition: background-color 0.15s ease, border-color 0.15s ease, box-shadow 0.15s ease; }
.execution-plan-item:hover { background: var(--uvp-table-row-hover-bg); border-color: var(--uvp-panel-border); }
.execution-plan-item.active { background: var(--uvp-brand-soft); border-color: color-mix(in srgb, var(--uvp-brand) 42%, transparent); box-shadow: inset 3px 0 0 var(--uvp-brand); }
.execution-plan-icon { display: grid; width: 30px; height: 30px; flex: 0 0 auto; color: var(--uvp-text-tertiary); background: var(--uvp-table-header-bg); border-radius: 8px; place-items: center; }
.execution-plan-item.active .execution-plan-icon { color: var(--uvp-brand-strong); background: var(--uvp-panel-bg); }
.execution-plan-main { display: flex; min-width: 0; flex: 1; flex-direction: column; }
.execution-plan-name { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: 8px; }
.execution-plan-name strong { overflow: hidden; font-size: 13px; font-weight: 650; text-overflow: ellipsis; white-space: nowrap; }
.execution-plan-name i { flex: 0 0 auto; color: var(--uvp-text-tertiary); font-size: 10px; font-style: normal; }
.execution-plan-name i.enabled { color: rgb(var(--green-6)); }
.execution-plan-main > small { overflow: hidden; margin-top: 5px; color: var(--uvp-text-tertiary); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.execution-plan-meta { margin-top: 8px; color: var(--uvp-text-secondary); font-size: 11px; }
.execution-plan-meta b { color: var(--uvp-brand-strong); font-size: 13px; font-weight: 680; }
.execution-channel-panel { display: flex; min-width: 0; min-height: 0; flex: 1; flex-direction: column; overflow: hidden; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: 12px; }
.execution-channel-header { display: flex; flex: 0 0 auto; min-height: 76px; align-items: center; justify-content: space-between; gap: 18px; padding: 14px 18px; background: var(--uvp-search-panel-bg); border-bottom: 1px solid var(--uvp-panel-border); }
.execution-channel-title { display: flex; min-width: 180px; align-items: center; gap: 10px; }
.execution-channel-title > span { display: grid; width: 36px; height: 36px; flex: 0 0 auto; color: var(--uvp-brand-strong); background: var(--uvp-brand-soft); border-radius: 9px; place-items: center; }
.execution-channel-title > div { display: flex; min-width: 0; flex-direction: column; }
.execution-channel-title strong { overflow: hidden; font-size: 15px; font-weight: 680; text-overflow: ellipsis; white-space: nowrap; }
.execution-channel-title small { margin-top: 3px; color: var(--uvp-text-tertiary); font-size: 11px; }
.execution-plan-overview { display: flex; min-width: 0; align-items: stretch; }
.execution-plan-overview > span { display: flex; min-width: 118px; max-width: 230px; flex-direction: column; gap: 4px; padding: 0 16px; border-left: 1px solid var(--uvp-border-subtle); }
.execution-plan-overview small { color: var(--uvp-text-tertiary); font-size: 10px; }
.execution-plan-overview b { overflow: hidden; color: var(--uvp-text-secondary); font-size: 12px; font-weight: 620; text-overflow: ellipsis; white-space: nowrap; }
.execution-plan-overview b.enabled { color: rgb(var(--green-6)); }
.execution-channel-search { flex: 0 0 auto; margin: 12px; }
.execution-channel-table-wrap { min-width: 0; min-height: 0; flex: 1; overflow: hidden; margin: 0 12px 12px; border-radius: 8px; }
.execution-channel-table-wrap :deep(.uvp-data-table) { height: 100%; min-height: 0; }
.execution-legend { display: flex; min-height: 44px; align-items: center; gap: 14px; color: var(--uvp-text-secondary); font-size: 12px; }
.execution-legend span { display: inline-flex; align-items: center; gap: 5px; white-space: nowrap; }
.dot,
.execution-state i { width: 7px; height: 7px; border-radius: 50%; background: var(--uvp-text-disabled); }
.dot.success,
.execution-state.success i { background: rgb(var(--green-6)); }
.dot.warning,
.execution-state.warning i { background: rgb(var(--orange-6)); }
.entity-cell { display: flex; min-width: 0; flex-direction: column; }
.entity-cell span,
.entity-cell small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.entity-cell small { margin-top: 2px; color: var(--uvp-text-tertiary); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 10px; }
.match-state { color: var(--uvp-text-tertiary); }
.match-state.matched { color: var(--uvp-brand-strong); font-weight: 620; }
.execution-state { display: inline-flex; align-items: center; gap: 6px; color: var(--uvp-text-secondary); }
.execution-state.success { color: rgb(var(--green-6)); }
.execution-state.warning,
.error-text { color: var(--uvp-warning); }
@media (max-width: 768px) {
  .recording-schedules-toolbar { align-items: stretch; flex-wrap: wrap; gap: 10px; }
  .schedule-view-switch { flex: 1 0 auto; }
  .schedule-view-switch button { flex: 1; justify-content: center; }
  .toolbar-actions { width: 100%; justify-content: flex-end; }
  .recording-schedules-page :deep(.uvp-search-panel__fields),
  .recording-schedules-page :deep(.uvp-search-panel__fields > *) { width: 100% !important; }
  .execution-legend { display: none; }
  .execution-workspace { overflow-y: auto; flex-direction: column; }
  .execution-plan-panel { width: auto; min-width: 0; min-height: 210px; flex: 0 0 auto; }
  .execution-plan-list { flex-direction: row; overflow-x: auto; overflow-y: hidden; }
  .execution-plan-item { width: 230px; flex: 0 0 230px; }
  .execution-channel-panel { min-height: 560px; flex: 0 0 auto; }
  .execution-channel-header { align-items: flex-start; flex-direction: column; }
  .execution-plan-overview { width: 100%; }
  .execution-plan-overview > span { min-width: 0; flex: 1; padding: 0 10px; }
  .execution-plan-overview > span:first-child { padding-left: 0; border-left: 0; }
}
</style>
