import { afterEach, describe, expect, it, vi } from "vitest";

import { assertPCMA8000, createAudioLevelMeter, preferPCMA8000, waitForIceGatheringComplete } from "./talkPublisher";

describe("talkPublisher", () => {
  it("只把 PCMA/8000 设置为首选编码", () => {
    const setCodecPreferences = vi.fn();
    const transceiver = { setCodecPreferences } as unknown as RTCRtpTransceiver;
    const codecs = [
      { mimeType: "audio/opus", clockRate: 48000, channels: 2 },
      { mimeType: "audio/PCMA", clockRate: 8000, channels: 1 }
    ];

    preferPCMA8000(transceiver, codecs);

    expect(setCodecPreferences).toHaveBeenCalledWith([codecs[1]]);
  });

  it("浏览器没有 PCMA/8000 时拒绝发布", () => {
    expect(() => preferPCMA8000({ setCodecPreferences: vi.fn() } as any, [
      { mimeType: "audio/opus", clockRate: 48000, channels: 2 }
    ])).toThrow("PCMA/8000");
    expect(() => assertPCMA8000("v=0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\na=rtpmap:111 opus/48000/2\r\n"))
      .toThrow("PCMA/8000");
  });

  it("等待 ICE gathering complete 后才返回", async () => {
    const events = new EventTarget();
    const connection = {
      iceGatheringState: "gathering",
      addEventListener: events.addEventListener.bind(events),
      removeEventListener: events.removeEventListener.bind(events)
    } as unknown as RTCPeerConnection;

    const waiting = waitForIceGatheringComplete(connection, 1000);
    (connection as any).iceGatheringState = "complete";
    events.dispatchEvent(new Event("icegatheringstatechange"));
    await expect(waiting).resolves.toBe(true);
  });

  it("已经收齐时立即返回", async () => {
    const connection = { iceGatheringState: "complete" } as unknown as RTCPeerConnection;
    await expect(waitForIceGatheringComplete(connection, 1000)).resolves.toBe(true);
  });

  // ⛔ STUN 不可达时 iceGatheringState 永远到不了 complete。这里必须**降级不失败**：
  // 抛错会把整个对讲挂掉，而 host candidate 其实早就到手了，同网段靠它就能建连。
  it("收集超时只降级返回 false，不抛错", async () => {
    const connection = {
      iceGatheringState: "gathering",
      addEventListener: vi.fn(),
      removeEventListener: vi.fn()
    } as unknown as RTCPeerConnection;

    await expect(waitForIceGatheringComplete(connection, 20)).resolves.toBe(false);
  });
});

describe("createAudioLevelMeter", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  const stream = { getTracks: () => [] } as unknown as MediaStream;

  /** 装一个假的 AudioContext，并把 requestAnimationFrame 换成可手动推进的版本。 */
  function installAudioContext(sampleValue: number) {
    let pending: FrameRequestCallback | null = null;
    const cancel = vi.fn();
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      pending = callback;
      return 1;
    });
    vi.stubGlobal("cancelAnimationFrame", cancel);

    const close = vi.fn().mockResolvedValue(undefined);
    const resume = vi.fn().mockResolvedValue(undefined);
    const disconnectAnalyser = vi.fn();
    const disconnectSource = vi.fn();
    const analyser = {
      fftSize: 0,
      smoothingTimeConstant: 0,
      getFloatTimeDomainData: (target: Float32Array) => target.fill(sampleValue),
      disconnect: disconnectAnalyser
    };
    const context = {
      createAnalyser: () => analyser,
      createMediaStreamSource: () => ({ connect: vi.fn(), disconnect: disconnectSource }),
      resume,
      close
    };
    // 构造函数返回对象时 JS 会用它作为实例，等价于造了一个 AudioContext。
    vi.stubGlobal("AudioContext", class { constructor() { return context as any; } });
    vi.stubGlobal("webkitAudioContext", undefined);

    return {
      cancel, close, resume, analyser, disconnectAnalyser, disconnectSource,
      tick: () => { const callback = pending; pending = null; callback?.(0); }
    };
  }

  it("没有 AudioContext 时静默降级为 null", () => {
    vi.stubGlobal("AudioContext", undefined);
    vi.stubGlobal("webkitAudioContext", undefined);
    expect(createAudioLevelMeter(stream, vi.fn())).toBeNull();
  });

  it("按 dB 映射电平并在停止时归零、释放资源", () => {
    // RMS 0.05 ≈ -26dB，落在「正常说话」区间；线性 RMS 会得到 0.05，波形几乎不动。
    const fake = installAudioContext(0.05);
    const levels: number[] = [];
    const meter = createAudioLevelMeter(stream, level => levels.push(level));
    expect(meter).not.toBeNull();

    for (let frame = 0; frame < 40; frame++) fake.tick();

    const settled = levels[levels.length - 1];
    expect(settled).toBeGreaterThan(0.4);
    expect(settled).toBeLessThan(0.75);

    meter!.stop();
    expect(levels[levels.length - 1]).toBe(0);
    expect(fake.cancel).toHaveBeenCalled();
    expect(fake.disconnectSource).toHaveBeenCalled();
    expect(fake.disconnectAnalyser).toHaveBeenCalled();
    expect(fake.close).toHaveBeenCalled();
  });

  it("静音时电平停在 0，不会因底噪抖出波形", () => {
    const fake = installAudioContext(0);
    const levels: number[] = [];
    createAudioLevelMeter(stream, level => levels.push(level))!;
    for (let frame = 0; frame < 20; frame++) fake.tick();
    expect(levels.every(level => level === 0)).toBe(true);
    expect(levels.length).toBeLessThanOrEqual(1);
  });
});
