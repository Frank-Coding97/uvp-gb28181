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
:deep(.arco-select-view),
:deep(.arco-picker) {
  min-height: 40px;
  background: var(--uvp-search-control-bg) !important;
  border: 1px solid var(--uvp-search-secondary-btn-border) !important;
  border-radius: 10px !important;
  box-shadow: var(--uvp-search-control-shadow) !important;
}

:deep(.arco-input-wrapper:hover),
:deep(.arco-select-view:hover),
:deep(.arco-picker:hover),
:deep(.arco-input-wrapper.arco-input-focus),
:deep(.arco-select-view-focus),
:deep(.arco-picker-focused) {
  border-color: var(--uvp-brand) !important;
  box-shadow: var(--uvp-search-control-focus-shadow) !important;
}

:deep(.arco-btn) {
  min-width: 88px;
  height: 40px;
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

/* ⛔⛔ 2026-11-04 删掉这里原有的「非主按钮 = 白底」规则（`.uvp-search-panel__actions
   :deep(.arco-btn:not(.arco-btn-primary))` 白底 + 蓝字 hover）。
   它是**全部 38 个列表页**「重置 / 导出」等次级按钮发白的唯一来源：本组件被
   `s-layout-search` 复用，该选择器特异性 0,3,0，压过按钮真源 0,1,0 ⇒ 真源里定的
   浅蓝底一直显示不出来。现在次级按钮交给 `src/styles/uvp-buttons.scss`，
   本文件只保留「档位」（40px）与主按钮的色阶微调。 */

/* extra 槽的主按钮沿用「新增」绿；但要避开已挂语义类的按钮，
   否则 `:deep(.arco-btn-primary)` 会把刷新/取消类也刷成绿。
   ⛔⛔ 已知边界（2026-11-04 实测）：本规则**只在浅色生效**。暗色下
   `arco-overrides.scss` 的 `body[arco-theme="dark"] :is(.arco-btn-primary, …)
   { background: var(--uvp-solid-bg) !important }` 特异性约 0,8,1 且同样带 `!important`，
   这里无论怎么补 `!important` 都压不过它 ⇒ 不挂语义类的「新增」在暗色下仍是实心蓝
   （老板在定时任务页看到的就是这个）。
   ⇒ **结论：「新增」按钮一律显式写 `class="uvp-create-btn"`**（它在暗色兜底的 `:not()` 里被排除，
   两套主题都稳）。本规则只当浅色的兜底 + 保持与历史观感一致，别靠它。
   ⛔ 排除列表要连 status 三色一起排，否则 `type="primary" status="danger"` 会被刷成绿。 */
.uvp-search-panel__extra
  :deep(
    .arco-btn-primary:where(
        :not(
          .uvp-create-btn,
          .uvp-refresh-btn,
          .uvp-stop-btn,
          .uvp-btn-neutral,
          .uvp-btn-secondary,
          .arco-btn-status-danger,
          .arco-btn-status-warning,
          .arco-btn-status-success
        )
      )
  ) {
  background: var(--uvp-btn-success-bg);
  border-color: var(--uvp-btn-success-bg);
  box-shadow: 0 10px 18px -15px rgb(22 132 95 / 58%);
}

.uvp-search-panel__extra
  :deep(
    .arco-btn-primary:where(
        :not(
          .uvp-create-btn,
          .uvp-refresh-btn,
          .uvp-stop-btn,
          .uvp-btn-neutral,
          .uvp-btn-secondary,
          .arco-btn-status-danger,
          .arco-btn-status-warning,
          .arco-btn-status-success
        )
      ):hover
  ) {
  background: var(--uvp-btn-success-bg-hover);
  border-color: var(--uvp-btn-success-bg-hover);
  box-shadow: 0 12px 20px -15px rgb(15 111 79 / 64%);
}

@media (width <= 1280px) {
  .uvp-search-panel__extra {
    grid-column: 1 / -1;
    justify-content: flex-start;
    padding-top: 2px;
  }
}

@media (width <= 768px) {
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
