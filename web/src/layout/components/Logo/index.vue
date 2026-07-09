<template>
    <div :class="layoutType == 'layoutHead' ? 'logo_head no-border' : 'logo_head'">
        <div class="logo_box" :class="(collapsed || layoutType == 'layoutHead') && 'padding-unset'">
            <!-- <img v-if="sysLogo" :src="sysLogo" alt="系统logo" style="width: 32px; height: 32px;" />
            <s-svg-icon v-else name="snow" :size="32" /> -->
            <div class="logo_mark">
                <LogoSvg :imageUrl="sysLogo" :width="26" :height="26" />
            </div>
            <div class="logo_text" v-if="isTitle">
                <div class="logo_title_row">
                    <span :class="isDark ? 'logo_title dark' : 'logo_title'">UVP</span>
                    <span class="logo_badge">GB28181</span>
                </div>
                <span class="logo_subtitle">{{ subtitle }}</span>
            </div>
        </div>
    </div>
</template>

<script setup lang="ts">
import { storeToRefs } from "pinia";
import { useThemeConfig } from "@/store/modules/theme-config";
const themeStore = useThemeConfig();
const { collapsed, asideDark, layoutType } = storeToRefs(themeStore);
import { handleUrl } from "@/utils/app"
import { useSysConfigStore } from "@/store/modules/sys-config";
import LogoSvg from "@/components/s-logo/index.vue";

// 获取系统配置
const sysConfigStore = useSysConfigStore();
const { systemConfig } = storeToRefs(sysConfigStore);

// 全局title
const title = import.meta.env.VITE_GLOB_APP_TITLE;



// 从系统配置中获取标题
const bannerTitle = computed(() => {
    return systemConfig.value?.systemName || title;
});

const subtitle = computed(() => {
    return bannerTitle.value.replace(/^UVP\s*/, "") || "统一视频接入平台";
});

// 从系统配置中获取logo
const sysLogo = computed(() => {
    return handleUrl(systemConfig.value?.systemLogo);
});




// 黑暗模式的文字渲染
const isDark = computed(() => {
    if (asideDark.value && layoutType.value != "layoutHead") {
        return true;
    } else {
        return false;
    }
});

// 是否展示标题
const isTitle = computed(() => {
    if (!collapsed.value || layoutType.value == "layoutHead") {
        return true;
    } else {
        return false;
    }
});
</script>

<style lang="scss" scoped>
// 头部
.logo_head {
    position: relative;
    box-sizing: border-box;
    display: flex;
    align-items: center;
    justify-content: flex-start;
    min-height: calc(var(--uvp-header-height) - 1px);
    padding: 8px 18px 7px 20px;
    background: transparent;
    border-bottom: 0;

    &::after {
        position: absolute;
        right: 20px;
        bottom: 0;
        left: 28px;
        height: 1px;
        content: "";
        background: linear-gradient(90deg, rgb(148 163 184 / 0%), rgb(148 163 184 / 24%) 18%, rgb(148 163 184 / 10%) 72%, rgb(148 163 184 / 0%));
    }

    .logo_box {
        display: flex;
        align-items: center;
        column-gap: 11px;
        width: 100%;
        padding: 0;
        overflow: hidden;
        background: transparent;
        border: 0;
        border-radius: 0;
        box-shadow: none;
    }

    .logo_mark {
        display: grid;
        flex: 0 0 40px;
        width: 40px;
        height: 40px;
        place-items: center;
        background: var(--uvp-sidebar-brand-mark-bg);
        border: 1px solid rgb(190 213 236 / 58%);
        border-radius: 12px;
        box-shadow: inset 0 1px 0 rgb(255 255 255 / 86%), 0 10px 20px -18px rgb(37 99 235 / 34%);
    }

    .logo_text {
        display: flex;
        flex-direction: column;
        min-width: 0;
        row-gap: 4px;
    }

    .logo_title_row {
        display: flex;
        align-items: center;
        min-width: 0;
        column-gap: 7px;
    }

    // 折叠或者是横向布局-去掉padding，logo居中
    .padding-unset {
        justify-content: center;
        padding-right: 0;
        padding-left: 0;
    }

    .logo_title {
        box-sizing: border-box;
        max-width: 88px;
        overflow: hidden;
        text-overflow: ellipsis;
        font-size: 18px;
        font-weight: 780;
        color: var(--uvp-sidebar-title);
        text-align: left;
        white-space: nowrap;
        line-height: 1.05;
    }

    .logo_badge {
        flex: 0 0 auto;
        padding: 2px 6px;
        font-size: 10px;
        font-weight: 720;
        line-height: 1.2;
        color: var(--uvp-brand-strong);
        background: rgb(37 99 235 / 7%);
        border: 1px solid rgb(37 99 235 / 12%);
        border-radius: 999px;
    }

    .logo_subtitle {
        max-width: 172px;
        overflow: hidden;
        text-overflow: ellipsis;
        font-size: 12px;
        font-weight: 520;
        line-height: 1.2;
        color: var(--uvp-text-tertiary);
        white-space: nowrap;
    }

    .dark {
        color: #ffffff;
    }
}

.no-border {
    border: unset;
}
</style>
