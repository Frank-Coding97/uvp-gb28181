<template>
  <div class="tools uvp-list-toolbar">
    <a-space class="tools-group tools-group-left" wrap>
      <slot name="left"> </slot>
    </a-space>
    <a-space class="tools-group tools-group-right" wrap>
      <slot name="right"> </slot>
    </a-space>
  </div>
</template>

<script setup lang="ts"></script>

<style lang="scss" scoped>
.tools {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: flex-start;
  justify-content: space-between;
}

.tools-group {
  min-width: 0;
}

.tools-group-left {
  flex: 1;
}

.tools-group-right {
  flex: none;
  justify-content: flex-end;
}

@media (max-width: 768px) {
  .tools-group-right {
    justify-content: flex-start;
  }
}
</style>
