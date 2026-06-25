<template>
  <a-layout-header class="header">
    <HeaderLeft />
    <HeaderRight />
  </a-layout-header>
</template>
<script setup lang="ts">
import HeaderLeft from "@/layout/components/Header/components/header-left/index.vue";
import HeaderRight from "@/layout/components/Header/components/header-right/index.vue";
</script>

<style lang="scss" scoped>
.header {
  position: relative;
  box-sizing: border-box;
  display: flex;
  align-items: center;
  height: 56px;
  padding: 0 24px;
  background: #fff;
  border-bottom: 1px solid #e8e8e8;
  z-index: 100;
  flex-shrink: 0;
}
</style>
