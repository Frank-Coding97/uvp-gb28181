<template>
  <div class="header_setting" :class="isMobile && 'head-absolute-fix'">
    <!-- SIP 引导提醒:未配置/启动失败时才显示 -->
    <SipSetupBell />
    <RecordingDownloadCenter />
    <!-- 通知 -->
    <a-popover position="bottom" trigger="click">
      <a-button size="mini" type="text" class="icon_btn notice" id="system-notice">
        <template #icon>
          <icon-notification :size="18" />
        </template>
      </a-button>
      <template #content>
        <Notice />
      </template>
    </a-popover>
    <!-- 全屏 -->
    <a-tooltip :content="$t(`system.${fullScreen ? 'full-screen' : 'exit-full-screen'}`)">
      <a-button size="mini" type="text" class="icon_btn" id="system-fullscreen" @click="onFullScreen">
        <template #icon>
          <icon-fullscreen :size="18" v-if="fullScreen" />
          <icon-fullscreen-exit :size="18" v-else />
        </template>
      </a-button>
    </a-tooltip>
    <!-- 系统设置 -->
    <a-tooltip :content="$t(`system.system-settings`)">
      <a-button size="mini" type="text" class="icon_btn" id="system-settings" @click="onSystemSetting">
        <template #icon>
          <icon-settings :size="18" />
        </template>
      </a-button>
    </a-tooltip>
    <!-- 颜色模式 -->
    <a-dropdown trigger="click" position="bottom" @select="onThemeModeSelect">
      <a-button size="mini" type="text" class="icon_btn" id="system-dark">
        <template #icon>
          <icon-sun-fill :size="18" v-if="!darkMode" />
          <icon-moon-fill :size="18" v-else />
        </template>
      </a-button>
      <template #content>
        <a-doption value="light" :disabled="!darkMode">明亮模式</a-doption>
        <a-doption value="nightOps" :disabled="darkMode && darkModeStyle === 'nightOps'">夜间蓝灰</a-doption>
        <a-doption value="frostedBlack" :disabled="darkMode && darkModeStyle === 'frostedBlack'">磨砂黑</a-doption>
      </template>
    </a-dropdown>
    <!-- 我的 -->
    <a-dropdown trigger="click" position="br" :popup-max-height="false">
      <div class="my_setting" id="system-my-setting">
        <a-image width="32" height="32" fit="cover" :src="account.avatar" class="my_image" />
        <span class="user-nickname">{{ account.nickName }}</span>
        <div class="icon_down">
          <icon-down style="stroke-width: 3" />
        </div>
      </div>
      <template #content>
        <!-- 个人中心 -->
        <a-doption class="uvp-user-menu-option" @click="onPerson(1)">
          <template #default>
            <span class="uvp-user-menu-icon"><icon-user :size="16" /></span>
            <span>{{ $t(`system.personal-information`) }}</span>
          </template>
        </a-doption>
        <!-- 修改密码 -->
        <a-doption class="uvp-user-menu-option" @click="onPerson(2)">
          <template #default>
            <span class="uvp-user-menu-icon"><icon-lock :size="16" /></span>
            <span>{{ $t(`system.change-password`) }}</span>
          </template>
        </a-doption>
        <!-- 项目地址 -->
        <a-doption class="uvp-user-menu-option" @click="onProject">
          <template #default>
            <span class="uvp-user-menu-icon"><icon-link :size="16" /></span>
            <span>{{ $t(`system.project-address`) }}</span>
          </template>
        </a-doption>
        <a-divider margin="0" />
        <!-- 退出登录 -->
        <a-doption class="uvp-user-menu-option uvp-user-menu-option--danger" @click="logOut">
          <template #default>
            <span class="uvp-user-menu-icon"><icon-poweroff :size="16" /></span>
            <span>{{ $t(`system.logout`) }}</span>
          </template>
        </a-doption>
      </template>
    </a-dropdown>
  </div>
  <SystemSettings :system-open="systemOpen" @system-cancel="systemOpen = false" />
</template>

<script setup lang="ts">
import Notice from "@/layout/components/Header/components/Notice/index.vue";
import SipSetupBell from "@/layout/components/Header/components/SipSetupBell.vue";
import RecordingDownloadCenter from "@/layout/components/Header/components/RecordingDownloadCenter.vue";
import SystemSettings from "@/layout/components/Header/components/system-settings/index.vue";
//import myImage from "@/assets/img/my-image.jpg";
import { Modal } from "@arco-design/web-vue";
import { useRouter } from "vue-router";
import { storeToRefs } from "pinia";
//import { useUserInfoStore } from "@/store/modules/user-info";
import { useDevicesSize } from "@/hooks/useDevicesSize";
import { useRouteConfigStore } from "@/store/modules/route-config";
import { useThemeConfig } from "@/store/modules/theme-config";
import { useThemeMethods } from "@/hooks/useThemeMethods";
import { logout } from "@/api/user";
import { recordingDownloadCoordinator } from "@/views/gb28181/cloud-recordings/recordingDownloadService";
const router = useRouter();
const { isMobile } = useDevicesSize();
const themeStore = useThemeConfig();
const { darkMode, darkModeStyle } = storeToRefs(themeStore);
//const userStore = useUserInfoStore();
//const { account } = storeToRefs(userStore);
import { useUserStoreHook } from "@/store/modules/user";
const account = useUserStoreHook().account;

// 系统设置
const systemOpen = ref(false);
const onSystemSetting = () => {
  systemOpen.value = true;
};

