<template>
  <div class="cascade-resources-page">
    <a-alert v-if="!canView" type="warning">当前账号无权查看级联共享与目录。</a-alert>
    <a-spin v-else :loading="loading" class="cascade-resources-body">
      <a-alert v-if="error" type="error">{{ error }} <a-button size="small" @click="load">重试</a-button></a-alert>
      <BusinessGroupManager
        v-else-if="platform"
        :key="platform.id"
        ref="workbench"
        :visible="true"
        :platform-id="platform.id"
        :platform-name="platform.name"
        :local-device-id="platform.localDeviceId"
        :platform="platform"
        :can-write="canWrite"
        :can-manage="canManage"
        @update:visible="returnToPlatforms"
        @publish-group-updated="platform = $event"
      />
    </a-spin>
  </div>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from "vue";
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from "vue-router";
import { getCascadePlatform, type CascadePlatform } from "@/api/gb28181";
import { hasPermission } from "@/hooks/usePermission";
import BusinessGroupManager from "./BusinessGroupManager.vue";

const route = useRoute();
const router = useRouter();
const canView = computed(() => hasPermission("gb28181:cascade:view"));
const canWrite = computed(() => hasPermission("gb28181:cascade:share"));
const canManage = computed(() => hasPermission("gb28181:cascade:manage"));
const platformId = computed(() => Number(route.params.id));
const platform = ref<CascadePlatform | null>(null);
const loading = ref(false);
const error = ref("");
const workbench = ref<InstanceType<typeof BusinessGroupManager> | null>(null);
let generation = 0;

async function load() {
  const token = ++generation;
  const id = platformId.value;
  platform.value = null;
  error.value = "";
  loading.value = false;
  if (!canView.value) return;
  if (!Number.isSafeInteger(id) || id <= 0) {
    error.value = "平台编号无效，请返回平台列表。";
    return;
  }
  loading.value = true;
  try {
    const response: any = await getCascadePlatform(id);
    if (token !== generation || !canView.value) return;
    const result = (response?.data?.data || response?.data || response) as CascadePlatform;
    if (result.id !== id) throw new Error("平台详情不匹配，请重新读取。");
    platform.value = result;
  } catch (cause: any) {
    if (token === generation) error.value = cause?.response?.data?.message || cause?.message || "平台加载失败，请重试。";
  } finally {
    if (token === generation) loading.value = false;
  }
}

function returnToPlatforms() {
  void router.push("/gb28181/cascade");
}
const protectDraft = () => workbench.value?.requestExit() ?? true;
onBeforeRouteLeave(protectDraft);
onBeforeRouteUpdate(protectDraft);
watch([platformId, canView], load, { immediate: true });
onUnmounted(() => {
  generation++;
});
</script>

<style scoped>
/**
 * ⛔⛔ 这一层必须是「定高」的，否则下面共享通道表的 `:scroll="{ y: '100%' }"` 全是空转。
 *   路由出口 `.layout-main-content` 本身是定高 flex 列（height:100% / overflow:hidden auto），
 *   但 flex 子项默认 `min-height: auto` ⇒ 本层会被内容顶高（实测 1505px > 视口 800px），
 *   于是 `height:100%` 一路退化成 `auto`：表格根节点跟着内容长到 1291px，表体 scrollHeight
 *   等于 clientHeight（1245 = 1245）**完全滚不动**，滚动条落到浏览器窗口上、表头照样滚走。
 *   补 `min-height: 0` 后本层=路由出口高度，整条链才有确定高度。
 *   ⛔ 别把这两行当"可以删的冗余"：删了不报错、不报测试失败，只是滚动条又跑回外层。
 */
.cascade-resources-page {
  box-sizing: border-box;
  display: flex;
  flex: 1;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  padding: 20px;
}
.cascade-resources-body {
  display: flex;
  flex: 1;
  flex-direction: column;
  width: 100%;
  min-height: 0;
}

@media (width <= 600px) {
  .cascade-resources-page {
    padding: 12px;
  }
}
</style>
