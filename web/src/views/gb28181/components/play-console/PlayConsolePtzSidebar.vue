<script setup lang="ts">
import { inject } from "vue";
import {
  Circle,
  CircleSlash,
  Compass,
  Crosshair,
  Focus as FocusIcon,
  Gauge,
  Info,
  Loader2,
  Mic,
  Move3d,
  Navigation,
  ScanEye,
  Square,
  Target,
  Video,
  ZoomIn,
  ZoomOut
} from "@lucide/vue";
import { PLAY_CONSOLE_CONTEXT } from "./playConsoleContext";
import PtzWiperCard from "./PtzWiperCard.vue";

const context = inject(PLAY_CONSOLE_CONTEXT) as Record<string, any> | undefined;
if (!context) throw new Error("PlayConsolePtzSidebar must be rendered inside PlayConsoleLinked");
const {
  canPtzPanel,
  activeTab,
  ptzMode,
  setPtzMode,
  joystickDragging,
  joystickHandleStyle,
  startJoystick,
  moveJoystick,
  endJoystick,
  handleJoystickKeydown,
  handleJoystickKeyup,
  talkMode,
  talkState,
  talkAvailable,
  capabilityActionTitle,
  isAudioCapable,
  toggleTalk,
  talkLevel,
  talkButtonText,
  moveSpeed,
  sendPtz,
  precisePan,
  preciseTilt,
  preciseZoom,
  sendPrecise,
  readPreciseStatus,
  canControlDevice,
  wiperCanSend,
  wiperState,
  wiperError,
  wiperChipTitle,
  toggleWiper,
  dragZoomMode,
  dragZoomAction,
  isAdvancedPending,
  toggleDragZoomMode,
  runAdvancedAction,
  targetTrackPending,
  submitTargetTrack,
  targetTrackMode,
  toggleTargetTrackMode,
  targetTrackIntentText,
  targetTrackStatus,
  targetTrackError
} = context;
</script>

