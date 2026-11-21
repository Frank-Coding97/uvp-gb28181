<template>
  <div class="header_left">
    <ButtonCollapsed />
  </div>
</template>

<script setup lang="ts">
import ButtonCollapsed from "@/layout/components/Header/components/button-collapsed/index.vue";
</script>

<style lang="scss" scoped>
.header_left {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
}
</style>
