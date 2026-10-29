<script setup lang="ts">
/**
 * DeviceConfigWeekPlan - 录像计划的「周几 → 时段」编辑器（`VideoRecordPlan/Schedule`）
 *
 * 形态：7 行（周一 ~ 周日），每行一条 0~24 小时时间轴 + 该天的时段编辑条。
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
 *
 * ## 为什么从「纯输入框」改成「时间轴 + 输入条」
 *
 * ⛔ 原来 7 行 × 每段两个输入框，只能"读数字"：一眼看不出哪天没排、哪天压了两段、
 *    现在这一刻在不在录。轴是**给人扫的**，输入框是**给人改的**，两者都要，不是二选一。
 * ⛔ 时间轴这一版**不做拖拽**：拖拽要处理碰撞、吸附、最小粒度，成本翻倍，而现场多数是
 *    "复制一份改两处"。点空白处新增一段（全天）、点色块把焦点跳到对应的起止输入框，够用。
 */
import { computed } from "vue";
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
const viewDays = computed<ConfigScheduleDay[]>(() => {
  const source = Array.isArray(props.modelValue) ? props.modelValue : [];
  return WEEKDAY_LABELS.map((_, index) => {
    const weekDayNum = index + 1;
    const matched = source.find(day => Number(day?.weekDayNum) === weekDayNum);
    return {
      weekDayNum,
      segments: (matched?.segments ?? []).map(segment => ({ ...segment }))
    };
  });
});

/**
 * 今天是周几（1=周一 … 7=周日）。
 *
 * ⛔ `getDay()` 里周日是 0，协议里是 7，这个换算漏了就会把"今天"标到周一那一行，
 *    而周计划恰恰是拿来判断"此刻在不在录"的，标错等于给错答案。
 */
const todayWeekDay = computed(() => {
  const day = new Date().getDay();
  return day === 0 ? 7 : day;
});

/** 当前时刻在一天里的百分比位置（画"现在"竖线用）。 */
const nowPercent = computed(() => {
  const now = new Date();
  return ((now.getHours() * 3600 + now.getMinutes() * 60 + now.getSeconds()) / 86400) * 100;
});

/** `HH:MM:SS` → 一天里的百分比。格式不认识时按 0 处理，让色块退化成一个点而不是崩掉。 */
function percentOf(time: string | undefined): number {
  const match = /^(\d{1,2}):(\d{1,2})(?::(\d{1,2}))?$/.exec((time ?? "").trim());
  if (!match) return 0;
  const seconds = Number(match[1]) * 3600 + Number(match[2]) * 60 + Number(match[3] ?? 0);
  if (!Number.isFinite(seconds)) return 0;
  return Math.min(100, Math.max(0, (seconds / 86400) * 100));
}

/**
 * 一段的几何（left / width 百分比）。
 *
 * ⛔ 宽度给一个 0.4% 的下限：一整段被删到只剩几秒时，按比例算出来不足 1px，
 *    看起来像"这段没了"，实际还在、还会下发。留一条头发丝，人才知道要回去改。
 * ⛔ 结束早于开始（填反了）时不画成负宽：取 0 宽，靠下面的输入框红边提示，不在这里假装正常。
 */
function segmentStyle(segment: { start: string; stop: string }) {
  const start = percentOf(segment.start);
  const stop = percentOf(segment.stop);
  const width = Math.max(stop > start ? stop - start : 0, 0.4);
  return { left: `${start}%`, width: `${width}%` };
}

/** 段内标签只在色块够宽时才显示，窄段显示时间反而糊成一团。 */
function segmentLabel(segment: { start: string; stop: string }) {
  return `${(segment.start || "--").slice(0, 5)}-${(segment.stop || "--").slice(0, 5)}`;
}

function commit(next: ConfigScheduleDay[]) {
  // 只发"有时段的天"：空天留在数组里会让脏值统计和下发报文都多出无意义条目。
  emit(
    "update:modelValue",
    next.filter(day => day.segments.length > 0)
  );
}

function toggleDay(weekDayNum: number, enabled: boolean) {
  const next = viewDays.value.map(day => ({ ...day, segments: [...day.segments] }));
  const target = next.find(day => day.weekDayNum === weekDayNum);
  if (!target) return;
  target.segments = enabled ? [{ start: FULL_DAY_START, stop: FULL_DAY_STOP }] : [];
  commit(next);
}

