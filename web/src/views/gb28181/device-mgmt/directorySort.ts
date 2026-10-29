import type { DirectoryNode } from "./api";

/**
 * 「在线设备排最上边」排序（老板 2026-10-29 要求）。
 *
 * ⛔ 三条约束，少一条树就会"自己抖"：
 *  1. 只调**同级兄弟**的顺序：父子层级、展开状态、选中项一概不动 —— 排序是纯展示变换，
 *     不写回 `trees`，所以后台增量同步（mergeDirectorySync）仍然按后端顺序做 diff。
 *  2. 只重排**设备叶子**（带 `node.device` 的节点）：在线在前、离线在后；
 *     同一在线状态内保持后端返回的原始顺序（显式按下标兜底，不依赖 sort 的稳定性）。
 *  3. 目录节点（区域 / 部门 / 分组 / 未分组）位置**一律不动**：后端顺序就是行政区划编码、
 *     部门树、业务分组的业务顺序，按「目录下面几个在线」重排会让每次刷新都换位置，比不排更糟。
 *
 * 同层既有子目录又有直属设备时，设备的**槽位**不变（还是那几个位置），只是槽位里装哪台设备变了。
 * 顺序没变的子树一律返回原引用，不制造无谓重渲染。
 */
export function sortDevicesOnlineFirst(nodes: DirectoryNode[]): DirectoryNode[] {
  const ordered = nodes.map(sortChildren);

  const deviceSlots = ordered.filter(node => Boolean(node.device));
  if (deviceSlots.length < 2) return sameOrder(ordered, nodes) ? nodes : ordered;

  const onlineFirst = deviceSlots
    .map((node, index) => ({ node, index }))
    .sort((left, right) => {
      const onlineDelta = Number(Boolean(right.node.device?.online)) - Number(Boolean(left.node.device?.online));
      return onlineDelta !== 0 ? onlineDelta : left.index - right.index;
    })
    .map(item => item.node);

  let cursor = 0;
  const result = ordered.map(node => (node.device ? onlineFirst[cursor++] : node));
  return sameOrder(result, nodes) ? nodes : result;
}

function sortChildren(node: DirectoryNode): DirectoryNode {
  if (!node.children?.length) return node;
  const children = sortDevicesOnlineFirst(node.children);
  return children === node.children ? node : { ...node, children };
}

/** 逐项比引用：顺序和成员都没变就复用原数组，避免下游 computed 白白失效。 */
function sameOrder(next: DirectoryNode[], previous: DirectoryNode[]): boolean {
  return next.length === previous.length && next.every((node, index) => node === previous[index]);
}