<template>
  <div v-if="canPtzPanel" v-show="activeTab === 'ptz'" class="panel" data-testid="linked-side-ptz">
    <!-- 模式切换:连续控制 / 高级控制(2022) -->
    <div class="mode-switch">
      <button data-testid="ptz-mode-speed" :class="{ active: ptzMode === 'speed' }" @click="setPtzMode('speed')">
        <Compass :size="13" />连续控制
      </button>
      <button data-testid="ptz-mode-precise" :class="{ active: ptzMode === 'precise' }" @click="setPtzMode('precise')">
        <Crosshair :size="13" />高级控制<span class="tag-2022">2022</span>
      </button>
    </div>

    <!-- 速度模式:拖拽摇杆 + 变倍 + 速度 -->
    <div v-show="ptzMode === 'speed'" class="ptz-speed">
      <div
        class="joystick-stage"
        :class="{ active: joystickDragging }"
        role="group"
        tabindex="0"
        aria-label="云台方向摇杆"
        @pointerdown.prevent="startJoystick"
        @pointermove.prevent="moveJoystick"
        @pointerup.prevent="endJoystick"
        @pointercancel.prevent="endJoystick"
        @keydown="handleJoystickKeydown"
        @keyup="handleJoystickKeyup"
      >
        <div class="joystick-base"></div>
        <div class="joystick-dots" aria-hidden="true">
          <span class="joystick-dot dot-top"></span>
          <span class="joystick-dot dot-top-right"></span>
          <span class="joystick-dot dot-right"></span>
          <span class="joystick-dot dot-bottom-right"></span>
          <span class="joystick-dot dot-bottom"></span>
          <span class="joystick-dot dot-bottom-left"></span>
          <span class="joystick-dot dot-left"></span>
          <span class="joystick-dot dot-top-left"></span>
        </div>
        <span class="joystick-label top">上</span>
        <span class="joystick-label diagonal top-right">右上</span>
        <span class="joystick-label right">右</span>
        <span class="joystick-label diagonal bottom-right">右下</span>
        <span class="joystick-label bottom">下</span>
        <span class="joystick-label diagonal bottom-left">左下</span>
        <span class="joystick-label left">左</span>
        <span class="joystick-label diagonal top-left">左上</span>
        <div class="joystick-handle" :style="joystickHandleStyle" aria-hidden="true">
          <span></span>
        </div>
      </div>

      <div class="talk-mode-switch" aria-label="对讲模式">
        <button
          :class="{ active: talkMode === 'broadcast' }"
          :disabled="talkState !== 'idle' || !talkAvailable"
          :title="capabilityActionTitle('broadcast', '广播')"
          @click="talkMode = 'broadcast'"
        >
          广播
        </button>
        <button
          :class="{ active: talkMode === 'talk' }"
          :disabled="talkState !== 'idle' || !talkAvailable"
          :title="capabilityActionTitle('talk', 'Talk')"
          @click="talkMode = 'talk'"
        >
          Talk
        </button>
      </div>
      <button
        class="talk-button"
        data-testid="talk-button"
        :class="{ active: talkState !== 'idle' }"
        :disabled="!isAudioCapable"
        :title="capabilityActionTitle(talkMode, talkMode === 'broadcast' ? '广播对讲' : '双向对讲')"
        :aria-pressed="talkState === 'talking'"
        @click="toggleTalk"
      >
        <Mic :size="14" />
        <span
          v-if="talkState === 'talking'"
          class="talk-wave"
          data-testid="talk-wave"
          :style="{ '--talk-level': String(talkLevel) }"
          aria-hidden="true"
        >
          <i v-for="bar in 4" :key="bar" :style="{ animationDelay: `${(bar - 1) * -0.17}s` }"></i>
        </span>
        <span>{{ talkButtonText }}</span>
      </button>

      <div class="speed-row">
        <label>
          <span><Gauge :size="12" />移动速度</span>
          <input v-model.number="moveSpeed" type="range" min="1" max="10" />
          <em>{{ moveSpeed }}</em>
        </label>
      </div>

      <div class="lens-grid">
        <div class="lens-item">
          <span class="lens-label"><ZoomIn :size="12" />变倍</span>
          <div class="lens-btns">
            <button
              title="放大"
              @pointerdown.prevent="sendPtz('放大')"
              @pointerup.prevent="sendPtz('停止')"
              @pointerleave="sendPtz('停止')"
              @pointercancel="sendPtz('停止')"
            >
              <ZoomIn :size="14" />
            </button>
            <button
              title="缩小"
              @pointerdown.prevent="sendPtz('缩小')"
              @pointerup.prevent="sendPtz('停止')"
              @pointerleave="sendPtz('停止')"
              @pointercancel="sendPtz('停止')"
            >
              <ZoomOut :size="14" />
            </button>
          </div>
        </div>
        <div class="lens-item">
          <span class="lens-label"><FocusIcon :size="12" />聚焦</span>
          <div class="lens-btns">
            <button
              title="远焦(按住连续)"
              @pointerdown.prevent="sendPtz('远焦')"
              @pointerup.prevent="sendPtz('镜头停止')"
              @pointerleave="sendPtz('镜头停止')"
              @pointercancel="sendPtz('镜头停止')"
            >
              远
            </button>
            <button
              title="近焦(按住连续)"
              @pointerdown.prevent="sendPtz('近焦')"
              @pointerup.prevent="sendPtz('镜头停止')"
              @pointerleave="sendPtz('镜头停止')"
              @pointercancel="sendPtz('镜头停止')"
            >
              近
            </button>
          </div>
        </div>
        <div class="lens-item">
          <span class="lens-label"><Circle :size="12" />光圈</span>
          <div class="lens-btns">
            <button
              title="开大(按住连续)"
              @pointerdown.prevent="sendPtz('光圈+')"
              @pointerup.prevent="sendPtz('镜头停止')"
              @pointerleave="sendPtz('镜头停止')"
              @pointercancel="sendPtz('镜头停止')"
            >
              +
            </button>
            <button
              title="缩小(按住连续)"
              @pointerdown.prevent="sendPtz('光圈-')"
              @pointerup.prevent="sendPtz('镜头停止')"
              @pointerleave="sendPtz('镜头停止')"
              @pointercancel="sendPtz('镜头停止')"
            >
              −
            </button>
          </div>
        </div>
      </div>

      <!-- 低频辅助控制并排，避免雨刷被连续控制区底部裁切。 -->
      <div class="ptz-aux-grid">
        <!-- 3D 拖拽：连续控制下的画面级手势。 -->
        <div v-if="canControlDevice" class="ptz-drag-zoom" data-testid="ptz-drag-zoom">
          <span class="lens-label"><Move3d :size="12" />3D 拖拽</span>
          <div class="drag-zoom-switch" aria-label="3D 拖拽方向">
            <button
              :class="{ active: dragZoomMode && dragZoomAction === 'drag_zoom_in' }"
              data-testid="ptz-drag-zoom-in"
              :title="capabilityActionTitle('dragZoom', '3D 放大')"
              :disabled="isAdvancedPending('drag_zoom_in')"
              :aria-pressed="dragZoomMode && dragZoomAction === 'drag_zoom_in'"
              @click="toggleDragZoomMode('drag_zoom_in')"
            >
              {{ dragZoomMode && dragZoomAction === "drag_zoom_in" ? "取消 3D 放大" : "3D 放大" }}
            </button>
            <button
              :class="{ active: dragZoomMode && dragZoomAction === 'drag_zoom_out' }"
              data-testid="ptz-drag-zoom-out"
              :title="capabilityActionTitle('dragZoom', '3D 缩小')"
              :disabled="isAdvancedPending('drag_zoom_out')"
              :aria-pressed="dragZoomMode && dragZoomAction === 'drag_zoom_out'"
              @click="toggleDragZoomMode('drag_zoom_out')"
            >
              {{ dragZoomMode && dragZoomAction === "drag_zoom_out" ? "取消 3D 缩小" : "3D 缩小" }}
            </button>
          </div>
        </div>
        <PtzWiperCard
          class="ptz-wiper-side"
          :can-send="wiperCanSend"
          :state="wiperState"
          :error="wiperError"
          :title="wiperChipTitle"
          @toggle="toggleWiper"
        />
      </div>
    </div>

    <!-- 高级控制模式(2022):精准 Pan/Tilt/Zoom、关键帧与目标跟踪 -->
    <div v-show="ptzMode === 'precise'" class="ptz-precise">
      <div class="precise-hint">
        <Info :size="12" />
        <span>基于 <strong>PTZPreciseCtrl</strong>(2022),精准角度定位。需设备支持精准 PTZ 协议。</span>
      </div>
      <div class="axis-row">
        <label>
          <span>Pan 水平角(°)</span>
          <div class="axis-ctrl">
            <input v-model.number="precisePan" type="range" min="0" max="360" step="0.1" />
            <input v-model.number="precisePan" type="number" min="0" max="360" step="0.1" class="axis-num" />
          </div>
        </label>
      </div>
      <div class="axis-row">
        <label>
          <span>Tilt 俯仰角(°)</span>
          <div class="axis-ctrl">
            <input v-model.number="preciseTilt" type="range" min="-90" max="90" step="0.1" />
            <input v-model.number="preciseTilt" type="number" min="-90" max="90" step="0.1" class="axis-num" />
          </div>
        </label>
      </div>
      <div class="axis-row">
        <label>
          <span>Zoom 变倍(x)</span>
          <div class="axis-ctrl">
            <input v-model.number="preciseZoom" type="range" min="1" max="32" step="0.1" />
            <input v-model.number="preciseZoom" type="number" min="1" max="32" step="0.1" class="axis-num" />
          </div>
        </label>
      </div>
      <div class="precise-actions">
        <button class="btn-primary sm" data-testid="ptz-precise-apply" @click="sendPrecise"><Target :size="13" />应用定位</button>
        <button class="btn-ghost sm" @click="readPreciseStatus"><Navigation :size="13" />读取当前位置</button>
      </div>
      <!-- 请求关键帧：高级控制下的一次性流操作。 -->
      <div v-if="canControlDevice" class="ptz-iframe" data-testid="ptz-iframe">
        <span class="lens-label"><Video :size="12" />关键帧</span>
        <button
          data-testid="ptz-iframe-request"
          :title="capabilityActionTitle('iFrame', '请求关键帧')"
          :disabled="isAdvancedPending('iframe')"
          @click="runAdvancedAction('iframe')"
        >
          <Loader2 v-if="isAdvancedPending('iframe')" :size="12" class="spin" /><Video v-else :size="12" />
          <span>请求关键帧</span>
        </button>
      </div>

      <!-- 目标跟踪：高级控制下的画面级即时命令。 -->
      <div v-if="canControlDevice" class="ptz-target-track" data-testid="ptz-target-track">
        <span class="lens-label"><ScanEye :size="12" />目标跟踪<span class="tag-2022">2022</span></span>
        <div class="target-track-switch" aria-label="目标跟踪方式">
          <button
            data-testid="ptz-target-track-auto"
            :title="capabilityActionTitle('targetTrack', '自动跟踪')"
            :disabled="targetTrackPending"
            @click="submitTargetTrack('Auto')"
          >
            <Loader2 v-if="targetTrackPending" :size="12" class="spin" /><ScanEye v-else :size="12" />
            <span>自动跟踪</span>
          </button>
          <button
            :class="{ active: targetTrackMode }"
            data-testid="ptz-target-track-manual"
            :title="capabilityActionTitle('targetTrack', '手动框选目标')"
            :disabled="targetTrackPending"
            :aria-pressed="targetTrackMode"
            @click="toggleTargetTrackMode"
          >
            <Square v-if="targetTrackMode" :size="12" /><ScanEye v-else :size="12" />
            <span>{{ targetTrackMode ? "取消框选" : "框选跟踪" }}</span>
          </button>
          <button
            data-testid="ptz-target-track-stop"
            :title="capabilityActionTitle('targetTrack', '停止跟踪')"
            :disabled="targetTrackPending"
            @click="submitTargetTrack('Stop')"
          >
            <CircleSlash :size="12" />
            <span>停止跟踪</span>
          </button>
        </div>
        <!-- ⛔ 这三行文字合起来才是完整口径,少一行都会被读成"设备在做某事":
                    「平台最近一次下发…」(全知的一侧) + 「已下发,设备未回执」(没有回执) +
                    「平台无法得知设备实际状态」(所以别问平台设备现在在跟踪什么)。 -->
        <p class="target-track-state" data-testid="target-track-intent">
          {{ targetTrackIntentText }}
        </p>
        <p v-if="targetTrackStatus" class="target-track-status" data-testid="target-track-status">
          {{ targetTrackStatus }}
        </p>
        <p v-else class="target-track-tip" data-testid="target-track-tip">
          「自动跟踪」「停止跟踪」一键下发；「框选跟踪」要在画面上框住目标（坐标按画面实际渲染尺寸换算）。
        </p>
        <p v-if="targetTrackError" class="target-track-error" data-testid="target-track-error">
          {{ targetTrackError }}
        </p>
      </div>
    </div>
  </div>
