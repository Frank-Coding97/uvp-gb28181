export const STANDALONE_SETUP_PATH = "/standalone-setup";
export const STANDALONE_SETUP_STATUS_PATH = "/api/standalone/setup/status";
export const STANDALONE_SETUP_ADMIN_PATH = "/api/standalone/setup/admin";
export const BOOTSTRAP_TOKEN_QUERY = "bootstrap_token";
export const BOOTSTRAP_TOKEN_HEADER = "X-UVP-Setup-Token";
export const BCRYPT_MAX_PASSWORD_BYTES = 72;

export type StandaloneSetupPhase = "pending_admin" | "pending_sip" | "complete";

export interface StandaloneSetupStatus {
  phase: StandaloneSetupPhase;
  standalone: true;
}

export type StandaloneSetupProbe =
  | { kind: "standalone"; status: StandaloneSetupStatus }
  | { kind: "legacy" }
  | { kind: "unavailable" };

export type StandaloneSetupAdminResult =
  | { outcome: "created"; phase: "pending_sip" }
  | { outcome: "unauthorized"; status: 401 | 403 }
  | { outcome: "conflict"; status: 409 }
  | { outcome: "failed"; status: number }
  | { outcome: "missing-token" };

export type Fetcher = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

const nativeFetch: Fetcher = (input, init) => globalThis.fetch(input, init);
let statusProbePromise: Promise<StandaloneSetupProbe> | undefined;
let bootstrapTokenRead = false;
let bootstrapToken: string | null = null;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function isPhase(value: unknown): value is StandaloneSetupPhase {
  return value === "pending_admin" || value === "pending_sip" || value === "complete";
}

function parseStatus(value: unknown): StandaloneSetupStatus | null {
  if (!isRecord(value) || value.standalone !== true || !isPhase(value.phase)) return null;
  return { standalone: true, phase: value.phase };
}

export async function requestStandaloneSetupStatus(fetcher: Fetcher = nativeFetch): Promise<StandaloneSetupProbe> {
  try {
    const response = await fetcher(STANDALONE_SETUP_STATUS_PATH, {
      method: "GET",
      cache: "no-store",
      headers: { Accept: "application/json" }
    });
    if (response.status === 404) return { kind: "legacy" };
    if (!response.ok) return { kind: "unavailable" };
    const status = parseStatus(await response.json());
    return status ? { kind: "standalone", status } : { kind: "unavailable" };
  } catch {
    return { kind: "unavailable" };
  }
}

/** Cache only an in-flight/successful probe; transient failures are retried on a later navigation. */
export function loadStandaloneSetupStatus(fetcher: Fetcher = nativeFetch): Promise<StandaloneSetupProbe> {
  if (statusProbePromise) return statusProbePromise;
  const probe = requestStandaloneSetupStatus(fetcher);
  statusProbePromise = probe.then(result => {
    if (result.kind === "unavailable") statusProbePromise = undefined;
    return result;
  });
  return statusProbePromise;
}

function cacheStandalonePhase(phase: StandaloneSetupPhase) {
  statusProbePromise = Promise.resolve({
    kind: "standalone",
    status: { standalone: true, phase }
  });
}

export async function createStandaloneAdmin(
  payload: { username: string; password: string },
  setupToken: string,
  fetcher: Fetcher = nativeFetch
): Promise<StandaloneSetupAdminResult> {
  if (!setupToken) return { outcome: "missing-token" };

  const response = await fetcher(STANDALONE_SETUP_ADMIN_PATH, {
    method: "POST",
    cache: "no-store",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
      [BOOTSTRAP_TOKEN_HEADER]: setupToken
    },
    body: JSON.stringify(payload)
  });

  if (response.status === 401 || response.status === 403) {
    return { outcome: "unauthorized", status: response.status };
  }
  if (response.status === 409) {
    cacheStandalonePhase("pending_sip");
    return { outcome: "conflict", status: 409 };
  }
  if (response.status !== 201) return { outcome: "failed", status: response.status };

  try {
    const result = await response.json();
    if (isRecord(result) && result.phase === "pending_sip") {
      cacheStandalonePhase("pending_sip");
      return { outcome: "created", phase: "pending_sip" };
    }
  } catch {
    // A successful response without the locked response shape is not accepted as setup completion.
  }
  return { outcome: "failed", status: 201 };
}

function queryValue(value: unknown): string | null {
  if (typeof value === "string") return value;
  if (Array.isArray(value) && typeof value[0] === "string") return value[0];
  return null;
}

function removeBootstrapTokenFromHash() {
  if (typeof window === "undefined" || !window.location.hash) return;
  const hashRoute = window.location.hash.slice(1);
  const queryStart = hashRoute.indexOf("?");
  if (queryStart < 0) return;

  const path = hashRoute.slice(0, queryStart);
  const params = new URLSearchParams(hashRoute.slice(queryStart + 1));
  if (!params.has(BOOTSTRAP_TOKEN_QUERY)) return;
  params.delete(BOOTSTRAP_TOKEN_QUERY);

  const nextHash = params.toString() ? `${path}?${params.toString()}` : path;
  window.history.replaceState(
    window.history.state,
    document.title,
    `${window.location.pathname}${window.location.search}#${nextHash}`
  );
}

/** Read the launcher credential once, retain it only in this module/component memory, then scrub the hash URL. */
export function readBootstrapTokenOnce(routeQuery?: Record<string, unknown>): string | null {
  if (bootstrapTokenRead) return bootstrapToken;
  bootstrapTokenRead = true;

  const fromRoute = queryValue(routeQuery?.[BOOTSTRAP_TOKEN_QUERY]);
  const fromHash =
    typeof window === "undefined"
      ? null
      : new URLSearchParams(window.location.hash.split("?")[1] || "").get(BOOTSTRAP_TOKEN_QUERY);
  bootstrapToken = fromRoute ?? fromHash;
  removeBootstrapTokenFromHash();
  return bootstrapToken;
}

export function forgetBootstrapToken() {
  bootstrapToken = null;
}

export function isBcryptPasswordLengthValid(password: string): boolean {
  return new TextEncoder().encode(password).byteLength <= BCRYPT_MAX_PASSWORD_BYTES;
}

export function standaloneSetupNavigation(
  path: string,
  query: Record<string, unknown>,
  probe: StandaloneSetupProbe
): { path: string; query?: Record<string, unknown> } | null {
  if (probe.kind !== "standalone") return null;
  if (probe.status.phase === "pending_admin" && path !== STANDALONE_SETUP_PATH) {
    return { path: STANDALONE_SETUP_PATH, query };
  }
  if (probe.status.phase !== "pending_admin" && path === STANDALONE_SETUP_PATH) {
    return { path: "/login" };
  }
  return null;
}

/** Test isolation only; production code never persists or resets this state. */
export function resetStandaloneSetupStateForTests() {
  statusProbePromise = undefined;
  bootstrapTokenRead = false;
  bootstrapToken = null;
}
