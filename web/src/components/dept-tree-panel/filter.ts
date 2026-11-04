import type { DivisionItem } from "@/api/department";

/**
 * 部门树关键字过滤（纯函数，不改入参）。
 *
 * 三条规则，缺一条都会让搜索结果「看着像坏的」：
 *   · 命中节点自己保留；
 *   · 命中节点的**父链必须一起保留**，否则匹配项挂不到树上 = 搜不到；
 *   · 命中节点下的**无关分支裁掉**，否则一搜就回到整棵树。
 */
export const filterDeptTree = (nodes: DivisionItem[], keyword: string): DivisionItem[] => {
  const kw = keyword.trim();
  if (!kw) return nodes;
  return nodes
    .map(node => ({
      ...node,
      children: node.children?.length ? filterDeptTree(node.children, kw) : []
    }))
    .filter(node => node.name.includes(kw) || (node.children?.length ?? 0) > 0);
};