</template>

<style scoped lang="scss">
.panel {
  display: grid;
  gap: 10px;
}

/* ═══════════ 云台面板 ═══════════ */
.mode-switch {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 4px;
  padding: 4px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
}
.mode-switch button {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  justify-content: center;
  padding: 6px 8px;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: 6px;
  transition: all 0.15s ease;
}
.mode-switch button.active {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--uvp-brand) 26%, transparent);
}
.tag-2022 {
  padding: 1px 4px;
  font-size: 8.5px;
  font-weight: 700;
  color: var(--uvp-brand-cyan);
  letter-spacing: 0.05em;
  background: color-mix(in srgb, var(--uvp-brand-cyan) 14%, transparent);
  border: 1px solid color-mix(in srgb, var(--uvp-brand-cyan) 30%, transparent);
  border-radius: 3px;
}

/* 拖拽摇杆 */
.joystick-stage {
  position: relative;
  width: min(176px, 100%);
  aspect-ratio: 1;
  margin: 10px auto 6px;
  touch-action: none;
  cursor: grab;
  user-select: none;
  border-radius: 50%;
}
.joystick-stage.active {
  cursor: grabbing;
}
.joystick-stage:focus-visible {
  outline: 2px solid var(--uvp-brand);
  outline-offset: 3px;
}
.joystick-base {
  position: absolute;
  inset: 0;
  background: radial-gradient(
    circle at 36% 28%,
    color-mix(in srgb, white 18%, var(--uvp-brand-soft)) 0%,
    var(--uvp-brand-soft) 46%,
    color-mix(in srgb, var(--uvp-text-primary) 12%, var(--uvp-list-toolbar-bg)) 100%
  );
  border: 2px solid color-mix(in srgb, var(--uvp-brand) 30%, var(--uvp-panel-border));
  border-radius: 50%;
  box-shadow:
    inset 0 3px 4px color-mix(in srgb, white 14%, transparent),
    inset 0 -8px 14px color-mix(in srgb, var(--uvp-text-primary) 14%, transparent),
    0 8px 18px color-mix(in srgb, var(--uvp-brand) 15%, transparent),
    0 2px 4px color-mix(in srgb, var(--uvp-text-primary) 15%, transparent);
}
.joystick-base::before {
  position: absolute;
  inset: 25px;
  content: "";
  background: radial-gradient(
    circle at 44% 38%,
    color-mix(in srgb, var(--uvp-brand-soft) 30%, var(--uvp-list-toolbar-bg)) 0%,
    var(--uvp-list-toolbar-bg) 62%,
    color-mix(in srgb, var(--uvp-text-primary) 8%, var(--uvp-list-toolbar-bg)) 100%
  );
  border: 1px solid color-mix(in srgb, var(--uvp-brand) 18%, var(--uvp-panel-border));
  border-radius: 50%;
  box-shadow:
    inset 0 5px 10px color-mix(in srgb, var(--uvp-text-primary) 11%, transparent),
    inset 0 -2px 4px color-mix(in srgb, white 8%, transparent),
    0 1px 0 color-mix(in srgb, white 10%, transparent);
}
.joystick-base::after {
  position: absolute;
  inset: 31px;
  content: "";
  border: 1px solid color-mix(in srgb, var(--uvp-brand) 14%, var(--uvp-panel-border));
  border-radius: 50%;
  box-shadow: 0 0 0 1px color-mix(in srgb, var(--uvp-text-primary) 4%, transparent);
}
.joystick-dots {
  position: absolute;
  inset: 0;
  z-index: 2;
  pointer-events: none;
}
.joystick-dot {
  position: absolute;
  width: 7px;
  height: 7px;
  background: radial-gradient(
    circle at 34% 28%,
    color-mix(in srgb, white 42%, var(--uvp-text-tertiary)) 0 18%,
    var(--uvp-text-tertiary) 58%,
    color-mix(in srgb, var(--uvp-text-primary) 35%, var(--uvp-text-tertiary)) 100%
  );
  border: 1px solid color-mix(in srgb, var(--uvp-text-primary) 12%, transparent);
  border-radius: 50%;
  box-shadow:
    inset 0 1px 1px color-mix(in srgb, white 26%, transparent),
    0 1px 2px color-mix(in srgb, var(--uvp-text-primary) 24%, transparent);
}
.joystick-dot.dot-top {
  top: 22px;
  left: 50%;
  transform: translateX(-50%);
}
.joystick-dot.dot-top-right {
  top: 38px;
  right: 38px;
}
.joystick-dot.dot-right {
  top: 50%;
  right: 22px;
  transform: translateY(-50%);
}
.joystick-dot.dot-bottom-right {
  right: 38px;
  bottom: 38px;
}
.joystick-dot.dot-bottom {
  bottom: 22px;
  left: 50%;
  transform: translateX(-50%);
}
.joystick-dot.dot-bottom-left {
  bottom: 38px;
  left: 38px;
}
.joystick-dot.dot-left {
  top: 50%;
  left: 22px;
  transform: translateY(-50%);
}
.joystick-dot.dot-top-left {
  top: 38px;
  left: 38px;
}
.joystick-label {
  position: absolute;
  z-index: 3;
  font-size: 9px;
  line-height: 1;
  color: var(--uvp-text-tertiary);
  pointer-events: none;
}
.joystick-label.top {
  top: 9px;
  left: 50%;
  transform: translateX(-50%);
}
.joystick-label.top-right {
  top: 8px;
  right: 8px;
}
.joystick-label.right {
  top: 50%;
  right: 9px;
  transform: translateY(-50%);
}
.joystick-label.bottom-right {
  right: 8px;
  bottom: 8px;
}
.joystick-label.bottom {
  bottom: 9px;
  left: 50%;
  transform: translateX(-50%);
}
.joystick-label.bottom-left {
  bottom: 8px;
  left: 8px;
}
.joystick-label.left {
  top: 50%;
  left: 9px;
  transform: translateY(-50%);
}
.joystick-label.top-left {
  top: 8px;
  left: 8px;
}
.joystick-handle {
  position: absolute;
  top: 50%;
  left: 50%;
  z-index: 4;
  display: grid;
  place-items: center;
  width: 52px;
  height: 52px;
  pointer-events: none;
  background: radial-gradient(
    circle at 34% 26%,
    color-mix(in srgb, white 88%, var(--uvp-brand)) 0 6%,
    color-mix(in srgb, white 30%, var(--uvp-brand)) 20%,
    var(--uvp-brand) 56%,
    var(--uvp-brand-strong) 100%
  );
  border: 5px solid color-mix(in srgb, white 58%, var(--uvp-brand));
  border-radius: 50%;
  box-shadow:
    inset 4px 4px 8px color-mix(in srgb, white 34%, transparent),
    inset -6px -8px 11px color-mix(in srgb, black 24%, transparent),
    0 9px 16px color-mix(in srgb, var(--uvp-brand) 34%, transparent),
    0 3px 4px color-mix(in srgb, black 28%, transparent),
    0 0 0 4px var(--uvp-brand-soft),
    0 0 0 5px color-mix(in srgb, var(--uvp-brand) 30%, transparent);
  transition: transform 0.22s cubic-bezier(0.2, 0.8, 0.2, 1);
}
.joystick-stage.active .joystick-handle {
  box-shadow:
    inset 3px 3px 7px color-mix(in srgb, white 28%, transparent),
    inset -5px -6px 9px color-mix(in srgb, black 28%, transparent),
    0 5px 10px color-mix(in srgb, var(--uvp-brand) 28%, transparent),
    0 2px 3px color-mix(in srgb, black 24%, transparent),
    0 0 0 4px var(--uvp-brand-soft),
    0 0 0 5px color-mix(in srgb, var(--uvp-brand) 38%, transparent);
  transition: none;
}
.joystick-handle span {
  position: absolute;
  top: 9px;
  left: 11px;
  width: 17px;
  height: 9px;
  background: linear-gradient(145deg, color-mix(in srgb, white 74%, transparent), transparent);
  border-radius: 50%;
  opacity: 0.82;
  filter: blur(0.2px);
}

