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
        ><MoveHorizontal :size="13" />自动扫描<button
          v-if="state === 'start-sent'"
          class="cruise-running-chip"
          data-testid="scan-running-chip"
          @click="emit('command', 'scan_stop')"
        >
          <span class="cruise-running-dot" />启动已下发<Square :size="10" /></button
      ></span>
    </header>
    <div class="scan-panel" data-testid="scan-panel">
      <div class="scan-row">
        <label class="scan-label" for="scan-group-input">组号</label
        ><input
          id="scan-group-input"
          :value="group"
          class="scan-number"
          type="number"
          :min="groupMin"
          :max="groupMax"
          data-testid="scan-group-input"
          @input="emit('update:group', Number(($event.target as HTMLInputElement).value))"
        /><button
          class="btn-primary sm scan-toggle"
          data-testid="scan-toggle"
          :disabled="!canSend"
          @click="emit('command', state === 'start-sent' ? 'scan_stop' : 'scan_start')"
        >
          <Play v-if="state === 'stopped'" :size="11" /><Square v-else :size="11" /><span>{{
            state === "start-sent" ? "停止扫描" : "开始扫描"
          }}</span>
        </button>
      </div>
      <div class="scan-row">
        <button
          class="btn-ghost sm scan-bound-btn"
          data-testid="scan-set-left"
          :disabled="!canSend"
          @click="emit('command', 'scan_set_left')"
        >
          设左边界</button
        ><button
          class="btn-ghost sm scan-bound-btn"
          data-testid="scan-set-right"
          :disabled="!canSend"
          @click="emit('command', 'scan_set_right')"
        >
          设右边界
        </button>
      </div>
      <div class="scan-row">
        <label class="scan-label" for="scan-speed-input">速度</label
        ><input
          id="scan-speed-input"
          :value="speed"
          class="scan-number"
          type="number"
          :min="speedMin"
          :max="speedMax"
          data-testid="scan-speed-input"
          @input="emit('update:speed', Number(($event.target as HTMLInputElement).value))"
        /><button
          class="btn-ghost sm"
          data-testid="scan-set-speed"
          :disabled="!canSend || speedInvalid"
          @click="emit('command', 'scan_set_speed')"
        >
          下发
        </button>
      </div>
      <p v-if="error" class="scan-error" data-testid="scan-error">{{ error }}</p>
      <p v-else class="scan-hint" data-testid="scan-hint">
        扫描只在左右边界之间来回，与预置位无关；边界需先把云台转到目标位置再设置
      </p>
    </div>
  </section>
</template>
