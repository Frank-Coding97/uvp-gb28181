<template>
  <a-menu
    :breakpoint="layoutType != 'layoutHead' ? 'xl' : undefined"
    :mode="'vertical'"
    :theme="asideDark ? 'dark' : 'light'"
    :collapsed="collapsed"
    :auto-scroll-into-view="true"
    :auto-open-selected="true"
    :accordion="isAccordion"
    :selected-keys="[selectedMenu]"
    :open-keys="openKeys"
    @menu-item-click="onMenuItem"
    @update:open-keys="onOpenKeysChange"
  >
    <MenuItem :route-tree="props.routeTree" />
  </a-menu>
</template>

<script setup lang="ts">
import MenuItem from "@/layout/components/Menu/menu-item.vue";
import { useRoutingMethod } from "@/hooks/useRoutingMethod";
import { storeToRefs } from "pinia";
import { useThemeConfig } from "@/store/modules/theme-config";
const route = useRoute();
const router = useRouter();
const themeStore = useThemeConfig();
const { collapsed, isAccordion, layoutType, asideDark } = storeToRefs(themeStore);

interface Props {
  routeTree: Menu.MenuOptions[];
}
// props的数据类型
// type类型参考：https://cn.vuejs.org/guide/typescript/composition-api.html#typing-component-props
const props = withDefaults(defineProps<Props>(), {
  routeTree: () => []
});

const onMenuItem = (path: string) => {
  const nodeId = typeof route.query.nodeId === "string" && /^\d+$/.test(route.query.nodeId)
    ? route.query.nodeId
    : undefined;
  if (path.startsWith("/media/") && nodeId) {
    return router.push({ path, query: { nodeId } });
  }
  return router.push(path);
};

const routePathList = computed(() => {
  const { getAllParentRoute } = useRoutingMethod();
  return getAllParentRoute(route.matched.at(-1).path) || [];
});

const selectedMenu = computed(() => {
  const find = routePathList.value;
  if (!find) return "";
  let path = "";
  for (let i = find.length - 1; i >= 0; i--) {
    if (find[i].meta && !find[i].meta.hide) {
      path = find[i].path;
      break;
    }
  }
  return path;
});

const routeOpenKeys = computed(() => {
  const selected = selectedMenu.value;
  return routePathList.value
    .filter(item => item.path !== selected && item.meta && !item.meta.hide && item.children?.length)
    .map(item => item.path);
});

const openKeys = ref<string[]>([]);

watch(
  routeOpenKeys,
  keys => {
    openKeys.value = keys;
  },
  { immediate: true }
);

const onOpenKeysChange = (keys: string[]) => {
  openKeys.value = keys;
};
</script>

<style lang="scss" scoped></style>
