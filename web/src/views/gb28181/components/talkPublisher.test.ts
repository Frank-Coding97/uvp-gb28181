import { describe, expect, it, vi } from "vitest";

import { assertPCMA8000, preferPCMA8000, waitForIceGatheringComplete } from "./talkPublisher";

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
    await expect(waiting).resolves.toBeUndefined();
  });
});
