<template>
  <a-layout class="layout" :has-sider="true">
    <Aside v-if="isPc" />
    <a-layout class="layout-right">
      <Header />
      <Main />
      <Footer v-if="isFooter" />
    </a-layout>
  </a-layout>
</template>

<script setup lang="ts">
import Aside from "@/layout/components/Aside/index.vue";
import Header from "@/layout/components/Header/index.vue";
import Main from "@/layout/components/Main/index.vue";
import Footer from "@/layout/components/Footer/index.vue";
import { storeToRefs } from "pinia";
import { useThemeConfig } from "@/store/modules/theme-config";
import { useDevicesSize } from "@/hooks/useDevicesSize";

defineOptions({ name: "LayoutDefaults" });

const themeStore = useThemeConfig();
let { isFooter } = storeToRefs(themeStore);

const { isPc } = useDevicesSize();
</script>

<style lang="scss" scoped>
.layout {
  height: 100vh;
  padding: var(--uvp-workspace-gap);
  column-gap: var(--uvp-workspace-gap);
  background: var(--uvp-navigation-bg);
}

.layout-right {
  display: grid;
  grid-template-rows: auto 1fr auto;
  min-width: 0;
  height: calc(100vh - var(--uvp-workspace-gap) - var(--uvp-workspace-gap));
  overflow: hidden;
  background: var(--uvp-workspace-bg);
  border: 1px solid var(--uvp-workspace-border);
  border-left-color: transparent;
  border-radius: var(--uvp-workspace-radius);
  box-shadow: var(--uvp-workspace-shadow);
}

@media (max-width: 1024px) {
  .layout {
    padding: 0;
  }

  .layout-right {
    height: 100vh;
    border-radius: 0;
    border-right: 0;
    border-bottom: 0;
    border-left: 0;
  }
}
</style>
