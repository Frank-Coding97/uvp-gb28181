<script setup lang="ts">
import { ChevronDown, Hash, Inbox, Loader2, Navigation, Plus, RefreshCcw, Trash2, X } from "lucide-vue-next";

type PresetItem = {
  id: number;
  name: string;
};

defineProps<{
  presets: PresetItem[];
  visiblePresets: PresetItem[];
  hasMore: boolean;
  activeId: number | null;
  moreVisible: boolean;
  syncing: boolean;
  syncLabel: string;
  syncTitle: string;
  tooltipDelay: number;
}>();

const emit = defineEmits<{
  sync: [];
  add: [];
  call: [id: number];
  delete: [id: number];
  "update:moreVisible": [value: boolean];
}>();
</script>

<template>
  <section class="linked-section linked-card" data-testid="preset-card">
    <header class="linked-card-hd">
      <span class="section-title">
        <Hash :size="13" />预置位<em v-if="presets.length" class="preset-count">{{ presets.length }}</em>
      </span>
      <span class="linked-card-actions">
        <button
          class="resource-sync-btn"
          data-testid="preset-sync-btn"
          :class="{ syncing }"
          :disabled="syncing"
          :title="syncTitle"
          @click="emit('sync')"
        >
          <Loader2 v-if="syncing" :size="11" class="resource-sync-spin" />
          <RefreshCcw v-else :size="11" />
          <span>{{ syncLabel }}</span>
        </button>
        <button class="preset-save-btn" data-testid="preset-save-btn" @click="emit('add')">
          <Plus :size="12" /><span>添加</span>
        </button>
      </span>
    </header>

    <div v-if="presets.length === 0" class="preset-empty" data-testid="preset-empty">
      <Inbox :size="24" class="preset-empty-glyph" />
      <p class="preset-empty-line">暂无预置位</p>
      <p class="preset-empty-hint">设备上已有的可用「同步」读回</p>
    </div>

    <div v-else class="preset-grid">
      <div v-for="preset in visiblePresets" :key="preset.id" class="preset-tile" :class="{ active: activeId === preset.id }">
        <a-tooltip
          :content="`#${preset.id} ${preset.name}`"
          position="top"
          :mouse-enter-delay="tooltipDelay"
          :data-testid="`preset-tile-tooltip-${preset.id}`"
        >
          <button class="preset-tile-hit preset-item" @click="emit('call', preset.id)">
            <span class="preset-idx">#{{ preset.id }}</span>
            <span class="preset-name">{{ preset.name }}</span>
          </button>
        </a-tooltip>
        <button class="preset-tile-del" :title="`删除 #${preset.id}`" @click.stop="emit('delete', preset.id)">
          <X :size="11" />
        </button>
      </div>

      <a-popover
        v-if="hasMore"
        :popup-visible="moreVisible"
        trigger="click"
        position="bottom"
        :content-style="{ padding: 0 }"
        class="preset-more-popover-trigger"
        @popup-visible-change="emit('update:moreVisible', $event)"
      >
        <button class="preset-tile-more" data-testid="preset-more-btn">
          <span>更多 · {{ presets.length }}</span>
          <ChevronDown :size="11" />
        </button>
        <template #content>
          <div class="preset-popover" data-testid="preset-popover">
            <header class="preset-popover-hd">
              <span
                ><Hash :size="12" />全部预置位<em class="preset-count">{{ presets.length }}</em></span
              >
            </header>
            <div class="preset-popover-list">
              <div
                v-for="preset in presets"
                :key="preset.id"
                class="preset-popover-row"
                :class="{ active: activeId === preset.id }"
                data-testid="preset-popover-row"
              >
                <span class="preset-popover-idx">#{{ preset.id }}</span>
                <a-tooltip
                  :content="`#${preset.id} ${preset.name}`"
                  position="top"
                  :mouse-enter-delay="tooltipDelay"
                  :data-testid="`preset-popover-tooltip-${preset.id}`"
                >
                  <span class="preset-popover-name">{{ preset.name }}</span>
                </a-tooltip>
                <div class="preset-popover-actions">
                  <button class="preset-popover-call" title="调用此预置位" @click="emit('call', preset.id)">
                    <Navigation :size="11" />
                  </button>
                  <button class="preset-popover-del" title="删除此预置位" @click="emit('delete', preset.id)">
                    <Trash2 :size="11" />
                  </button>
                </div>
              </div>
            </div>
          </div>
        </template>
      </a-popover>
    </div>
  </section>
</template>
