<template>
  <div :class="asideDark ? 'aside dark' : 'aside'">
    <Logo />
    <a-layout-sider :collapsed="collapsed" breakpoint="xl" class="layout_side" :width="200">
      <a-scrollbar style="height: 100%; overflow: auto" outer-class="scrollbar"><Menu :route-tree="routeTree" /></a-scrollbar>
    </a-layout-sider>
  </div>
</template>

<script setup lang="ts">
import Logo from "@/layout/components/Logo/index.vue";
import Menu from "@/layout/components/Menu/index.vue";
import { storeToRefs } from "pinia";
import { useThemeConfig } from "@/store/modules/theme-config";
import { useRouteConfigStore } from "@/store/modules/route-config";
const themeStore = useThemeConfig();
const { collapsed, asideDark } = storeToRefs(themeStore);
const routerStore = useRouteConfigStore();
const { routeTree } = storeToRefs(routerStore);
</script>

<style lang="scss" scoped>
.aside {
  display: flex;
  flex-direction: column;
  height: 100vh;
  background: #fff;
  border-right: 1px solid #e8e8e8;
}
.dark {
  background: #232324;
}
.layout_side {
  flex: 1;
  overflow: hidden;

  .scrollbar {
    height: 100%;
  }
}

// 修改左侧滚动条宽度
:deep(.arco-scrollbar-thumb-direction-vertical .arco-scrollbar-thumb-bar) {
  width: 4px;
  margin-left: 8px;
}

// 去掉右侧阴影/边线(由外层 .aside 提供)
:deep(.arco-layout-sider-light) {
  border-right: none;
  box-shadow: unset;
  background: transparent;
}

// 去掉 sider 自带背景
.arco-layout-sider {
  background: transparent;
}

// === Menu 样式覆写 — 跟 prototype 一致(浅色/蓝色 active/左侧 3px 蓝条) ===
:deep(.arco-menu-light) {
  background: transparent;

  // 取消 Arco 默认菜单 padding
  .arco-menu-inner {
    padding: 8px 0 !important;
  }

  // 菜单项
  .arco-menu-item {
    height: 40px;
    line-height: 40px;
    margin: 0 !important;
    padding: 0 20px !important;
    border-radius: 0 !important;
    border-left: 3px solid transparent;
    color: #666;
    font-size: 14px;
    transition: all .15s;

    &:hover {
      background: #f0f7ff !important;
      color: #1890ff !important;
    }

    .arco-icon {
      width: 18px;
      height: 18px;
    }
  }

  // 选中态
  .arco-menu-item.arco-menu-selected {
    background: #e6f7ff !important;
    color: #1890ff !important;
    border-left-color: #1890ff;
    font-weight: 500;
  }

  // 子菜单
  .arco-menu-inline-header {
    height: 40px;
    line-height: 40px;
    margin: 0 !important;
    padding: 0 20px !important;
    color: #666;
    font-size: 14px;

    &:hover {
      background: #f0f7ff !important;
      color: #1890ff !important;
    }
  }

  .arco-menu-inline-header.arco-menu-selected {
    color: #1890ff !important;
    font-weight: 500;
  }

  // 子菜单展开后的子项缩进
  .arco-menu-inline-content .arco-menu-item {
    padding-left: 48px !important;
  }
}

// 折叠菜单的 icon 居中
:deep(.arco-menu-vertical.arco-menu-collapsed) {
  .arco-menu-has-icon {
    justify-content: center;
    padding: 0;
  }
  .arco-menu-icon {
    padding: 10px 0;
    margin-right: 0;
  }
  .arco-menu-title {
    display: none;
  }
}
</style>