@media (prefers-reduced-motion: reduce) {
  .joystick-handle {
    transition: none;
  }
  .ptz-direction-stack {
    opacity: 0.82;
    animation: none;
  }
}

.talk-mode-switch {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  width: 100%;
  max-width: 200px;
  padding: 2px;
  margin: 6px auto 0;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 7px;
}
.talk-mode-switch button {
  height: 22px;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: 5px;
}
.talk-mode-switch button.active {
  font-weight: 600;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
}
.talk-mode-switch button:disabled {
  cursor: not-allowed;
  opacity: 0.42;
}

.talk-button {
  display: flex;
  gap: 7px;
  align-items: center;
  justify-content: center;
  width: 100%;
  max-width: 200px;
  height: 34px;
  margin: 8px auto 2px;
  font-size: 11px;
  font-weight: 600;
  color: var(--uvp-brand);
  touch-action: none;
  cursor: pointer;
  user-select: none;
  background: var(--uvp-brand-soft);
  border: 1px solid color-mix(in srgb, var(--uvp-brand) 34%, var(--uvp-panel-border));
  border-radius: 8px;
  transition: all 0.15s ease;
}
.talk-button:hover:not(:disabled) {
  border-color: var(--uvp-brand);
}
.talk-button.active {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
  border-color: var(--uvp-danger-border);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--uvp-danger) 10%, transparent);
}
.talk-button:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

