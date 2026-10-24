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
  /** 通道音频开关（点播是否接收音频），透传给播放器决定是否出声/显示音频控件 */
  audioEnabled?: boolean;
}

export const usePlaybackConsoleStore = defineStore("playback-console", () => {
  const visible = ref(false);
  const channel = ref<PlaybackConsoleChannel | null>(null);
  const displayMode = ref<PlaybackConsoleDisplayMode>("expanded");
  /**
   * 每次 `open()` 递增的请求序号。
   *
   * ⛔ 不能靠 `visible` + 通道标识来驱动开播：控制台已经开着同一个通道时再点一次
   *    「点播」，这两个值**一个都没变**，控制台的 watcher 不会触发 —— 用户看到的就是
   *    "点了点播没反应"（画面还是上一次的错误态）。计数器的语义是「每点一次点播就是
   *    一次新的开播请求」，与"面板是否已打开"解耦。
   */
  const openSeq = ref(0);

  function open(nextChannel: PlaybackConsoleChannel) {
    channel.value = { ...nextChannel };
    displayMode.value = "expanded";
    visible.value = true;
    openSeq.value += 1;
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
    openSeq,
    open,
    close,
    setDisplayMode,
    minimize,
    restore
  };
});
