import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createDashboardGrid,
  deriveDashboardColumns,
  normalizeGridChange,
  type DashboardGridEngine
} from "./dashboardGridAdapter";

describe("dashboard grid adapter", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("derives deterministic desktop, tablet and mobile columns", () => {
    expect(deriveDashboardColumns(1440)).toBe(20);
    expect(deriveDashboardColumns(1024)).toBe(10);
    expect(deriveDashboardColumns(390)).toBe(1);
  });

  it("normalizes geometry without persisting pixels", () => {
    expect(normalizeGridChange([{ id: "sip-rpm", x: 2, y: 1, w: 3, h: 2 }])).toEqual([
      { id: "sip-rpm", x: 2, y: 1, w: 3, h: 2 }
    ]);
  });

  it("starts read-only, toggles edit mode and destroys the engine", () => {
    const host = document.createElement("div");
    document.body.append(host);
    const enableMove = vi.fn();
    const enableResize = vi.fn();
    const destroy = vi.fn();
    const on = vi.fn();
    const off = vi.fn();
    const engine = { enableMove, enableResize, destroy, on, off } as unknown as DashboardGridEngine;
    const factory = vi.fn(() => engine);

    const grid = createDashboardGrid(host, factory);

    expect(factory).toHaveBeenCalledWith(
      expect.objectContaining({ column: 20, disableDrag: true, disableResize: true, margin: "18px 8px" }),
      host
    );
    grid.setEditing(true);
    expect(enableMove).toHaveBeenCalledWith(true);
    expect(enableResize).toHaveBeenCalledWith(true);
    grid.setEditing(false);
    expect(enableMove).toHaveBeenLastCalledWith(false);
    expect(enableResize).toHaveBeenLastCalledWith(false);
    grid.destroy();
    expect(off).toHaveBeenCalledWith("change");
    expect(destroy).toHaveBeenCalledWith(false);
  });

  it("initializes and tears down the real GridStack engine", () => {
    const host = document.createElement("div");
    host.className = "grid-stack";
    host.innerHTML = `
      <div class="grid-stack-item" gs-id="one" gs-x="0" gs-y="0" gs-w="3" gs-h="2">
        <div class="grid-stack-item-content">one</div>
      </div>
      <div class="grid-stack-item" gs-id="two" gs-x="3" gs-y="0" gs-w="3" gs-h="2">
        <div class="grid-stack-item-content">two</div>
      </div>`;
    document.body.append(host);

    const grid = createDashboardGrid(host);
    grid.setEditing(true);
    grid.setEditing(false);
    grid.destroy();

    expect(host.isConnected).toBe(true);
  });
});
