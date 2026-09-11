<template>
  <a-modal
    :visible="visible"
    :width="'min(1000px, 96vw)'"
    modal-class="uvp-system-dialog recording-schedule-detail-dialog"
    align-center
    :modal-style="{
      height: 'min(820px, calc(100vh - 48px))',
      maxHeight: 'calc(100vh - 48px)',
      display: 'flex',
      flexDirection: 'column'
    }"
    :body-style="{ flex: '1 1 auto', minHeight: 0, maxHeight: 'none', overflowY: 'auto' }"
    :mask-closable="false"
    :esc-to-close="true"
    unmount-on-close
    @update:visible="emit('update:visible', $event)"
    @cancel="emit('update:visible', false)"
  >
    <template #title>
      <div class="schedule-drawer-title">
        <span><CalendarClock :size="18" /></span>
        <div><strong>计划详情</strong><small>{{ schedule.name }}</small></div>
      </div>
    </template>

    <div class="schedule-drawer-content">
      <section class="schedule-summary-strip">
        <div><span>状态</span><a-tag :color="schedule.enabled ? 'green' : 'gray'">{{ schedule.enabled ? "已启用" : "已停用" }}</a-tag></div>
        <div><span>执行周期</span><strong>{{ schedule.cycle }}</strong></div>
        <div><span>应用通道</span><strong>{{ schedule.channelCount }} 个</strong></div>
      </section>

      <a-tabs v-model:active-key="activeDetailTab" class="schedule-detail-tabs">
        <a-tab-pane key="schedule">
          <template #title><span class="detail-tab-label"><CalendarClock :size="14" />录像配置</span></template>

          <section class="schedule-drawer-section schedule-current-state">
            <div><span>当前时段</span><strong>{{ schedule.current }}</strong></div>
            <div><span>下次切换</span><strong>{{ schedule.nextChange }}</strong></div>
          </section>

          <section class="schedule-drawer-section weekly-schedule-section">
            <div class="section-title"><div><h3>每周时段</h3><span>蓝色区域表示计划录像时间</span></div></div>
            <WeeklyScheduleGrid :slots="detailWeekSlots" :editable="false" />
          </section>
        </a-tab-pane>

        <a-tab-pane key="channels">
          <template #title><span class="detail-tab-label"><Link2 :size="14" />已分配通道</span></template>

          <section class="schedule-drawer-section assigned-channel-section">
            <div class="section-title">
              <div><h3>已分配通道</h3><span>以下通道的录像模式为“按计划”</span></div>
            </div>
			<a-table class="uvp-data-table assigned-channel-table" :data="assignedChannels" :loading="channelsLoading" :pagination="assignedChannelPagination" :bordered="false" :scroll="{ y: 360 }" @page-change="emit('channelPageChange', $event)">
              <template #columns>
                <a-table-column title="通道" :width="190"><template #cell="{ record }"><div class="entity-cell"><span>{{ record.name }}</span><small>{{ record.code }}</small></div></template></a-table-column>
                <a-table-column title="所属设备" :width="170" data-index="device" />
                <a-table-column title="实际状态" :width="128"><template #cell="{ record }"><a-tag :color="stateColor(record.state)">{{ record.state }}</a-tag></template></a-table-column>
                <a-table-column title="下次切换" data-index="next" />
              </template>
            </a-table>
          </section>
        </a-tab-pane>
      </a-tabs>

    </div>

    <template #footer>
      <div class="detail-actions">
        <a-button type="primary" @click="emit('update:visible', false)">关闭</a-button>
      </div>
    </template>
  </a-modal>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { CalendarClock, Link2 } from "@lucide/vue";
import WeeklyScheduleGrid from "./WeeklyScheduleGrid.vue";
import type { RecordingSchedule, ScheduleChannel } from "../types";

const props = withDefaults(defineProps<{ visible: boolean; mode: "detail"; plan: RecordingSchedule | null; assignedChannels?: ScheduleChannel[]; channelsLoading?: boolean; channelTotal?: number; channelPage?: number }>(), {
  assignedChannels: () => [], channelsLoading: false, channelTotal: 0, channelPage: 1
});
const emit = defineEmits<{
  (event: "update:visible", value: boolean): void;
	(event: "channelPageChange", value: number): void;
}>();

