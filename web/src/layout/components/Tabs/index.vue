<template>
  <div class="tabs">
    <a-tabs
      :editable="true"
      :hide-content="true"
      :active-key="currentRoute.path"
      size="medium"
      type="line"
      @tab-click="onTabs"
      @delete="onDelete"
    >
      <a-tab-pane v-for="item of tabsList" :key="item.path" :closable="!item.meta.affix">
        <template #title>
          <a-dropdown trigger="contextMenu" position="bl" :popup-max-height="false">
            <span class="tabs-tab-title" title="右键打开标签操作">
              <MenuItemIcon v-if="item.meta.svgIcon || item.meta.icon" :svg-icon="item.meta.svgIcon" :icon="item.meta.icon" />
              <span>{{ $t(`menu.${item.meta.title}`) }}</span>
            </span>
            <template #content>
              <a-doption @click="refresh(item)">
                <template #icon><icon-refresh /></template>
                {{ $t(`system.refresh`) }}
              </a-doption>
              <a-doption :disabled="item.meta.affix" @click="onDelete(item.path)">
                <template #icon><icon-close /></template>
                {{ $t(`system.close-current`) }}
              </a-doption>
              <a-doption @click="closeSides('left', item.path)">
                <template #icon><icon-left /></template>
                {{ $t(`system.close-left-side`) }}
              </a-doption>
              <a-doption @click="closeSides('right', item.path)">
                <template #icon><icon-right /></template>
                {{ $t(`system.close-right-side`) }}
              </a-doption>
              <a-doption @click="closeOther('other', item.path)">
                <template #icon><icon-close-circle /></template>
                {{ $t(`system.close-other`) }}
              </a-doption>
              <a-doption @click="closeOther('all', item.path)">
                <template #icon><icon-folder-delete /></template>
                {{ $t(`system.close-all`) }}
              </a-doption>
            </template>
          </a-dropdown>
        </template>
      </a-tab-pane>
    </a-tabs>
    <div class="tabs_setting">
      <a-space>
        <!-- 录像缓存任务入口：角标 = 正在缓存到服务器的任务数。
             放在全屏/主题切换旁边，和"系统级操作"这一排保持一致。 -->
        <RecordCacheTaskCenter />
        <a-tooltip :content="$t(`system.${fullScreen ? 'full-screen' : 'exit-full-screen'}`)" position="bottom" mini>
          <button
            id="system-tabs-fullscreen"
            class="tabs-action"
            type="button"
            :aria-label="$t(`system.${fullScreen ? 'full-screen' : 'exit-full-screen'}`)"
            @click="onFullScreen"
          >
            <icon-fullscreen v-if="fullScreen" :size="18" />
            <icon-fullscreen-exit v-else :size="18" />
          </button>
        </a-tooltip>
        <a-tooltip :content="darkMode ? '明亮' : '暗色'" position="bottom" mini>
          <button
            id="system-tabs-theme"
            class="tabs-action"
            type="button"
            :aria-label="darkMode ? '明亮' : '暗色'"
            @click="toggleThemeMode"
          >
            <icon-sun-fill v-if="!darkMode" :size="18" />
            <icon-moon-fill v-else :size="18" />
          </button>
        </a-tooltip>
      </a-space>
    </div>
  </div>
</template>

<script setup lang="ts">
import { storeToRefs } from "pinia";
import { nextTick } from "vue";
import { useRouter } from "vue-router";
import { useRouteConfigStore } from "@/store/modules/route-config";
import { useThemeConfig } from "@/store/modules/theme-config";
import { useHeaderDisplayActions } from "../Header/useHeaderDisplayActions";
import MenuItemIcon from "@/layout/components/Menu/menu-item-icon.vue";
import RecordCacheTaskCenter from "@/layout/components/Header/components/RecordCacheTaskCenter.vue";
const router = useRouter();
const routerStore = useRouteConfigStore();
const themeStore = useThemeConfig();
const { tabsList, currentRoute } = storeToRefs(routerStore);
const { darkMode, toggleThemeMode, fullScreen, onFullScreen } = useHeaderDisplayActions();

// 点击标签页，如果标签页存在，则跳转
const onTabs = (key: string) => {
  router.push(key);
};

