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

  it("resolves icon names returned in lowercase by legacy menu data", () => {
    expect(getLucideIconComponent("lucide:servercog")).toBeDefined();
    expect(getLucideIconComponent("lucide:gitbranch")).toBeDefined();
    expect(getLucideIconComponent("lucide:clapperboard")).toBeDefined();
  });
});
