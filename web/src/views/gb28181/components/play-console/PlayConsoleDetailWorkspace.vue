<script setup lang="ts">
defineProps<{
  isConfigWorkspace?: boolean;
}>();
</script>

<template>
  <div class="linked-info-bar" :class="{ 'config-detail-bar': isConfigWorkspace }" data-testid="play-console-detail-workspace">
    <slot />
  </div>
</template>

<style lang="scss">
.linked-info-bar {
  --play-console-detail-height: 180px;

  box-sizing: border-box;
  display: grid;
  grid-row: 2;
  grid-column: 1 / -1;
  gap: 0;
  height: var(--play-console-detail-height);
  min-height: 0;
  padding: 0;
  overflow: hidden;
  background: transparent;
  border: 0;
  border-radius: 0;
  box-shadow: none;
}
.linked-detail {
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  gap: 6px;
  height: var(--play-console-detail-height);
  min-height: 0;
  padding: 0;
  overflow: auto;
  scrollbar-color: color-mix(in srgb, var(--uvp-text-tertiary) 28%, transparent) transparent;
  scrollbar-width: thin;
}
.linked-detail-actions {
  height: var(--play-console-detail-height);
}

@media (width <= 720px) {
  .linked-info-bar {
    height: var(--play-console-detail-height);
  }
  .linked-detail {
    height: var(--play-console-detail-height);
    overflow: auto;
  }
}
</style>
<style lang="scss" src="./detail-cards.scss"></style>
