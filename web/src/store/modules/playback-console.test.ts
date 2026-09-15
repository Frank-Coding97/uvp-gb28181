import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";
import { usePlaybackConsoleStore } from "./playback-console";

const northGate = {
  id: 1,
  channelId: "channel-1",
  deviceId: "device-1",
  name: "北门",
  status: 1,
};

const southGate = {
  id: 2,
  channelId: "channel-2",
  deviceId: "device-2",
  name: "南门",
  status: 1,
};

describe("global playback console store", () => {
  beforeEach(() => setActivePinia(createPinia()));

  it("只维护一个播放控制台，并在切换通道时恢复完整模式", () => {
    const store = usePlaybackConsoleStore();

    store.open(northGate);
    expect(store.visible).toBe(true);
    expect(store.channel).toEqual(northGate);
    expect(store.displayMode).toBe("expanded");

    store.minimize();
    expect(store.displayMode).toBe("minimized");

    store.open(southGate);
    expect(store.channel).toEqual(southGate);
    expect(store.displayMode).toBe("expanded");

    store.close();
    expect(store.visible).toBe(false);
    expect(store.channel).toBeNull();
    expect(store.displayMode).toBe("expanded");
  });
});
