<template>
  <div>
    <s-lang-provider v-if="!leavingAfterPasswordChange">
      <component :is="layouts[resolvedLayoutType]" />
      <PlaybackConsoleHost v-if="!mustChangePassword" />
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
import { ref } from "vue";

const userStore = useUserStoreHook();
const { mustChangePassword } = storeToRefs(userStore);
const sysConfigStore = useSysConfigStore();
const router = useRouter();
const leavingAfterPasswordChange = ref(false);
const onInitialPasswordChanged = async () => {
  // 先卸载系统内容，避免 logout 清除改密标志后重新挂载业务组件。
  leavingAfterPasswordChange.value = true;
  sysConfigStore.systemConfig.defaultusername = "";
  sysConfigStore.systemConfig.defaultpassword = "";
  try {
    await userStore.logOut();
  } finally {
    await router.replace("/login");
  }
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
