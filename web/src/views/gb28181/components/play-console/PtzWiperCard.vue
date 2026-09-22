<script setup lang="ts">
import { Play, Square, Waves } from "lucide-vue-next";
defineProps<{ canSend: boolean; state: "off" | "on-sent"; error: string; title: string }>();
const emit = defineEmits<{ (e: "toggle"): void }>();
</script>
<template>
  <section class="linked-section linked-card" data-testid="wiper-card">
    <header class="linked-card-hd">
      <span class="section-title"><Waves :size="13" />雨刷</span
      ><span v-if="state === 'on-sent'" class="linked-card-actions"
        ><button class="status-chip active" data-testid="wiper-running-chip" @click="emit('toggle')">已下发</button></span
      >
    </header>
    <div class="scan-panel" data-testid="wiper-panel">
      <div class="scan-row">
        <button
          class="btn-primary sm scan-toggle"
          data-testid="wiper-toggle"
          :disabled="!canSend"
          :title="title"
          @click="emit('toggle')"
        >
          <Play v-if="state === 'off'" :size="11" /><Square v-else :size="11" /><span>{{
            state === "on-sent" ? "关闭雨刷" : "开启雨刷"
          }}</span>
        </button>
      </div>
      <p v-if="error" class="scan-error" data-testid="wiper-error">{{ error }}</p>
      <p v-else class="scan-hint" data-testid="wiper-hint">
        标准只命名了编号 1 = 雨刷；辅助开关没有查询命令，这里只能确认指令已下发
      </p>
    </div>
  </section>
</template>
