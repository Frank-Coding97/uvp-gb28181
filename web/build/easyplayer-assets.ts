import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import type { Plugin } from "vite";

/** Keep the main script, workers and their relative WASM import on one content version. */
export function easyPlayerAssets(): Plugin {
  const names = ["EasyPlayer-pro.js", "EasyPlayer-decode.js", "EasyPlayer-lib.js", "EasyPlayer-pro.wasm"];
  let resources: Array<{ name: string; source: Buffer }> = [];
  let base = "";
  return {
    name: "uvp-easyplayer-assets",
    apply: "build",
    configResolved(config) {
      if (!config.publicDir) throw new Error("EasyPlayer requires publicDir");
      resources = names.map(name => ({ name, source: readFileSync(join(config.publicDir, "easyplayer", name)) }));
      const hash = createHash("sha256");
      resources.forEach(({ name, source }) => hash.update(name).update("\0").update(source));
      base = `/easyplayer/${hash.digest("hex").slice(0, 16)}/`;
    },
    transformIndexHtml(html) {
      return html.replace('src="/easyplayer/EasyPlayer-pro.js"', `src="${base}EasyPlayer-pro.js"`);
    },
    generateBundle() {
      resources.forEach(({ name, source }) => {
        this.emitFile({ type: "asset", fileName: base.slice(1) + name, source });
      });
    }
  };
}
