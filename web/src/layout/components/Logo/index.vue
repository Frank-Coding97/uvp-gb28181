<template>
  <div :class="['logo_head', { 'no-border': layoutType == 'layoutHead', collapsed }]">
    <div class="logo_box" :class="(collapsed || layoutType == 'layoutHead') && 'padding-unset'">
      <!-- <img v-if="sysLogo" :src="sysLogo" alt="系统logo" style="width: 32px; height: 32px;" />
            <s-svg-icon v-else name="snow" :size="32" /> -->
      <div class="logo_mark">
        <LogoSvg :imageUrl="sysLogo" :width="40" :height="40" />
      </div>
      <div class="logo_text" v-if="isTitle">
        <div class="logo_title_row">
          <span :class="isDark ? 'logo_title dark' : 'logo_title'">UVP</span>
          <span class="logo_badge">GB28181</span>
        </div>
        <div class="logo_meta_row">
          <a-popover
            v-if="canRead"
            v-model:popup-visible="noticeVisible"
            trigger="click"
            position="right"
            :content-style="{ padding: 0 }"
          >
            <button
              class="logo_version"
              :class="{ 'logo_version--pending': pending, 'logo_version--dark': isDark }"
              type="button"
              data-testid="logo-version"
              :aria-label="pending ? `当前版本 v${currentVersion}，有新版本 v${latestVersion}` : `当前版本 v${currentVersion}`"
            >
              v{{ currentVersion }}<span v-if="pending" class="logo_version_dot" aria-hidden="true" />
            </button>
            <template #content>
              <div class="version-card" data-testid="logo-version-card">
                <div class="version-card_head">
                  <span>当前版本</span>
                  <button
                    class="version-card_refresh"
                    type="button"
                    aria-label="重新读取更新状态"
                    :disabled="refreshing"
                    @click="refresh"
                  >
                    <icon-refresh :size="14" :class="{ 'version-card_spin': refreshing }" />
                  </button>
                </div>
                <div class="version-card_current">v{{ currentVersion }}</div>
                <p v-if="!notice?.checkEnabled" class="version-card_hint">未配置官方更新源，当前只能离线导入升级包。</p>
                <template v-else-if="pending">
                  <p class="version-card_latest">最新版本：v{{ latestVersion }}</p>
                  <div class="version-card_update">
                    <span class="version-card_update-title">有新版本可用！</span>
                    <span class="version-card_update-version">v{{ latestVersion }}</span>
                  </div>
                  <button class="version-card_action" type="button" @click="goUpgrade">前往升级</button>
                  <div v-if="canIgnore" class="version-card_links">
                    <span />
                    <button type="button" :disabled="ignoring" @click="ignoreCurrentVersion">忽略此版本</button>
                  </div>
                </template>
                <template v-else>
                  <p class="version-card_latest">
                    {{ notice?.hasUpdate ? `已忽略 v${latestVersion}` : "已是最新版本" }}
                  </p>
                  <div class="version-card_links">
                    <span>{{ checkedAtText }}</span>
                    <button v-if="notice?.ignored && canIgnore" type="button" :disabled="ignoring" @click="restoreIgnoredVersion">
                      取消忽略
                    </button>
                  </div>
                </template>
              </div>
            </template>
          </a-popover>
          <span v-else class="logo_version logo_version--plain" :class="{ 'logo_version--dark': isDark }">
            v{{ currentVersion }}
          </span>
          <span class="logo_subtitle">{{ subtitle }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { storeToRefs } from "pinia";
import { ref } from "vue";
import { useRouter } from "vue-router";
import { useThemeConfig } from "@/store/modules/theme-config";
const themeStore = useThemeConfig();
const { collapsed, asideDark, layoutType } = storeToRefs(themeStore);
import { handleUrl } from "@/utils/app";
import { useSysConfigStore } from "@/store/modules/sys-config";
import LogoSvg from "@/components/s-logo/index.vue";
import { useUpgradeNotice } from "@/hooks/useUpgradeNotice";

/**
 * 版本徽标：后台每 6 小时查一次官方源，这里只读缓存。
 * 有新版本时冒橙点，点开是更新卡片（照 Sub2API 的形态）。
 */
const {
  notice,
  canRead,
  canIgnore,
  currentVersion,
  latestVersion,
  checkedAt,
  pending,
  refreshing,
  ignoring,
  refresh,
  ignoreCurrentVersion,
  restoreIgnoredVersion
} = useUpgradeNotice();
const noticeVisible = ref(false);
const router = useRouter();

const checkedAtText = computed(() => {
  if (!checkedAt.value) return "尚未完成一次检查";
  const time = new Date(checkedAt.value);
  if (Number.isNaN(time.getTime())) return "尚未完成一次检查";
  return `上次检查 ${time.toLocaleString("zh-CN", { hour12: false })}`;
});

const goUpgrade = () => {
  noticeVisible.value = false;
  // check=1 让升级页落地即自动查一次，省掉「再点一次检查在线更新」。
  void router.push({ path: "/system/service-upgrade", query: { check: "1" } });
};

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
    background: linear-gradient(
      90deg,
      rgb(148 163 184 / 0%),
      rgb(148 163 184 / 24%) 18%,
      rgb(148 163 184 / 10%) 72%,
      rgb(148 163 184 / 0%)
    );
  }

  &.collapsed {
    padding-right: 0;
    padding-left: 0;
  }

  .logo_box {
    display: flex;
    column-gap: 11px;
    align-items: center;
    width: 100%;
    padding: 0;
    overflow: hidden;
    background: transparent;
    border: 0;
    border-radius: 0;
    box-shadow: none;
  }

  .logo_mark {
    display: block;
    flex: 0 0 40px;
    width: 40px;
    height: 40px;
    overflow: hidden;
  }

  .logo_text {
    display: flex;
    flex-direction: column;
    row-gap: 4px;
    min-width: 0;
  }

  .logo_title_row {
    display: flex;
    column-gap: 7px;
    align-items: center;
    min-width: 0;
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
    line-height: 1.05;
    color: var(--uvp-sidebar-title);
    text-align: left;
    white-space: nowrap;
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

  .logo_meta_row {
    display: flex;
    column-gap: 6px;
    align-items: center;
    min-width: 0;
  }

  // 版本徽标。灵感来自 Sub2API：常显版本号，有新版本才变橙并冒点。
  .logo_version {
    display: inline-flex;
    flex: 0 0 auto;
    gap: 4px;
    align-items: center;
    height: 19px;
    padding: 0 7px;
    font-size: 11px;
    font-weight: 640;
    font-variant-numeric: tabular-nums;
    line-height: 1;
    color: var(--uvp-text-secondary);
    white-space: nowrap;
    cursor: pointer;
    background: rgb(148 163 184 / 12%);
    border: 1px solid rgb(148 163 184 / 22%);
    border-radius: 999px;
    transition:
      background 0.15s ease,
      color 0.15s ease,
      border-color 0.15s ease;

    &:hover {
      color: var(--uvp-brand);
      background: var(--uvp-brand-soft);
      border-color: color-mix(in srgb, var(--uvp-brand) 34%, transparent);
    }

    &--pending {
      color: #b45309;
      background: #fef3c7;
      border-color: #fcd34d;

      &:hover {
        color: #92400e;
        background: #fde68a;
        border-color: #fbbf24;
      }
    }

    &--plain {
      cursor: default;

      &:hover {
        color: var(--uvp-text-secondary);
        background: rgb(148 163 184 / 12%);
        border-color: rgb(148 163 184 / 22%);
      }
    }

    &--dark {
      color: rgb(203 213 225 / 88%);
      background: rgb(148 163 184 / 16%);
      border-color: rgb(148 163 184 / 28%);

      &.logo_version--plain:hover {
        color: rgb(203 213 225 / 88%);
        background: rgb(148 163 184 / 16%);
        border-color: rgb(148 163 184 / 28%);
      }
    }
  }

  .logo_version_dot {
    flex: 0 0 auto;
    width: 6px;
    height: 6px;
    background: #f59e0b;
    border-radius: 50%;
    box-shadow: 0 0 0 2px rgb(245 158 11 / 24%);
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

// 更新卡片。它由 popover teleport 到 body，配色只跟随全局主题，
// 不能沿用侧边栏的 asideDark。
.version-card {
  box-sizing: border-box;
  width: 244px;
  padding: 14px 16px 12px;
  font-size: 12px;
  color: var(--uvp-text-primary);
}

.version-card_head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 12px;
  font-weight: 600;
  color: var(--uvp-text-secondary);
}

.version-card_refresh {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  color: var(--uvp-text-tertiary);
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: 6px;

  &:hover:not(:disabled) {
    color: var(--uvp-brand);
    background: var(--uvp-brand-soft);
  }

  &:disabled {
    cursor: not-allowed;
    opacity: 0.5;
  }
}

.version-card_spin {
  animation: version-card-spin 0.8s linear infinite;
}

@keyframes version-card-spin {
  to {
    transform: rotate(360deg);
  }
}

.version-card_current {
  margin: 6px 0 2px;
  font-size: 22px;
  font-weight: 720;
  font-variant-numeric: tabular-nums;
  line-height: 1.2;
  color: var(--uvp-text-primary);
  text-align: center;
  letter-spacing: -0.01em;
}

.version-card_latest {
  margin: 0 0 10px;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
  text-align: center;
}

.version-card_hint {
  margin: 8px 0 2px;
  font-size: 12px;
  line-height: 1.5;
  color: var(--uvp-text-tertiary);
}

// 有新版本：橙色提示块 + 主按钮（照 Sub2API 的层次）
.version-card_update {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 10px 12px;
  margin-bottom: 10px;
  background: #fff8e6;
  border: 1px solid #fbd88b;
  border-radius: 10px;
}

.version-card_update-title {
  font-size: 13px;
  font-weight: 700;
  color: #b45309;
}

.version-card_update-version {
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  color: #b45309;
}

.version-card_action {
  width: 100%;
  height: 34px;
  font-size: 13px;
  font-weight: 600;
  color: var(--uvp-solid-text);
  cursor: pointer;
  background: var(--uvp-solid-bg);
  border: 0;
  border-radius: 8px;

  &:hover {
    background: var(--uvp-solid-hover-bg);
  }
}

.version-card_links {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 8px;
  font-size: 11px;
  color: var(--uvp-text-tertiary);

  button {
    padding: 0;
    font-size: 11px;
    color: var(--uvp-text-tertiary);
    cursor: pointer;
    background: transparent;
    border: 0;

    &:hover:not(:disabled) {
      color: var(--uvp-brand);
      text-decoration: underline;
    }

    &:disabled {
      cursor: not-allowed;
      opacity: 0.6;
    }
  }
}
</style>
