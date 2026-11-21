export interface ReceiverCodec {
  mimeType: string;
  clockRate?: number;
  channels?: number;
}

export interface BrowserCodecCapabilities {
  webrtc: boolean;
  video: readonly ReceiverCodec[] | null;
  audio: readonly ReceiverCodec[] | null;
}

export interface PlaybackCodecTrack {
  kind: string;
  codec: string;
  ready: boolean;
  sampleRate?: number;
  channels?: number;
}

export interface ProtocolCompatibility {
  disabledReason?: string;
  warning?: string;
}

function codecName(raw: string): string {
  const name = raw
    .toUpperCase()
    .replace(/^(VIDEO|AUDIO)\//, "")
    .replace(/[.\s_-]/g, "");
  if (/^(H264|AVC(?:1|3)?)/.test(name)) return "H264";
  if (/^(H265|HEVC|HVC1|HEV1)/.test(name)) return "H265";
  if (["PCMA", "G711A", "ALAW"].includes(name)) return "PCMA";
  if (["PCMU", "G711U", "MULAW"].includes(name)) return "PCMU";
  if (/^(AAC|MP4A)/.test(name)) return "AAC";
  return ["VP8", "VP9", "AV1", "OPUS", "G722", "MP3"].includes(name) ? name : "";
}

export function readBrowserCodecCapabilities(): BrowserCodecCapabilities {
  const read = (kind: "video" | "audio"): ReceiverCodec[] | null => {
    try {
      if (typeof RTCRtpReceiver === "undefined") return null;
      const codecs = RTCRtpReceiver.getCapabilities?.(kind)?.codecs.filter(
        codec =>
          codec.mimeType.toLowerCase().startsWith(`${kind}/`) &&
          !/\/(rtx|red|ulpfec|flexfec-03|cn|telephone-event)$/i.test(codec.mimeType)
      );
      return codecs?.length ? codecs : null;
    } catch {
      return null;
    }
  };
  return { webrtc: typeof RTCPeerConnection !== "undefined", video: read("video"), audio: read("audio") };
}

export function evaluateProtocolCompatibility(
  protocol: string,
  tracks: readonly PlaybackCodecTrack[],
  browser: BrowserCodecCapabilities,
  audioEnabled: boolean
): ProtocolCompatibility {
  const isRtc = protocol === "webrtc" || protocol === "webrtcs";
  const video = tracks.find(track => track.kind === "video" && track.ready);
  const videoCodec = codecName(video?.codec || "");
  if (isRtc) {
    if (!browser.webrtc) return { disabledReason: "当前浏览器不支持 WebRTC，请使用 WS-FLV 等协议" };
    // 接收能力只用于否定明确缺失的编码，不能保证 profile/level 和最终 SDP 协商成功。
    if (video && videoCodec && browser.video?.length && !browser.video.some(codec => codecName(codec.mimeType) === videoCodec)) {
      return { disabledReason: `当前视频为 ${video.codec}，此浏览器未提供对应的 WebRTC 接收能力，请使用 WS-FLV 等协议` };
    }
  }

  const audio = audioEnabled ? tracks.find(track => track.kind === "audio" && track.ready) : undefined;
  const audioCodec = codecName(audio?.codec || "");
  if (!audio || !audioCodec) return {};
  if (isRtc && browser.audio?.length) {
    // RTP 的时钟频率不总等于音频采样率：G.722 固定用 8 kHz，Opus 固定用 48 kHz。
    const clockRate = audioCodec === "G722" ? 8000 : audioCodec === "OPUS" ? 48000 : audio.sampleRate;
    const supported = browser.audio.some(
      codec =>
        codecName(codec.mimeType) === audioCodec &&
        (!clockRate || !codec.clockRate || clockRate === codec.clockRate) &&
        (audioCodec === "OPUS" || !audio.channels || !codec.channels || audio.channels === codec.channels)
    );
    if (!supported) {
      const rate = audio.sampleRate ? ` / ${audio.sampleRate} Hz` : "";
      return { warning: `当前音频为 ${audio.codec}${rate}，与此浏览器的 WebRTC 接收能力不匹配，可能没有声音；仍可尝试播放画面` };
    }
  }
  // 当前 HLS 的 TS 音频路径没有 G.711 解码；不影响视频入口，也不限制 FLV 的 WASM 兜底。
  if ((protocol === "hls" || protocol === "https-hls") && (audioCodec === "PCMA" || audioCodec === "PCMU")) {
    return { warning: `当前音频为 ${audio.codec}，HLS 播放可能没有声音；如需声音，建议使用 WS-FLV` };
  }
  return {};
}
