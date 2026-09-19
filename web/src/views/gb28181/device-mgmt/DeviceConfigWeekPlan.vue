<script setup lang="ts">
/**
 * DeviceConfigWeekPlan - 录像计划的「周几 → 时段」编辑器（`VideoRecordPlan/Schedule`）
 *
 * 形态：7 行（周一 ~ 周日），每行一个开关 + 若干时段。
 *
 * ## 三条不能"顺手简化"的口径
 *
 * 1. ⛔ **关掉一天 = 该天整条不发**，不是发一条"0 秒时段"。标准原文
 *    「如当天无录像计划可缺少」——两者的线格式与对端行为都不同。
 *    所以这里的关 = 把该天的 `segments` 清空（构建侧见到空就跳过整天）。
 * 2. ⛔ 时段保留到**秒**。协议里 `StartSec` / `StopSec` 都是必选整数；
 *    界面只让填到分钟，就等于每次下发把设备上原有的秒清零 ——
 *    静默改写，而且回读对账还会显示"一致"（因为比的是平台发出去的值）。
 * 3. 新建时段给 `00:00:00 ~ 23:59:59`（全天）：这是安防场景最常见的计划，
 *    而不是随手的 `00:00~00:00` 空区间 —— 那种默认值一发出去就是"零长时段"。
 */
import { CalendarPlus, Plus, Trash2 } from "@lucide/vue";
import { WEEKDAY_LABELS, type ConfigScheduleDay } from "./deviceConfigGroups";

const props = withDefaults(
  defineProps<{
    modelValue: ConfigScheduleDay[];
    disabled?: boolean;
  }>(),
  { disabled: false }
);

const emit = defineEmits<{ "update:modelValue": [value: ConfigScheduleDay[]] }>();

/** 全天的起止，作为新增时段的默认值。 */
const FULL_DAY_START = "00:00:00";
const FULL_DAY_STOP = "23:59:59";

/**
 * 把 props 折成"7 天都在"的完整视图。
 *
 * ⛔ 补齐缺的天而不是只渲染回读到的天：设备只回了周三时，界面上必须还能看到
 *    周一/周二那两行（且是"未启用"），否则用户根本没法给那两天加计划 ——
 *    缺行看起来像"这个界面不支持"。
 */
function days(): ConfigScheduleDay[] {
  const source = Array.isArray(props.modelValue) ? props.modelValue : [];
  return WEEKDAY_LABELS.map((_, index) => {
    const weekDayNum = index + 1;
    const matched = source.find(day => Number(day?.weekDayNum) === weekDayNum);
    return {
      weekDayNum,
      segments: (matched?.segments ?? []).map(segment => ({ ...segment }))
    };
  });
}

function commit(next: ConfigScheduleDay[]) {
  // 只发"有时段的天"：空天留在数组里会让脏值统计和下发报文都多出无意义条目。
  emit(
    "update:modelValue",
    next.filter(day => day.segments.length > 0)
  );
}

function toggleDay(weekDayNum: number, enabled: boolean) {
  const next = days();
  const target = next.find(day => day.weekDayNum === weekDayNum);
  if (!target) return;
  target.segments = enabled ? [{ start: FULL_DAY_START, stop: FULL_DAY_STOP }] : [];
  commit(next);
}

function addSegment(weekDayNum: number) {
  const next = days();
  const target = next.find(day => day.weekDayNum === weekDayNum);
  if (!target) return;
  target.segments = [...target.segments, { start: FULL_DAY_START, stop: FULL_DAY_STOP }];
  commit(next);
}

function removeSegment(weekDayNum: number, index: number) {
  const next = days();
  const target = next.find(day => day.weekDayNum === weekDayNum);
  if (!target) return;
  target.segments = target.segments.filter((_, position) => position !== index);
  commit(next);
}

function setSegment(weekDayNum: number, index: number, edge: "start" | "stop", raw: string) {
  const next = days();
  const target = next.find(day => day.weekDayNum === weekDayNum);
  const segment = target?.segments[index];
  if (!target || !segment) return;
  segment[edge] = raw;
  commit(next);
}

/** 汇总：已配置几天、共几个时段（页脚用）。 */
function summary(): string {
  const configured = days().filter(day => day.segments.length > 0);
  const segments = configured.reduce((total, day) => total + day.segments.length, 0);
  return `${configured.length} 天 · ${segments} 个时段`;
}
</script>

