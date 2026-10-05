<script setup lang="ts">
import { MoveHorizontal, Play, Square } from "lucide-vue-next";
defineProps<{
  group: number;
  speed: number;
  groupMin: number;
  groupMax: number;
  speedMin: number;
  speedMax: number;
  state: "stopped" | "start-sent";
  activeGroup: number | null;
  canSend: boolean;
  speedInvalid: boolean;
  error: string;
}>();
const emit = defineEmits<{
  (e: "update:group", value: number): void;
  (e: "update:speed", value: number): void;
  (e: "command", value: string): void;
}>();
</script>
<template>
  <section class="linked-section linked-card" data-testid="scan-card">
    <header class="linked-card-hd">
      <span class="section-title"
        ><MoveHorizontal :size="13" />自动扫描<a-button
          type="text"
          size="mini"
          html-type="button"
          v-if="state === 'start-sent'"
          class="cruise-running-chip"
          data-testid="scan-running-chip"
          aria-label="停止扫描"
          @click="emit('command', 'scan_stop')"
        >
          <span class="cruise-running-dot" />启动已下发<Square :size="10" /></a-button
      ></span>
    </header>
    <div class="scan-panel" data-testid="scan-panel">
      <div class="scan-row">
        <label class="scan-label" for="scan-group-input">组号</label
        ><a-input
          id="scan-group-input"
          :model-value="group"
          class="scan-number"
          type="text"
          inputmode="numeric"
          size="small"
          data-testid="scan-group-input"
          aria-label="扫描组号"
          @input="emit('update:group', Number($event ?? 0))"
        /><a-button
          type="primary"
          size="mini"
          html-type="button"
          class="btn-primary sm scan-toggle"
          data-testid="scan-toggle"
          :disabled="!canSend"
          @click="emit('command', state === 'start-sent' ? 'scan_stop' : 'scan_start')"
        >
          <Play v-if="state === 'stopped'" :size="11" /><Square v-else :size="11" /><span>{{
            state === "start-sent" ? "停止扫描" : "开始扫描"
          }}</span>
        </a-button>
      </div>
      <div class="scan-row">
        <a-button
          type="outline"
          size="mini"
          html-type="button"
          class="btn-ghost sm scan-bound-btn"
          data-testid="scan-set-left"
          :disabled="!canSend"
          @click="emit('command', 'scan_set_left')"
        >
          设左边界</a-button
        ><a-button
          type="outline"
          size="mini"
          html-type="button"
          class="btn-ghost sm scan-bound-btn"
          data-testid="scan-set-right"
          :disabled="!canSend"
          @click="emit('command', 'scan_set_right')"
        >
          设右边界
        </a-button>
      </div>
      <div class="scan-row">
        <label class="scan-label" for="scan-speed-input">速度</label
        ><a-input
          id="scan-speed-input"
          :model-value="speed"
          class="scan-number"
          type="text"
          inputmode="numeric"
          size="small"
          data-testid="scan-speed-input"
          aria-label="扫描速度"
          @input="emit('update:speed', Number($event ?? 0))"
        /><a-button
          type="outline"
          size="mini"
          html-type="button"
          class="btn-ghost sm"
          data-testid="scan-set-speed"
          :disabled="!canSend || speedInvalid"
          @click="emit('command', 'scan_set_speed')"
        >
          应用速度
        </a-button>
      </div>
      <p v-if="error" class="scan-error" data-testid="scan-error">{{ error }}</p>
      <p v-else class="scan-hint" data-testid="scan-hint">
        扫描只在左右边界之间来回，与预置位无关；边界需先把云台转到目标位置再设置
      </p>
    </div>
  </section>
</template>
