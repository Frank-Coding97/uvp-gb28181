<template>
  <div class="layout-main-shell">
    <a-watermark :content="watermark" v-bind="watermarkConfig" class="layout-main-watermark">
      <a-layout-content class="layout-main-content">
        <router-view v-slot="{ Component, route }">
          <s-main-transition>
            <keep-alive :include="cacheRoutes">
              <component
                :is="createComponentWrapper(Component, route)"
                :key="resolveMediaRouteRenderKey(route)"
                v-if="refreshPage"
              />
            </keep-alive>
          </s-main-transition>
        </router-view>
      </a-layout-content>
    </a-watermark>
  </div>
</template>

<script setup lang="ts">
import { storeToRefs } from "pinia";
import { useThemeConfig } from "@/store/modules/theme-config";
import { useRouteConfigStore } from "@/store/modules/route-config";
import { resolveMediaRouteRenderKey } from "./mediaRouteKey";
const themeStore = useThemeConfig();
let { refreshPage, watermark, watermarkStyle, watermarkRotate, watermarkGap } = storeToRefs(themeStore);
const routerStore = useRouteConfigStore();
const { cacheRoutes } = storeToRefs(routerStore);

// 组件包装器
const wrapperMap = new Map();
// 为每个路由创建一个独立的组件包装器（wrapper），使 <keep-alive> 能够正确缓存同一个组件在不同路由下的多个实例。
const createComponentWrapper = (component: any, route: any) => {
  // 守卫：组件不存在（如路由未匹配到）则直接返回
  if (!component) return;
  // 如果路由未开启 keepAlive 缓存，则无需包装，直接渲染原始组件
  if (!route.meta?.keepAlive) return component;
  // 包装器名称、组件 key 与 keep-alive include 必须使用同一个路由身份。
  const wrapperName = resolveMediaRouteRenderKey(route);
  // 从缓存 Map 中查找是否已存在该路径对应的包装器
  let wrapper = wrapperMap.get(wrapperName);
  if (!wrapper) {
    // 创建包装器组件：name 用于 keep-alive 的 include 匹配，render 返回原始组件的 VNode
    wrapper = { name: wrapperName, render: () => h(component) };
    // 将包装器存入 Map 缓存，避免重复创建
    wrapperMap.set(wrapperName, wrapper);
  }
  // 返回包装器组件定义，让 <component :is> 能正确应用 :key，
  // 避免返回 VNode 时 key 被忽略导致快速切换白屏
  return wrapper;
};

// 水印配置
const watermarkConfig = computed(() => {
  return {
    font: watermarkStyle.value,
    rotate: watermarkRotate.value,
    gap: watermarkGap.value
  };
});
</script>

<style lang="scss" scoped>
.layout-main-shell {
  display: flex;
  flex: 1;
  height: 100%;
  min-height: 0;
  overflow: hidden;
}

:deep(.layout-main-watermark) {
  flex: 1;
  height: 100%;
}

.layout-main-content {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  overflow-x: hidden;
  overflow-y: auto;
}

// 修改左侧滚动条宽度-主要针对main窗口内的滚动条
:deep(.arco-scrollbar-thumb-direction-vertical .arco-scrollbar-thumb-bar) {
  width: 4px;
  margin-left: 8px;
}
</style>