// 颜色模式
const onThemeModeSelect = (value: string) => {
  darkMode.value = value !== "light";
  if (darkMode.value) {
    darkModeStyle.value = value;
  }
  const { setDarkMode } = useThemeMethods();
  setDarkMode();
};

// 全屏
const fullScreen = ref(true);
const onFullScreen = () => {
  if (!document.fullscreenElement) {
    document.documentElement.requestFullscreen();
    fullScreen.value = false;
  } else {
    if (document.exitFullscreen) {
      document.exitFullscreen();
      fullScreen.value = true;
    }
  }
};

// 个人中心
const onPerson = (type: number) => {
  router.push({
    path: "/system/userinfo",
    query: {
      id: account.id,
      userName: account.username,
      type
    }
  });
};

// 项目地址
const onProject = () => {
  window.open("https://gitee.com/Frank-Coding/uvp-gb28181", "_blank");
};

// 退出登录
const logOut = () => {
  Modal.warning({
    title: "提示",
    content: "确定退出登录？",
    hideCancel: false,
    closable: true,
    onBeforeOk: async () => {
      try {
        await recordingDownloadCoordinator.cancelAll();
        // 用户退出
        //await userStore.logOut();
        await logout().catch((error: any) => {
          // 根据项目规范，区分是否为请求取消
          if (!error.isCancelRequest) {
            console.warn("退出登录API调用失败，但继续执行本地清理:", error);
            // 可以选择显示警告信息，但不阻断流程
            // Message.warning("退出登录请求失败，但已清理本地数据");
          }
        });
        await useUserStoreHook().logOut();
        router.replace("/login");
        // 清除路由数据
        useRouteConfigStore().resetRoute();
        return true;
      } catch {
        return false;
      }
    }
  });
};
</script>

<style lang="scss" scoped>
.head-absolute-fix {
  position: absolute;
  top: 0;
  right: $padding;
}

.header_setting {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 100%;
  background-color: transparent;

  > .icon_btn {
    box-sizing: border-box;
    display: flex;
    align-items: center;
    justify-content: space-around;
    width: 32px;
    height: 32px;
    margin-left: 12px;
    color: var(--uvp-text-secondary);
    border-radius: 8px;
    transition:
      background-color 0.2s ease,
      color 0.2s ease;
  }

  > .icon_btn:hover {
    color: var(--uvp-brand);
    background: var(--uvp-brand-soft);
  }

  .my_setting {
    display: flex;
    align-items: center;
    justify-content: space-between;
    height: 38px;
    margin-left: 12px;
    padding: 0 8px 0 4px;
    overflow: hidden;
    color: var(--uvp-text-primary);
    border-radius: 999px;
    cursor: pointer;
    transition:
      background-color 0.2s ease,
      box-shadow 0.2s ease;

    &:hover {
      background: var(--uvp-brand-soft);
      box-shadow: inset 0 0 0 1px rgb(37 99 235 / 8%);
    }

    .my_image {
      margin-right: 8px;
      border-radius: 50%;
    }

    .user-nickname {
      white-space: nowrap;
    }

    .icon_down {
      margin: 0 0 0 5px;
      color: var(--uvp-text-tertiary);
      transform: rotate(0deg);
      transition: transform 0.2s;
    }
  }
}

.notice {
  position: relative;

  &::before {
    position: absolute;
    top: -4px;
    right: -2px;
    width: 6px;
    height: 6px;
    content: "";
    background: $color-danger;
    border: 2px solid var(--uvp-workspace-bg);
    border-radius: 50%;
  }
}

:deep(.arco-dropdown-open) {
  .icon_down {
    transform: rotate(180deg) !important;
  }
}

:deep(.arco-dropdown) {
  min-width: 178px;
  padding: 6px;
  background: rgb(255 255 255 / 98%);
  border: 1px solid var(--uvp-list-panel-border);
  border-radius: 12px;
  box-shadow: 0 16px 42px -28px rgb(31 45 61 / 55%);
}

:deep(.arco-dropdown-list) {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

:deep(.uvp-user-menu-option) {
  min-height: 38px;
  padding: 0 10px;
  color: var(--uvp-text-secondary);
  border-radius: 9px;
  transition:
    background-color 0.18s ease,
    color 0.18s ease;
}

:deep(.uvp-user-menu-option:hover) {
  color: var(--uvp-brand-strong);
  background: rgb(37 99 235 / 7%);
}

:deep(.uvp-user-menu-option .arco-dropdown-option-content) {
  display: flex;
  gap: 9px;
  align-items: center;
  width: 100%;
  font-size: 14px;
  font-weight: 520;
}

:deep(.uvp-user-menu-icon) {
  display: inline-grid;
  width: 24px;
  height: 24px;
  color: #60758b;
  place-items: center;
  background: rgb(100 116 139 / 8%);
  border-radius: 7px;
}

:deep(.uvp-user-menu-option:hover .uvp-user-menu-icon) {
  color: var(--uvp-brand-strong);
  background: rgb(37 99 235 / 10%);
}

:deep(.uvp-user-menu-arrow) {
  margin-left: auto;
  color: var(--uvp-text-tertiary);
}

:deep(.uvp-user-menu-option--danger) {
  color: #c24141;
}

:deep(.uvp-user-menu-option--danger .uvp-user-menu-icon) {
  color: #c24141;
  background: rgb(209 67 67 / 8%);
}

:deep(.uvp-user-menu-option--danger:hover) {
  color: #b42323;
  background: rgb(209 67 67 / 8%);
}

:deep(.arco-dropdown .arco-divider) {
  margin: 4px -6px;
  border-color: #e8eef6;
}
</style>
