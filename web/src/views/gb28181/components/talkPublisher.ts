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
  const pcma = codecs.filter(codec => codec.mimeType.toLowerCase() === "audio/pcma" && codec.clockRate === 8000);
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

export interface AudioLevelMeter {
  stop(): void;
}

export type AudioLevelSnapshot = (level: number, spectrum: number[]) => void;

export const TALK_ACTIVE_POLL_INTERVAL_MS = 100;

/** 无 STUN 时只需给 host candidate 留短暂窗口；有 STUN 时保留公网映射的兜底时间。 */
export function talkIceGatheringTimeoutMs(iceServers?: { urls: string[] }[]): number {
  return iceServers?.length ? 1_200 : 250;
}

/**
 * 采集流的实时电平（0..1），用于「正在说话」的波动指示。
 *
 * ⛔ 只接 `AnalyserNode`，**绝不 connect 到 destination** —— 一旦连上声卡就是本地回放，立刻啸叫。
 * ⛔ 拿不到 AudioContext 的环境（jsdom / 老浏览器）返回 `null`，调用方静默降级：电平指示是装饰，
 * 不能因为它不可用而让对讲本身失败。
 * ⛔ 电平按 dB 映射（-60dB→0，0dB→1），不是线性 RMS —— 线性映射下正常说话只在 0.05 附近，
 * 波形几乎不动。
 */
export function createAudioLevelMeter(stream: MediaStream, onLevel: AudioLevelSnapshot): AudioLevelMeter | null {
  const Ctor: typeof AudioContext | undefined = (window as any).AudioContext || (window as any).webkitAudioContext;
  if (!Ctor) return null;
  let context: AudioContext;
  try {
    context = new Ctor();
  } catch {
    return null;
  }

  const analyser = context.createAnalyser();
  analyser.fftSize = 2048;
  analyser.smoothingTimeConstant = 0.6;
  const source = context.createMediaStreamSource(stream);
  source.connect(analyser);
  if (typeof context.resume === "function") void context.resume().catch(() => undefined);

  const samples = new Float32Array(analyser.fftSize);
  const frequencyData = new Uint8Array(analyser.frequencyBinCount || analyser.fftSize / 2);
  const smoothedSpectrum = Array.from({ length: 11 }, () => 0);
  const emittedSpectrum = Array.from({ length: 11 }, () => 0);
  // 只展示人声相关频率，避免把柱子浪费在接近 Nyquist 的超高频上。
  const minFrequency = 80;
  const maxFrequency = Math.min(8000, context.sampleRate / 2);
  const binWidth = context.sampleRate / analyser.fftSize;
  const bands = smoothedSpectrum.map((_, band) => {
    const low = minFrequency * Math.pow(maxFrequency / minFrequency, band / smoothedSpectrum.length);
    const high = minFrequency * Math.pow(maxFrequency / minFrequency, (band + 1) / smoothedSpectrum.length);
    const start = Math.max(1, Math.floor(low / binWidth));
    return { start, end: Math.min(frequencyData.length, Math.max(start + 1, Math.floor(high / binWidth))) };
  });
  let smoothed = 0;
  let lastEmitted = -1;
  let frame = 0;

  const tick = () => {
    analyser.getFloatTimeDomainData(samples);
    let sum = 0;
    for (let i = 0; i < samples.length; i++) sum += samples[i] * samples[i];
    const rms = Math.sqrt(sum / samples.length);
    const db = 20 * Math.log10(Math.max(rms, 1e-8));
    const level = Math.min(1, Math.max(0, (db + 60) / 60));
    // 起音快、释放慢，与音量表一致；否则波形会随帧噪声乱跳。
    smoothed += (level - smoothed) * (level > smoothed ? 0.55 : 0.12);
    if (typeof analyser.getByteFrequencyData === "function") {
      analyser.getByteFrequencyData(frequencyData);
      for (let band = 0; band < smoothedSpectrum.length; band++) {
        const { start, end } = bands[band];
        let peak = 0;
        for (let index = start; index < end; index++) peak = Math.max(peak, frequencyData[index] / 255);
        const gated = peak < 0.06 ? 0 : peak;
        smoothedSpectrum[band] += (gated - smoothedSpectrum[band]) * (gated > smoothedSpectrum[band] ? 0.5 : 0.16);
      }
    }
    const spectrumChanged = smoothedSpectrum.some((value, index) => Math.abs(value - emittedSpectrum[index]) >= 0.025);
    // 变化不足 2% 就不回调，省掉无意义的 DOM 写入；频谱变化单独触发回调，保证每根柱子独立响应。
    if (Math.abs(smoothed - lastEmitted) >= 0.02 || spectrumChanged) {
      lastEmitted = smoothed;
      smoothedSpectrum.forEach((value, index) => {
        emittedSpectrum[index] = value;
      });
      onLevel(smoothed, [...smoothedSpectrum]);
    }
    frame = window.requestAnimationFrame(tick);
  };
  frame = window.requestAnimationFrame(tick);

  return {
    stop() {
      window.cancelAnimationFrame(frame);
      onLevel(
        0,
        smoothedSpectrum.map(() => 0)
      );
      source.disconnect();
      analyser.disconnect();
      if (typeof context.close === "function") void context.close().catch(() => undefined);
    }
  };
}

