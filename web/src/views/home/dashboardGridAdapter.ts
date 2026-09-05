import { GridStack, type GridStackNode, type GridStackOptions } from "gridstack";

export interface DashboardGridGeometry {
  id: string;
  x: number;
  y: number;
  w: number;
  h: number;
}

export type DashboardGridEngine = Pick<GridStack, "enableMove" | "enableResize" | "destroy" | "on" | "off" | "getColumn" | "isIgnoreChangeCB">;
export type DashboardGridFactory = (options: GridStackOptions, element: HTMLElement) => DashboardGridEngine;

export interface DashboardGridHandle {
  setEditing(editing: boolean): void;
  destroy(): void;
}

export interface DashboardGridCallbacks {
  onChange?(items: DashboardGridGeometry[]): void;
}

const defaultGridFactory: DashboardGridFactory = (options, host) => {
  // Cache the saved 20-column geometry before applying a smaller viewport.
  const { columnOpts, ...desktopOptions } = options;
  const engine = GridStack.init(desktopOptions, host);
  if (!engine) throw new Error("仪表盘网格初始化失败");
  engine.updateOptions({ columnOpts });
  return engine;
};

export function normalizeGridChange(nodes: Array<Pick<GridStackNode, "id" | "x" | "y" | "w" | "h">>): DashboardGridGeometry[] {
  return nodes.flatMap(node => {
    if (!node.id) return [];
    return [{ id: node.id, x: node.x ?? 0, y: node.y ?? 0, w: node.w ?? 1, h: node.h ?? 1 }];
  });
}

export function createDashboardGrid(
  element: HTMLElement,
  factory: DashboardGridFactory = defaultGridFactory,
  callbacks: DashboardGridCallbacks = {}
): DashboardGridHandle {
  const engine = factory(
    {
      column: 20,
      columnOpts: { columnMax: 20, breakpointForWindow: true, layout: "move", breakpoints: [{ w: 1279, c: 8, layout: "list" }, { w: 767, c: 1 }] },
      cellHeight: 80,
      disableDrag: true,
      disableResize: true,
      float: false,
      margin: "10px 8px",
      minRow: 1
    },
    element
  );
  let editing = false;
  const syncEditing = () => {
    const canDrag = editing && engine.getColumn() === 20;
    engine.enableMove(canDrag);
    engine.enableResize(canDrag);
  };
  const changeHandler = (_event: Event, nodes: GridStackNode[]) => {
    syncEditing();
    // Responsive coordinates are presentation only; never save them over the desktop layout.
    if (editing && engine.getColumn() === 20 && !engine.isIgnoreChangeCB()) callbacks.onChange?.(normalizeGridChange(nodes));
  };
  engine.on("change", changeHandler);
  engine.on("resizecontent", syncEditing);

  return {
    setEditing(value: boolean) {
      editing = value;
      syncEditing();
    },
    destroy() {
      engine.off("change");
      engine.off("resizecontent");
      engine.destroy(false);
    }
  };
}
