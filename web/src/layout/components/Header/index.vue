<template>
  <a-layout-header class="header" :class="{ 'header--compact': !isTabs || isMobile }">
    <HeaderLeft />
    <div v-if="isTabs && !isMobile" class="header_tabs">
      <Tabs />
    </div>
    <HeaderRight />
  </a-layout-header>
</template>
<script setup lang="ts">
import { storeToRefs } from "pinia";
import HeaderLeft from "@/layout/components/Header/components/header-left/index.vue";
import HeaderRight from "@/layout/components/Header/components/header-right/index.vue";
import Tabs from "@/layout/components/Tabs/index.vue";
import { useDevicesSize } from "@/hooks/useDevicesSize";
import { useThemeConfig } from "@/store/modules/theme-config";

const { isMobile } = useDevicesSize();
const { isTabs } = storeToRefs(useThemeConfig());
</script>

<style lang="scss" scoped>
.header {
  position: relative;
  box-sizing: border-box;
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 12px;
  height: var(--uvp-header-height);
  padding: 8px var(--uvp-header-padding-x);
  background: var(--uvp-workspace-bg);
  border-bottom: 1px solid var(--uvp-workspace-border);
}

.header_tabs {
  display: flex;
  min-width: 0;
  height: 40px;
  align-items: center;
  overflow: hidden;
}

.header_tabs :deep(.tabs) {
  width: 100%;
  border-bottom: 0;
}

.header--compact {
  grid-template-columns: minmax(0, 1fr) auto;
}
</style>
