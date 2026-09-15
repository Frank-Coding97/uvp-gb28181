<template>
  <div class="week-grid-scroll" @mouseup="stopPaint" @mouseleave="stopPaint">
    <div :class="['week-slot-grid', { editable }]" role="grid" aria-label="每周半小时录像时段">
      <div class="week-grid-header">
        <div class="week-grid-corner">星期 / 时间</div>
        <div v-for="hour in hours" :key="hour" class="hour-cell">{{ hour }}</div>
        <div v-if="editable" class="week-grid-actions-title">快捷操作</div>
      </div>

      <div v-for="(dayName, dayIndex) in dayNames" :key="dayName" class="week-grid-row" role="row">
        <div class="day-cell"><strong>{{ dayName }}</strong><small>{{ daySlotSummary(dayIndex) }}</small></div>
        <div class="slot-cells">
          <template v-for="slotIndex in slotIndexes" :key="slotIndex">
            <button
              v-if="editable"
              type="button"
              :class="['time-slot', { active: slotActive(dayIndex, slotIndex) }]"
              :title="slotTitle(slotIndex)"
              :aria-label="`${dayName} ${slotTitle(slotIndex)}`"
              :aria-pressed="slotActive(dayIndex, slotIndex)"
              @mousedown.prevent="startPaint(dayIndex, slotIndex)"
              @mouseenter="paintSlot(dayIndex, slotIndex)"
            />
            <span v-else class="time-slot readonly" :class="{ active: slotActive(dayIndex, slotIndex) }" :title="`${dayName} ${slotTitle(slotIndex)}`" />
          </template>
        </div>
        <div v-if="editable" class="day-actions">
          <a-button size="mini" @click="fillDay(dayIndex)">全天</a-button>
          <a-button size="mini" status="danger" @click="clearDay(dayIndex)">清空</a-button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from "vue";

const SLOT_MINUTES = 30;
const SLOTS_PER_DAY = (24 * 60) / SLOT_MINUTES;
const dayNames = ["周一", "周二", "周三", "周四", "周五", "周六", "周日"];
const hours = Array.from({ length: 24 }, (_, index) => index);
const slotIndexes = Array.from({ length: 48 }, (_, index) => index);

const props = withDefaults(defineProps<{ slots: boolean[][]; editable?: boolean }>(), { editable: false });
const emit = defineEmits<{ (event: "update:slots", slots: boolean[][]): void }>();
const painting = ref(false);
const paintValue = ref(true);

function normalizedSlots() {
  return dayNames.map((_, dayIndex) => Array.from({ length: SLOTS_PER_DAY }, (_, slotIndex) => Boolean(props.slots[dayIndex]?.[slotIndex])));
}

function slotActive(dayIndex: number, slotIndex: number) {
  return Boolean(props.slots[dayIndex]?.[slotIndex]);
}

function updateSlots(mutator: (nextSlots: boolean[][]) => void) {
  if (!props.editable) return;
  const nextSlots = normalizedSlots();
  mutator(nextSlots);
  emit("update:slots", nextSlots);
}

function startPaint(dayIndex: number, slotIndex: number) {
  if (!props.editable) return;
  painting.value = true;
  paintValue.value = !slotActive(dayIndex, slotIndex);
  updateSlots(nextSlots => { nextSlots[dayIndex][slotIndex] = paintValue.value; });
}

function paintSlot(dayIndex: number, slotIndex: number) {
  if (!props.editable || !painting.value) return;
  updateSlots(nextSlots => { nextSlots[dayIndex][slotIndex] = paintValue.value; });
}

function stopPaint() {
  painting.value = false;
}

function fillDay(dayIndex: number) {
  updateSlots(nextSlots => { nextSlots[dayIndex].fill(true); });
}

function clearDay(dayIndex: number) {
  updateSlots(nextSlots => { nextSlots[dayIndex].fill(false); });
}

function slotToTime(slot: number) {
  const totalMinutes = slot * SLOT_MINUTES;
  const hour = Math.floor(totalMinutes / 60);
  const minute = totalMinutes % 60;
  return `${String(hour).padStart(2, "0")}:${String(minute).padStart(2, "0")}`;
}

