import type { BusinessBinding, BusinessNode, CascadeChannelProjection } from "@/api/gb28181";

const GB_ID = /^\d{20}$/;
const MAX_NODE_NAME_LENGTH = 128;

export function createBusinessNodeCode(
  localDeviceId: string,
  kind: BusinessNode["kind"],
  nodes: BusinessNode[],
  reservedCodes: Iterable<string> = []
) {
  if (!GB_ID.test(localDeviceId)) return "";
  const type = kind === "group" ? "215" : "216";
  const identityDigit = localDeviceId[13];
  const prefix = `${localDeviceId.slice(0, 10)}${type}${identityDigit}`;
  const used = [...nodes.map(node => node.code), ...reservedCodes]
    .filter(code => code.startsWith(prefix) && /^\d{6}$/.test(code.slice(14)))
    .map(code => Number(code.slice(14)));
  const sequence = Math.max(0, ...used) + 1;
  return sequence <= 999999 ? `${prefix}${String(sequence).padStart(6, "0")}` : "";
}

export function businessNodePath(nodes: BusinessNode[], code: string): BusinessNode[] {
  const nodesByCode = new Map(nodes.map(node => [node.code, node]));
  const path: BusinessNode[] = [];
  const visited = new Set<string>();
  let current = nodesByCode.get(code);
  while (current && !visited.has(current.code)) {
    visited.add(current.code);
    path.unshift(current);
    if (!current.parentCode) break;
    current = nodesByCode.get(current.parentCode);
  }
  return path;
}

export function businessNodeLabel(nodes: BusinessNode[], node: BusinessNode): string {
  const hasSiblingWithSameName = nodes.some(
    candidate => candidate.code !== node.code && candidate.parentCode === node.parentCode && candidate.name === node.name
  );
  return hasSiblingWithSameName ? `${node.name} · ${node.code}` : node.name;
}

export function formatBusinessNodePath(nodes: BusinessNode[], code: string): string {
  return businessNodePath(nodes, code)
    .map(node => businessNodeLabel(nodes, node))
    .filter(Boolean)
    .join(" / ");
}

export function searchBusinessNodeCodes(nodes: BusinessNode[], query: string): Set<string> {
  const normalizedQuery = query.trim().toLocaleLowerCase();
  if (!normalizedQuery) return new Set(nodes.map(node => node.code));

  const matches = new Set<string>();
  for (const node of nodes) {
    if (!`${node.name}\n${node.code}`.toLocaleLowerCase().includes(normalizedQuery)) continue;
    businessNodePath(nodes, node.code).forEach(ancestor => matches.add(ancestor.code));
  }
  return matches;
}

export function descendantCodes(nodes: BusinessNode[], code: string): Set<string> {
  const result = new Set<string>([code]);
  let changed = true;
  while (changed) {
    changed = false;
    nodes.forEach(node => {
      if (result.has(node.parentCode) && !result.has(node.code)) {
        result.add(node.code);
        changed = true;
      }
    });
  }
  return result;
}

export function businessNodeErrors(
  nodes: BusinessNode[],
  bindings: BusinessBinding[],
  channels: CascadeChannelProjection[],
  reservedCodes: Iterable<string> = []
): string[] {
  const errors: string[] = [];
  const roots = nodes.filter(node => node.kind === "group");
  if (nodes.length && roots.length !== 1) errors.push("业务分组必须且只能有一个根节点");
  const codes = new Set<string>();
  const reserved = new Set(reservedCodes);
  const organizations = new Set(nodes.filter(node => node.kind === "organization").map(node => node.code));
  nodes.forEach(node => {
    const expectedType = node.kind === "group" ? "215" : "216";
    if (!GB_ID.test(node.code) || node.code.slice(10, 13) !== expectedType) {
      errors.push("节点国标编号必须为 20 位数字，且第 11–13 位类型码与节点一致（根 215、组织 216）");
    }
    if (codes.has(node.code)) errors.push("节点国标编号重复");
    if (reserved.has(node.code)) errors.push("节点国标编号与本平台、上级或已共享资源编号冲突");
    codes.add(node.code);
    if (!node.name.trim()) errors.push("节点名称不能为空");
    if (node.name.length > MAX_NODE_NAME_LENGTH) errors.push("节点名称最多 128 个字符");
    if (node.kind === "group" && node.parentCode !== "") errors.push("业务根父节点必须为空");
    if (node.kind === "organization" && !nodes.some(parent => parent.code === node.parentCode)) {
      errors.push("业务分组存在循环或孤立节点");
    }
  });

  const nodesByCode = new Map(nodes.map(node => [node.code, node]));
  for (const node of nodes) {
    const visited = new Set<string>();
    let current: BusinessNode | undefined = node;
    while (current?.kind === "organization") {
      if (visited.has(current.code)) {
        errors.push("业务分组存在循环或孤立节点");
        break;
      }
      visited.add(current.code);
      current = nodesByCode.get(current.parentCode);
      if (!current) {
        errors.push("业务分组存在循环或孤立节点");
        break;
      }
    }
  }

  const sharedChannelIds = new Set(channels.filter(channel => channel.active !== false).map(channel => channel.sourceChannelId));
  const boundChannelIds = new Set<number>();
  bindings.forEach(binding => {
    if (!sharedChannelIds.has(binding.sourceChannelId)) errors.push("只能挂载当前上级已共享的有效通道");
    if (!organizations.has(binding.nodeCode)) errors.push("通道只能挂载到虚拟组织节点");
    if (boundChannelIds.has(binding.sourceChannelId)) errors.push("同一通道只能挂载到一个业务分组");
    boundChannelIds.add(binding.sourceChannelId);
  });
  return [...new Set(errors)];
}

