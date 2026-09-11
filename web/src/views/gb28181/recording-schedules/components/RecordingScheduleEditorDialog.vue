<template>
  <a-modal
    :visible="visible"
    :width="'min(1360px, 98vw)'"
    modal-class="uvp-system-dialog schedule-editor-dialog"
    :modal-style="{ maxHeight: 'calc(100vh - 48px)' }"
    :body-style="{ maxHeight: 'calc(100vh - 176px)', overflowY: 'auto' }"
    :mask-closable="false"
    :esc-to-close="true"
    unmount-on-close
    @update:visible="emit('update:visible', $event)"
    @cancel="close"
    @ok="save"
  >
    <template #title>
      <div class="schedule-editor-title">
        <span><CalendarClock :size="18" /></span>
        <div><strong>{{ mode === "create" ? "新建录像计划" : "编辑录像计划" }}</strong><small>以半小时为最小粒度配置每周录像时段</small></div>
      </div>
    </template>

    <div class="schedule-editor-content">
      <a-form :model="form" layout="vertical" class="schedule-editor-form">
        <div class="basic-form-grid">
          <a-form-item label="计划名称" required>
            <a-input v-model="form.name" allow-clear placeholder="例如：工作日全天" :max-length="40" show-word-limit />
          </a-form-item>
          <a-form-item label="计划状态">
            <div class="enabled-field"><a-switch v-model="form.enabled" /><span>{{ form.enabled ? "已启用" : "已停用" }}</span></div>
          </a-form-item>
          <a-form-item label="计划说明">
            <a-input v-model="form.description" allow-clear placeholder="说明适用区域或业务场景" :max-length="120" />
          </a-form-item>
        </div>
      </a-form>

      <section class="slot-editor-section">
        <div class="slot-editor-heading">
          <div>
            <h3>每周录像时段</h3>
            <p>单击方格切换；按住鼠标拖动可连续涂抹或擦除。</p>
          </div>
          <div class="slot-editor-meta">
            <span><i />蓝色表示录像</span>
            <strong>已选择 {{ selectedSlotCount }} 格 · {{ selectedHours }} 小时</strong>
            <a-button class="clear-all-button" size="small" type="primary" status="danger" :disabled="selectedSlotCount === 0" @click="clearAllDays">一键清空</a-button>
          </div>
        </div>

        <WeeklyScheduleGrid :slots="weekSlots" editable @update:slots="updateWeekSlots" />

        <div class="selected-period-summary">
          <div class="selected-period-summary-title">
            <Clock3 :size="15" />
            <div><strong>已选时间区间</strong><small>{{ selectedPeriodGroups.length }} 天 · {{ selectedPeriodCount }} 段</small></div>
          </div>
          <div v-if="selectedPeriodGroups.length === 0" class="selected-period-empty">暂未选择录像时段</div>
          <div v-else class="selected-period-groups">
            <div v-for="group in selectedPeriodGroups" :key="group.name" class="selected-period-group">
              <strong>{{ group.name }}</strong>
              <div>
                <span v-for="period in group.periods" :key="`${period.start}-${period.end}`" class="selected-period-chip">{{ period.start }}–{{ period.end }}</span>
              </div>
            </div>
          </div>
        </div>

        <div class="slot-scale-hint"><Clock3 :size="14" />每格 30 分钟，计划边界只允许落在整点或半点。</div>
      </section>
    </div>

    <template #footer>
      <a-space>
        <a-button @click="close">取消</a-button>
        <a-button type="primary" @click="save"><template #icon><Save :size="15" /></template>保存计划</a-button>
      </a-space>
    </template>
  </a-modal>
</template>

