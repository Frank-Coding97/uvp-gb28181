import { afterEach, expect, it, vi } from "vitest";

const names = ["EasyPlayer-decode.js", "EasyPlayer-lib.js", "EasyPlayer-pro.wasm"];

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  document.head.innerHTML = "";
  vi.resetModules();
});

function browser() {
  const script = document.createElement("script");
  script.type = "application/x-uvp-test";
  script.src = "/easyplayer/0123456789abcdef/EasyPlayer-pro.js";
  document.head.append(script);
  const blobs: Blob[] = [];
  const create = vi.spyOn(URL, "createObjectURL").mockImplementation(blob => {
    blobs.push(blob as Blob);
    return `blob:https://example.test/${blobs.length}`;
  });
  vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
  class WorkerStub {
    onmessage: (() => void) | null = null;
    onerror: (() => void) | null = null;
    terminate = vi.fn();
    constructor() {
      queueMicrotask(() => this.onmessage?.());
    }
  }
  vi.stubGlobal("Worker", WorkerStub);
  const fetch = vi.fn(async (url: string) => new Response(url.endsWith(".wasm") ? "wasm" : "decoder", { status: 200 }));
  vi.stubGlobal("fetch", fetch);
  return { fetch, create, blobs };
}

it("shares one resource download across concurrent preparation and later players", async () => {
  const { fetch, blobs } = browser();
  const { preparePlayerDecoder } = await import("./playerDecoderResources");
  const [first, concurrent] = await Promise.all([preparePlayerDecoder(), preparePlayerDecoder()]);
  expect(first).toBeTruthy();
  expect(first).toBe(concurrent);
  expect(await preparePlayerDecoder()).toBe(first);
  expect(fetch).toHaveBeenCalledTimes(3);
  for (const name of names) expect(fetch).toHaveBeenCalledWith(expect.stringContaining(`/0123456789abcdef/${name}`));
  const bootstrap = await blobs.at(-1)!.text();
  expect(bootstrap).toContain('JESSIBUCA_PRO_WASM_URL="blob:https://example.test/4"');
  expect(bootstrap).toContain("#EasyPlayer-decode.js");
  expect(bootstrap).toContain("blob:https://example.test/2");
  expect(bootstrap).toContain("blob:https://example.test/3");
  expect(first).toBe("blob:https://example.test/5#");
});

it("keeps the original decoder path available when preparation fails", async () => {
  const { fetch } = browser();
  fetch.mockRejectedValue(new Error("resource unavailable"));
  const { preparePlayerDecoder } = await import("./playerDecoderResources");
  expect(await preparePlayerDecoder()).toBeUndefined();
  expect(await preparePlayerDecoder()).toBeUndefined();
  expect(fetch).toHaveBeenCalledTimes(3);
});

it("uses the original decoder when Blob workers are unavailable", async () => {
  const { fetch } = browser();
  vi.stubGlobal("Worker", undefined);
  const { preparePlayerDecoder } = await import("./playerDecoderResources");
  expect(await preparePlayerDecoder()).toBeUndefined();
  expect(fetch).not.toHaveBeenCalled();
});
