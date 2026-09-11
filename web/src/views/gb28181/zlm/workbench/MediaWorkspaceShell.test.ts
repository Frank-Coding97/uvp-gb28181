import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import MediaWorkspaceShell from "./MediaWorkspaceShell.vue";

const pages = [
  "MediaOverview.vue",
  "MediaMonitoring.vue",
  "IngressManagement.vue",
  "RecordingCenter.vue",
  "NodeManagement.vue",
  "SchedulingManagement.vue"
];

describe("MediaWorkspaceShell", () => {
  it("keeps primary navigation in the system sidebar and only renders page controls", async () => {
    const wrapper = mount(MediaWorkspaceShell, {
      props: {
        title: "媒体监控",
        description: "流和会话运行态",
        views: [
          { key: "streams", label: "流媒体" },
          { key: "sessions", label: "会话" }
        ],
        activeView: "streams",
        scope: "all",
        nodes: [],
        showToolbarActions: true
      },
      slots: {
        streams: "<div data-panel='streams'>streams</div>",
        sessions: "<div data-panel='sessions'>sessions</div>"
      }
    });

    const blocks = wrapper.findAll("[data-shell-block]").map(block => block.attributes("data-shell-block"));
    expect(blocks).toEqual(["scope", "tabs", "panel"]);
    expect(wrapper.find("aside[aria-label='流媒体管理导航']").exists()).toBe(false);
    expect(wrapper.find(".media-workbench-header").exists()).toBe(false);
    expect(wrapper.find("[data-panel='streams']").exists()).toBe(true);
    expect(wrapper.find("[data-panel='sessions']").exists()).toBe(true);
    expect(wrapper.get("[data-panel-view='streams']").attributes("aria-hidden")).toBe("false");
    expect(wrapper.get("[data-panel-view='sessions']").attributes("aria-hidden")).toBe("true");

    await wrapper.get("button[data-view='sessions']").trigger("click");
    expect(wrapper.emitted("update:activeView")?.[0]).toEqual(["sessions"]);

    await wrapper.get("button[aria-label='关闭媒体监控自动刷新']").trigger("click");
    await wrapper.get("button[aria-label='刷新媒体监控']").trigger("click");
    expect(wrapper.emitted("update:autoRefresh")?.[0]).toEqual([false]);
    expect(wrapper.emitted("refresh")).toHaveLength(1);
  });

  it("keeps only the node selector by default on always-live media pages", () => {
    const wrapper = mount(MediaWorkspaceShell, {
      props: {
        title: "运行总览",
        description: "节点实时运行态",
        views: [{ key: "overview", label: "运行态" }],
        activeView: "overview",
        scope: 2,
        nodes: [{ id: 2, name: "zlm-220", state: "active" }]
      }
    });

    expect(wrapper.get("[aria-label='节点范围']").attributes("model-value")).toBe("2");
    expect(wrapper.find("button[aria-label='刷新节点目录']").exists()).toBe(false);
    expect(wrapper.find("button[aria-label='关闭运行总览自动刷新']").exists()).toBe(false);
    expect(wrapper.find("button[aria-label='刷新运行总览']").exists()).toBe(false);
  });

  it("uses a borderless tray-style active indicator for workspace views", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/MediaWorkspaceShell.vue"), "utf8");
    expect(source).toMatch(/\.media-workspace-shell__tabs\s*\{[^}]*background:\s*transparent;[^}]*border:\s*0;/s);
    expect(source).toContain(".media-workspace-shell__tabs button.is-active::after");
    expect(source).not.toMatch(/button\.is-active\s*\{[^}]*background:\s*var\(--zlm-brand-50\)/s);
  });

  it("keeps a bounded content-only layout that adapts to the available page width", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/MediaWorkspaceShell.vue"), "utf8");
    expect(source).toMatch(/height:\s*100%/);
    expect(source).toMatch(/min-height:\s*0/);
    expect(source).toMatch(/overflow:\s*auto/);
    expect(source).toContain("padding: calc(var(--uvp-main-padding) + 4px) calc(var(--uvp-main-padding) + 8px) var(--uvp-workspace-gap)");
    expect(source).toMatch(/container-type:\s*inline-size/);
    expect(source).toMatch(/@container\s+media-workspace-content\s*\(max-width:\s*720px\)/);
    expect(source).not.toContain("media-workspace-shell__sidebar");
    expect(source).not.toContain("MediaWorkspaceTabs");
    expect(source).not.toContain("MediaWorkbenchHeader");
    expect(source).toContain("media-workspace-shell__controls");
    expect(source).toMatch(/showToolbarActions:\s*false/);
  });

  it("keeps all six canonical route components on the shared shell contract", () => {
    for (const page of pages) {
      const source = readFileSync(resolve(process.cwd(), `src/views/gb28181/zlm/workbench/${page}`), "utf8");
      expect(source, page).toContain("MediaWorkspaceShell");
      expect(source, page).toContain(":active-view");
      expect(source, page).not.toMatch(/getZLM|listZLM|createZLM|updateZLM|deleteZLM/);
    }
  });
});