/**
 * 把麦克风采集失败翻译成「现场人员照着能做」的中文。
 *
 * ⛔ 别把 `error.message` 直接弹出去：浏览器给的是英文（`Permission denied`、
 * `Requested device not found`、`Could not start audio source`），现场人员看到等于没提示，
 * 有些浏览器还会把 message 留空，最后只剩一句"启动失败"。
 * ⛔ 也**不能**把所有失败都说成"浏览器不支持麦克风"：http 局域网地址下
 * `navigator.mediaDevices` 根本不存在（`isSecureContext === false`），
 * 换成 HTTPS / localhost 立刻就好 —— 那是部署方式问题，不是设备问题。
 *
 * @param error getUserMedia 抛出的 DOMException（或用 `null` 表示"浏览器根本没有这个 API"）
 * @param secureContext 当前页面是否安全上下文，调用方传 `window.isSecureContext !== false`
 */
export function describeMicError(error: unknown, secureContext = true): string {
  if (!secureContext) {
    return "当前页面不是安全上下文（非 HTTPS 的局域网地址），浏览器禁止采集麦克风；请改用 HTTPS 或 localhost 访问";
  }
  const name = (error as { name?: string } | null)?.name || "";
  switch (name) {
    case "NotAllowedError":
    case "PermissionDeniedError":
    case "SecurityError":
      return "麦克风权限被拒绝，请在浏览器地址栏的站点权限里允许麦克风后重试";
    case "NotFoundError":
    case "DevicesNotFoundError":
      return "没有检测到麦克风设备，请检查麦克风是否已连接并被系统识别";
    case "NotReadableError":
    case "TrackStartError":
      return "麦克风被其它程序占用或硬件异常，请关闭占用麦克风的程序后重试";
    case "OverconstrainedError":
      return "当前麦克风不支持所需的采集参数，请更换设备后重试";
    case "AbortError":
      return "麦克风启动被中断，请重试";
    default:
      return (error as { message?: string } | null)?.message || "麦克风采集失败";
  }
}

/**
 * 等 ICE 收集结束。上游（ZLM 的 WHIP 端点）不支持 PATCH，候选必须在 POST 的 offer 里
 * 一次带齐，所以不能按标准 WHIP 的 trickle 模式发空候选。
 *
 * ⛔ 收集超时**不是失败**：STUN 只是「拿到自己 NAT 之后的映射地址」的优化，超时说明
 * STUN 不可达，而 host candidate 早在超时之前就已经在手了 —— 同网段部署靠它就能建连。
 * 这里原本是 reject，代价是：某个媒体节点的 STUN 端口没映射出来，浏览器干等 5s 后
 * 对讲直接报错，连「拿已有候选试一次」的机会都没有。降级必须优于失败，所以改成 resolve。
 *
 * @returns 是否真的收齐（`false` = 按已有候选发布，调用方应提示降级）
 */
export async function waitForIceGatheringComplete(connection: RTCPeerConnection, timeoutMs = 2000): Promise<boolean> {
  if (connection.iceGatheringState === "complete") return true;
  return new Promise<boolean>(resolve => {
    const cleanup = () => {
      window.clearTimeout(timeout);
      connection.removeEventListener("icegatheringstatechange", onChange);
    };
    const timeout = window.setTimeout(() => {
      cleanup();
      // 极端竞态：超时与完成同时发生，以实际状态为准。
      resolve(connection.iceGatheringState === "complete");
    }, timeoutMs);
    const onChange = () => {
      if (connection.iceGatheringState !== "complete") return;
      cleanup();
      resolve(true);
    };
    connection.addEventListener("icegatheringstatechange", onChange);
  });
}
