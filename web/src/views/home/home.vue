<template>
  <div class="snow-page">
    <div class="home-page">
      <!-- 常用功能 -->
      <Shortcut />
      <!-- 第三板指标 -->
      <TargetBox />
      <!-- 财务指标 -->
      <Finance />
      <!-- 数据图 -->
      <DataBox />
    </div>
  </div>
</template>

<script setup lang="ts">
import Shortcut from "@/views/home/components/shortcut.vue";
import TargetBox from "@/views/home/components/target-box.vue";
import Finance from "@/views/home/components/finance.vue";
import DataBox from "@/views/home/components/data-box.vue";
</script>

<style lang="scss" scoped>
.home-page {
  display: flex;
  flex-direction: column;
  gap: 24px;
  min-height: 100%;
}

:deep(.shortcut-box),
:deep(.target-box),
:deep(.finance-box) {
  margin-top: 0;
  padding: 24px;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: var(--uvp-panel-radius);
  box-shadow: var(--uvp-panel-shadow);
}

:deep(.data-box) {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 24px;
  margin-top: 0;
}

:deep(.data-box > div) {
  min-width: 0;
  min-height: 360px;
  padding: 24px;
  overflow: hidden;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: var(--uvp-panel-radius);
  box-shadow: var(--uvp-panel-shadow);
}

:deep(.box-title),
:deep(.data-title) {
  font-size: 14px;
  font-weight: 600;
  color: var(--uvp-text-primary);
}

:deep(.data-subtext) {
  color: var(--uvp-text-tertiary);
}

@media (max-width: 1024px) {
  :deep(.data-box) {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 768px) {
  .home-page {
    gap: 16px;
  }

  :deep(.shortcut-box),
  :deep(.target-box),
  :deep(.finance-box),
  :deep(.data-box > div) {
    padding: 16px;
  }
}
</style>
