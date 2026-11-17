export interface ShareCredentials {
  publicId: string;
  signature: string;
  embed: boolean;
}

export interface PublicShare {
  name: string;
  online: boolean;
  hasAudio: boolean;
  expiresAt: string;
  serverTime: string;
}

export interface ViewerSession {
  viewerToken: string;
  expiresAt: string;
  httpFlvPath?: string;
  hlsPath?: string;
}

export interface ViewerStatus {
  expiresAt: string;
  serverTime: string;
}

interface ApiEnvelope<T> {
  code: number;
  message?: string;
  data: T;
}

export function parseShareFragment(hash: string): ShareCredentials | null {
  if (!hash.startsWith("#")) return null;
  const values = new URLSearchParams(hash.slice(1));
  const publicId = values.get("id")?.trim();
  const signature = values.get("sig")?.trim();
  if (!publicId || !signature) return null;
  return { publicId, signature, embed: values.get("embed") === "1" };
}

export function createShareURL(
  location: Pick<Location, "origin" | "pathname">,
  credentials: ShareCredentials,
  embed = false
): string {
  const fragment = new URLSearchParams({ id: credentials.publicId, sig: credentials.signature });
  if (embed) fragment.set("embed", "1");
  return `${location.origin}${location.pathname}#${fragment.toString()}`;
}

export function escapeHTMLAttribute(value: string): string {
  return value.replace(
    /[&<>"']/g,
    char => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[char] || char
  );
}

export async function resolveShare(credentials: ShareCredentials): Promise<PublicShare> {
  return post("/api/public/video-share/resolve", { publicId: credentials.publicId, signature: credentials.signature });
}

export async function createViewer(credentials: ShareCredentials): Promise<ViewerSession> {
  return post("/api/public/video-share/viewers", { publicId: credentials.publicId, signature: credentials.signature });
}

export async function getViewerStatus(viewerToken: string): Promise<ViewerStatus> {
  return post("/api/public/video-share/viewers/status", { viewerToken });
}

export async function closeViewer(viewerToken: string): Promise<void> {
  await fetch(`/api/public/video-share/viewers/${encodeURIComponent(viewerToken)}`, {
    method: "DELETE",
    credentials: "omit",
    cache: "no-store",
    redirect: "error",
    referrerPolicy: "no-referrer"
  });
}

async function post<T>(path: string, body: unknown): Promise<T> {
  let response: Response;
  try {
    response = await fetch(path, {
      method: "POST",
      credentials: "omit",
      cache: "no-store",
      redirect: "error",
      referrerPolicy: "no-referrer",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      body: JSON.stringify(body)
    });
  } catch {
    throw new Error("网络暂时不可用，请重试");
  }
  if (response.status === 410) throw new Error("分享已失效，请联系分享人");
  if (!response.ok) throw new Error("暂时无法验证分享，请重试");
  let envelope: ApiEnvelope<T>;
  try {
    envelope = (await response.json()) as ApiEnvelope<T>;
  } catch {
    throw new Error("分享服务返回了无效响应");
  }
  if (envelope.code !== 0 || envelope.data == null) throw new Error(envelope.message || "暂时无法验证分享，请重试");
  return envelope.data;
}