/* 「正在说话」的波形：4 根 bar 相位错开各自起伏，整体幅度再由采集电平（--talk-level，0..1）缩放。
 * 静态动画保证「一直在动」，电平缩放保证「动得和声音有关」。拿不到 AudioContext 时电平恒为 0，
 * 波形仍以 0.4 倍显示，不会变成空按钮。 */
.talk-wave {
  display: inline-flex;
  flex: none;
  gap: 2px;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 13px;
  transform: scaleY(calc(0.4 + var(--talk-level, 0) * 0.6));
  transform-origin: center;
  transition: transform 0.08s linear;
}
.talk-wave i {
  width: 2px;
  height: 100%;
  background: currentColor;
  border-radius: 1px;
  animation: talk-wave-pulse 0.9s ease-in-out infinite;
}

@keyframes talk-wave-pulse {
  0%,
  100% {
    transform: scaleY(0.32);
  }
  50% {
    transform: scaleY(1);
  }
}

@media (prefers-reduced-motion: reduce) {
  .talk-wave i {
    transform: scaleY(0.8);
    animation: none;
  }
}

.speed-row {
  padding: 6px 2px;
}
.speed-row label {
  display: grid;
  grid-template-columns: auto 1fr auto;
  gap: 8px;
  align-items: center;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.speed-row label > span {
  display: inline-flex;
  gap: 4px;
  align-items: center;
}
.speed-row input[type="range"] {
  accent-color: var(--uvp-brand);
}
.speed-row em {
  font-family: ui-monospace, Menlo, monospace;
  font-style: normal;
  font-weight: 600;
  color: var(--uvp-brand);
}

/* 镜头(变倍/聚焦/光圈) */
.lens-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 6px;
  margin-top: 6px;
}

