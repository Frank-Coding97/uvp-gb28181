<script setup lang="ts">
import { storeToRefs } from "pinia";
import { usePlaybackConsoleStore, type PlaybackConsoleDisplayMode } from "@/store/modules/playback-console";
import PlayConsoleLinked from "@/views/gb28181/components/PlayConsoleLinked.vue";

const consoleStore = usePlaybackConsoleStore();
const { visible, channel, displayMode } = storeToRefs(consoleStore);

function handleVisibleChange(nextVisible: boolean) {
  if (!nextVisible) consoleStore.close();
}

function handleDisplayModeChange(mode: PlaybackConsoleDisplayMode) {
  consoleStore.setDisplayMode(mode);
}
</script>

<template>
  <PlayConsoleLinked
    :visible="visible"
    :channel="channel"
    :display-mode="displayMode"
    @update:visible="handleVisibleChange"
    @update:display-mode="handleDisplayModeChange"
  />
</template>