function addSegment(weekDayNum: number) {
  const next = viewDays.value.map(day => ({ ...day, segments: [...day.segments] }));
  const target = next.find(day => day.weekDayNum === weekDayNum);
  if (!target) return;
  target.segments = [...target.segments, { start: FULL_DAY_START, stop: FULL_DAY_STOP }];
  commit(next);
}

function removeSegment(weekDayNum: number, index: number) {
  const next = viewDays.value.map(day => ({ ...day, segments: [...day.segments] }));
  const target = next.find(day => day.weekDayNum === weekDayNum);
  if (!target) return;
  target.segments = target.segments.filter((_, position) => position !== index);
  commit(next);
}

function setSegment(weekDayNum: number, index: number, edge: "start" | "stop", raw: string) {
  const next = viewDays.value.map(day => ({ ...day, segments: [...day.segments] }));
  const target = next.find(day => day.weekDayNum === weekDayNum);
  const segment = target?.segments[index];
  if (!target || !segment) return;
  segment[edge] = raw;
  commit(next);
}

/**
 * 点色块 → 把焦点送到该段的"开始"输入框。
 *
 * ⛔ 不是"点色块就开始拖拽"，也不是"点色块弹一个时间选择器"：前者这一版不做，
 *    后者会让人以为改的是别处。跳到那条输入框，改哪儿一目了然，键盘接着就能打。
 */
function focusSegment(weekDayNum: number, index: number) {
  const node = document.querySelector<HTMLInputElement>(`[data-testid="dcw-start-${weekDayNum}-${index}"] input`);
  node?.focus();
  node?.select?.();
}

/** 一天里有几段（行右侧的计数）。 */
function daySummary(day: ConfigScheduleDay) {
  return day.segments.length ? `${day.segments.length} 段` : "不录像";
}

/** 汇总：已配置几天、共几个时段（页脚用）。 */
const summaryText = computed(() => {
  const configured = viewDays.value.filter(day => day.segments.length > 0);
  const segments = configured.reduce((total, day) => total + day.segments.length, 0);
  return `${configured.length} 天 · ${segments} 个时段`;
});

/** 整套计划里最早的一段开始 / 最晚的一段结束，页脚拿去当"覆盖时段"的概括。 */
const coverageText = computed(() => {
  const all = viewDays.value.flatMap(day => day.segments);
  if (!all.length) return "未排班";
  const starts = all.map(segment => percentOf(segment.start));
  const stops = all.map(segment => percentOf(segment.stop));
  const fmt = (percent: number) => {
    const total = Math.round((percent / 100) * 86400);
    const pad = (value: number) => String(value).padStart(2, "0");
    return `${pad(Math.floor(total / 3600) % 24)}:${pad(Math.floor((total % 3600) / 60))}`;
  };
  return `${fmt(Math.min(...starts))} – ${fmt(Math.max(...stops))}`;
});
</script>

