<script setup lang="ts">
import { Maximize2, PictureInPicture2, RadioTower, X } from "@lucide/vue";

defineProps<{
  isMinimized: boolean;
  title: string;
  channelId?: string;
  phase: string;
  sessionStatusClass: Record<string, boolean>;
  sessionStatusText: string;
  elapsedText: string;
}>();

const emit = defineEmits<{
  (event: "drag", value: PointerEvent): void;
  (event: "minimize"): void;
  (event: "restore"): void;
  (event: "close"): void;
}>();
</script>

<template>
  <div
    class="console-title"
    :class="{ 'is-minimized': isMinimized }"
    data-testid="play-console-drag-handle"
    @pointerdown="emit('drag', $event)"
  >
    <span class="title-icon"><RadioTower :size="18" /></span>
    <div class="title-text">
      <strong>{{ isMinimized ? title : "播放控制台" }}</strong>
      <span v-if="!isMinimized">{{ title }} · {{ channelId || "未选择通道" }}</span>
    </div>
    <span v-if="!isMinimized" class="session-badge" :class="sessionStatusClass">
      <span class="dot"></span>{{ sessionStatusText }}
      <em v-if="phase === 'playing'" class="session-elapsed mono">{{ elapsedText }}</em>
    </span>
    <div class="console-window-actions">
      <template v-if="!isMinimized">
        <button
          type="button"
          class="console-window-action is-minimize"
          data-testid="play-console-minimize"
          title="切换为小窗播放"
          aria-label="切换为小窗播放"
          @pointerdown.stop
          @click.stop="emit('minimize')"
        >
          <PictureInPicture2 :size="15" aria-hidden="true" />
          <span>小窗</span>
        </button>
        <button
          type="button"
          class="console-window-action is-close"
          data-testid="play-console-close"
          title="关闭并停止播放"
          aria-label="关闭并停止播放"
          @pointerdown.stop
          @click.stop="emit('close')"
        >
          <X :size="15" aria-hidden="true" />
          <span>关闭</span>
        </button>
      </template>
      <template v-else>
        <button
          type="button"
          class="console-window-action is-compact"
          data-testid="play-console-restore"
          title="恢复播放控制台"
          aria-label="恢复播放控制台"
          @pointerdown.stop
          @click.stop="emit('restore')"
        >
          <Maximize2 :size="15" />
        </button>
        <button
          type="button"
          class="console-window-action is-close is-compact"
          data-testid="play-console-close-mini"
          title="关闭并停止播放"
          aria-label="关闭并停止播放"
          @pointerdown.stop
          @click.stop="emit('close')"
        >
          <X :size="15" />
        </button>
      </template>
    </div>
  </div>
</template>
<style scoped lang="scss">
.console-title {
  display: flex;
  gap: 10px;
  align-items: center;
  width: 100%;
  min-width: 0;
}
.console-title.is-minimized {
  cursor: grab;
  user-select: none;
}
.console-title.is-minimized:active {
  cursor: grabbing;
}
.title-icon {
  display: inline-grid;
  place-items: center;
  width: 32px;
  height: 32px;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-radius: 9px;
}
.title-text {
  display: grid;
  gap: 2px;
  min-width: 0;
}
.title-text strong {
  font-size: 14px;
  color: var(--uvp-text-primary);
}
.title-text span {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}

