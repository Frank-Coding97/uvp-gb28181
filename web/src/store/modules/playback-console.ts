import { defineStore } from "pinia";
import { ref } from "vue";

export type PlaybackConsoleDisplayMode = "expanded" | "minimized";

export interface PlaybackConsoleChannel {
  id: number;
  channelId: string;
  deviceId: string;
  name?: string;
  alias?: string;
  manufacturer?: string;
  model?: string;
  ptzType?: number;
  status: number;
  streamTransport?: string;
}

export const usePlaybackConsoleStore = defineStore("playback-console", () => {
  const visible = ref(false);
  const channel = ref<PlaybackConsoleChannel | null>(null);
  const displayMode = ref<PlaybackConsoleDisplayMode>("expanded");

  function open(nextChannel: PlaybackConsoleChannel) {
    channel.value = { ...nextChannel };
    displayMode.value = "expanded";
    visible.value = true;
  }

  function close() {
    visible.value = false;
    channel.value = null;
    displayMode.value = "expanded";
  }

  function setDisplayMode(mode: PlaybackConsoleDisplayMode) {
    if (!visible.value) return;
    displayMode.value = mode;
  }

  function minimize() {
    setDisplayMode("minimized");
  }

  function restore() {
    setDisplayMode("expanded");
  }

  return {
    visible,
    channel,
    displayMode,
    open,
    close,
    setDisplayMode,
    minimize,
    restore,
  };
});
