<script setup lang="ts">
import type { Component } from "vue";

type PlayConsoleTab = {
  key: string;
  label: string;
  icon: Component;
  description: string;
};

defineProps<{
  tabs: PlayConsoleTab[];
  activeTab: string;
  pictureDirtyCount: number;
  pictureDraftSummary: string;
}>();

const emit = defineEmits<{
  (event: "update:activeTab", value: string): void;
}>();
</script>

<template>
  <nav v-if="tabs.length" class="tabs" aria-label="播放工作区">
    <button
      v-for="tab in tabs"
      :key="tab.key"
      type="button"
      class="tab"
      :class="{ active: activeTab === tab.key }"
      :aria-current="activeTab === tab.key ? 'page' : undefined"
      :data-testid="`linked-tab-${tab.key}`"
      :title="tab.description"
      @click="emit('update:activeTab', tab.key)"
    >
      <component :is="tab.icon" :size="14" />
      <span>{{ tab.label }}</span>
      <em
        v-if="tab.key === 'deviceconfig' && activeTab !== 'deviceconfig' && pictureDirtyCount > 0"
        class="tab-draft-dot"
        data-testid="linked-tab-draft-dot"
        :title="`${pictureDraftSummary} 未下发`"
      ></em>
    </button>
  </nav>
</template>

<style scoped lang="scss">
.tabs {
  display: grid;
  grid-auto-columns: minmax(0, 1fr);
  grid-auto-flow: column;
  gap: 4px;
  padding: 4px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 10px;
}
.tab {
  position: relative;
  display: inline-flex;
  flex-direction: column;
  gap: 3px;
  align-items: center;
  justify-content: center;
  padding: 8px 4px;
  font-size: 10.5px;
  color: var(--uvp-text-tertiary);
  letter-spacing: 0.02em;
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: 7px;
  transition: all 0.15s ease;
}
.tab:hover {
  color: var(--uvp-text-secondary);
  background: color-mix(in srgb, var(--uvp-brand) 6%, transparent);
}
.tab.active {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--uvp-brand) 30%, transparent);
}
.tab-draft-dot {
  position: absolute;
  top: 6px;
  right: 8px;
  width: 6px;
  height: 6px;
  background: var(--uvp-warning, #b66b12);
  border-radius: 50%;
}
</style>
