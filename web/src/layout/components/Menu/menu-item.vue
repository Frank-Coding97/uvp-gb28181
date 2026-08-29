<template>
  <template v-if="isMediaMenuChildren">
    <a-menu-item-group
      v-for="section in mediaPresentation.sections"
      :key="`media-section-${section.key}`"
      class="uvp-media-menu-group"
    >
      <template #title>
        <span class="uvp-media-menu-group__title">
          <span class="uvp-media-menu-group__icon" aria-hidden="true">
            <MenuItemIcon :icon="section.icon" />
          </span>
          <span>{{ $t(`menu.${section.titleKey}`) }}</span>
        </span>
      </template>
      <MenuItem :route-tree="section.items" />
    </a-menu-item-group>
    <MenuItem v-if="mediaPresentation.ungrouped.length" :route-tree="mediaPresentation.ungrouped" />
  </template>
  <template v-else v-for="item in props.routeTree" :key="item.path">
    <a-sub-menu v-if="menuShow(item)" :key="item.path">
      <template #icon v-if="item.meta.svgIcon || item.meta.icon">
        <MenuItemIcon :svg-icon="item.meta.svgIcon" :icon="item.meta.icon" />
      </template>
      <template #title>{{ $t(`menu.${item.meta.title}`) }}</template>
      <MenuItem :route-tree="item.children || []" :parent-path="item.path" />
    </a-sub-menu>
    <a-menu-item v-else-if="aMenuShow(item)" :key="item?.path">
      <template #icon v-if="item.meta.svgIcon || item.meta.icon">
        <MenuItemIcon :svg-icon="item.meta.svgIcon" :icon="item.meta.icon" />
      </template>
      <span>{{ $t(`menu.${item.meta.title}`) }}</span>
    </a-menu-item>
  </template>
</template>

<script setup lang="ts">
import { computed } from "vue";
import MenuItem from "@/layout/components/Menu/menu-item.vue";
import MenuItemIcon from "@/layout/components/Menu/menu-item-icon.vue";
import { useMenuMethod } from "@/hooks/useMenuMethod";
import { groupMediaMenuItems, MEDIA_MENU_PARENT_PATH } from "./media-menu-sections";
defineOptions({ name: "MenuItem", inheritAttrs: false });

interface Props {
  routeTree: Menu.MenuOptions[];
  parentPath?: string;
}
// props的数据类型
// type类型参考：https://cn.vuejs.org/guide/typescript/composition-api.html#typing-component-props
const props = withDefaults(defineProps<Props>(), {
  routeTree: () => [],
  parentPath: ""
});

const { menuShow, aMenuShow } = useMenuMethod();
const isMediaMenuChildren = computed(() => props.parentPath === MEDIA_MENU_PARENT_PATH);
const mediaPresentation = computed(() => groupMediaMenuItems(props.routeTree));
</script>

<style lang="scss" scoped>
.uvp-media-menu-group__title {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.04em;
  line-height: 20px;
}

.uvp-media-menu-group__icon {
  display: inline-flex;
  flex: none;
  color: var(--color-text-4);
}

:deep(.uvp-media-menu-group > .arco-menu-group-title) {
  min-height: 30px;
  padding-top: 7px;
  padding-bottom: 3px;
  color: var(--color-text-3);
  cursor: default;
  user-select: none;
  background: transparent;
}

:deep(.uvp-media-menu-group > .arco-menu-group-title:hover) {
  color: var(--color-text-3);
  background: transparent;
}

:deep(.uvp-media-menu-group__icon .uvp-lucide-menu-icon) {
  width: 14px;
  height: 14px;
}
</style>
