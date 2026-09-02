import { GridStack, type GridStackNode, type GridStackOptions } from "gridstack";

export interface DashboardGridGeometry {
  id: string;
  x: number;
  y: number;
  w: number;
  h: number;
}

export type DashboardGridEngine = Pick<GridStack, "enableMove" | "enableResize" | "destroy" | "on" | "off">;
export type DashboardGridFactory = (options: GridStackOptions, element: HTMLElement) => DashboardGridEngine;

export interface DashboardGridHandle {
  setEditing(editing: boolean): void;
  destroy(): void;
}

export interface DashboardGridCallbacks {
  onChange?(items: DashboardGridGeometry[]): void;
}

const defaultGridFactory: DashboardGridFactory = (options, host) => {
  const engine = GridStack.init(options, host);
  if (!engine) throw new Error("仪表盘网格初始化失败");
  return engine;
};

export function deriveDashboardColumns(width: number): 1 | 10 | 20 {
  if (width < 768) return 1;
  if (width < 1200) return 10;
  return 20;
}

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
      disableDrag: true,
      disableResize: true,
      float: false,
      margin: "18px 8px",
      minRow: 1
    },
    element
  );
  const changeHandler = (_event: Event, nodes: GridStackNode[]) => callbacks.onChange?.(normalizeGridChange(nodes));
  engine.on("change", changeHandler);

  return {
    setEditing(editing: boolean) {
      engine.enableMove(editing);
      engine.enableResize(editing);
    },
    destroy() {
      engine.off("change");
      engine.destroy(false);
    }
  };
}
