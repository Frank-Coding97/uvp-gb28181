<script setup lang="ts">
import { Play, Square, Waves } from "lucide-vue-next";
defineProps<{ canSend: boolean; state: "off" | "on-sent"; error: string; title: string }>();
const emit = defineEmits<{ (e: "toggle"): void }>();
</script>
<template>
  <section class="wiper-compact" data-testid="wiper-card">
    <span class="wiper-label"><Waves :size="12" />雨刷</span>
    <button
      class="wiper-toggle"
      :class="{ active: state === 'on-sent' }"
      data-testid="wiper-toggle"
      :disabled="!canSend"
      :title="title"
      @click="emit('toggle')"
    >
      <Play v-if="state === 'off'" :size="11" /><Square v-else :size="11" /><span>{{
        state === "on-sent" ? "关闭雨刷" : "开启雨刷"
      }}</span>
    </button>
    <p v-if="error" class="wiper-error" data-testid="wiper-error">{{ error }}</p>
  </section>
</template>

<style scoped>
.wiper-compact {
  display: grid;
  gap: 3px;
  min-width: 0;
}
.wiper-label {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  font-size: 10.5px;
  color: var(--uvp-text-tertiary);
}
.wiper-toggle {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  justify-content: center;
  height: 22px;
  padding: 0 6px;
  font-size: 10.5px;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: transparent;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 5px;
  transition: all 0.15s ease;
}
.wiper-toggle:hover:not(:disabled) {
  color: var(--uvp-brand);
  border-color: var(--uvp-brand);
}
.wiper-toggle.active {
  font-weight: 600;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-color: var(--uvp-brand);
}
.wiper-toggle:disabled {
  cursor: not-allowed;
  opacity: 0.42;
}
.wiper-error {
  margin: 0;
  font-size: 10px;
  line-height: 1.3;
  color: var(--uvp-danger);
}
</style>
