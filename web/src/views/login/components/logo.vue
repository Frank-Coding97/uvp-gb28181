<template>
    <div class="banner_title">
        <LogoSvg :imageUrl="sysLogo" :width="40" :height="40" />
        {{ bannerTitle }}
    </div>
</template>
<script setup lang='ts'>
import { handleUrl } from "@/utils/app"
import { useSysConfigStore } from "@/store/modules/sys-config";
import { storeToRefs } from "pinia";
import LogoSvg from "@/components/s-logo/index.vue";

// 获取系统配置
const sysConfigStore = useSysConfigStore();
const { systemConfig } = storeToRefs(sysConfigStore);

// 全局title

// 从系统配置中获取logo
const sysLogo = computed(() => {
    return handleUrl(systemConfig.value?.systemLogo);
});
// 从系统配置中获取标题
const bannerTitle = computed(() => {
    return systemConfig.value?.systemName?.trim() || "统一视频接入平台";
});

</script>
<style lang='scss' scoped>
.banner_title {
    position: absolute;
    display: flex;
    top: 30px;
    left: 30px;
    column-gap: $margin-text;
    align-items: center;
    font-size: $font-size-title-2;
    font-weight: bold;
    color: $color-primary;
}
</style>