.ptz-aux-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr);
  gap: 6px;
  min-width: 0;
}

.ptz-aux-grid > * {
  min-width: 0;
}
.lens-grid.disabled {
  pointer-events: none;
  opacity: 0.42;
}
.lens-item {
  display: grid;
  gap: 6px;
  padding: 8px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
}
.lens-label {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  font-size: 10.5px;
  color: var(--uvp-text-tertiary);
}
.lens-btns {
  display: flex;
  gap: 4px;
}
.lens-btns button {
  flex: 1;
  height: 26px;
  padding: 0;
  font-size: 11px;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: transparent;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 5px;
}
.lens-btns button:hover:not(:disabled) {
  color: var(--uvp-brand);
  border-color: var(--uvp-brand);
}

/* 3D 拖拽(2026-09-20 从「高级」搬来):外盒沿用 .lens-item 的形态,让它在侧栏里
 * 和"变倍/聚焦/光圈"读成同一族;里面的分段按钮沿用 .lens-btns button 的尺寸语言。
 * 分段而不是两个独立按钮:放大/缩小是互斥的二选一(点另一个会换方向而不是叠加),
 * 分段控件把"只有一个生效"这件事直接画出来。 */
.ptz-drag-zoom {
  display: grid;
  gap: 4px;
  min-width: 0;
  padding: 6px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
}
.ptz-wiper-side {
  gap: 5px;
  width: 100%;
  height: auto;
  min-height: 0;
  padding: 6px;
}
.drag-zoom-switch {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 4px;
}
.drag-zoom-switch button {
  height: 26px;
  padding: 0;
  font-size: 11px;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: transparent;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 5px;
}
.drag-zoom-switch button:hover:not(:disabled) {
  color: var(--uvp-brand);
  border-color: var(--uvp-brand);
}
.drag-zoom-switch button.active {
  font-weight: 600;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-color: var(--uvp-brand);
}
.drag-zoom-switch button:disabled {
  cursor: not-allowed;
  opacity: 0.42;
}

