import { GridStack, type GridStackNode, type GridStackOptions } from "gridstack";

export interface DashboardGridGeometry {
  id: string;
  x: number;
  y: number;
  w: number;
  h: number;
}

export type DashboardGridEngine = Pick<GridStack, "enableMove" | "enableResize" | "destroy">;
export type DashboardGridFactory = (options: GridStackOptions, element: HTMLElement) => DashboardGridEngine;

export interface DashboardGridHandle {
  setEditing(editing: boolean): void;
  destroy(): void;
}

const defaultGridFactory: DashboardGridFactory = (options, host) => {
  const engine = GridStack.init(options, host);
  if (!engine) throw new Error("仪表盘网格初始化失败");
  return engine;
};

export function deriveDashboardColumns(width: number): 1 | 6 | 12 {
  if (width < 768) return 1;
  if (width < 1200) return 6;
  return 12;
}

export function normalizeGridChange(nodes: Array<Pick<GridStackNode, "id" | "x" | "y" | "w" | "h">>): DashboardGridGeometry[] {
  return nodes.flatMap(node => {
    if (!node.id) return [];
    return [{ id: node.id, x: node.x ?? 0, y: node.y ?? 0, w: node.w ?? 1, h: node.h ?? 1 }];
  });
}

export function createDashboardGrid(
  element: HTMLElement,
  factory: DashboardGridFactory = defaultGridFactory
): DashboardGridHandle {
  const engine = factory(
    {
      column: 12,
      disableDrag: true,
      disableResize: true,
      float: false,
      margin: 12,
      minRow: 1
    },
    element
  );

  return {
    setEditing(editing: boolean) {
      engine.enableMove(editing);
      engine.enableResize(editing);
    },
    destroy() {
      engine.destroy(false);
    }
  };
}