.console-window-actions {
  display: inline-flex;
  flex: 0 0 auto;
  gap: 6px;
  align-items: center;
  margin-left: 4px;
}
.console-title.is-minimized .console-window-actions {
  margin-left: auto;
}
.console-window-action {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  justify-content: center;
  min-width: 58px;
  height: 30px;
  padding: 0 9px;
  font-size: 11px;
  font-weight: 600;
  line-height: 1;
  color: var(--uvp-text-secondary);
  white-space: nowrap;
  cursor: pointer;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
  transition:
    color 0.15s ease,
    background 0.15s ease,
    border-color 0.15s ease,
    box-shadow 0.15s ease,
    transform 0.15s ease;
}
.console-window-action svg {
  flex: 0 0 auto;
}
.console-window-action.is-minimize {
  color: var(--uvp-brand);
  background: color-mix(in srgb, var(--uvp-brand) 7%, var(--uvp-panel-bg));
  border-color: color-mix(in srgb, var(--uvp-brand) 24%, var(--uvp-panel-border));
}
.console-window-action.is-minimize:hover {
  color: var(--uvp-brand-strong);
  background: var(--uvp-brand-soft);
  border-color: var(--uvp-brand);
  box-shadow: 0 4px 12px color-mix(in srgb, var(--uvp-brand) 14%, transparent);
}
.console-window-action.is-config {
  color: var(--uvp-brand-strong, var(--uvp-brand));
  background: color-mix(in srgb, var(--uvp-brand) 5%, var(--uvp-panel-bg));
  border-color: color-mix(in srgb, var(--uvp-brand) 22%, var(--uvp-panel-border));
}
.console-window-action.is-config:hover {
  color: var(--uvp-brand-strong);
  background: var(--uvp-brand-soft);
  border-color: var(--uvp-brand);
  box-shadow: 0 4px 12px color-mix(in srgb, var(--uvp-brand) 14%, transparent);
}
.console-window-action.is-close {
  color: color-mix(in srgb, var(--uvp-danger) 76%, var(--uvp-text-secondary));
  background: color-mix(in srgb, var(--uvp-danger) 4%, var(--uvp-panel-bg));
  border-color: color-mix(in srgb, var(--uvp-danger) 18%, var(--uvp-panel-border));
}
.console-window-action.is-close:hover {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
  border-color: var(--uvp-danger-border);
  box-shadow: 0 4px 12px color-mix(in srgb, var(--uvp-danger) 12%, transparent);
}
.console-window-action:focus-visible {
  outline: 2px solid color-mix(in srgb, var(--uvp-brand) 48%, transparent);
  outline-offset: 2px;
}
.console-window-action:active {
  transform: translateY(1px);
}
.console-window-action.is-compact {
  gap: 0;
  width: 28px;
  min-width: 28px;
  height: 28px;
  padding: 0;
}

.session-badge {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  padding: 4px 10px;
  margin-left: auto;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 999px;
}
.session-badge .dot {
  width: 6px;
  height: 6px;
  background: var(--uvp-text-tertiary);
  border-radius: 50%;
}
.session-badge.active {
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 12%, transparent);
  border-color: color-mix(in srgb, var(--uvp-brand-cyan) 30%, transparent);
}
.session-badge.active .dot {
  background: var(--uvp-brand-cyan);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--uvp-brand-cyan) 20%, transparent);
  animation: pulse 1.6s ease-in-out infinite;
}
.session-badge.loading {
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
  border-color: var(--uvp-warning-border);
}
.session-badge.loading .dot {
  background: var(--uvp-warning);
}
.session-badge.error {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
  border-color: var(--uvp-danger-border);
}
.session-badge.error .dot {
  background: var(--uvp-danger);
}
.session-badge.paused {
  color: #a78bfa;
  background: rgb(167 139 250 / 12%);
  border-color: rgb(167 139 250 / 28%);
}
.session-badge.warn {
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
  border-color: var(--uvp-warning-border);
}
.session-badge.warn .dot {
  background: var(--uvp-warning);
}
.session-elapsed {
  padding-left: 8px;
  margin-left: 2px;
  font-size: 10.5px;
  font-style: normal;
  font-weight: 600;
  color: color-mix(in srgb, currentcolor 70%, transparent);
  border-left: 1px solid color-mix(in srgb, currentcolor 24%, transparent);
}

@media (width <= 640px) {
  .console-title {
    flex-wrap: wrap;
  }
  .console-title:not(.is-minimized) .console-window-action {
    width: 30px;
    min-width: 30px;
    padding: 0;
  }
  .console-title:not(.is-minimized) .console-window-action span {
    display: none;
  }
}
</style>
