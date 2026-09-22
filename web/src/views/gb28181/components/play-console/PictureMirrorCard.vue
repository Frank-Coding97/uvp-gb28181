<script setup lang="ts">
import { FlipHorizontal } from "lucide-vue-next";

defineProps<{
  editable: boolean;
  mirror: string;
  options: readonly { value: string; label: string; shortLabel?: string }[];
  icon: (value: string) => unknown;
}>();
const emit = defineEmits<{ (e: "select", value: string): void }>();
</script>
<template>
  <section class="linked-section linked-card" data-testid="picture-mirror-card">
    <header class="linked-card-hd">
      <span class="section-title"><FlipHorizontal :size="13" />画面镜像</span>
      <span class="linked-card-actions"><span v-if="!editable" class="linked-card-note">不可编辑</span></span>
    </header>
    <div class="mirror-choice-grid">
      <button
        v-for="option in options"
        :key="option.value"
        type="button"
        class="mirror-choice"
        :class="{ active: mirror === option.value }"
        :disabled="!editable"
        :data-testid="`picture-mirror-${option.value}`"
        :title="`${option.label}（值 ${option.value}）`"
        @click="emit('select', option.value)"
      >
        <component :is="icon(option.value)" :size="16" /><span>{{ option.shortLabel || option.label }}</span>
      </button>
    </div>
  </section>
</template>
