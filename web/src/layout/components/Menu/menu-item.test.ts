import { mount } from "@vue/test-utils";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import MenuItem from "./menu-item.vue";

const WORKSPACE_PATHS = [
  "/media/overview",
  "/media/monitoring",
  "/media/ingress",
  "/media/recordings",
  "/media/nodes",
  "/media/scheduling"
];

const LEGACY_PATHS = [
  "/gb28181/zlm/overview",
  "/gb28181/zlm/runtime",
  "/gb28181/zlm/streams",
  "/gb28181/zlm/sessions",
  "/gb28181/zlm/proxies",
  "/gb28181/zlm/ffmpeg-sources",
  "/gb28181/zlm/rtp-servers",
  "/gb28181/cloud-recordings",
  "/gb28181/recording-schedules",
  "/gb28181/zlm/nodes",
  "/gb28181/zlm/config",
  "/gb28181/zlm/scheduler",
  "/gb28181/zlm/scheduler/logs"
];

function route(path: string, type = 2, hide = false, children?: Menu.MenuOptions[]): Menu.MenuOptions {
  return {
    id: path,
    parentId: "0",
    path,
    name: path,
    meta: { title: path, hide, disable: false, keepAlive: false, affix: false, roles: [], type },
    children
  };
}

const stubs = {
  "a-sub-menu": { template: "<nav class='sub-menu'><div class='sub-title'><slot name='title' /></div><slot /></nav>" },
  "a-menu-item": { template: "<button class='route-item'><slot name='icon' /><slot /></button>" },
  "a-menu-item-group": { template: "<section class='item-group'><h2><slot name='title' /></h2><slot /></section>" },
  MenuItemIcon: { template: "<i class='menu-icon' />" }
};

function mountMenu(children: Menu.MenuOptions[], parentPath = "/media") {
  return mount(MenuItem, {
    props: { routeTree: [route(parentPath, 1, false, children)] },
    global: {
      stubs,
      mocks: { $t: (key: string) => key }
    }
  });
}

describe("MenuItem media workspaces", () => {
  it("uses the ordinary recursive renderer without a media-only grouping branch", () => {
    const source = readFileSync(resolve(process.cwd(), "src/layout/components/Menu/menu-item.vue"), "utf8");

    expect(source).not.toContain("media-menu-sections");
    expect(source).not.toContain("a-menu-item-group");
    expect(source).not.toContain("uvp-media-menu-group");
  });

  it("renders six direct workspaces without visual groups or hidden legacy entries", () => {
    const children = [
      ...WORKSPACE_PATHS.map(path => route(path)),
      ...LEGACY_PATHS.map(path => route(path, 2, true)),
      route("/gb28181/zlm/nodes/:id", 2, true)
    ];
    const wrapper = mountMenu(children);

    expect(wrapper.findAll(".item-group")).toHaveLength(0);
    expect(wrapper.findAll(".sub-menu")).toHaveLength(1);
    expect(wrapper.findAll(".route-item").map(item => item.text())).toEqual(
      WORKSPACE_PATHS.map(path => `menu.${path}`)
    );
    for (const legacyPath of LEGACY_PATHS) {
      expect(wrapper.text()).not.toContain(`menu.${legacyPath}`);
    }
  });

  it("renders only workspaces present in the authorized route tree", () => {
    const wrapper = mountMenu([
      route("/media/monitoring"),
      route("/media/ingress"),
      route("/gb28181/zlm/streams", 2, true)
    ]);

    expect(wrapper.findAll(".item-group")).toHaveLength(0);
    expect(wrapper.findAll(".route-item").map(item => item.text())).toEqual([
      "menu./media/monitoring",
      "menu./media/ingress"
    ]);
  });

  it("leaves non-media route trees on the existing recursive renderer", () => {
    const wrapper = mountMenu(
      [route("/system/users"), route("/system/security", 1, false, [route("/system/security/audit")])],
      "/system"
    );

    expect(wrapper.findAll(".item-group")).toHaveLength(0);
    expect(wrapper.findAll(".route-item")).toHaveLength(2);
    expect(wrapper.findAll(".sub-menu")).toHaveLength(2);
  });
});
