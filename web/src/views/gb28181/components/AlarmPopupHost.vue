<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from "vue";
import { X } from "lucide-vue-next";
import { buildAlarmStreamUrl, type AlarmPopupEvent } from "@/api/gb28181-alarm-stream";
import {
  ALARM_POPUP_LEAVE_MS,
  ALARM_POPUP_TTL_MS,
  alarmPopupDetail,
  alarmPopupIdentifiers,
  alarmPopupKey,
  alarmPopupPriority,
  alarmPopupSubtitle,
  alarmPopupTitle,
  enqueueAlarmPopup,
  formatAlarmPopupTime,
  markAlarmPopupLeaving,
  removeAlarmPopup,
  type AlarmPopupItem
} from "./alarmPopupQueue";

/**
 * 设备告警右侧浮层（全局宿主，挂在 layout 里）。
 *
 * 数据来源是 SSE（`GET /api/gb28181/alarms?stream=1`），后端在报警报文解析处广播，
 * 因此「是否存储报警消息」关闭时同样能收到。开关在「国标服务配置 → 设备告警实时弹出」，
 * 关闭后后端不再推送，这里自然安静（连接仍然保持着，随时开启都立即生效，不用刷新）。
 *
 * 交互：新告警从右侧滑入，停留 ALARM_POPUP_TTL_MS 后自动滑出；
 * 最多同时堆 MAX_ALARM_POPUPS 张，超出的挤掉最旧那张。
 */

/** 断线兜底重连间隔。浏览器自身重连对 4xx 无效（会把 readyState 置为 CLOSED）。 */
const RECONNECT_DELAY_MS = 15000;

const items = ref<AlarmPopupItem[]>([]);
const timers = new Map<string, ReturnType<typeof setTimeout>>();
let stream: EventSource | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
let sequence = 0;

function clearTimer(key: string) {
  const timer = timers.get(key);
  if (!timer) return;
  clearTimeout(timer);
  timers.delete(key);
}

function drop(key: string) {
  clearTimer(key);
  items.value = removeAlarmPopup(items.value, key);
}

/** 开始离场：先播滑出动画，动画结束再真正从列表里删掉。 */
function dismiss(key: string) {
  const current = items.value.find(item => item.key === key);
  if (!current || current.leaving) {
    clearTimer(key);
    return;
  }
  items.value = markAlarmPopupLeaving(items.value, key);
  clearTimer(key);
  timers.set(
    key,
    setTimeout(() => drop(key), ALARM_POPUP_LEAVE_MS)
  );
}

function accept(event: AlarmPopupEvent) {
  sequence += 1;
  const key = alarmPopupKey(event, sequence);
  const next = enqueueAlarmPopup(items.value, { key, event, leaving: false });
  // enqueueAlarmPopup 遇到重复 key 会原样返回旧数组 ⇒ 同一条告警不会弹两次。
  if (next === items.value) return;
  items.value = next;
  clearTimer(key);
  timers.set(
    key,
    setTimeout(() => dismiss(key), ALARM_POPUP_TTL_MS)
  );
}

function openStream() {
  if (stream || typeof EventSource === "undefined") return;
  const source = new EventSource(buildAlarmStreamUrl());
  source.addEventListener("alarm", (raw: MessageEvent) => {
    try {
      accept(JSON.parse(raw.data) as AlarmPopupEvent);
    } catch {
      // 脏帧宁可不弹，也不能让一次解析失败影响后续推送。
    }
  });
  source.onerror = () => {
    // 服务端 4xx（token 过期、无告警权限）会让浏览器彻底放弃重连，
    // 这里自己兜一次，避免"登录续期后浮层永久静默"。
    if (source.readyState !== EventSource.CLOSED || reconnectTimer) return;
    source.close();
    stream = null;
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null;
      openStream();
    }, RECONNECT_DELAY_MS);
  };
  stream = source;
}

function closeStream() {
  if (reconnectTimer) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
  stream?.close();
  stream = null;
}

onMounted(openStream);

onBeforeUnmount(() => {
  closeStream();
  timers.forEach(timer => clearTimeout(timer));
  timers.clear();
  items.value = [];
});
</script>