<template>
  <div class="dcw" :class="{ 'is-disabled': disabled }">
    <div class="dcw-scale" aria-hidden="true">
      <span class="dcw-scale-gutter" />
      <span class="dcw-scale-track">
        <span>00</span>
        <span>06</span>
        <span>12</span>
        <span>18</span>
        <span>24</span>
      </span>
      <span class="dcw-scale-tail" />
    </div>

    <div
      v-for="day in viewDays"
      :key="day.weekDayNum"
      class="dcw-day"
      :class="{ 'is-today': day.weekDayNum === todayWeekDay, 'is-off': !day.segments.length }"
      :data-testid="`dcw-day-${day.weekDayNum}`"
    >
      <div class="dcw-row">
        <a-button
          html-type="button"
          type="text"
          class="dcw-switch"
          :class="{ 'is-on': day.segments.length > 0 }"
          :disabled="disabled"
          :aria-label="`周${WEEKDAY_LABELS[day.weekDayNum - 1]}录像计划`"
          :data-testid="`dcw-toggle-${day.weekDayNum}`"
          @click="toggleDay(day.weekDayNum, day.segments.length === 0)"
        >
          <i />
        </a-button>
        <span class="dcw-label">周{{ WEEKDAY_LABELS[day.weekDayNum - 1] }}</span>

        <div class="dcw-track">
          <span
            v-for="(segment, index) in day.segments"
            :key="index"
            class="dcw-block"
            :style="segmentStyle(segment)"
            :title="`${segmentLabel(segment)}，点击定位到输入框`"
            :data-testid="`dcw-block-${day.weekDayNum}-${index}`"
            @click="focusSegment(day.weekDayNum, index)"
          >
            <em>{{ segmentLabel(segment) }}</em>
          </span>
          <span v-if="!day.segments.length" class="dcw-track-empty">点这里新增时段</span>
          <span v-if="day.weekDayNum === todayWeekDay" class="dcw-now" :style="{ left: `${nowPercent}%` }" title="当前时刻" />
        </div>

        <span class="dcw-count">{{ daySummary(day) }}</span>
      </div>

      <div class="dcw-segments" :class="{ 'is-empty': !day.segments.length }">
        <div v-for="(segment, index) in day.segments" :key="index" class="dcw-segment">
          <a-input
            class="dcw-time"
            :model-value="segment.start"
            allow-clear
            :disabled="disabled"
            placeholder="HH:MM:SS"
            :aria-label="`周${WEEKDAY_LABELS[day.weekDayNum - 1]}第 ${index + 1} 段开始`"
            :data-testid="`dcw-start-${day.weekDayNum}-${index}`"
            @change="setSegment(day.weekDayNum, index, 'start', $event)"
          />
          <span class="dcw-dash">→</span>
          <a-input
            class="dcw-time"
            :model-value="segment.stop"
            allow-clear
            :disabled="disabled"
            placeholder="HH:MM:SS"
            :aria-label="`周${WEEKDAY_LABELS[day.weekDayNum - 1]}第 ${index + 1} 段结束`"
            :data-testid="`dcw-stop-${day.weekDayNum}-${index}`"
            @change="setSegment(day.weekDayNum, index, 'stop', $event)"
          />
          <a-button
            html-type="button"
            type="text"
            class="dcw-icon"
            :disabled="disabled"
            :aria-label="`删除周${WEEKDAY_LABELS[day.weekDayNum - 1]}第 ${index + 1} 段`"
            @click="removeSegment(day.weekDayNum, index)"
          >
            <Trash2 :size="11" />
          </a-button>
        </div>
        <a-button
          html-type="button"
          type="outline"
          class="dcw-add"
          :disabled="disabled"
          :aria-label="`为周${WEEKDAY_LABELS[day.weekDayNum - 1]}添加时段`"
          :data-testid="`dcw-add-${day.weekDayNum}`"
          @click="addSegment(day.weekDayNum)"
        >
          <Plus :size="10" />时段
        </a-button>
      </div>
    </div>

    <p class="dcw-foot">
      <CalendarPlus :size="10" />
      <span data-testid="dcw-summary">{{ summaryText }}</span>
      <span class="dcw-foot-sep">·</span>
      <span>{{ coverageText }}</span>
      <span class="dcw-foot-sep">·</span>
      <span>关掉某天表示该天没有录像计划（整条不下发），时刻按 HH:MM:SS 下发</span>
    </p>
  </div>
</template>

