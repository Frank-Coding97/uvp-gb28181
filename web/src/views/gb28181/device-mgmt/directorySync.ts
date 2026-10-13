import type { DirectoryNode } from "./api";

export interface DirectorySyncResult {
  revision: string;
  reset: boolean;
  list?: DirectoryNode[];
  roots: string[];
  upserts: Array<{ node: DirectoryNode; children: string[] }>;
  removed: string[];
}

export function mergeDirectorySync(tree: DirectoryNode[], result: DirectorySyncResult): DirectoryNode[] {
  if (result.reset) return result.list || [];
  if (
    !result.upserts.length &&
    !result.removed.length &&
    result.roots.length === tree.length &&
    result.roots.every((key, i) => tree[i].key === key)
  )
    return tree;
  const nodes = new Map<string, DirectoryNode>();
  const index = (items: DirectoryNode[]) =>
    items.forEach(item => {
      nodes.set(item.key, item);
      index(item.children || []);
    });
  index(tree);
  result.removed.forEach(key => nodes.delete(key));
  const changed = new Map(result.upserts.map(item => [item.node.key, item]));
  const build = (key: string): DirectoryNode => {
    const patch = changed.get(key);
    const old = nodes.get(key);
    const base = patch?.node || old;
    if (!base) throw new Error("目录增量基线不完整");
    const children = (patch?.children || old?.children?.map(child => child.key) || []).map(build);
    if (!patch && children.length === (old?.children?.length || 0) && children.every((child, i) => child === old?.children?.[i]))
      return base;
    return { ...base, children };
  };
  return result.roots.map(build);
}
