<template>
  <a-layout-header class="header">
    <HeaderLeft />
    <div v-if="isTabs" class="header_tabs">
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
import { useThemeConfig } from "@/store/modules/theme-config";

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
  overflow: hidden;
}

.header_tabs :deep(.tabs) {
  width: 100%;
  border-bottom: 0;
}

@media (max-width: 768px) {
  .header {
    grid-template-columns: auto minmax(0, 1fr);
    padding-right: 76px;
  }

  .header_tabs {
    min-width: 0;
  }
}
</style>