/* 请求关键帧:与 3D 拖拽同款"标签 + 动作"盒,但只有一个动作,故走两列(标签/按钮)排一行。 */
.ptz-iframe {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 8px;
  align-items: center;
  padding: 8px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
}
.ptz-iframe button {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  justify-content: center;
  height: 26px;
  padding: 0 8px;
  font-size: 11px;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: transparent;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 5px;
}
.ptz-iframe button:hover:not(:disabled) {
  color: var(--uvp-brand);
  border-color: var(--uvp-brand);
}
.ptz-iframe button:disabled {
  cursor: not-allowed;
  opacity: 0.42;
}

/* 目标跟踪（A.2.3.1.14）:三段"动作"而不是"方向" —— 但它们同样是三选一里的"当前态"
 * （框选进行中时「框选跟踪」是唯一亮着的那个),所以沿用分段控件的视觉。
 * ⛔ 三个按钮都是**独立动作**而不是切换开关:「自动跟踪」每点一次都是一条新指令,
 *    不要把「自动」画成某种"已开启"的常驻开关 —— 无应答命令没有"当前态"可言。 */
.ptz-target-track {
  display: grid;
  gap: 6px;
  padding: 8px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
}
.target-track-switch {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 4px;
}
.target-track-switch button {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  justify-content: center;
  height: 26px;
  padding: 0 4px;
  font-size: 11px;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: transparent;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 5px;
}
.target-track-switch button:hover:not(:disabled) {
  color: var(--uvp-brand);
  border-color: var(--uvp-brand);
}
.target-track-switch button.active {
  font-weight: 600;
  color: var(--uvp-warning);
  background: color-mix(in srgb, var(--uvp-warning) 14%, transparent);
  border-color: var(--uvp-warning);
}
.target-track-switch button:disabled {
  cursor: not-allowed;
  opacity: 0.42;
}

