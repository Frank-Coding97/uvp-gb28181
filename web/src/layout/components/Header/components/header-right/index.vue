<template>
  <div class="header_setting" :class="isMobile && 'head-absolute-fix'">
    <!-- SIP 引导提醒:未配置/启动失败时才显示 -->
    <SipSetupBell />
    <!-- 我的 -->
    <a-dropdown trigger="click" position="br" :popup-max-height="false">
      <button class="my_setting" id="system-my-setting" type="button" aria-label="账号">
        <a-image width="32" height="32" fit="cover" :src="account.avatar" :preview="false" class="my_image" />
        <span class="user-nickname">{{ accountDisplayName }}</span>
        <div class="icon_down">
          <icon-down style="stroke-width: 3" />
        </div>
      </button>
      <template #content>
        <div class="uvp-user-menu-profile">
          <a-image width="40" height="40" fit="cover" :src="account.avatar" :preview="false" class="uvp-user-menu-profile__avatar" />
          <div class="uvp-user-menu-profile__text">
            <strong class="uvp-user-menu-profile__name">{{ accountDisplayName }}</strong>
            <span class="uvp-user-menu-profile__account">{{ account.username }}</span>
          </div>
        </div>
        <a-divider margin="0" />
        <!-- 工作台 -->
        <RecordingDownloadCenter menu />
        <a-doption class="uvp-user-menu-option" @click="onSystemSetting">
          <template #default>
            <span class="uvp-user-menu-icon"><icon-settings :size="16" /></span>
            <span>{{ $t(`system.system-settings`) }}</span>
          </template>
        </a-doption>
        <a-divider margin="0" />
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
        <a-doption class="uvp-user-menu-option" @click="logOut">
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
import SipSetupBell from "@/layout/components/Header/components/SipSetupBell.vue";
import RecordingDownloadCenter from "@/layout/components/Header/components/RecordingDownloadCenter.vue";
import SystemSettings from "@/layout/components/Header/components/system-settings/index.vue";
//import myImage from "@/assets/img/my-image.jpg";
import { Modal } from "@arco-design/web-vue";
import { useRouter } from "vue-router";
//import { useUserInfoStore } from "@/store/modules/user-info";
import { useDevicesSize } from "@/hooks/useDevicesSize";
import { useRouteConfigStore } from "@/store/modules/route-config";
import { logout } from "@/api/user";
const router = useRouter();
const { isMobile } = useDevicesSize();
//const userStore = useUserInfoStore();
//const { account } = storeToRefs(userStore);
import { runUserLogoutCleanup, useUserStoreHook } from "@/store/modules/user";
const account = useUserStoreHook().account;
const accountDisplayName = computed(() => account.nickname || account.username || "用户");

// 系统设置
const systemOpen = ref(false);
const onSystemSetting = () => {
  systemOpen.value = true;
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
        await runUserLogoutCleanup();
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
        await useUserStoreHook().logOut(false);
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
  justify-content: flex-end;
  min-width: 0;
  gap: 8px;
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
    appearance: none;
    display: flex;
    align-items: center;
    gap: 8px;
    height: 40px;
    margin-left: 8px;
    padding: 4px 8px;
    overflow: hidden;
    color: var(--uvp-text-primary);
    background: transparent;
    border: 0;
    border-radius: 4px;
    cursor: pointer;
    transition:
      background-color 0.2s ease,
      color 0.2s ease;

    &:hover {
      background: color-mix(in srgb, var(--uvp-text-primary) 7%, transparent);
    }

    .my_image {
      flex: 0 0 32px;
      overflow: hidden;
      border-radius: 8px;
    }

    .user-nickname {
      max-width: 144px;
      overflow: hidden;
      font-size: 13px;
      font-weight: 520;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .icon_down {
      display: inline-flex;
      margin-left: 2px;
      color: var(--uvp-text-tertiary);
      transform: rotate(0deg);
      transition: transform 0.2s;
    }
  }
}

:deep(.arco-dropdown-open) {
  .icon_down {
    transform: rotate(180deg) !important;
  }
}

:global(.arco-dropdown:has(.uvp-user-menu-profile)) {
  width: 260px;
  min-width: 260px;
  padding: 4px;
  background: var(--uvp-popconfirm-bg) !important;
  border: 0 !important;
  border-radius: 12px;
  box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--uvp-text-primary) 9%, transparent), var(--uvp-popconfirm-shadow) !important;
}

:global(.arco-dropdown:has(.uvp-user-menu-profile) .arco-dropdown-list) {
  display: flex;
  flex-direction: column;
  gap: 0;
}

:global(.arco-dropdown:has(.uvp-user-menu-profile) .uvp-user-menu-profile) {
  box-sizing: border-box;
  display: flex;
  gap: 10px;
  align-items: center;
  height: 60px;
  min-height: 60px;
  padding: 8px;
}

:global(.arco-dropdown:has(.uvp-user-menu-profile) .uvp-user-menu-profile__avatar) {
  flex: 0 0 40px;
  overflow: hidden;
  border-radius: 9px;
}

:global(.arco-dropdown:has(.uvp-user-menu-profile) .uvp-user-menu-profile__text) {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}

:global(.arco-dropdown:has(.uvp-user-menu-profile) .uvp-user-menu-profile__name),
:global(.arco-dropdown:has(.uvp-user-menu-profile) .uvp-user-menu-profile__account) {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:global(.arco-dropdown:has(.uvp-user-menu-profile) .uvp-user-menu-profile__name) {
  color: var(--uvp-text-primary);
  font-size: 14px;
  font-weight: 600;
}

:global(.arco-dropdown:has(.uvp-user-menu-profile) .uvp-user-menu-profile__account) {
  color: var(--uvp-text-tertiary);
  font-size: 12px;
}

:global(.arco-dropdown:has(.uvp-user-menu-profile) .uvp-user-menu-option) {
  box-sizing: border-box;
  height: 36px;
  min-height: 36px;
  padding: 6px 8px;
  color: var(--uvp-text-secondary);
  background: transparent !important;
  border-radius: 8px;
  line-height: 24px;
  transition:
    background-color 0.18s ease,
    color 0.18s ease;
}

:global(.arco-dropdown:has(.uvp-user-menu-profile) .uvp-user-menu-option:hover) {
  color: var(--uvp-text-primary) !important;
  background: color-mix(in srgb, var(--uvp-text-primary) 7%, transparent) !important;
}

:global(.arco-dropdown:has(.uvp-user-menu-profile) .uvp-user-menu-option .arco-dropdown-option-content) {
  display: flex;
  gap: 12px;
  align-items: center;
  width: 100%;
  font-size: 13px;
  font-weight: 400;
}

:global(.arco-dropdown:has(.uvp-user-menu-profile) .uvp-user-menu-icon) {
  display: inline-grid;
  width: 20px;
  height: 20px;
  color: var(--uvp-text-tertiary);
  place-items: center;
}

:global(.arco-dropdown:has(.uvp-user-menu-profile) .uvp-user-menu-option:hover .uvp-user-menu-icon) {
  color: var(--uvp-text-primary);
}

:global(.arco-dropdown:has(.uvp-user-menu-profile) .arco-divider) {
  margin: 4px 0;
  border-color: color-mix(in srgb, var(--uvp-text-primary) 9%, transparent);
}
</style>
