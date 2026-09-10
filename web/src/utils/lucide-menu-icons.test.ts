import { existsSync, readdirSync, readFileSync } from "node:fs";
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

  it("resolves the online user menu icon stored in the database", () => {
    expect(getLucideIconName("lucide:UsersRound")).toBe("UsersRound");
    expect(getLucideIconComponent("lucide:UsersRound")).toBeDefined();
  });

  it("resolves the work order menu icon stored in the database", () => {
    expect(getLucideIconName("lucide:ClipboardList")).toBe("ClipboardList");
    expect(getLucideIconComponent("lucide:ClipboardList")).toBeDefined();
  });

  it("resolves icon names returned in lowercase by legacy menu data", () => {
    expect(getLucideIconComponent("lucide:servercog")).toBeDefined();
    expect(getLucideIconComponent("lucide:gitbranch")).toBeDefined();
    expect(getLucideIconComponent("lucide:clapperboard")).toBeDefined();
  });

  /**
   * 菜单图标是数据库里的字符串，解析不到就**静默不渲染**——作业单就因此少了图标。
   * 这里把迁移与快照里写进 `sys_menu` 的每个图标名都过一遍白名单，
   * 让"新增菜单但忘了登记图标"在测试阶段就暴露。
   */
  it("resolves every menu icon that migrations and snapshots write into sys_menu", () => {
    const values = collectMenuIconValues();
    // 前端可能被单独检出（没有 server/），此时没有可比对的来源。
    if (values.length === 0) return;
    const unresolved = values.filter(value => !getLucideIconComponent(value));
    expect(unresolved).toEqual([]);
  });
});

/** 从迁移目录与三个累积快照里收集所有 `lucide:*` 取值。 */
function collectMenuIconValues(): string[] {
  const databaseDir = resolve(process.cwd(), "../server/resource/database");
  const files: string[] = [];
  const migrationsDir = resolve(databaseDir, "gb28181/migrations");
  if (existsSync(migrationsDir)) {
    for (const name of readdirSync(migrationsDir)) {
      if (name.endsWith(".sql")) files.push(resolve(migrationsDir, name));
    }
  }
  for (const name of ["uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"]) {
    const file = resolve(databaseDir, name);
    if (existsSync(file)) files.push(file);
  }
  const values = new Set<string>();
  for (const file of files) {
    for (const match of readFileSync(file, "utf8").matchAll(/lucide:[A-Za-z]+/g)) {
      values.add(match[0]);
    }
  }
  return [...values];
}