/* 状态三行:意图 / 下发结果 / 说明。⛔ 字号与颜色都往"事实"靠,不要做成告警条 ——
 * 无回执是**协议事实**而不是异常,把它画成警告色会让用户以为出错了。 */
.target-track-state,
.target-track-status,
.target-track-tip,
.target-track-error {
  margin: 0;
  font-size: 10.5px;
  line-height: 1.5;
  overflow-wrap: anywhere;
}
.target-track-state {
  color: var(--uvp-text-secondary);
}
.target-track-status {
  font-weight: 600;
  color: var(--uvp-warning);
}
.target-track-tip {
  color: var(--uvp-text-tertiary);
}
.target-track-error {
  color: var(--uvp-danger);
}

/* 高级控制布局：定位参数与协议级动作在同一模式内分组。 */
[data-testid="linked-side-ptz"] {
  grid-template-rows: auto minmax(0, 1fr);
  height: 100%;
  min-height: 0;
}
.ptz-speed,
.ptz-precise {
  display: grid;
  gap: 10px;
  align-content: start;
  min-height: 0;
}

@media (width <= 640px) {
  .ptz-aux-grid {
    grid-template-columns: 1fr;
  }
}
.ptz-precise {
  grid-template-columns: minmax(0, 1fr);
}
.ptz-precise > .ptz-iframe {
  min-width: 0;
}
.precise-hint {
  display: flex;
  gap: 6px;
  align-items: flex-start;
  padding: 8px 10px;
  font-size: 10.5px;
  line-height: 1.5;
  color: var(--uvp-text-tertiary);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 8%, transparent);
  border: 1px solid color-mix(in srgb, var(--uvp-brand-cyan) 24%, transparent);
  border-radius: 8px;
}
.precise-hint strong {
  font-weight: 600;
  color: var(--uvp-brand-cyan);
}
.axis-row label {
  display: grid;
  gap: 4px;
}
.axis-row label > span {
  font-size: 10.5px;
  color: var(--uvp-text-tertiary);
}
.axis-ctrl {
  display: grid;
  grid-template-columns: 1fr 60px;
  gap: 6px;
  align-items: center;
}
.axis-ctrl input[type="range"] {
  accent-color: var(--uvp-brand);
}
.axis-num {
  height: 26px;
  padding: 0 6px;
  font-family: ui-monospace, Menlo, monospace;
  font-size: 11px;
  color: var(--uvp-text-secondary);
  text-align: right;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 5px;
}
.precise-actions {
  display: flex;
  gap: 6px;
}
.precise-actions button {
  flex: 1;
}
</style>
