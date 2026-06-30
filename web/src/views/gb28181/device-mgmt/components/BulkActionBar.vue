<script setup lang="ts">
/**
 * BulkActionBar — 批量操作栏(C4)
 *
 * 选中数 > 0 时从底部滑入;提供批量重新注册 / 批量删除等动作
 * 本期仅做骨架,真实批量接口 Phase 2 落地
 */
import { computed } from "vue";

interface Props {
  selectedCount: number;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  (e: "clear"): void;
  (e: "action", name: string): void;
}>();

const visible = computed(() => props.selectedCount > 0);
</script>

<template>
  <Transition name="slide-up">
    <div v-if="visible" class="bulk-action-bar">
      <span class="info">
        已选 <strong>{{ selectedCount }}</strong> 项
      </span>
      <div class="actions">
        <button type="button" class="btn" @click="emit('action', 'reregister')" disabled>批量重新注册</button>
        <button type="button" class="btn danger" @click="emit('action', 'delete')" disabled>批量删除</button>
      </div>
      <button type="button" class="btn-text" @click="emit('clear')">取消选择</button>
    </div>
  </Transition>
</template>

<style lang="scss" scoped>
.bulk-action-bar {
  position: fixed;
  bottom: 0;
  left: var(--aside-width);
  right: 0;
  display: flex;
  align-items: center;
  gap: var(--space-4);
  height: 48px;
  padding: 0 var(--space-6);
  background: var(--color-bg-2);
  border-top: 1px solid var(--color-border-2);
  box-shadow: 0 -4px 12px rgba(0, 0, 0, 0.2);
  z-index: 100;

  .info {
    font-size: var(--font-13);
    color: var(--color-text-2);

    strong {
      color: var(--primary-5);
      font-family: var(--font-mono);
    }
  }

  .actions {
    display: flex;
    gap: var(--space-2);
    flex: 1;
  }

  .btn {
    height: 30px;
    padding: 0 12px;
    background: var(--color-bg-3);
    border: 1px solid var(--color-border-2);
    border-radius: var(--dm-radius-1);
    color: var(--color-text-2);
    font-size: var(--font-12);
    cursor: pointer;

    &:disabled {
      opacity: 0.4;
      cursor: not-allowed;
    }

    &.danger:not(:disabled) {
      border-color: var(--status-danger-fade);
      color: var(--status-danger);
    }
  }

  .btn-text {
    appearance: none;
    border: 0;
    background: transparent;
    color: var(--color-text-4);
    font-size: var(--font-12);
    cursor: pointer;

    &:hover {
      color: var(--color-text-2);
    }
  }
}

.slide-up-enter-active,
.slide-up-leave-active {
  transition: transform var(--duration-slow) var(--ease-out),
    opacity var(--duration-slow) var(--ease-out);
}

.slide-up-enter-from,
.slide-up-leave-to {
  transform: translateY(100%);
  opacity: 0;
}
</style>