function slotTitle(slotIndex: number) {
  return `${slotToTime(slotIndex)}-${slotToTime(slotIndex + 1)}`;
}

function daySlotSummary(dayIndex: number) {
  const count = props.slots[dayIndex]?.filter(Boolean).length || 0;
  return count ? `${count * SLOT_MINUTES / 60} 小时` : "未设置";
}

onMounted(() => window.addEventListener("mouseup", stopPaint));
onBeforeUnmount(() => window.removeEventListener("mouseup", stopPaint));
</script>

<style scoped lang="scss">
.week-grid-scroll { max-width: 100%; overflow-x: auto; padding-bottom: 4px; }
.week-slot-grid { min-width: 946px; overflow: hidden; background: var(--uvp-panel-bg); border: 1px solid var(--color-border-3); border-radius: 10px; user-select: none; }
.week-slot-grid.editable { min-width: 1026px; }
.week-grid-header,
.week-grid-row { display: grid; grid-template-columns: 82px repeat(24, minmax(36px, 1fr)); }
.week-slot-grid.editable .week-grid-header,
.week-slot-grid.editable .week-grid-row { grid-template-columns: 82px repeat(24, minmax(36px, 1fr)) 80px; }
.week-grid-header { min-height: 34px; color: var(--uvp-text-secondary); background: var(--uvp-table-header-bg); border-bottom: 1px solid var(--color-border-3); }
.week-grid-corner,
.hour-cell,
.week-grid-actions-title { display: flex; align-items: center; justify-content: center; font-size: 11px; font-weight: 620; }
.week-grid-corner { position: sticky; left: 0; z-index: 4; background: var(--uvp-table-header-bg); border-right: 1px solid var(--color-border-3); }
.hour-cell { border-right: 1px solid var(--color-border-3); font-variant-numeric: tabular-nums; }
.week-grid-actions-title { position: sticky; right: 0; z-index: 4; background: var(--uvp-table-header-bg); border-left: 1px solid var(--color-border-3); box-shadow: -6px 0 8px -8px rgb(15 23 42 / 45%); }
.week-grid-row { min-height: 44px; border-bottom: 1px solid var(--color-border-3); }
.week-grid-row:last-child { border-bottom: 0; }
.day-cell { position: sticky; left: 0; z-index: 3; display: flex; flex-direction: column; align-items: center; justify-content: center; background: var(--uvp-panel-bg); border-right: 1px solid var(--color-border-3); box-shadow: 6px 0 8px -8px rgb(15 23 42 / 45%); }
.day-cell strong { font-size: 12px; }
.day-cell small { margin-top: 2px; color: var(--uvp-text-tertiary); font-size: 10px; }
.slot-cells { display: grid; grid-column: span 24; grid-template-columns: repeat(48, minmax(18px, 1fr)); }
.time-slot { display: block; width: 100%; height: 44px; box-sizing: border-box; padding: 0; background: var(--uvp-table-row-bg); border: 0; border-right: 1px solid var(--color-border-3); }
button.time-slot { cursor: crosshair; }
button.time-slot:hover { background: color-mix(in srgb, var(--uvp-brand) 12%, var(--uvp-table-row-bg)); }
.time-slot.readonly { cursor: default; }
.time-slot.active { background: linear-gradient(180deg, color-mix(in srgb, var(--uvp-brand) 88%, white), var(--uvp-brand)); box-shadow: inset 0 0 0 1px rgb(255 255 255 / 18%); }
.day-actions { position: sticky; right: 0; z-index: 3; display: flex; align-items: center; justify-content: center; gap: 4px; padding: 0 5px; background: var(--uvp-panel-bg); border-left: 1px solid var(--color-border-3); box-shadow: -6px 0 8px -8px rgb(15 23 42 / 45%); }
.day-actions :deep(.arco-btn-size-mini) { min-width: 34px; padding: 0 5px; border-radius: 6px; font-size: 11px; }
</style>
