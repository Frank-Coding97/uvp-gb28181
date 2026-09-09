<script setup lang="ts">
import { computed } from "vue";
import { Video } from "lucide-vue-next";
import { useSysConfigStore } from "@/store/modules/sys-config";
const config = useSysConfigStore();
const useIconCover = computed(() => config.systemConfig.playbackCover === "icon");

const props = defineProps<{ index: number }>();
const slotNumber = computed(() => String(props.index + 1).padStart(2, "0"));
</script>

<template>
    <div class="unplayed-cover" :aria-label="`${index + 1}号预览窗口，待接入`">
        <div class="cover-meta" aria-hidden="true">
            <span class="slot-number">窗口 {{ slotNumber }}</span>
            <span class="idle-state"><span class="idle-dot" />待接入</span>
        </div>
        <div class="brand-lockup" aria-hidden="true">
            <Video v-if="useIconCover" class="cover-icon" :size="56" :stroke-width="1.2" />
            <strong v-else class="cover-brand">UVP</strong>
            <span class="brand-caption">{{ useIconCover ? "等待视频接入" : "统一视频接入平台" }}</span>
        </div>
    </div>
</template>

<style scoped>
.unplayed-cover {
    position: relative;
    display: grid;
    width: 100%;
    min-height: 0;
    flex: 1;
    overflow: hidden;
    color: #F8FAFC;
    background: #090B0F;
    container-type: inline-size;
    place-items: center;
}
.cover-meta {
    position: absolute;
    top: 0;
    right: 0;
    left: 0;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 14px;
    color: #9CA3AF;
    font-size: 10px;
}
.slot-number {
    font-family: var(--zlm-font-mono);
    font-variant-numeric: tabular-nums;
}
.idle-state { display: inline-flex; align-items: center; gap: 6px; }
.idle-dot { width: 6px; height: 6px; background: #6B7280; border-radius: 50%; }
.brand-lockup {
    display: flex;
    align-items: center;
    flex-direction: column;
    gap: 10px;
}
.cover-icon { color: #778397; }
.cover-brand {
    color: transparent;
    background: linear-gradient(110deg, #F8FAFC 8%, #C7D7F2 54%, #72A2F5 100%);
    background-clip: text;
    filter: drop-shadow(0 8px 18px rgb(37 99 235 / 16%));
    font-family: var(--zlm-font-display);
    font-size: 52px;
    font-weight: 700;
    letter-spacing: 0;
    line-height: 1;
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
}
.brand-caption { color: #778397; font-size: 10px; font-weight: 500; letter-spacing: 0; line-height: 1.4; }

@container (max-width: 220px) {
    .cover-brand { font-size: 38px; }
    .brand-caption { font-size: 9px; }
}
@container (min-width: 520px) {
    .cover-brand { font-size: 68px; }
    .brand-caption { font-size: 11px; }
}
</style>
