<script setup lang="ts">
defineProps<{ visible: boolean; active: boolean }>();
</script>

<template>
  <div v-if="visible" v-show="active" class="linked-detail linked-detail-ptz" data-testid="linked-detail-ptz">
    <div class="linked-ptz-layout"><slot /></div>
  </div>
</template>

<style lang="scss">
.linked-detail-ptz {
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  gap: 6px;
  height: var(--play-console-detail-height, 180px);
  min-height: 0;
  overflow: auto;
}
.linked-ptz-layout {
  box-sizing: border-box;
  display: grid;
  grid-template-rows: minmax(0, 1fr);
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
  align-items: stretch;
  height: 100%;
  min-height: 0;
}

@media (width <= 960px) {
  .linked-ptz-layout {
    grid-template-rows: none;
    grid-template-columns: 1fr;
    height: auto;
  }
}
</style>
