<script setup lang="ts">
import { AlertTriangle, ChevronDown, Inbox, Loader2, Play, Plus, RefreshCcw, Route, Square, Trash2, X } from "lucide-vue-next";

type CruiseTrackItem = {
  id: number;
  name: string;
  enabled: boolean;
  pending: boolean;
  points: { presetId: number; speed: number | null; dwellSec: number | null }[] | null;
  source: string;
};

type CruiseTileState = "stop" | "idle" | "disabled" | "pending";

defineProps<{
  tracks: CruiseTrackItem[];
  visibleTracks: CruiseTrackItem[];
  hasMore: boolean;
  activeId: number | null;
  state: "stopped" | "start-sent";
  moreVisible: boolean;
  syncing: boolean;
  syncLabel: string;
  syncTitle: string;
  loadError: string;
  canAdd: boolean;
  tooltipDelay: number;
  tileTitle: (track: CruiseTrackItem) => string;
  tileState: (track: CruiseTrackItem) => CruiseTileState;
}>();

const emit = defineEmits<{
  sync: [];
  add: [];
  stop: [];
  toggle: [id: number];
  delete: [id: number];
  "update:moreVisible": [value: boolean];
}>();
</script>

<template>
  <section class="linked-section linked-card" data-testid="cruise-card">
    <header class="linked-card-hd">
      <span class="section-title">
        <Route :size="13" />巡航轨迹<em v-if="tracks.length" class="preset-count">{{ tracks.length }}</em>
        <button
          v-if="activeId !== null && state !== 'stopped'"
          class="cruise-running-chip"
          data-testid="cruise-running-chip"
          :title="`巡航 #${activeId} 启动指令已发送,点击停止`"
          @click="emit('stop')"
        >
          <span class="cruise-running-dot" :class="state" />
          启动已下发
          <Square :size="10" />
        </button>
      </span>
      <span class="linked-card-actions">
        <button
          class="resource-sync-btn"
          data-testid="cruise-sync-btn"
          :class="{ syncing }"
          :disabled="syncing"
          :title="syncTitle"
          @click="emit('sync')"
        >
          <Loader2 v-if="syncing" :size="11" class="resource-sync-spin" />
          <RefreshCcw v-else :size="11" />
          <span>{{ syncLabel }}</span>
        </button>
        <button
          class="preset-save-btn"
          data-testid="cruise-add-btn"
          :disabled="!canAdd"
          :title="canAdd ? '新建巡航轨迹(按顺序串联多个预置位)' : '需要先添加预置位才能新建巡航轨迹'"
          @click="emit('add')"
        >
          <Plus :size="12" /><span>添加</span>
        </button>
      </span>
    </header>

    <div v-if="loadError" class="preset-empty" data-testid="cruise-load-error">
      <AlertTriangle :size="24" class="preset-empty-glyph" />
      <p class="preset-empty-line">{{ loadError }}</p>
      <p class="preset-empty-hint">点「同步」重试</p>
    </div>

    <div v-else-if="tracks.length === 0" class="preset-empty" data-testid="cruise-empty">
      <Inbox :size="24" class="preset-empty-glyph" />
      <p class="preset-empty-line">暂无巡航轨迹</p>
      <p class="preset-empty-hint">设备上已有的可用「同步」读回</p>
    </div>

    <div v-else class="preset-grid">
      <a-tooltip
        v-for="track in visibleTracks"
        :key="track.id"
        :content="tileTitle(track)"
        position="top"
        :mouse-enter-delay="tooltipDelay"
        :data-testid="`cruise-tile-tooltip-${track.id}`"
      >
        <div
          class="preset-tile cruise-tile"
          :class="{
            active: activeId === track.id && state !== 'stopped',
            disabled: !track.enabled && !track.pending,
            pending: track.pending
          }"
          :data-testid="`cruise-tile-${track.id}`"
        >
          <button
            class="preset-tile-hit cruise-item"
            :disabled="!track.enabled && !track.pending"
            @click="emit('toggle', track.id)"
          >
            <Square v-if="tileState(track) === 'stop'" :size="10" class="cruise-tile-icon" />
            <Play v-else :size="10" class="cruise-tile-icon" />
            <span class="preset-name">{{ track.name }}</span>
            <span v-if="track.pending" class="cruise-status-badge">未验证</span>
          </button>
          <button class="preset-tile-del" :title="`删除巡航 #${track.id}`" @click.stop="emit('delete', track.id)">
            <X :size="11" />
          </button>
        </div>
      </a-tooltip>

      <a-popover
        v-if="hasMore"
        :popup-visible="moreVisible"
        trigger="click"
        position="bottom"
        :content-style="{ padding: 0 }"
        class="preset-more-popover-trigger"
        @popup-visible-change="emit('update:moreVisible', $event)"
      >
        <button class="preset-tile-more" data-testid="cruise-more-btn">
          <span>更多 · {{ tracks.length }}</span>
          <ChevronDown :size="11" />
        </button>
        <template #content>
          <div class="preset-popover" data-testid="cruise-popover">
            <header class="preset-popover-hd">
              <span
                ><Route :size="12" />全部巡航轨迹<em class="preset-count">{{ tracks.length }}</em></span
              >
            </header>
            <div class="preset-popover-list">
              <a-tooltip
                v-for="track in tracks"
                :key="track.id"
                :content="tileTitle(track)"
                position="top"
                :mouse-enter-delay="tooltipDelay"
                :data-testid="`cruise-popover-tooltip-${track.id}`"
              >
                <div
                  class="preset-popover-row"
                  :class="{ active: activeId === track.id && state !== 'stopped' }"
                  data-testid="cruise-popover-row"
                >
                  <span class="preset-popover-idx">#{{ track.id }}</span>
                  <span class="preset-popover-name">{{ track.name }}</span>
                  <div class="preset-popover-actions">
                    <button
                      class="preset-popover-call"
                      :disabled="!track.enabled && !track.pending"
                      @click="emit('toggle', track.id)"
                    >
                      <Square v-if="tileState(track) === 'stop'" :size="11" />
                      <Play v-else :size="11" />
                    </button>
                    <button class="preset-popover-del" title="删除此巡航轨迹" @click="emit('delete', track.id)">
                      <Trash2 :size="11" />
                    </button>
                  </div>
                </div>
              </a-tooltip>
            </div>
          </div>
        </template>
      </a-popover>
    </div>
  </section>
</template>
