import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import LoginArchitecture from "./LoginArchitecture.vue";

describe("login architecture diagram", () => {
  it("explains signal control, direct media delivery and optional cache accessibly", () => {
    const wrapper = mount(LoginArchitecture);
    const svg = wrapper.get('svg[role="img"]');
    const labels = svg
      .attributes("aria-labelledby")
      .split(" ")
      .map(id => wrapper.get(`#${id}`).text())
      .join(" ");
    expect(labels).toContain("信令控制与媒体传输分离");
    expect(labels).toContain("RTP/PS 视频流直接送达 ZLMediaKit");
    for (const label of ["摄像机", "NVR", "下级平台", "UVP GB28181", "数据库", "Redis / Memory"]) {
      expect(svg.text()).toContain(label);
    }
  });

  it("resolves every icon and animation reference to a definition in the diagram", () => {
    const wrapper = mount(LoginArchitecture);
    const references = wrapper.findAll("use, mpath");
    expect(references.length).toBeGreaterThan(0);
    for (const reference of references) {
      expect(wrapper.find(reference.attributes("href")).exists()).toBe(true);
    }
    expect(wrapper.get("image").attributes("href")).toContain("uvp-mark.png");
  });

  it("keeps media one-way and control/data two-way on continuously looping routes", () => {
    const wrapper = mount(LoginArchitecture);
    const particles = wrapper.findAll(".particle");
    expect(particles.length).toBeGreaterThan(0);
    const directions = new Map<string, Set<string>>();
    for (const particle of particles) {
      const motion = particle.get("animateMotion");
      expect(motion.attributes("repeatCount")).toBe("indefinite");
      const route = motion.get("mpath").attributes("href");
      const direction = motion.attributes("keyPoints") === "1;0" ? "reverse" : "forward";
      const seen = directions.get(route) ?? new Set<string>();
      seen.add(direction);
      directions.set(route, seen);
      if (particle.classes().includes("media")) {
        expect(direction).toBe("forward");
      }
    }
    for (const particle of particles) {
      const route = particle.get("mpath").attributes("href");
      expect(directions.get(route)?.size).toBe(particle.classes().includes("media") ? 1 : 2);
    }
  });
});
