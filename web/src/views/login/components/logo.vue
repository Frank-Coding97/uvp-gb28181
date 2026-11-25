<template>
  <div class="banner_title">
    <LogoSvg :imageUrl="sysLogo" :width="40" :height="40" />
    {{ bannerTitle }}
  </div>
</template>
<script setup lang="ts">
import { handleUrl } from "@/utils/app";
import { DEFAULT_PRODUCT_TITLE } from "@/utils/product-brand";
import { useSysConfigStore } from "@/store/modules/sys-config";
import { storeToRefs } from "pinia";
import LogoSvg from "@/components/s-logo/index.vue";

// 获取系统配置
const sysConfigStore = useSysConfigStore();
const { systemConfig } = storeToRefs(sysConfigStore);

// 全局title（后端配置没拿到时的兜底，见 product-brand.ts）
const title = import.meta.env.VITE_GLOB_APP_TITLE || DEFAULT_PRODUCT_TITLE;
// 从系统配置中获取logo
const sysLogo = computed(() => {
  return handleUrl(systemConfig.value?.systemLogo);
});
// 从系统配置中获取标题
const bannerTitle = computed(() => {
  return systemConfig.value?.systemName || title;
});
</script>
<style lang="scss" scoped>
.banner_title {
  position: absolute;
  top: 30px;
  left: 30px;
  display: flex;
  column-gap: $margin-text;
  align-items: center;
  font-size: $font-size-title-2;
  font-weight: bold;
  color: $color-primary;
}
</style>
