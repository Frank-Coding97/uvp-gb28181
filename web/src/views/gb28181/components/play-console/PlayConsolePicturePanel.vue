<script setup lang="ts">
defineProps<{ visible: boolean; active: boolean }>();
</script>

<template>
  <div v-if="visible" v-show="active" class="linked-detail linked-detail-actions" data-testid="linked-detail-picture">
    <div class="linked-picture-layout"><slot /></div>
  </div>
</template>

<style lang="scss">
.linked-detail-actions {
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  height: var(--play-console-detail-height, 180px);
  min-height: 0;
  overflow: auto;
}
.linked-picture-layout {
  box-sizing: border-box;
  display: grid;
  grid-template-rows: minmax(0, 1fr);
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
  align-items: stretch;
  height: 100%;
  min-height: 0;
}

.linked-picture-layout > .picture-osd-cell {
  grid-column: span 2;
}

@media (width <= 1080px) {
  .linked-picture-layout {
    grid-template-rows: none;
    grid-template-columns: 1fr;
    height: auto;
  }

  .linked-picture-layout > .picture-osd-cell {
    grid-column: span 1;
  }
}
</style>
