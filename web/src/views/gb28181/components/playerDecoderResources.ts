let prepared: Promise<string | undefined> | undefined;

/**
 * HTTP cache can be unavailable even when response headers are correct. Keep one
 * decoder resource set per page so a new player/core does not repeat the download.
 * EasyPlayer appends a filename to decoderPath; the Blob URL fragment selects the
 * original MSE or WASM worker without modifying either third-party file.
 */
export function preparePlayerDecoder(): Promise<string | undefined> {
  return (prepared ??= prepare());
}

async function supportsBlobWorker(): Promise<boolean> {
  if (typeof Worker === "undefined" || typeof URL.createObjectURL !== "function") return false;
  const url = URL.createObjectURL(new Blob(['self.postMessage("ready")'], { type: "application/javascript" }));
  let worker: Worker | undefined;
  try {
    return await new Promise<boolean>(resolve => {
      const timer = setTimeout(() => resolve(false), 1000);
      try {
        worker = new Worker(url);
        worker.onmessage = () => {
          clearTimeout(timer);
          resolve(true);
        };
        worker.onerror = event => {
          event.preventDefault();
          clearTimeout(timer);
          resolve(false);
        };
      } catch {
        clearTimeout(timer);
        resolve(false);
      }
    });
  } finally {
    worker?.terminate();
    URL.revokeObjectURL(url);
  }
}

async function prepare(): Promise<string | undefined> {
  const urls: string[] = [];
  try {
    const script = Array.from(document.scripts).find(script =>
      new URL(script.src, location.href).pathname.endsWith("/EasyPlayer-pro.js")
    );
    if (!script || !(await supportsBlobWorker())) return undefined;
    const base = new URL(".", script.src);
    const files = ["EasyPlayer-decode.js", "EasyPlayer-lib.js", "EasyPlayer-pro.wasm"];
    const blobs = await Promise.all(
      files.map(async name => {
        const response = await fetch(new URL(name, base).href);
        if (!response.ok) throw new Error("Player decoder resource unavailable");
        return response.blob();
      })
    );
    blobs.forEach(blob => urls.push(URL.createObjectURL(blob)));
    const bootstrap =
      `var JESSIBUCA_PRO_WASM_URL=${JSON.stringify(urls[2])};` +
      `importScripts(self.location.hash === "#EasyPlayer-decode.js" ? ${JSON.stringify(urls[0])} : ${JSON.stringify(urls[1])});`;
    urls.push(URL.createObjectURL(new Blob([bootstrap], { type: "application/javascript" })));
    // Object URLs belong to this document and are released when it unloads.
    return urls[3] + "#";
  } catch {
    urls.forEach(url => URL.revokeObjectURL(url));
    return undefined;
  }
}
