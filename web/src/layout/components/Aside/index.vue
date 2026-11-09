<template>
  <div :class="['aside', { dark: asideDark, collapsed }]">
    <Logo />
    <a-layout-sider :collapsed="collapsed" breakpoint="xl" class="layout_side" :width="256">
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
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  height: 100%;
}
.dark {
  background: #232324;
}
.layout_side {
  box-sizing: border-box;
  flex: 1;
  padding-top: 18px;
  overflow: hidden;

  /* ⛔⛔ 侧栏只允许 a-scrollbar 这一层滚动。
     Arco 的 LayoutSider 自带 `.arco-layout-sider-children { height:100%; overflow:auto }`，
     Arco 的 Menu 又自带 `.arco-menu-inner { height:100%; overflow:auto }`；
     而这里外面还套了一个 a-scrollbar。只要上面任意一层判定「内容比可视区高几个像素」
     （flex 分配出小数高度、Logo 高度变化、菜单项增减都会触发），它就会在自己右边缘
     再画一条 6px 的**原生**滚动条 —— 紧挨着 a-scrollbar 的 4px 自绘滑块，
     实机表现就是「侧栏双滚动条」（2026-11-09 用户截图取证：两条滑块相距约 6px，
     外侧那条颜色 #c3cfdd = --uvp-scrollbar-thumb 实色，内侧那条是 0.8 透明度的同色）。
     ⇒ 把中间两层的滚动能力收掉，滚动权唯一交给 a-scrollbar。 */
  :deep(.arco-layout-sider-children) {
    overflow: hidden;
  }

  /* `.arco-menu-inner` 若继续 `overflow:auto` 且高度确定，会把菜单内容**裁掉且不可滚**；
     改成 visible 后菜单自然撑高，由外层 a-scrollbar 统一滚。 */
  :deep(.arco-menu-inner) {
    overflow: visible;
  }

  .scrollbar {
    height: 100%;
  }
}

// 修改左侧滚动条宽度
:deep(.arco-scrollbar-thumb-direction-vertical .arco-scrollbar-thumb-bar) {
  width: 4px;
  margin-left: 10px;
}

// 去掉右侧阴影并替换为边线
:deep(.arco-layout-sider-light) {
  border-right: 0;
  box-shadow: unset;
}

// 解决折叠菜单的icon不居中问题
:deep(.arco-menu-vertical.arco-menu-collapsed) {
  // 消除icon的自带padding值，并且让元素居中
  .arco-menu-has-icon {
    justify-content: center;
    padding: 0;

    .arco-menu-icon {
      margin-right: 0;
    }
  }

  // 消除title占位
  .arco-menu-title {
    display: none;
  }
}

// 去掉sider背景
.arco-layout-sider {
  background: var(--uvp-navigation-bg);
}
</style>
