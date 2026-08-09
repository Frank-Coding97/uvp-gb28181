export interface AudioCodecCapability {
  mimeType: string;
  clockRate: number;
  channels?: number;
  sdpFmtpLine?: string;
}

export function preferPCMA8000(
  transceiver: RTCRtpTransceiver,
  codecs: AudioCodecCapability[] = RTCRtpSender.getCapabilities?.("audio")?.codecs || []
) {
  const pcma = codecs.filter(codec =>
    codec.mimeType.toLowerCase() === "audio/pcma" && codec.clockRate === 8000
  );
  if (!pcma.length) throw new Error("当前浏览器不支持 PCMA/8000 音频编码");
  if (typeof transceiver.setCodecPreferences !== "function") {
    throw new Error("当前浏览器无法锁定 PCMA/8000 音频编码");
  }
  transceiver.setCodecPreferences(pcma as any);
}

export function assertPCMA8000(sdp: string) {
  const normalized = String(sdp || "").replace(/\r\n/g, "\n");
  const match = normalized.match(/^a=rtpmap:(\d+)\s+PCMA\/8000(?:\/1)?\s*$/im);
  if (!match) throw new Error("WebRTC 实际协商结果不是 PCMA/8000");
  const audioLine = normalized.split("\n").find(line => line.startsWith("m=audio ")) || "";
  const payloads = audioLine.trim().split(/\s+/).slice(3);
  if (!payloads.includes(match[1])) throw new Error("WebRTC 音频媒体未选择 PCMA/8000");
}

export function waitForIceGatheringComplete(connection: RTCPeerConnection, timeoutMs = 5000) {
  if (connection.iceGatheringState === "complete") return Promise.resolve();
  return new Promise<void>((resolve, reject) => {
    const timeout = window.setTimeout(() => {
      cleanup();
      reject(new Error("等待 WebRTC ICE 收集完成超时"));
    }, timeoutMs);
    const onChange = () => {
      if (connection.iceGatheringState !== "complete") return;
      cleanup();
      resolve();
    };
    const cleanup = () => {
      window.clearTimeout(timeout);
      connection.removeEventListener("icegatheringstatechange", onChange);
    };
    connection.addEventListener("icegatheringstatechange", onChange);
  });
}
