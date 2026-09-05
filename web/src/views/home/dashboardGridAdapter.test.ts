import { afterEach, describe, expect, it, vi } from "vitest";
import { GridStack } from "gridstack";
import {
  createDashboardGrid,
  normalizeGridChange,
  type DashboardGridEngine
} from "./dashboardGridAdapter";

describe("dashboard grid adapter", () => {
  afterEach(() => {
    document.body.innerHTML = "";
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it.each([390, 767, 768, 1024, 1200, 1279, 1280, 1440])("reflows at %ipx without overwriting desktop geometry", (width) => {
    vi.stubGlobal("innerWidth", width);
    const host = document.createElement("div");
    host.className = "grid-stack";
    host.setAttribute("gs-animate", "false");
    host.innerHTML = [0, 4, 8, 12, 16].map((x, i) => `
      <div class="grid-stack-item" gs-id="card-${i}" gs-x="${x}" gs-y="0" gs-w="4" gs-h="2" gs-min-w="3">
        <div class="grid-stack-item-content">card ${i}</div>
      </div>`).join("");
    document.body.append(host);
    const init = vi.spyOn(GridStack, "init");
    const onChange = vi.fn();
    const handle = createDashboardGrid(host, undefined, { onChange });
    const engine = init.mock.results[0].value as GridStack;
    const geometry = () => engine.engine.nodes.map(n => ({ id: n.id, x: n.x, y: n.y, w: n.w, h: n.h })).sort((a, b) => a.id!.localeCompare(b.id!));
    const desktop = [0, 4, 8, 12, 16].map((x, i) => ({ id: `card-${i}`, x, y: 0, w: 4, h: 2 }));
    expect(engine.getColumn()).toBe(width < 768 ? 1 : width < 1280 ? 8 : 20);
    handle.setEditing(true);
    for (const nextWidth of [390, 1024, 1440, 390, 1440]) {
      vi.stubGlobal("innerWidth", nextWidth);
      engine.onResize(nextWidth);
      expect(engine.getColumn()).toBe(nextWidth < 768 ? 1 : nextWidth < 1280 ? 8 : 20);
      expect(!!engine.opts.disableDrag).toBe(nextWidth < 1280);
      expect(!!engine.opts.disableResize).toBe(nextWidth < 1280);
      if (nextWidth === 1024) expect(geometry().slice(0, 2).map(n => ({ x: n.x, y: n.y, w: n.w }))).toEqual([{ x: 0, y: 0, w: 4 }, { x: 4, y: 0, w: 4 }]);
      if (nextWidth === 390) expect(geometry().every(n => n.x === 0 && n.w === 1)).toBe(true);
      if (nextWidth === 1440) expect(geometry()).toEqual(desktop);
    }
    expect(onChange).not.toHaveBeenCalled();
    engine.update(host.firstElementChild as HTMLElement, { h: 3 });
    expect(onChange).toHaveBeenCalledWith(expect.arrayContaining([expect.objectContaining({ id: "card-0", h: 3 })]));
    handle.destroy();
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
    const engine = { enableMove, enableResize, destroy, on, off, getColumn: () => 20, isIgnoreChangeCB: () => false } as unknown as DashboardGridEngine;
    const factory = vi.fn(() => engine);

    const grid = createDashboardGrid(host, factory);

    expect(factory).toHaveBeenCalledWith(
      expect.objectContaining({ column: 20, cellHeight: 80, disableDrag: true, disableResize: true, margin: "10px 8px" }),
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
    host.setAttribute("gs-animate", "false");
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