const emptySchedule: RecordingSchedule = {
  id: "",
  name: "—",
  enabled: false,
  description: "",
  cycle: "未配置",
  timeSummary: "—",
  channelCount: 0,
  updatedAt: "—",
  current: "—",
  nextChange: "—",
  days: ["周一", "周二", "周三", "周四", "周五", "周六", "周日"].map(name => ({ name, enabled: false, periods: [] }))
};
const schedule = computed(() => props.plan || emptySchedule);
const activeDetailTab = ref<"schedule" | "channels">("schedule");
const detailWeekSlots = computed(() => {
  const slots = Array.from({ length: 7 }, () => Array.from({ length: 48 }, () => false));
  schedule.value.days.forEach((day, dayIndex) => {
    day.periods.forEach(period => {
      const start = timeToSlot(period.start);
      const end = timeToSlot(period.end);
      if (period.nextDay || end <= start) {
        for (let index = start; index < 48; index += 1) slots[dayIndex][index] = true;
        const nextDayIndex = (dayIndex + 1) % slots.length;
        for (let index = 0; index < end; index += 1) slots[nextDayIndex][index] = true;
        return;
      }
      for (let index = start; index < end; index += 1) slots[dayIndex][index] = true;
    });
  });
  return slots;
});
const assignedChannels = computed(() => props.assignedChannels);
const channelsLoading = computed(() => props.channelsLoading);
const assignedChannelPagination = computed(() => ({ current: props.channelPage, pageSize: 10, total: props.channelTotal, showTotal: true, showJumper: true }));

watch(() => props.visible, visible => {
  if (visible) activeDetailTab.value = "schedule";
});

function timeToSlot(value: string) {
  const [hour = "0", minute = "0"] = value.split(":");
  return Math.min(48, Math.max(0, (Number(hour) * 60 + Number(minute)) / 30));
}

function stateColor(state: string) {
  return state === "录像中" ? "green" : state === "等待设备上线" ? "orange" : "gray";
}
</script>

<style scoped lang="scss">
.schedule-drawer-content { display: flex; height: 100%; min-height: 0; flex-direction: column; color: var(--uvp-text-primary); }
.schedule-drawer-title { display: flex; align-items: center; gap: 10px; min-width: 0; }
.schedule-drawer-title > span { display: grid; width: 34px; height: 34px; flex: 0 0 auto; color: var(--uvp-brand-strong); background: var(--uvp-brand-soft); border-radius: 8px; place-items: center; }
.schedule-drawer-title > div { display: flex; min-width: 0; flex-direction: column; }
.schedule-drawer-title strong { font-size: 15px; }
.schedule-drawer-title small { overflow: hidden; color: var(--uvp-text-tertiary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.schedule-summary-strip { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); margin: -22px -24px 0; padding: 15px 24px; background: var(--uvp-search-panel-bg); border-bottom: 1px solid var(--uvp-panel-border); }
.schedule-summary-strip > div { display: flex; min-width: 0; flex-direction: column; gap: 5px; padding: 0 14px; border-right: 1px solid var(--uvp-border-subtle); }
.schedule-summary-strip > div:first-child { padding-left: 0; }
.schedule-summary-strip > div:last-child { border-right: 0; }
.schedule-summary-strip span { color: var(--uvp-text-tertiary); font-size: 11px; }
.schedule-summary-strip strong { overflow: hidden; color: var(--uvp-text-primary); font-size: 13px; text-overflow: ellipsis; white-space: nowrap; }
.schedule-detail-tabs { flex: 1; min-height: 0; margin-top: 10px; }
.schedule-detail-tabs :deep(.arco-tabs-nav) { margin-bottom: 0; }
.schedule-detail-tabs :deep(.arco-tabs-content) { min-height: 0; }
.detail-tab-label { display: inline-flex; align-items: center; gap: 6px; }
.schedule-drawer-section { padding: 14px 0 0; border-bottom: 0; }
.schedule-current-state { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); padding: 10px 14px; margin-top: 12px; background: var(--uvp-brand-soft); border: 1px solid color-mix(in srgb, var(--uvp-brand) 16%, transparent); border-radius: 10px; }
.schedule-current-state > div { display: flex; flex-direction: column; gap: 4px; }
.schedule-current-state span { color: var(--uvp-text-tertiary); font-size: 11px; }
.schedule-current-state strong { color: var(--uvp-brand-strong); font-size: 13px; }
.section-title { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 14px; }
.section-title h3 { margin: 0; font-size: 14px; font-weight: 650; }
.section-title span { display: block; margin-top: 3px; color: var(--uvp-text-tertiary); font-size: 12px; }
.assigned-channel-section { padding-top: 16px; }
.assigned-channel-table { min-height: auto; }
.entity-cell { display: flex; min-width: 0; flex-direction: column; }
.entity-cell span,
.entity-cell small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.entity-cell small { margin-top: 2px; color: var(--uvp-text-tertiary); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 10px; }
.detail-actions { display: flex; width: 100%; justify-content: flex-end; gap: 10px; }
@media (max-width: 720px) {
  .schedule-summary-strip { grid-template-columns: 1fr; gap: 10px; }
  .schedule-summary-strip > div { padding: 0; border-right: 0; }
  .schedule-current-state { grid-template-columns: 1fr; gap: 10px; }
}
</style>

<style lang="scss">
.arco-modal-wrapper:has(> .recording-schedule-detail-dialog) {
  overflow: hidden;
}
</style>
