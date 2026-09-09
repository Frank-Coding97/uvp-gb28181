import { beforeEach, describe, expect, it, vi } from "vitest";

const getAccessToken = vi.hoisted(() => vi.fn());
vi.mock("@/utils/auth", () => ({ getAccessToken }));
import { openRealtimeLogStream } from "./realtime-business-log";

describe("realtime business log stream", () => {
  beforeEach(() => { getAccessToken.mockReturnValue({ accessToken: "token-a" }); vi.restoreAllMocks(); });

  it("uses Authorization header and parses SSE blocks", async () => {
    const body = new ReadableStream({ start(controller) { controller.enqueue(new TextEncoder().encode('event: ready\ndata: {"latestSequence":1,"subscriptionId":"s"}\n\nevent: message\ndata: {"eventId":"e","sequence":1,"event":"gb28181.play.started"}\n\n')); controller.close(); } });
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(body, { status: 200 }));
    const items: any[] = [];
    await openRealtimeLogStream({ deviceId: "device-a", since: 7 }, item => items.push(item), new AbortController().signal);
    expect(fetchMock).toHaveBeenCalledWith("/api/gb28181/logs/stream?deviceId=device-a&since=7", expect.objectContaining({ headers: expect.objectContaining({ Authorization: "Bearer token-a" }) }));
    expect(items.map(item => item.type)).toEqual(["ready", "message"]);
  });

  it("propagates non-2xx responses and cancels the reader", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("forbidden", { status: 403 }));
    await expect(openRealtimeLogStream({}, () => undefined, new AbortController().signal)).rejects.toThrow("(403)");
  });

  it("parses control events without requiring a message payload", async () => {
    const body = new ReadableStream({ start(controller) { controller.enqueue(new TextEncoder().encode('event: ping\ndata: {"at":"now"}\n\nevent: auth_expired\ndata: {"reason":"max_connection_lifetime"}\n\n')); controller.close(); } });
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(body, { status: 200 }));
    const items: any[] = [];
    await openRealtimeLogStream({}, item => items.push(item), new AbortController().signal);
    expect(items).toEqual([{ type: "ping", data: { at: "now" } }, { type: "auth_expired", data: { reason: "max_connection_lifetime" } }]);
  });
});
