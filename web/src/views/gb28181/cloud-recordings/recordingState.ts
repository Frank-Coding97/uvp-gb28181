import type { RecordingAvailability, RecordingFileQuery } from "./api";

export interface AvailabilityPresentation {
  label: string;
  color: "green" | "orange" | "red" | "gray";
  canAccess: boolean;
}

const availabilityPresentations: Record<RecordingAvailability, AvailabilityPresentation> = {
  available: { label: "可播放", color: "green", canAccess: true },
  node_offline: { label: "节点离线", color: "orange", canAccess: false },
  node_missing: { label: "节点已移除", color: "gray", canAccess: false },
  file_missing: { label: "文件已缺失", color: "red", canAccess: false },
  access_unavailable: { label: "暂不可访问", color: "orange", canAccess: false }
};

export function defaultRecordingQuery(): RecordingFileQuery {
  return {
    page: 1,
    pageSize: 20
  };
}

export function availabilityPresentation(value: RecordingAvailability): AvailabilityPresentation {
  return availabilityPresentations[value] ?? { label: "暂不可访问", color: "orange", canAccess: false };
}

export function recordingErrorPresentation(error: unknown): string {
  const status = (error as { response?: { status?: number } })?.response?.status;
  switch (status) {
    case 404:
      return "录像文件不存在";
    case 403:
      return "访问权限已失效";
    case 410:
      return "访问凭据已过期";
    case 416:
      return "请求的录像范围无效";
    case 502:
    case 503:
      return "录像节点暂不可用";
    default:
      return "请求失败，请稍后重试";
  }
}

export function createLatestRequestCoordinator() {
  let currentToken = 0;
  let controller: AbortController | null = null;
  return {
    next() {
      controller?.abort();
      controller = new AbortController();
      const token = ++currentToken;
      return { token, signal: controller.signal };
    },
    isCurrent(token: number) {
      return token === currentToken && controller?.signal.aborted === false;
    },
    dispose() {
      currentToken += 1;
      controller?.abort();
      controller = null;
    }
  };
}

export function createPlaybackRecovery() {
  let recovered = false;
  return {
    next(currentTime: number) {
      const retry = !recovered;
      recovered = true;
      return { retry, resumeAt: currentTime };
    },
    reset() {
      recovered = false;
    }
  };
}

export function createPollingController<T>(load: () => Promise<T>, publish: (value: T) => void, intervalMs: number) {
  let generation = 0;
  let timer: ReturnType<typeof setTimeout> | null = null;

  const run = async (runGeneration: number) => {
    try {
      const value = await load();
      if (runGeneration !== generation) return;
      publish(value);
    } finally {
      if (runGeneration === generation) timer = setTimeout(() => void run(runGeneration), intervalMs);
    }
  };

  return {
    start() {
      generation += 1;
      if (timer) clearTimeout(timer);
      timer = null;
      void run(generation);
    },
    stop() {
      generation += 1;
      if (timer) clearTimeout(timer);
      timer = null;
    }
  };
}