// Closing from a tab menu is relative to that tab, not necessarily the active route.
const closeTabs = (paths: string[], preferredPath?: string) => {
  const removable = tabsList.value
    .filter((item: Menu.MenuOptions) => paths.includes(item.path) && !item.meta.affix)
    .map((item: Menu.MenuOptions) => item.path);
  if (!removable.length) return;
  tabsList.value = tabsList.value.filter((item: Menu.MenuOptions) => !removable.includes(item.path));
  routerStore.removeRoutePaths(removable);
  if (removable.includes(currentRoute.value.path)) {
    const fallback = tabsList.value.find((item: Menu.MenuOptions) => item.path === preferredPath) ?? tabsList.value.at(-1);
    if (fallback) router.push(fallback.path);
  }
};

const onDelete = (path: string) => closeTabs([path]);

const refresh = async (item: Menu.MenuOptions) => {
  if (currentRoute.value.path !== item.path) await router.push(item.path);
  if (currentRoute.value.path !== item.path) return;
  themeStore.setRefreshPage(false);
  if (item.meta.keepAlive) routerStore.removeRouteName(item.path);
  await nextTick();
  themeStore.setRefreshPage(true);
  if (item.meta.keepAlive) routerStore.setRoutePaths(item.path);
};

const closeSides = (side: "left" | "right", path: string) => {
  const index = tabsList.value.findIndex((item: Menu.MenuOptions) => item.path === path);
  if (index < 0) return;
  const items = side === "left" ? tabsList.value.slice(0, index) : tabsList.value.slice(index + 1);
  closeTabs(
    items.map((item: Menu.MenuOptions) => item.path),
    path
  );
};

const closeOther = (type: "other" | "all", path: string) => {
  closeTabs(
    tabsList.value
      .filter((item: Menu.MenuOptions) => type === "all" || item.path !== path)
      .map((item: Menu.MenuOptions) => item.path),
    path
  );
};
</script>

<style lang="scss" scoped>
.tabs {
  box-sizing: border-box;
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-width: 0;
  height: 40px;
  overflow: hidden;
  .tabs_setting {
    flex: 0 0 auto;
    margin: 0 0 0 $margin;
    .tabs-action {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      width: 28px;
      height: 28px;
      padding: 0;
      color: $color-text-2;
      appearance: none;
      cursor: pointer;
      background: transparent;
      border: 0;
      border-radius: 6px;

      &:hover {
        color: rgb(var(--primary-6));
        background: var(--color-primary-light-1);
      }
    }
  }
}
.tabs :deep(.arco-tabs) {
  flex: 1;
  min-width: 0;
  overflow: hidden;
}
:deep(.arco-tabs-nav-type-line) {
  height: 40px;
}
:deep(.arco-tabs-nav) {
  min-width: 0;

  &::before {
    background: unset;
  }
}
:deep(.arco-tabs-nav-type-line .arco-tabs-tab) {
  height: 34px;
  padding: 6px 10px;
  margin: 0 2px;
  border-radius: 8px;
  transition:
    color 0.2s ease,
    background-color 0.2s ease;
}
:deep(.arco-tabs-nav-type-line.arco-tabs-nav-horizontal > .arco-tabs-tab:first-of-type) {
  margin-left: 2px;
}
:deep(.arco-tabs-tab-active),
:deep(.arco-tabs-tab-active:hover) {
  position: relative;
  z-index: 1;
  font-weight: 500;
  color: rgb(var(--primary-6));
  background: var(--color-primary-light-1);
}
:deep(.arco-tabs-nav-ink) {
  display: none;
}
:deep(.arco-tabs-nav-tab) {
  // 移入展示关闭icon
  min-width: 0;
  overflow: hidden;

  // 移入展示关闭icon
  .arco-tabs-tab-closable {
    .arco-tabs-tab-close-btn svg {
      width: 1em;
      opacity: 0.55;
      transition: opacity 0.2s ease;
    }
    &:hover .arco-tabs-tab-close-btn svg {
      opacity: 1;
    }
  }

  // 消除tab移入的背景色
  &:hover .arco-tabs-tab-title::before {
    background: unset;
  }
}

.tabs-tab-title {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  min-width: 0;

  :deep(svg) {
    flex: 0 0 auto;
    width: 16px;
    height: 16px;
  }
}
</style>
