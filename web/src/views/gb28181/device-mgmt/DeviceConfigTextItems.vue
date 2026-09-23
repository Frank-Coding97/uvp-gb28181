<script setup lang="ts">
/**
 * DeviceConfigTextItems - 变长文本行编辑器（`texts` kind 的**通用**渲染实现）
 *
 * 三列：文字 / X / Y。为什么不是"一个 textarea 加两个滑杆"：
 * 协议里每条 `Item` 各自带 X/Y（最多 8 条可以分散在画面不同位置），
 * 拍成"一段文本 + 一组坐标"就表达不了这件事。
 *
 * ## 现在谁在用它（2026-09-20 之后）
 *
 * **没有分组在用。** OSD 组已经改走对象块形态（`DeviceConfigOsdBlocks.vue`）：
 * 位置改成"在画面上拖动 + 数值折叠"，X/Y 手输框不再是主要操作方式。
 *
 * ⛔ 但**别删**：它是 `ConfigTextsField`（`texts` kind）的正式渲染实现。
 *    删了会让那个 kind 变成"有类型声明、没有渲染分支"。将来别组需要变长文本列表时，
 *    直接复用这里即可 —— 通用循环里的 `v-else-if="field.kind === 'texts'"` 分支还在指着它。
 *
 * ⛔ 新增行**不预填坐标**（默认 0,0）：预填一个看起来合理的坐标会让人以为
 *    "平台帮我对齐了"，而实际值是随手编的。坐标是使用者的决定。
 */
import { Info, Plus, Trash2 } from "@lucide/vue";
import type { ConfigTextItem } from "./deviceConfigGroups";

const props = withDefaults(
  defineProps<{
    modelValue: ConfigTextItem[];
    disabled?: boolean;
    maxItems: number;
    maxLength: number;
  }>(),
  { disabled: false }
);

const emit = defineEmits<{ "update:modelValue": [value: ConfigTextItem[]] }>();

function rows(): ConfigTextItem[] {
  return Array.isArray(props.modelValue) ? props.modelValue : [];
}

function commit(next: ConfigTextItem[]) {
  emit("update:modelValue", next);
}

function setText(index: number, raw: string) {
  const next = rows().map(row => ({ ...row }));
  const target = next[index];
  if (!target) return;
  target.text = raw;
  commit(next);
}

function setCoord(index: number, axis: "x" | "y", raw: string) {
  const next = rows().map(row => ({ ...row }));
  const target = next[index];
  if (!target) return;
  const parsed = Number(String(raw).trim());
  target[axis] = Number.isFinite(parsed) ? parsed : 0;
  commit(next);
}

function addRow() {
  if (rows().length >= props.maxItems) return;
  commit([...rows(), { text: "", x: 0, y: 0 }]);
}

function removeRow(index: number) {
  commit(rows().filter((_, position) => position !== index));
}

/** 已用/上限：专业客户端都会显示，用来交代"还能加几条"。 */
function counterText(): string {
  return `${rows().length} / ${props.maxItems}`;
}
</script>

<template>
  <div class="dct" :class="{ 'is-disabled': disabled }">
    <div v-if="!rows().length" class="dct-empty">未配置叠加文字</div>
    <div v-for="(row, index) in rows()" :key="index" class="dct-row" :data-testid="`dct-row-${index}`">
      <input
        class="dct-input"
        type="text"
        :value="row.text"
        :maxlength="maxLength"
        :disabled="disabled"
        :placeholder="`第 ${index + 1} 条文字`"
        :aria-label="`第 ${index + 1} 条叠加文字`"
        :data-testid="`dct-text-${index}`"
        @change="setText(index, ($event.target as HTMLInputElement).value)"
      />
      <label class="dct-coord">
        <span>X</span>
        <input
          class="dct-input is-coord"
          type="text"
          inputmode="numeric"
          :value="row.x"
          :disabled="disabled"
          :aria-label="`第 ${index + 1} 条文字 X`"
          @change="setCoord(index, 'x', ($event.target as HTMLInputElement).value)"
        />
      </label>
      <label class="dct-coord">
        <span>Y</span>
        <input
          class="dct-input is-coord"
          type="text"
          inputmode="numeric"
          :value="row.y"
          :disabled="disabled"
          :aria-label="`第 ${index + 1} 条文字 Y`"
          @change="setCoord(index, 'y', ($event.target as HTMLInputElement).value)"
        />
      </label>
      <button
        type="button"
        class="dct-del"
        :disabled="disabled"
        :aria-label="`删除第 ${index + 1} 条`"
        :data-testid="`dct-del-${index}`"
        @click="removeRow(index)"
      >
        <Trash2 :size="11" />
      </button>
    </div>
    <div class="dct-foot">
      <button
        type="button"
        class="dct-add"
        :disabled="disabled || rows().length >= maxItems"
        data-testid="dct-add"
        @click="addRow"
      >
        <Plus :size="11" />添加一条
      </button>
      <span class="dct-count" data-testid="dct-count">{{ counterText() }}</span>
      <span class="dct-axis-hint"><Info :size="10" />坐标为像素，基准是设备声明的坐标画布（不是画面解码尺寸）</span>
    </div>
  </div>
</template>

<style scoped lang="scss">
.dct {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  gap: 5px;
  min-width: 0;

  &.is-disabled {
    opacity: 0.55;
  }
}

.dct-empty {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}

.dct-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 68px 68px 22px;
  gap: 5px;
  align-items: center;
}

.dct-input {
  width: 100%;
  height: 24px;
  padding: 0 7px;
  font-size: 12px;
  color: var(--uvp-text-primary);
  background: var(--uvp-dialog-control-bg, #ffffff);
  border: 1px solid var(--uvp-panel-border, #dbe4f0);
  border-radius: 4px;

  &:focus {
    outline: none;
    border-color: var(--uvp-brand);
  }

  &.is-coord {
    text-align: right;
  }
}

.dct-coord {
  display: inline-flex;
  gap: 3px;
  align-items: center;

  span {
    font-size: 10px;
    color: var(--uvp-text-tertiary);
  }
}

.dct-del,
.dct-add {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  height: 22px;
  padding: 0;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: var(--uvp-dialog-control-bg, #f8fbff);
  border: 1px solid var(--uvp-panel-border, #dbe4f0);
  border-radius: 4px;

  &:hover:not(:disabled) {
    color: var(--uvp-brand);
    border-color: var(--uvp-brand);
  }

  &:disabled {
    color: var(--uvp-text-tertiary);
    cursor: not-allowed;
    opacity: 0.45;
  }
}

.dct-foot {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-top: 2px;
}

.dct-add {
  gap: 3px;
  height: 22px;
  padding: 0 8px;
  font-size: 11px;
}

.dct-count {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}

.dct-axis-hint {
  display: inline-flex;
  gap: 2px;
  align-items: center;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
}
</style>
