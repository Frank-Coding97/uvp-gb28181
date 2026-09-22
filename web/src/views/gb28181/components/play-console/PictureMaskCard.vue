<script setup lang="ts">
import { CircleSlash, Inbox, Plus, RefreshCcw, Scan, X } from "lucide-vue-next";

type Region = { seq: number; used: boolean; coords: number[] };
const props = defineProps<{
  usedCount: number;
  retainedMode: boolean;
  maskOn: boolean;
  maskPending: boolean;
  editable: boolean;
  switchTitle: string;
  switchText: string;
  canAddRegion: boolean;
  maxRegions: number;
  error: string;
  notice: string;
  factsMissing: boolean;
  absentTypes: string[];
  retainedRegionCount: number;
  regions: Region[];
  canvasSize: { width: number; height: number } | null;
  canvasFromDevice: boolean;
  coordinateText: string;
  coordsText: (coords: number[]) => string;
}>();
const emit = defineEmits<{
  (e: "toggle", value: boolean): void;
  (e: "read"): void;
  (e: "add"): void;
  (e: "remove", seq: number): void;
}>();
</script>

<template>
  <section class="linked-section linked-card" data-testid="picture-mask-card">
    <header class="linked-card-hd">
      <span class="section-title">
        <Scan :size="13" />画面遮挡
        <em v-if="usedCount && !retainedMode" class="preset-count">{{ usedCount }}</em>
      </span>
      <span class="linked-card-actions">
        <button
          class="mask-switch"
          :class="{ on: maskOn, pending: maskPending }"
          :disabled="!editable"
          data-testid="picture-mask-switch"
          :title="switchTitle"
          @click="emit('toggle', !maskOn)"
        >
          {{ switchText }}
        </button>
        <button class="resource-sync-btn" data-testid="picture-read-btn" title="重新向设备查询画面配置" @click="emit('read')">
          <RefreshCcw :size="11" /><span>读取</span>
        </button>
        <button
          class="preset-save-btn"
          data-testid="picture-mask-add-btn"
          :disabled="!editable || !canAddRegion"
          :title="
            !editable ? '需要先读到设备配置才能编辑' : canAddRegion ? '在播放画面上框选新的遮挡区' : `遮挡区最多 ${maxRegions} 个`
          "
          @click="emit('add')"
        >
          <Plus :size="12" /><span>新建</span>
        </button>
      </span>
    </header>
    <p v-if="error" class="picture-card-error" data-testid="picture-card-error">{{ error }}</p>
    <p v-else-if="notice" class="picture-card-notice" data-testid="picture-mask-notice">{{ notice }}</p>
    <div v-if="factsMissing" class="preset-empty" data-testid="picture-mask-unread">
      <Inbox :size="24" class="preset-empty-glyph" />
      <p class="preset-empty-line">{{ absentTypes.length ? "设备未返回遮挡配置" : "尚未读取设备遮挡配置" }}</p>
      <p class="preset-empty-hint">
        {{ absentTypes.length ? "该设备没有这个配置类型（2016 版无遮挡）" : "点「读取」先看设备当前挡在哪儿" }}
      </p>
    </div>
    <div v-else-if="retainedMode" class="preset-empty" data-testid="picture-mask-retained">
      <CircleSlash :size="24" class="preset-empty-glyph" />
      <p class="preset-empty-line">遮挡已停用</p>
      <p class="preset-empty-hint">
        设备仍保留 {{ retainedRegionCount }} 个区域 —— 上次「停用」没能把它们清掉（国标里 `On`
        与区域列表是独立节点，设备有权保留）；再点「启用」会让它们重新生效。
      </p>
    </div>
    <div v-else-if="usedCount === 0" class="preset-empty" data-testid="picture-mask-blank">
      <Inbox :size="24" class="preset-empty-glyph" />
      <p class="preset-empty-line">未设置遮挡区</p>
      <p class="preset-empty-hint">点「新建」，然后在画面上拖出要遮挡的范围</p>
    </div>
    <div v-else class="mask-slot-grid">
      <div
        v-for="region in regions"
        :key="region.seq"
        class="mask-slot"
        :class="{ used: region.used }"
        :data-testid="`picture-mask-slot-${region.seq}`"
      >
        <span class="mask-slot-idx">#{{ region.seq }}</span
        ><span v-if="region.used" class="mask-slot-coords">{{ props.coordsText(region.coords) }}</span
        ><span v-else class="mask-slot-blank">空位</span>
        <button
          v-if="region.used"
          class="mask-slot-del"
          :title="`删除遮挡区 ${region.seq}`"
          :data-testid="`picture-mask-del-${region.seq}`"
          @click="emit('remove', region.seq)"
        >
          <X :size="11" />
        </button>
      </div>
    </div>
    <p v-if="canvasSize" class="mask-canvas-note" :class="{ 'is-unverified': !canvasFromDevice }" data-testid="picture-mask-base">
      {{ coordinateText }}
    </p>
  </section>
</template>