<script setup lang="ts">
import { computed, reactive, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { CalendarClock, Clock3, Save } from "@lucide/vue";
import WeeklyScheduleGrid from "./WeeklyScheduleGrid.vue";
import type { RecordingSchedule, ScheduleDay, SchedulePeriod } from "../types";

type EditorMode = "create" | "edit";
const SLOT_MINUTES = 30;
const SLOTS_PER_DAY = (24 * 60) / SLOT_MINUTES;
const dayNames = ["周一", "周二", "周三", "周四", "周五", "周六", "周日"];

const props = defineProps<{ visible: boolean; mode: EditorMode; plan: RecordingSchedule | null }>();
const emit = defineEmits<{
  (event: "update:visible", value: boolean): void;
  (event: "save", plan: RecordingSchedule): void;
}>();

const emptyPlan = (): RecordingSchedule => ({
  id: "",
  name: "",
  enabled: true,
  description: "",
  cycle: "未配置",
  timeSummary: "—",
  channelCount: 0,
  updatedAt: "刚刚",
  current: "等待计划首次执行",
  nextChange: "保存后计算",
  days: dayNames.map(name => ({ name, enabled: false, periods: [] }))
});
const clone = (value: RecordingSchedule) => JSON.parse(JSON.stringify(value)) as RecordingSchedule;
const form = reactive<RecordingSchedule>(emptyPlan());
const weekSlots = reactive<boolean[][]>(dayNames.map(() => Array.from({ length: SLOTS_PER_DAY }, () => false)));

const selectedSlotCount = computed(() => weekSlots.reduce((total, day) => total + day.filter(Boolean).length, 0));
const selectedHours = computed(() => Number((selectedSlotCount.value * SLOT_MINUTES / 60).toFixed(1)));
const selectedPeriodGroups = computed(() => dayNames
  .map((name, dayIndex) => ({ name, periods: slotsToPeriods(weekSlots[dayIndex]) }))
  .filter(group => group.periods.length > 0));
const selectedPeriodCount = computed(() => selectedPeriodGroups.value.reduce((total, group) => total + group.periods.length, 0));

function syncEditor() {
  const source = clone(props.plan || emptyPlan());
  Object.assign(form, source);
  weekSlots.forEach(daySlots => daySlots.fill(false));
  source.days.forEach((day, dayIndex) => {
    day.periods.forEach(period => applyPeriod(dayIndex, period));
  });
}

function close() {
  emit("update:visible", false);
}

function updateWeekSlots(nextSlots: boolean[][]) {
  weekSlots.splice(0, weekSlots.length, ...nextSlots.map(daySlots => [...daySlots]));
}

function clearAllDays() {
  if (selectedSlotCount.value === 0) return;
  weekSlots.forEach(daySlots => daySlots.fill(false));
  Message.success("已清空全部录像时段");
}

function save() {
  if (!form.name.trim()) {
    Message.warning("请输入计划名称");
    return;
  }
  if (form.enabled && selectedSlotCount.value === 0) {
    Message.warning("启用计划至少需要选择一个录像时段");
    return;
  }
  const saved = clone(form);
  saved.id ||= `schedule-${Date.now()}`;
  saved.days = dayNames.map((name, dayIndex) => {
    const periods = slotsToPeriods(weekSlots[dayIndex]);
    return { name, enabled: periods.length > 0, periods };
  });
  saved.cycle = cycleLabel(saved.days);
  saved.timeSummary = timeSummary(saved.days);
  saved.updatedAt = "刚刚";
  emit("save", saved);
}

function applyPeriod(dayIndex: number, period: SchedulePeriod) {
  const start = timeToSlot(period.start);
  const end = timeToSlot(period.end);
  if (period.nextDay || end <= start) {
    for (let index = start; index < SLOTS_PER_DAY; index += 1) weekSlots[dayIndex][index] = true;
    const nextDayIndex = (dayIndex + 1) % dayNames.length;
    for (let index = 0; index < end; index += 1) weekSlots[nextDayIndex][index] = true;
    return;
  }
  for (let index = start; index < end; index += 1) weekSlots[dayIndex][index] = true;
}

function slotsToPeriods(daySlots: boolean[]): SchedulePeriod[] {
  const runs: Array<[number, number]> = [];
  let start = -1;
  daySlots.forEach((active, index) => {
    if (active && start < 0) start = index;
    if (!active && start >= 0) {
      runs.push([start, index]);
      start = -1;
    }
  });
  if (start >= 0) runs.push([start, SLOTS_PER_DAY]);
  return runs.map(([from, to]) => ({
    start: slotToTime(from),
    end: slotToTime(to)
  }));
}

function timeToSlot(value: string) {
  const [hour = "0", minute = "0"] = value.split(":");
  return Math.min(SLOTS_PER_DAY, Math.max(0, (Number(hour) * 60 + Number(minute)) / SLOT_MINUTES));
}

function slotToTime(slot: number) {
  const totalMinutes = slot * SLOT_MINUTES;
  const hour = Math.floor(totalMinutes / 60);
  const minute = totalMinutes % 60;
  return `${String(hour).padStart(2, "0")}:${String(minute).padStart(2, "0")}`;
}


function cycleLabel(days: ScheduleDay[]) {
  const enabled = days.filter(day => day.enabled).map(day => day.name);
  if (enabled.length === 7) return "每天";
  if (enabled.join() === "周一周二周三周四周五") return "周一至周五";
  if (enabled.join() === "周六周日") return "周末";
  return enabled.join("、") || "未配置";
}

function periodLabel(day: ScheduleDay) {
  return day.periods.map(period => `${period.start}-${period.nextDay ? "次日 " : ""}${period.end}`).join("，");
}

function timeSummary(days: ScheduleDay[]) {
  const first = days.find(day => day.enabled);
  return first ? periodLabel(first) : "—";
}

watch(() => [props.visible, props.mode, props.plan] as const, ([visible]) => { if (visible) syncEditor(); }, { immediate: true, deep: true });
</script>

<style scoped lang="scss">
.schedule-editor-title { display: flex; align-items: center; gap: 10px; min-width: 0; }
.schedule-editor-title > span { display: grid; width: 34px; height: 34px; flex: 0 0 auto; color: var(--uvp-brand-strong); background: var(--uvp-brand-soft); border-radius: 8px; place-items: center; }
.schedule-editor-title > div { display: flex; min-width: 0; flex-direction: column; }
.schedule-editor-title strong { font-size: 15px; }
.schedule-editor-title small { color: var(--uvp-text-tertiary); font-size: 12px; }
.schedule-editor-content { color: var(--uvp-text-primary); }
.basic-form-grid { display: grid; grid-template-columns: minmax(260px, 1fr) 150px minmax(300px, 1.35fr); gap: 14px; }
.schedule-editor-form :deep(.arco-form-item) { margin-bottom: 12px; }
.schedule-editor-form :deep(.arco-input-wrapper) { min-height: 38px; border-radius: 8px; }
.enabled-field { display: flex; min-height: 38px; align-items: center; gap: 8px; color: var(--uvp-text-secondary); }
.slot-editor-section { padding-top: 14px; border-top: 1px solid var(--uvp-border-subtle); }
.slot-editor-heading { display: flex; align-items: flex-end; justify-content: space-between; gap: 16px; margin-bottom: 12px; }
.slot-editor-heading h3 { margin: 0; font-size: 14px; }
.slot-editor-heading p { margin: 4px 0 0; color: var(--uvp-text-tertiary); font-size: 12px; }
.slot-editor-meta { display: flex; align-items: center; gap: 14px; color: var(--uvp-text-secondary); font-size: 12px; white-space: nowrap; }
.slot-editor-meta span { display: inline-flex; align-items: center; gap: 5px; }
.slot-editor-meta i { width: 12px; height: 12px; background: var(--uvp-brand); border-radius: 3px; }
.slot-editor-meta strong { color: var(--uvp-brand-strong); }
.selected-period-summary { display: grid; height: 148px; box-sizing: border-box; grid-template-columns: 130px minmax(0, 1fr); gap: 12px; margin-top: 12px; overflow: hidden; padding: 12px 14px; background: var(--uvp-table-header-bg); border: 1px solid var(--color-border-2); border-radius: 8px; }
.selected-period-summary-title { display: flex; align-items: flex-start; gap: 7px; color: var(--uvp-text-primary); font-size: 12px; line-height: 24px; }
.selected-period-summary-title > div { display: flex; flex-direction: column; line-height: 18px; }
.selected-period-summary-title small { margin-top: 2px; color: var(--uvp-text-tertiary); font-size: 11px; font-weight: 400; }
.selected-period-empty { color: var(--uvp-text-tertiary); font-size: 12px; line-height: 24px; }
.selected-period-groups { display: flex; min-width: 0; flex-direction: column; gap: 8px; overflow-y: auto; padding-right: 4px; }
.selected-period-group { display: grid; grid-template-columns: 42px minmax(0, 1fr); align-items: start; gap: 8px; }
.selected-period-group > strong { color: var(--uvp-text-secondary); font-size: 12px; line-height: 24px; }
.selected-period-group > div { display: flex; flex-wrap: wrap; gap: 6px; }
.selected-period-chip { display: inline-flex; align-items: center; min-height: 24px; padding: 0 8px; color: var(--uvp-brand-strong); background: var(--uvp-brand-soft); border: 1px solid color-mix(in srgb, var(--uvp-brand) 28%, transparent); border-radius: 6px; font-size: 11px; font-variant-numeric: tabular-nums; }
.slot-scale-hint { display: flex; align-items: center; gap: 6px; margin-top: 10px; color: var(--uvp-text-tertiary); font-size: 12px; }
@media (max-width: 820px) {
  .basic-form-grid { grid-template-columns: 1fr; gap: 0; }
  .slot-editor-heading { align-items: flex-start; flex-direction: column; }
  .slot-editor-meta { flex-wrap: wrap; white-space: normal; }
  .selected-period-summary { grid-template-columns: 1fr; }
}
</style>
