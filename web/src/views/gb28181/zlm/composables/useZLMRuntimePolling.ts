import { onBeforeUnmount, onMounted, watch, type Ref } from "vue";

export interface ZLMRuntimePollingOptions<T> {
  load: (nodeId: number, signal: AbortSignal) => Promise<T>;
  publish: (value: T, nodeId: number) => void;
  onError?: (error: unknown, nodeId: number) => void;
  intervalMs?: number;
}

export interface ZLMRuntimePollingController {
  start: () => void;
  setNode: (nodeId: number | null) => void;
  setVisible: (visible: boolean) => void;
  setPaused: (paused: boolean) => void;
  refresh: () => void;
  dispose: () => void;
}

function isAbortError(error: unknown) {
  return error instanceof DOMException
    ? error.name === "AbortError"
    : (error as { name?: string } | null)?.name === "AbortError";
}

export function createZLMRuntimePollingController<T>(options: ZLMRuntimePollingOptions<T>): ZLMRuntimePollingController {
  const intervalMs = Math.max(250, options.intervalMs ?? 5000);
  let nodeId: number | null = null;
  let running = false;
  let visible = true;
  let paused = false;
  let disposed = false;
  let generation = 0;
  let timer: ReturnType<typeof setTimeout> | null = null;
  let request: AbortController | null = null;

  function eligible() {
    return running && !disposed && visible && !paused && nodeId !== null;
  }

  function cancelCurrent() {
    generation += 1;
    if (timer !== null) clearTimeout(timer);
    timer = null;
    request?.abort();
    request = null;
  }

  function launch() {
    if (!eligible()) return;
    const currentNode = nodeId as number;
    const currentGeneration = generation;
    const controller = new AbortController();
    request = controller;
    let loading: Promise<T>;
    try {
      loading = options.load(currentNode, controller.signal);
    } catch (error) {
      loading = Promise.reject(error);
    }

    void loading
      .then(value => {
        if (generation !== currentGeneration || controller.signal.aborted || currentNode !== nodeId || !eligible()) return;
        options.publish(value, currentNode);
      })
      .catch(error => {
        if (generation !== currentGeneration || controller.signal.aborted || isAbortError(error) || currentNode !== nodeId || !eligible()) return;
        options.onError?.(error, currentNode);
      })
      .finally(() => {
        if (generation !== currentGeneration || controller.signal.aborted || currentNode !== nodeId || !eligible()) return;
        if (request === controller) request = null;
        timer = setTimeout(restart, intervalMs);
      });
  }

  function restart() {
    cancelCurrent();
    launch();
  }

  return {
    start() {
      if (disposed) return;
      running = true;
      restart();
    },
    setNode(nextNodeId) {
      const normalized = Number.isSafeInteger(nextNodeId) && Number(nextNodeId) > 0 ? Number(nextNodeId) : null;
      if (nodeId === normalized) return;
      nodeId = normalized;
      restart();
    },
    setVisible(nextVisible) {
      if (visible === nextVisible) return;
      visible = nextVisible;
      restart();
    },
    setPaused(nextPaused) {
      if (paused === nextPaused) return;
      paused = nextPaused;
      restart();
    },
    refresh() {
      restart();
    },
    dispose() {
      if (disposed) return;
      disposed = true;
      running = false;
      cancelCurrent();
    }
  };
}

export interface UseZLMRuntimePollingOptions<T> extends ZLMRuntimePollingOptions<T> {
  nodeId: Ref<number | null>;
  paused?: Ref<boolean>;
}

export function useZLMRuntimePolling<T>(options: UseZLMRuntimePollingOptions<T>) {
  const controller = createZLMRuntimePollingController(options);
  const stopNodeWatch = watch(options.nodeId, value => controller.setNode(value), { immediate: true });
  const stopPausedWatch = options.paused
    ? watch(options.paused, value => controller.setPaused(value), { immediate: true })
    : undefined;

  const handleVisibilityChange = () => controller.setVisible(document.visibilityState === "visible");

  onMounted(() => {
    controller.setVisible(document.visibilityState === "visible");
    document.addEventListener("visibilitychange", handleVisibilityChange);
    controller.start();
  });

  onBeforeUnmount(() => {
    stopNodeWatch();
    stopPausedWatch?.();
    document.removeEventListener("visibilitychange", handleVisibilityChange);
    controller.dispose();
  });

  return { refresh: controller.refresh, dispose: controller.dispose };
}
