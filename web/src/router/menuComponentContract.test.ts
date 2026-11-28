import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * 菜单种子里写的 `component` 必须真的存在，否则线上表现是：
 * 点菜单 → `Component "default" ... is not a valid component` → 「路由错误 Invalid route component」，
 * 整页白屏；而单测、lint、类型检查全都是绿的（component 是数据库里的字符串，编译器看不见）。
 *
 * ⛔ 踩过（2026-11-21）：把设备录像页从 `recording-center/device/index` 挪回
 *    `gb28181/device-record-playback/index` 并删了旧目录，种子里只改了其中一处引用，
 *    开发库又是早就跑过迁移的老数据 ⇒ 菜单点进去直接白屏。
 *
 * ⛔ 这里只校验 `views/` 下的组件：`plugins/*` 是插件运行时目录，本仓没有源码也正常
 *    （140248 `/plugins/example` 就是这种，长期如此）。
 */
const seeds = resolve(process.cwd(), "../server/resource/database/baseline/seeds/sys_menu.jsonl");

interface MenuSeed {
  id: number;
  path: string;
  title: string;
  component: string;
  deleted_at: string | null;
}

function readMenus(): MenuSeed[] {
  return readFileSync(seeds, "utf8")
    .split("\n")
    .filter(line => line.trim().length > 0)
    .map(line => JSON.parse(line) as MenuSeed);
}

describe("menu component contract", () => {
  it("keeps every seeded menu component resolvable under src/views", () => {
    const broken = readMenus()
      .filter(menu => !menu.deleted_at && menu.component && !menu.component.startsWith("plugins/"))
      .filter(menu => {
        try {
          readFileSync(resolve(process.cwd(), `src/views/${menu.component}.vue`));
          return false;
        } catch {
          return true;
        }
      })
      .map(menu => `${menu.id} ${menu.path} → ${menu.component}`);

    expect(broken).toEqual([]);
  });

  it("keeps the 设备录像 menu and the legacy hidden route on the same component", () => {
    const menus = readMenus();
    const menu = menus.find(item => item.path === "/recording-center/device");
    const legacy = menus.find(item => item.path === "/gb28181/device-record-playback/:channelId");

    expect(menu?.component).toBe("gb28181/device-record-playback/index");
    // 两条都指向同一份实现：设备列表点「录像」优先落到上面那条菜单路由（?channel=id），
    // 只有菜单没授权时才回落到隐藏的旧路由（老书签、只授权了旧菜单的角色）。
    expect(legacy?.component).toBe(menu?.component);
  });
});
