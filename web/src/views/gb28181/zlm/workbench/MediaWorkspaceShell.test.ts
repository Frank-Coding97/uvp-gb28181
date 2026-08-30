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
  it("renders header, scope and tabs in order while keeping inactive panels mounted", async () => {
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
        nodes: []
      },
      slots: {
        streams: "<div data-panel='streams'>streams</div>",
        sessions: "<div data-panel='sessions'>sessions</div>"
      }
    });

    const blocks = wrapper.findAll("[data-shell-block]").map(block => block.attributes("data-shell-block"));
    expect(blocks).toEqual(["header", "scope", "tabs", "panel"]);
    expect(wrapper.find("[data-panel='streams']").exists()).toBe(true);
    expect(wrapper.find("[data-panel='sessions']").exists()).toBe(true);
    expect(wrapper.get("[data-panel-view='streams']").attributes("aria-hidden")).toBe("false");
    expect(wrapper.get("[data-panel-view='sessions']").attributes("aria-hidden")).toBe("true");

    await wrapper.get("button[data-view='sessions']").trigger("click");
    expect(wrapper.emitted("update:activeView")?.[0]).toEqual(["sessions"]);
  });

  it("keeps a bounded full-height scroll chain at desktop and narrow widths", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/MediaWorkspaceShell.vue"), "utf8");
    expect(source).toMatch(/height:\s*100%/);
    expect(source).toMatch(/min-height:\s*0/);
    expect(source).toMatch(/overflow:\s*auto/);
    expect(source).toMatch(/@media\s*\(max-width:\s*768px\)/);
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