<style scoped lang="scss">
.dcw {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  gap: 2px;
  min-width: 0;

  &.is-disabled {
    color: var(--uvp-text-disabled);

    :deep(.dcw-time.arco-input-disabled) {
      color: var(--uvp-text-disabled);
      background: var(--uvp-shell-muted);
      border-color: var(--uvp-panel-border, #dbe4f0);
    }

    .dcw-icon.arco-btn[type="button"],
    .dcw-add.arco-btn[type="button"] {
      color: var(--uvp-text-disabled);
      background: var(--uvp-shell-muted);
      border-color: var(--uvp-panel-border, #dbe4f0);
    }
  }
}

/* 顶部刻度：与下面的轨道共用同一套栅格（开关 26 + 标签 34 + 轨道 1fr + 计数 44），
   否则刻度会和色块错位 —— 错位以后"这块是不是压到 12 点"就没法判断了。 */
.dcw-scale {
  display: grid;
  grid-template-columns: 68px minmax(0, 1fr) 44px;
  gap: 8px;
  align-items: center;
  padding-bottom: 2px;
}
.dcw-scale-track {
  display: flex;
  justify-content: space-between;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
}

.dcw-day {
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: 3px 0;
  border-top: 1px solid var(--uvp-border-subtle);

  &.is-off .dcw-label {
    color: var(--uvp-text-tertiary);
  }
}

.dcw-row {
  display: grid;
  grid-template-columns: 68px minmax(0, 1fr) 44px;
  gap: 8px;
  align-items: center;
}

.dcw-switch.arco-btn[type="button"] {
  display: inline-flex;
  flex: none;
  align-items: center;
  width: 26px;
  height: 14px;
  padding: 0 2px;
  cursor: pointer;
  background: var(--uvp-panel-border, #d5dfee);
  border: none;
  border-radius: 7px;
  transition: background 0.15s;

  i {
    width: 10px;
    height: 10px;
    background: var(--uvp-solid-text, #ffffff);
    border-radius: 50%;
    transition: transform 0.15s;
  }

  &.is-on {
    /* ⛔ 实心面用 --uvp-solid-bg：--uvp-brand 是文字色，暗色下是浅蓝，
       白滑块压上去几乎看不见（对比度约 2:1）。 */
    background: var(--uvp-solid-bg);

    i {
      transform: translateX(12px);
    }
  }

  &:disabled {
    color: var(--uvp-text-disabled);
    cursor: not-allowed;
    background: var(--uvp-dialog-control-bg);
    opacity: 1;
  }
}

.dcw-label {
  font-size: 12px;
  color: var(--uvp-text-secondary);

  .is-today & {
    font-weight: 600;
    color: var(--uvp-text-primary);
  }
}

.dcw-track {
  position: relative;
  height: 20px;
  overflow: hidden;
  background: var(--uvp-shell-muted);
  border-radius: 4px;
}

.dcw-block {
  position: absolute;
  top: 0;
  bottom: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  cursor: pointer;

  /* 实心块 + 白字：两套主题下都用 --uvp-solid-*，别拿 --uvp-brand 当底色。 */
  background: var(--uvp-solid-bg);
  transition: filter 0.12s;

  em {
    padding: 0 3px;
    overflow: hidden;
    text-overflow: ellipsis;
    font-size: 9px;
    font-style: normal;
    color: var(--uvp-solid-text);
    white-space: nowrap;
  }

  &:hover {
    filter: brightness(1.12);
  }
}

.dcw-track-empty {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
}

.dcw-now {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 1px;
  pointer-events: none;
  background: var(--uvp-danger);

  &::before {
    position: absolute;
    top: -2px;
    left: -2px;
    width: 5px;
    height: 5px;
    content: "";
    background: var(--uvp-danger);
    border-radius: 50%;
  }
}

.dcw-count {
  font-size: 10px;
  color: var(--uvp-text-tertiary);
  text-align: right;
}

.dcw-segments {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
  align-items: center;

  /* ⛔ 预留一个固定高度：没排班的天也要占位，否则开关一开一关整张表会上下跳一行。 */
  min-height: 24px;
  padding-left: 68px;

  &.is-empty {
    min-height: 6px;
  }
}

.dcw-segment {
  display: inline-flex;
  gap: 3px;
  align-items: center;
}

.dcw-time {
  width: 68px;
  height: 22px;
  font-size: 11px;
  color: var(--uvp-text-primary);
  background: var(--uvp-dialog-control-bg, #ffffff);
  border: 1px solid var(--uvp-panel-border, #dbe4f0);
  border-radius: 4px;

  &:focus-within {
    border-color: var(--uvp-brand);
  }

  :deep(.arco-input) {
    padding: 0 5px;
    font-size: 11px;
    color: var(--uvp-text-primary);
    text-align: center;
  }
}

.dcw-dash {
  font-size: 10px;
  color: var(--uvp-text-tertiary);
}

.dcw-icon.arco-btn[type="button"],
.dcw-add.arco-btn[type="button"] {
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
    color: var(--uvp-text-disabled);
    cursor: not-allowed;
    background: var(--uvp-shell-muted);
    opacity: 1;
  }
}

.dcw-icon.arco-btn[type="button"] {
  width: 22px;
  height: auto;
  min-height: 0;
  padding: 0;
}

.dcw-add.arco-btn[type="button"] {
  gap: 2px;
  height: auto;
  min-height: 0;
  padding: 0 7px;
  font-size: 10px;
}

.dcw-foot {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
  align-items: center;
  margin: 4px 0 0;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
}

.dcw-foot-sep {
  opacity: 0.6;
}

.dcw-time :deep(.arco-input:disabled) {
  color: var(--uvp-text-secondary);
}
</style>
