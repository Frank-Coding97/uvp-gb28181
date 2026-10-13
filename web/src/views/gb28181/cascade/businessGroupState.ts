import type { BusinessBinding, BusinessNode, CascadeChannelProjection } from "@/api/gb28181";

const GB_ID = /^\d{20}$/;
const MAX_NODE_NAME_LENGTH = 128;

export function createBusinessNodeCode(localDeviceId: string, kind: BusinessNode["kind"], nodes: BusinessNode[]) {
  if (!GB_ID.test(localDeviceId)) return "";
  const type = kind === "group" ? "215" : "216";
  const identityDigit = localDeviceId[13];
  const prefix = `${localDeviceId.slice(0, 10)}${type}${identityDigit}`;
  const used = nodes
    .filter(node => node.code.startsWith(prefix) && /^\d{6}$/.test(node.code.slice(14)))
    .map(node => Number(node.code.slice(14)));
  const sequence = Math.max(0, ...used) + 1;
  return sequence <= 999999 ? `${prefix}${String(sequence).padStart(6, "0")}` : "";
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
  channels: CascadeChannelProjection[]
): string[] {
  const errors: string[] = [];
  const roots = nodes.filter(node => node.kind === "group");
  if (nodes.length && roots.length !== 1) errors.push("业务分组必须且只能有一个根节点");
  const codes = new Set<string>();
  const organizations = new Set(nodes.filter(node => node.kind === "organization").map(node => node.code));
  nodes.forEach(node => {
    const expectedType = node.kind === "group" ? "215" : "216";
    if (!GB_ID.test(node.code) || node.code.slice(10, 13) !== expectedType) {
      errors.push("节点国标编号必须为 20 位数字，且第 11–13 位类型码与节点一致（根 215、组织 216）");
    }
    if (codes.has(node.code)) errors.push("节点国标编号重复");
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
