import { describe, expect, it, vi } from "vitest";
import { closeViewer, createShareURL, escapeHTMLAttribute, parseShareFragment, resolveShare } from "./shareSession";

describe("standalone share session", () => {
  it("reads credentials only from the URL fragment and recognizes embedded mode", () => {
    expect(parseShareFragment("?id=query&sig=bad")).toBeNull();
    expect(parseShareFragment("#id=public&sig=secret&embed=1")).toEqual({
      publicId: "public",
      signature: "secret",
      embed: true
    });
  });

  it("builds normal and iframe URLs from the same credentials", () => {
    const credentials = { publicId: "public", signature: "secret", embed: false };
    expect(createShareURL({ origin: "https://uvp.example", pathname: "/share.html" }, credentials)).toBe(
      "https://uvp.example/share.html#id=public&sig=secret"
    );
    expect(createShareURL({ origin: "https://uvp.example", pathname: "/share.html" }, credentials, true)).toBe(
      "https://uvp.example/share.html#id=public&sig=secret&embed=1"
    );
  });

  it("exchanges anonymously without sending cookies and keeps credentials in the POST body", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ code: 0, data: { name: "Lobby", online: true, hasAudio: true } }), {
        status: 200,
        headers: { "Content-Type": "application/json" }
      })
    );
    await resolveShare({ publicId: "public", signature: "secret", embed: false });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/public/video-share/resolve",
      expect.objectContaining({
        method: "POST",
        credentials: "omit",
        cache: "no-store",
        redirect: "error",
        body: JSON.stringify({ publicId: "public", signature: "secret" })
      })
    );
    fetchMock.mockRestore();
  });

  it("releases a viewer without cookies and escapes user-controlled iframe attributes", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }));
    await closeViewer("token/with path");
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/public/video-share/viewers/token%2Fwith%20path",
      expect.objectContaining({ method: "DELETE", credentials: "omit", referrerPolicy: "no-referrer" })
    );
    expect(escapeHTMLAttribute(`a"b&<c>`)).toBe("a&quot;b&amp;&lt;c&gt;");
    fetchMock.mockRestore();
  });
});
