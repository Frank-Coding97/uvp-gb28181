<template>
  <div class="uvp-search-panel">
    <div class="uvp-search-panel__surface">
      <div class="uvp-search-panel__main">
        <div class="uvp-search-panel__fields">
          <slot name="fields">
            <slot />
          </slot>
        </div>
        <div v-if="$slots.actions" class="uvp-search-panel__actions">
          <slot name="actions" />
        </div>
      </div>
      <div v-if="$slots.extra" class="uvp-search-panel__extra">
        <slot name="extra" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
defineOptions({ name: "SLayoutSearch" });
</script>

<style lang="scss" scoped>
.uvp-search-panel {
  display: flex;
  flex-direction: column;
  min-width: 0;
  margin-bottom: 12px;
}

.uvp-search-panel__surface {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  column-gap: 14px;
  align-items: flex-start;
  padding: 12px 14px;
  background: var(--uvp-search-panel-bg);
  border: 1px solid var(--uvp-list-panel-border);
  border-radius: 14px;
  box-shadow: var(--uvp-search-panel-shadow);
}

.uvp-search-panel__main {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  min-width: 0;
}

.uvp-search-panel__fields {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  min-width: 0;
}

.uvp-search-panel__actions,
.uvp-search-panel__extra {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
}

.uvp-search-panel__actions {
  gap: 8px;
  white-space: nowrap;
}

.uvp-search-panel__extra {
  position: relative;
  gap: 10px;
}

:deep(.arco-input-wrapper),
:deep(.arco-select-view-single),
:deep(.arco-picker) {
  min-height: 44px;
  background: var(--uvp-search-control-bg);
  border-color: transparent;
  border-radius: 10px;
  box-shadow: var(--uvp-search-control-shadow);
}

:deep(.arco-input-wrapper:hover),
:deep(.arco-select-view-single:hover),
:deep(.arco-picker:hover),
:deep(.arco-input-wrapper.arco-input-focus),
:deep(.arco-select-view-focus),
:deep(.arco-picker-focused) {
  border-color: transparent;
  box-shadow: var(--uvp-search-control-focus-shadow);
}

:deep(.arco-btn) {
  min-width: 88px;
  height: 44px;
  border-radius: 8px;
  transition:
    background-color 0.18s ease,
    border-color 0.18s ease,
    box-shadow 0.18s ease,
    color 0.18s ease;
}

:deep(.arco-btn + .arco-btn) {
  margin-left: 0;
}

.uvp-search-panel__actions :deep(.arco-btn-primary) {
  background: #2563eb;
  border-color: #2563eb;
  box-shadow: none;
}

.uvp-search-panel__actions :deep(.arco-btn-primary:hover) {
  background: #1d4ed8;
  border-color: #1d4ed8;
  box-shadow: 0 8px 16px -14px rgb(37 99 235 / 55%);
}

.uvp-search-panel__actions :deep(.arco-btn:not(.arco-btn-primary)) {
  color: var(--uvp-text-secondary);
  background: var(--uvp-search-secondary-btn-bg);
  border-color: var(--uvp-search-secondary-btn-border);
  box-shadow: var(--uvp-search-secondary-btn-shadow);
}

.uvp-search-panel__actions :deep(.arco-btn:not(.arco-btn-primary):hover) {
  color: var(--uvp-brand-strong);
  background: var(--uvp-search-secondary-btn-hover-bg);
  border-color: rgb(37 99 235 / 24%);
}

.uvp-search-panel__extra :deep(.arco-btn-primary) {
  background: #16845f;
  border-color: #16845f;
  box-shadow: 0 10px 18px -15px rgb(22 132 95 / 58%);
}

.uvp-search-panel__extra :deep(.arco-btn-primary:hover) {
  background: #0f6f4f;
  border-color: #0f6f4f;
  box-shadow: 0 12px 20px -15px rgb(15 111 79 / 64%);
}

@media (max-width: 1280px) {
  .uvp-search-panel__extra {
    grid-column: 1 / -1;
    padding-top: 2px;
    justify-content: flex-start;
  }
}

@media (max-width: 768px) {
  .uvp-search-panel__surface {
    grid-template-columns: minmax(0, 1fr);
    padding: 12px;
  }

  .uvp-search-panel__actions,
  .uvp-search-panel__extra {
    justify-content: flex-start;
  }
}
</style>
