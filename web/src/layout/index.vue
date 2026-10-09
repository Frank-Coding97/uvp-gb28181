<template>
  <div>
    <s-lang-provider>
      <component :is="layouts[resolvedLayoutType]" />
      <PlaybackConsoleHost />
      <InitialPasswordDialog v-if="mustChangePassword" @success="onInitialPasswordChanged" />
    </s-lang-provider>
  </div>
</template>

<script setup lang="ts">
import { storeToRefs } from "pinia";
import { useThemeConfig } from "@/store/modules/theme-config";
import PlaybackConsoleHost from "@/layout/components/PlaybackConsoleHost.vue";
import InitialPasswordDialog from "@/views/login/components/InitialPasswordDialog.vue";
import { useUserStoreHook } from "@/store/modules/user";
import { useSysConfigStore } from "@/store/modules/sys-config";
import { useRouter } from "vue-router";

const userStore = useUserStoreHook();
const { mustChangePassword } = storeToRefs(userStore);
const sysConfigStore = useSysConfigStore();
const router = useRouter();
const onInitialPasswordChanged = async () => {
  await userStore.logOut();
  await sysConfigStore.getConfig();
  await router.replace("/login");
};

const themeStore = useThemeConfig();
const { layoutType } = storeToRefs(themeStore);
const resolvedLayoutType = computed(() => (layoutType.value === "layoutDefaults" ? layoutType.value : "layoutDefaults"));

// 引入组件-异步组件
const layouts: any = {
  layoutDefaults: defineAsyncComponent(() => import("@/layout/layout-defaults/index.vue")),
  layoutHead: defineAsyncComponent(() => import("@/layout/layout-head/index.vue")),
  layoutMixing: defineAsyncComponent(() => import("@/layout/layout-mixing/index.vue"))
};
</script>

<style lang="scss" scoped></style>