<template>
  <div class="dcw" :class="{ 'is-disabled': disabled }">
    <div v-for="day in days()" :key="day.weekDayNum" class="dcw-day" :data-testid="`dcw-day-${day.weekDayNum}`">
      <button
        type="button"
        class="dcw-switch"
        :class="{ 'is-on': day.segments.length > 0 }"
        :disabled="disabled"
        :aria-label="`周${WEEKDAY_LABELS[day.weekDayNum - 1]}录像计划`"
        :data-testid="`dcw-toggle-${day.weekDayNum}`"
        @click="toggleDay(day.weekDayNum, day.segments.length === 0)"
      >
        <i />
      </button>
      <span class="dcw-label">周{{ WEEKDAY_LABELS[day.weekDayNum - 1] }}</span>

      <div class="dcw-segments">
        <span v-if="!day.segments.length" class="dcw-none">无录像计划（该天整条不下发）</span>
        <div v-for="(segment, index) in day.segments" :key="index" class="dcw-segment">
          <input
            class="dcw-time"
            type="text"
            :value="segment.start"
            :disabled="disabled"
            placeholder="HH:MM:SS"
            :aria-label="`周${WEEKDAY_LABELS[day.weekDayNum - 1]}第 ${index + 1} 段开始`"
            :data-testid="`dcw-start-${day.weekDayNum}-${index}`"
            @change="setSegment(day.weekDayNum, index, 'start', ($event.target as HTMLInputElement).value)"
          />
          <span class="dcw-dash">→</span>
          <input
            class="dcw-time"
            type="text"
            :value="segment.stop"
            :disabled="disabled"
            placeholder="HH:MM:SS"
            :aria-label="`周${WEEKDAY_LABELS[day.weekDayNum - 1]}第 ${index + 1} 段结束`"
            :data-testid="`dcw-stop-${day.weekDayNum}-${index}`"
            @change="setSegment(day.weekDayNum, index, 'stop', ($event.target as HTMLInputElement).value)"
          />
          <button
            type="button"
            class="dcw-icon"
            :disabled="disabled"
            :aria-label="`删除周${WEEKDAY_LABELS[day.weekDayNum - 1]}第 ${index + 1} 段`"
            @click="removeSegment(day.weekDayNum, index)"
          >
            <Trash2 :size="11" />
          </button>
        </div>
        <button
          type="button"
          class="dcw-add"
          :disabled="disabled"
          :data-testid="`dcw-add-${day.weekDayNum}`"
          @click="addSegment(day.weekDayNum)"
        >
          <Plus :size="10" />时段
        </button>
      </div>
    </div>
    <p class="dcw-foot">
      <CalendarPlus :size="10" />
      <span data-testid="dcw-summary">{{ summary() }}</span>
      <span class="dcw-foot-sep">·</span>
      <span>时刻按 HH:MM:SS 下发；关掉某天表示该天没有录像计划（整条不下发）</span>
    </p>
  </div>
</template>

<style scoped lang="scss">
.dcw {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  gap: 4px;
  min-width: 0;

  &.is-disabled {
    opacity: 0.55;
  }
}

.dcw-day {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  min-height: 24px;
}

.dcw-switch {
  display: inline-flex;
  flex: none;
  align-items: center;
  width: 26px;
  height: 14px;
  padding: 0 2px;
  margin-top: 4px;
  cursor: pointer;
  background: var(--uvp-panel-border, #d5dfee);
  border: none;
  border-radius: 7px;
  transition: background 0.15s;

  i {
    width: 10px;
    height: 10px;
    background: #ffffff;
    border-radius: 50%;
    transition: transform 0.15s;
  }

  &.is-on {
    background: var(--uvp-brand);

    i {
      transform: translateX(12px);
    }
  }

  &:disabled {
    cursor: not-allowed;
    opacity: 0.5;
  }
}

.dcw-label {
  flex: none;
  width: 26px;
  margin-top: 2px;
  font-size: 12px;
  color: var(--uvp-text-secondary);
}

.dcw-segments {
  display: flex;
  flex: 1 1 auto;
  flex-wrap: wrap;
  gap: 5px;
  align-items: center;
  min-width: 0;
}

.dcw-none {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}

.dcw-segment {
  display: inline-flex;
  gap: 3px;
  align-items: center;
}

.dcw-time {
  width: 70px;
  height: 22px;
  padding: 0 5px;
  font-size: 12px;
  color: var(--uvp-text-primary);
  text-align: center;
  background: var(--uvp-dialog-control-bg, #ffffff);
  border: 1px solid var(--uvp-panel-border, #dbe4f0);
  border-radius: 4px;

  &:focus {
    outline: none;
    border-color: var(--uvp-brand);
  }
}

.dcw-dash {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}

.dcw-icon,
.dcw-add {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  height: 22px;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: var(--uvp-dialog-control-bg, #f8fbff);
  border: 1px solid var(--uvp-panel-border, #dbe4f0);
  border-radius: 4px;

  &:hover:not(:disabled) {
    color: var(--uvp-brand);
    border-color: var(--uvp-brand);
  }

  &:disabled {
    color: var(--uvp-text-tertiary);
    cursor: not-allowed;
    opacity: 0.45;
  }
}

.dcw-icon {
  width: 22px;
  padding: 0;
}

.dcw-add {
  gap: 2px;
  padding: 0 7px;
  font-size: 11px;
}

.dcw-foot {
  display: flex;
  gap: 5px;
  align-items: center;
  margin: 3px 0 0;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
}

.dcw-foot-sep {
  opacity: 0.6;
}
</style>
