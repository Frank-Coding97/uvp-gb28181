<template>
  <a-input-tag
    v-model="localTags"
    :placeholder="placeholder"
    :allow-clear="allowClear"
    :disabled="disabled"
    :max-tag-count="maxTagCount"
    :readonly="readonly"
    :size="size"
  />
</template>

<script setup lang="ts">
import { computed } from 'vue';

interface Props {
  /**
   * 绑定的值，为逗号分割的字符串
   * @example "tag1,tag2,tag3"
   */
  modelValue?: string;
  /** 占位符 */
  placeholder?: string;
  /** 是否显示清空按钮 */
  allowClear?: boolean;
  /** 是否禁用 */
  disabled?: boolean;
  /** 最大标签数量 */
  maxTagCount?: number | 'responsive';
  /** 是否只读 */
  readonly?: boolean;
  /** 尺寸 */
  size?: 'small' | 'medium' | 'large';
  /** 分隔符，默认为逗号 */
  separator?: string;
}

interface Emits {
  (e: 'update:modelValue', value: string): void;
}

const props = withDefaults(defineProps<Props>(), {
  modelValue: '',
  placeholder: '请输入标签，按 Enter 键添加',
  allowClear: true,
  disabled: false,
  readonly: false,
  size: 'medium',
  separator: ','
});

const emit = defineEmits<Emits>();

/**
 * 本地标签数组
 * 从逗号分割字符串转换为数组
 */
const localTags = computed({
  get(): string[] {
    if (!props.modelValue || typeof props.modelValue !== 'string') {
      return [];
    }
    return props.modelValue
      .split(props.separator)
      .map(tag => tag.trim())
      .filter(tag => tag.length > 0);
  },
  set(value: string[]) {
    // 将标签数组转换为逗号分割字符串
    const stringValue = value
      .map(tag => tag.trim())
      .filter(tag => tag.length > 0)
      .join(props.separator);
    emit('update:modelValue', stringValue);
  }
});
</script>

<style lang="scss" scoped>
// 组件样式可以在此添加
</style>