function activeChannelIds(channels: CascadeChannelProjection[]): Set<number> {
  return new Set(channels.filter(channel => channel.active !== false).map(channel => channel.sourceChannelId));
}

function effectiveBindings(
  nodes: BusinessNode[],
  bindings: BusinessBinding[],
  channels: CascadeChannelProjection[]
): Map<number, BusinessBinding> {
  const organizationCodes = new Set(nodes.filter(node => node.kind === "organization").map(node => node.code));
  const sharedIds = activeChannelIds(channels);
  const result = new Map<number, BusinessBinding>();
  for (const binding of bindings) {
    if (
      sharedIds.has(binding.sourceChannelId) &&
      organizationCodes.has(binding.nodeCode) &&
      !result.has(binding.sourceChannelId)
    ) {
      result.set(binding.sourceChannelId, binding);
    }
  }
  return result;
}

export interface BusinessGroupCounts {
  sharedChannels: number;
  groupedChannels: number;
  ungroupedChannels: number;
  directByNode: Map<string, number>;
  inclusiveByNode: Map<string, number>;
}

export function businessGroupCounts(
  nodes: BusinessNode[],
  bindings: BusinessBinding[],
  channels: CascadeChannelProjection[]
): BusinessGroupCounts {
  const directByNode = new Map(nodes.map(node => [node.code, 0]));
  const inclusiveByNode = new Map(nodes.map(node => [node.code, 0]));
  const bindingsByChannel = effectiveBindings(nodes, bindings, channels);
  const nodesByCode = new Map(nodes.map(node => [node.code, node]));

  for (const binding of bindingsByChannel.values()) {
    directByNode.set(binding.nodeCode, (directByNode.get(binding.nodeCode) || 0) + 1);
    const visited = new Set<string>();
    let current = nodesByCode.get(binding.nodeCode);
    while (current && !visited.has(current.code)) {
      visited.add(current.code);
      inclusiveByNode.set(current.code, (inclusiveByNode.get(current.code) || 0) + 1);
      current = current.parentCode ? nodesByCode.get(current.parentCode) : undefined;
    }
  }

  const sharedChannels = activeChannelIds(channels).size;
  const groupedChannels = bindingsByChannel.size;
  return {
    sharedChannels,
    groupedChannels,
    ungroupedChannels: sharedChannels - groupedChannels,
    directByNode,
    inclusiveByNode
  };
}

export function ungroupedChannels(
  nodes: BusinessNode[],
  bindings: BusinessBinding[],
  channels: CascadeChannelProjection[]
): CascadeChannelProjection[] {
  const groupedIds = new Set(effectiveBindings(nodes, bindings, channels).keys());
  return channels.filter(channel => channel.active !== false && !groupedIds.has(channel.sourceChannelId));
}

export interface BusinessBindingResult {
  bindings: BusinessBinding[];
  errors: string[];
}

export function assignBusinessBindings(
  nodes: BusinessNode[],
  bindings: BusinessBinding[],
  channels: CascadeChannelProjection[],
  sourceChannelIds: number[],
  targetNodeCode: string
): BusinessBindingResult {
  const target = nodes.find(node => node.code === targetNodeCode);
  if (!target || target.kind !== "organization") {
    return {
      bindings: bindings.map(binding => ({ ...binding })),
      errors: [target?.kind === "group" ? "通道只能挂载到虚拟组织节点" : "目标组织不存在或已删除"]
    };
  }

  const uniqueIds = [...new Set(sourceChannelIds)];
  const sharedIds = activeChannelIds(channels);
  if (uniqueIds.some(sourceChannelId => !sharedIds.has(sourceChannelId))) {
    return {
      bindings: bindings.map(binding => ({ ...binding })),
      errors: ["只能挂载当前上级已共享的有效通道"]
    };
  }

  const selectedIds = new Set(uniqueIds);
  const nextBindings = bindings.filter(binding => !selectedIds.has(binding.sourceChannelId)).map(binding => ({ ...binding }));
  uniqueIds.forEach(sourceChannelId => nextBindings.push({ sourceChannelId, nodeCode: targetNodeCode }));
  return { bindings: nextBindings, errors: [] };
}

export function removeBusinessBindings(
  bindings: BusinessBinding[],
  sourceChannelIds: number[],
  targetNodeCode?: string
): BusinessBinding[] {
  const selectedIds = new Set(sourceChannelIds);
  return bindings
    .filter(binding => !selectedIds.has(binding.sourceChannelId) || (targetNodeCode && binding.nodeCode !== targetNodeCode))
    .map(binding => ({ ...binding }));
}

export interface BusinessGroupDraft {
  nodes: BusinessNode[];
  bindings: BusinessBinding[];
}

export function isBusinessGroupDirty(baseline: BusinessGroupDraft, draft: BusinessGroupDraft): boolean {
  const normalizedNodes = (nodes: BusinessNode[]) =>
    nodes
      .map(node => [node.code, node.name, node.kind, node.parentCode, node.sort] as const)
      .sort((left, right) => left[0].localeCompare(right[0]));
  const normalizedBindings = (bindings: BusinessBinding[]) =>
    bindings
      .map(binding => [binding.sourceChannelId, binding.nodeCode] as const)
      .sort((left, right) => left[0] - right[0] || left[1].localeCompare(right[1]));
  return (
    JSON.stringify(normalizedNodes(baseline.nodes)) !== JSON.stringify(normalizedNodes(draft.nodes)) ||
    JSON.stringify(normalizedBindings(baseline.bindings)) !== JSON.stringify(normalizedBindings(draft.bindings))
  );
}
