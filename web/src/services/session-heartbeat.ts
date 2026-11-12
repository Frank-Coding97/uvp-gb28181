import { sessionHeartbeatAPI } from "@/api/online-user";
import { hasRefreshToken } from "@/utils/auth";

const HEARTBEAT_INTERVAL_MS = 60_000;

let running = false;
let timer: ReturnType<typeof setTimeout> | undefined;
let requestInFlight = false;
let lastAttemptAt: number | undefined;
let generation = 0;

function clearHeartbeatTimer() {
  if (timer !== undefined) {
    clearTimeout(timer);
    timer = undefined;
  }
}

function scheduleHeartbeat(delay = HEARTBEAT_INTERVAL_MS) {
  clearHeartbeatTimer();
  if (!running || document.visibilityState !== "visible" || !hasRefreshToken()) return;
  timer = setTimeout(() => {
    timer = undefined;
    void runHeartbeat();
  }, delay);
}

async function runHeartbeat() {
  if (!running || requestInFlight || document.visibilityState !== "visible" || !hasRefreshToken()) return;

  const elapsed = lastAttemptAt === undefined ? HEARTBEAT_INTERVAL_MS : Date.now() - lastAttemptAt;
  if (elapsed < HEARTBEAT_INTERVAL_MS) {
    scheduleHeartbeat(HEARTBEAT_INTERVAL_MS - elapsed);
    return;
  }

  const requestGeneration = generation;
  requestInFlight = true;
  lastAttemptAt = Date.now();
  try {
    await sessionHeartbeatAPI();
  } catch {
    // 401/503 behavior is centralized in the HTTP interceptor.
  } finally {
    requestInFlight = false;
    if (!running) return;
    if (requestGeneration !== generation) {
      void runHeartbeat();
      return;
    }
    scheduleHeartbeat();
  }
}

function handleVisibilityChange() {
  if (document.visibilityState === "hidden") {
    clearHeartbeatTimer();
    return;
  }
  void runHeartbeat();
}

export function startSessionHeartbeat() {
  if (!running) {
    running = true;
    generation += 1;
    document.addEventListener("visibilitychange", handleVisibilityChange);
  }
  void runHeartbeat();
}

export function stopSessionHeartbeat() {
  if (running) document.removeEventListener("visibilitychange", handleVisibilityChange);
  running = false;
  generation += 1;
  lastAttemptAt = undefined;
  clearHeartbeatTimer();
}
