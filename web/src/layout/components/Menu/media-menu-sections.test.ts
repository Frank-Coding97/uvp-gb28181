import { describe, expect, it } from "vitest";

import {
  MEDIA_MENU_PARENT_PATH,
  MEDIA_MENU_SECTIONS,
  groupMediaMenuItems
} from "./media-menu-sections";
import zhCN from "@/lang/modules/zhCN";
import enUS from "@/lang/modules/enUS";

function menuItem(path: string, options: { hide?: boolean; type?: number } = {}): Menu.MenuOptions {
  return {
    id: path,
    parentId: MEDIA_MENU_PARENT_PATH,
    path,
    name: path,
    meta: {
      title: path,
      hide: options.hide ?? false,
      disable: false,
      keepAlive: false,
      affix: false,
      roles: [],
      type: options.type ?? 2
    }
  };
}

describe("media menu visual sections", () => {
  it("orders all 13 real routes into four stable visual groups and keeps unknown routes last", () => {
    const stablePaths = MEDIA_MENU_SECTIONS.flatMap(section => section.paths);
    expect(stablePaths).toHaveLength(13);
    expect(new Set(stablePaths).size).toBe(13);
    const input = [menuItem("/gb28181/zlm/custom"), ...stablePaths.toReversed().map(path => menuItem(path))];
    const before = input.map(item => item.path);

    const result = groupMediaMenuItems(input);

    expect(result.sections.map(section => section.key)).toEqual(["cluster", "monitoring", "ingress", "capabilities"]);
    expect(result.sections.flatMap(section => section.items.map(item => item.path))).toEqual(stablePaths);
    expect(result.ungrouped.map(item => item.path)).toEqual(["/gb28181/zlm/custom"]);
    expect(input.map(item => item.path)).toEqual(before);
  });

  it("drops hidden entries from presentation and omits empty headings", () => {
    const result = groupMediaMenuItems([
      menuItem("/gb28181/zlm/overview", { hide: true }),
      menuItem("/gb28181/zlm/streams"),
      menuItem("/gb28181/zlm/custom", { hide: true })
    ]);

    expect(result.sections.map(section => section.key)).toEqual(["monitoring"]);
    expect(result.sections[0].items.map(item => item.path)).toEqual(["/gb28181/zlm/streams"]);
    expect(result.ungrouped).toEqual([]);
  });

  it("does not hide an unknown visible direct child, including a future directory", () => {
    const unknownPage = menuItem("/gb28181/zlm/future");
    const unknownDirectory = menuItem("/gb28181/zlm/future-tools", { type: 1 });
    const result = groupMediaMenuItems([unknownPage, unknownDirectory]);

    expect(result.sections).toEqual([]);
    expect(result.ungrouped).toEqual([unknownPage, unknownDirectory]);
  });

  it("provides concise Chinese and English labels for every visual heading", () => {
    const titleKeys = MEDIA_MENU_SECTIONS.map(section => section.titleKey);

    expect(titleKeys.map(key => zhCN.menu[key])).toEqual(["集群", "监控", "接入", "能力"]);
    expect(titleKeys.map(key => enUS.menu[key])).toEqual(["Cluster", "Monitoring", "Ingress", "Capabilities"]);
  });
});
