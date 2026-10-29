// @vitest-environment node
import { realpathSync, mkdtempSync, mkdirSync, readFileSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { build } from "vite";
import { afterEach, expect, it } from "vitest";
import { easyPlayerAssets } from "../../build/easyplayer-assets";

const roots: string[] = [];
const files = ["EasyPlayer-pro.js", "EasyPlayer-decode.js", "EasyPlayer-lib.js", "EasyPlayer-pro.wasm"];
afterEach(() => roots.splice(0).forEach(root => rmSync(root, { recursive: true, force: true })));

it("keeps all player resources in one stable content version and invalidates it when the decoder changes", async () => {
  const root = realpathSync(mkdtempSync(join(tmpdir(), "uvp-player-assets-")));
  roots.push(root);
  mkdirSync(join(root, "public/easyplayer"), { recursive: true });
  files.forEach(name => writeFileSync(join(root, "public/easyplayer", name), `original-${name}`));
  writeFileSync(join(root, "index.html"), '<html><head><script src="/easyplayer/EasyPlayer-pro.js"></script></head></html>');
  const compile = async () => {
    await build({ root, configFile: false, plugins: [easyPlayerAssets()], logLevel: "silent" });
    const html = readFileSync(join(root, "dist/index.html"), "utf8");
    const script = html.match(/src="(\/easyplayer\/[a-f0-9]{16}\/EasyPlayer-pro\.js)"/)?.[1];
    expect(script).toBeTruthy();
    const dir = script!.substring(0, script!.lastIndexOf("/"));
    files.forEach(name => {
      expect(readFileSync(join(root, "dist", dir, name))).toEqual(readFileSync(join(root, "public/easyplayer", name)));
    });
    // Old tabs must retain their fixed resource URLs during a release.
    expect(readFileSync(join(root, "dist/easyplayer/EasyPlayer-pro.js"), "utf8")).toBe("original-EasyPlayer-pro.js");
    return script;
  };
  const first = await compile();
  expect(await compile()).toBe(first);
  writeFileSync(join(root, "public/easyplayer/EasyPlayer-pro.wasm"), "updated-wasm");
  expect(await compile()).not.toBe(first);
  // 这个用例真的跑三次 vite build（每次都要起 rollup 打包），默认 5s 超时在 CI 上必挂。
}, 120_000);
