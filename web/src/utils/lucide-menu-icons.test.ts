import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { getLucideIconComponent, getLucideIconName } from "./lucide-menu-icons";

describe("lucide menu icons", () => {
  it("resolves the cascade menu icon stored in the database", () => {
    expect(getLucideIconName("lucide:GitBranch")).toBe("GitBranch");
    expect(getLucideIconComponent("lucide:GitBranch")).toBeDefined();
  });

  it("resolves the GB service configuration icon stored in the database", () => {
    expect(getLucideIconName("lucide:ServerCog")).toBe("ServerCog");
    expect(getLucideIconComponent("lucide:ServerCog")).toBeDefined();
  });

  it("resolves the media management icon stored in the database", () => {
    expect(getLucideIconName("lucide:Clapperboard")).toBe("Clapperboard");
    expect(getLucideIconComponent("lucide:Clapperboard")).toBeDefined();
  });

  it("resolves the dashboard icon stored in the database", () => {
    expect(getLucideIconName("lucide:Gauge")).toBe("Gauge");
    expect(getLucideIconComponent("lucide:Gauge")).toBeDefined();
  });

  it("resolves the media overview icon stored in the database", () => {
    expect(getLucideIconName("lucide:LayoutDashboard")).toBe("LayoutDashboard");
    expect(getLucideIconComponent("lucide:LayoutDashboard")).toBeDefined();
  });

  it("resolves the online user menu icon stored in the database", () => {
    expect(getLucideIconName("lucide:UsersRound")).toBe("UsersRound");
    expect(getLucideIconComponent("lucide:UsersRound")).toBeDefined();
  });

  it("resolves the snapshot library icon stored in the database", () => {
    expect(getLucideIconName("lucide:Images")).toBe("Images");
    expect(getLucideIconComponent("lucide:Images")).toBeDefined();
  });

  it("resolves the runtime log menu icon stored in the database", () => {
    expect(getLucideIconName("lucide:Terminal")).toBe("Terminal");
    expect(getLucideIconComponent("lucide:Terminal")).toBeDefined();
  });

  it("resolves the record cache menu icon stored in the database", () => {
    // 录像缓存菜单（sys_menu 140610）在库里写的就是 `lucide:HardDriveDownload`。
    // ⛔ 2026-10-04 的线上表现是"菜单没有图标"：不是数据没写，而是白名单里没有这一项，
    //    `getLucideIconComponent` 返回 undefined，`menu-item-icon.vue` 三个分支全落空 ⇒ 什么都不渲染。
    expect(getLucideIconName("lucide:HardDriveDownload")).toBe("HardDriveDownload");
    expect(getLucideIconComponent("lucide:HardDriveDownload")).toBeDefined();
  });

  it("resolves icon names returned in lowercase by legacy menu data", () => {
    expect(getLucideIconComponent("lucide:servercog")).toBeDefined();
    expect(getLucideIconComponent("lucide:gitbranch")).toBeDefined();
    expect(getLucideIconComponent("lucide:clapperboard")).toBeDefined();
  });
});

/**
 * ⛔⛔ 菜单图标白名单覆盖率护栏（2026-10-04）。
 *
 * 症状：侧边栏某个菜单**没有图标**，而库里 `sys_menu.icon` 明明是 `lucide:Xxx`。
 *
 * 根因：图标名是**DB 数据**与**前端白名单**两处各自维护的。DB 里写了什么名字，
 * 前端 `lucideMenuIcons` 里就必须有一项 —— 少一项不会报错、不会告警，
 * 只是 `getLucideIconComponent()` 返回 undefined，图标静默消失。
 * 「录像缓存」菜单就是这么丢的（库里是 `lucide:HardDriveDownload`，白名单没收录）。
 *
 * 修法有两半，缺一不可：
 *   1. 把缺失的图标补进白名单；
 *   2. 用这条用例把「DB 里用到的每个 lucide 图标都能解析」钉死 ——
 *      否则下次新增菜单还会以同样的方式静默丢图标。
 *
 * 扫描范围 = 三方言全量建库脚本 + 增量迁移（菜单数据只可能出现在这两处）。
 */
describe("lucide menu icons：菜单数据覆盖率", () => {
  const DATABASE_DIR = resolve(process.cwd(), "..", "server", "resource", "database");
  const SOURCES = [
    "uvp-gb28181.sql",
    "postgresql_converted.sql",
    "sqlserver_converted.sql",
    "gb28181/migrations/2026-10-02-firmware-repository-menu.sql",
    "gb28181/migrations/2026-10-03-record-cache.sql"
  ];

  function collectIcons(): Set<string> {
    const names = new Set<string>();
    for (const relative of SOURCES) {
      const absolute = resolve(DATABASE_DIR, relative);
      expect(existsSync(absolute), `菜单数据源不存在：${relative}（路径变了就更新 SOURCES）`).toBe(true);
      for (const matched of readFileSync(absolute, "utf8").matchAll(/lucide:([A-Za-z0-9]+)/g)) {
        names.add(matched[1]);
      }
    }
    return names;
  }

  it("建库脚本里出现的每一个 lucide 图标都能被前端解析", () => {
    const icons = collectIcons();
    // 防止扫描逻辑失效（正则写错 / 文件被改名）后静默通过。
    expect(icons.size).toBeGreaterThanOrEqual(30);
    const unresolved = [...icons].filter(name => !getLucideIconComponent(`lucide:${name}`)).sort();
    expect(unresolved).toEqual([]);
  });
});
