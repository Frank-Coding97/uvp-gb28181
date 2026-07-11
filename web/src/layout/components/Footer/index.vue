<template>
  <a-layout-footer class="footer">
    <div class="footer_title" @click="onFooter">{{ systemCopyright }} {{ systemRecordNo }}</div>
  </a-layout-footer>
</template>

<script setup lang="ts">
import { useSysConfigStore } from "@/store/modules/sys-config";
import { storeToRefs } from "pinia";
import { getDisplaySystemCopyright, getDisplaySystemRecordNo } from "@/utils/system-footer";

const sysConfigStore = useSysConfigStore();
const { systemConfig } = storeToRefs(sysConfigStore);

const systemCopyright = computed(() => {
  return getDisplaySystemCopyright(systemConfig.value?.systemCopyright);
});

const systemRecordNo = computed(() => {
  return getDisplaySystemRecordNo(systemConfig.value?.systemRecordNo);
});

const onFooter = () => {
  window.open("https://github.com/qxkjsoft/ginfast-back", "_blank");
};
</script>

<style lang="scss" scoped>
.footer {
  box-sizing: border-box;
  display: flex;
  align-items: center;
  height: 30px;
  border-top: $border-1 solid $color-border-2;
}

.footer_title {
  margin: 0 auto;
  color: $color-text-4;
  text-align: center;
  cursor: pointer;
}
</style>