<template>
  <TransitionGroup v-if="items.length" tag="div" name="uvp-alarm-popup" class="uvp-alarm-popups" role="status" aria-live="polite">
    <article
      v-for="item in items"
      :key="item.key"
      class="uvp-alarm-popup"
      :class="[`is-level-${alarmPopupPriority(item.event)}`, { 'is-leaving': item.leaving }]"
      data-testid="alarm-popup"
    >
      <div class="uvp-alarm-popup__body">
        <div class="uvp-alarm-popup__head">
          <span class="uvp-alarm-popup__title">{{ alarmPopupTitle(item.event) }}</span>
          <span class="uvp-alarm-popup__time">{{ formatAlarmPopupTime(item.event.receivedAt) }}</span>
        </div>
        <p v-if="alarmPopupSubtitle(item.event)" class="uvp-alarm-popup__subtitle">
          {{ alarmPopupSubtitle(item.event) }}
        </p>
        <div class="uvp-alarm-popup__ids">
          <span v-for="identifier in alarmPopupIdentifiers(item.event)" :key="identifier.label" class="uvp-alarm-popup__id">
            <span class="uvp-alarm-popup__id-label">{{ identifier.label }}</span>
            <code class="uvp-alarm-popup__id-value" :title="identifier.value">{{ identifier.value }}</code>
          </span>
        </div>
        <p v-if="alarmPopupDetail(item.event)" class="uvp-alarm-popup__detail">
          {{ alarmPopupDetail(item.event) }}
        </p>
      </div>
      <button type="button" class="uvp-alarm-popup__close" aria-label="关闭告警提示" @click="dismiss(item.key)">
        <X :size="14" />
      </button>
    </article>
  </TransitionGroup>
</template>

<style scoped lang="scss">
.uvp-alarm-popups {
  position: fixed;
  top: 76px;
  right: 20px;
  z-index: 2100;
  display: flex;
  flex-direction: column;
  gap: 10px;
  width: 336px;
  max-width: calc(100vw - 32px);

  /* 容器不挡点击：只有卡片本身接收事件，浮层覆盖区域下的页面照常可操作。 */
  pointer-events: none;
}

.uvp-alarm-popup {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  padding: 12px 10px 12px 14px;
  pointer-events: auto;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-left-width: 3px;
  border-radius: 12px;
  box-shadow: 0 18px 48px -28px rgb(15 23 42 / 32%);

  /* 级别色只染左侧色条，不整块铺底：暗色主题下大面积实心色会压垮对比度。 */
  &.is-level-1 {
    border-left-color: var(--uvp-danger);
  }

  &.is-level-2 {
    border-left-color: #e9821f;
  }

  &.is-level-3 {
    border-left-color: #d9a520;
  }

  &.is-level-4 {
    border-left-color: var(--uvp-brand);
  }

  &.is-level-0 {
    border-left-color: var(--uvp-text-tertiary);
  }

  &.is-leaving {
    pointer-events: none;
  }
}

.uvp-alarm-popup__body {
  flex: 1;
  min-width: 0;
}

.uvp-alarm-popup__head {
  display: flex;
  gap: 8px;
  align-items: baseline;
}

.uvp-alarm-popup__title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 14px;
  font-weight: 600;
  line-height: 20px;
  color: var(--uvp-text-primary);
  white-space: nowrap;
}

.uvp-alarm-popup__time {
  flex: none;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  color: var(--uvp-text-tertiary);
}

.uvp-alarm-popup__subtitle,
.uvp-alarm-popup__detail {
  margin: 4px 0 0;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 12px;
  line-height: 18px;
  white-space: nowrap;
}

.uvp-alarm-popup__subtitle {
  color: var(--uvp-text-secondary);
}

.uvp-alarm-popup__detail {
  color: var(--uvp-text-tertiary);
}

/* 设备 ID / 通道 ID：等宽字体 + 可整段选中，方便直接复制去搜设备树。 */
.uvp-alarm-popup__ids {
  display: flex;
  flex-direction: column;
  gap: 1px;
  margin-top: 4px;
}

.uvp-alarm-popup__id {
  display: flex;
  gap: 6px;
  align-items: baseline;
  min-width: 0;
  font-size: 11px;
  line-height: 16px;
}

.uvp-alarm-popup__id-label {
  flex: none;
  color: var(--uvp-text-tertiary);
}

.uvp-alarm-popup__id-value {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  color: var(--uvp-text-secondary);
  white-space: nowrap;
  user-select: all;
}

.uvp-alarm-popup__close {
  display: inline-flex;
  flex: none;
  align-items: center;
  justify-content: center;
  width: 20px;
  min-width: 20px;
  height: 20px;
  padding: 0;
  color: var(--uvp-text-tertiary);
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: 6px;

  &:hover {
    color: var(--uvp-text-primary);
    background: var(--uvp-shell-muted);
  }
}

/* 从右侧滑入 / 滑出。时长与 alarmPopupQueue.ALARM_POPUP_LEAVE_MS 保持一致。 */
.uvp-alarm-popup-enter-active,
.uvp-alarm-popup-leave-active {
  transition:
    transform 0.32s cubic-bezier(0.22, 0.61, 0.36, 1),
    opacity 0.32s ease;
}

.uvp-alarm-popup-enter-from,
.uvp-alarm-popup-leave-to {
  opacity: 0;
  transform: translateX(110%);
}

.uvp-alarm-popup-move {
  transition: transform 0.32s cubic-bezier(0.22, 0.61, 0.36, 1);
}

@media (prefers-reduced-motion: reduce) {
  .uvp-alarm-popup-enter-active,
  .uvp-alarm-popup-leave-active,
  .uvp-alarm-popup-move {
    transition-duration: 0.01ms;
  }
}
</style>
