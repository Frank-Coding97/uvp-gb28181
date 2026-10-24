import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";
import { usePlaybackConsoleStore } from "./playback-console";

const northGate = {
  id: 1,
  channelId: "channel-1",
  deviceId: "device-1",
  name: "北门",
  status: 1
};

const southGate = {
  id: 2,
  channelId: "channel-2",
  deviceId: "device-2",
  name: "南门",
  status: 1
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

  it("每点一次点播都递增请求序号，关闭不清零", () => {
    // 「点播」的语义是"现在播一路"，不是"打开面板"。控制台已经开着同一通道时，
    // `visible` 和通道都保持不变，只有这个序号能告诉控制台"这是一次新的开播请求"。
    const store = usePlaybackConsoleStore();
    expect(store.openSeq).toBe(0);

    store.open(northGate);
    expect(store.openSeq).toBe(1);

    store.open(northGate);
    expect(store.openSeq).toBe(2);

    store.close();
    // ⛔ 关闭不清零：清零后重新打开会和上一次的值撞车，watcher 又不触发了。
    expect(store.openSeq).toBe(2);
    store.open(southGate);
    expect(store.openSeq).toBe(3);
  });
});
