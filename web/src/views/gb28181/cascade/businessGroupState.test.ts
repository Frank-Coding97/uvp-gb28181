import { describe, expect, it } from "vitest";
import type { BusinessBinding, BusinessNode, CascadeChannelProjection } from "@/api/gb28181";
import {
  assignBusinessBindings,
  businessNodeLabel,
  businessGroupCounts,
  businessNodeErrors,
  businessNodePath,
  createBusinessNodeCode,
  descendantCodes,
  isBusinessGroupDirty,
  removeBusinessBindings,
  searchBusinessNodeCodes,
  ungroupedChannels
} from "./businessGroupState";

const localDeviceId = "34020000002000000001";
const root: BusinessNode = {
  code: "34020000002150000001",
  name: "业务根",
  kind: "group",
  parentCode: "",
  sort: 0
};
const organization: BusinessNode = {
  code: "34020000002160000002",
  name: "组织",
  kind: "organization",
  parentCode: root.code,
  sort: 0
};
const childOrganization: BusinessNode = {
  code: "34020000002160000003",
  name: "子组织",
  kind: "organization",
  parentCode: organization.code,
  sort: 0
};
const channel: CascadeChannelProjection = {
  sourceChannelId: 31,
  sourceDeviceId: 3,
  publishedChannelId: "34020000001320000031",
  name: "已共享通道",
  parentOverride: "",
  ptzAllowed: false,
  active: true
};

function makeChannel(sourceChannelId: number, name = `通道${sourceChannelId}`): CascadeChannelProjection {
  return { ...channel, sourceChannelId, publishedChannelId: `340200000013200${String(sourceChannelId).padStart(6, "0")}`, name };
}

describe("cascade business-group state", () => {
  it("generates stable-type 20-digit codes and advances across collisions", () => {
    expect(createBusinessNodeCode(localDeviceId, "group", [])).toBe(root.code);
    expect(createBusinessNodeCode(localDeviceId, "organization", [organization])).toBe("34020000002160000003");
    expect(createBusinessNodeCode("bad", "group", [])).toBe("");
  });

  it("skips externally reserved platform, parent and shared codes when suggesting a node code", () => {
    const nextOrganizationCode = "34020000002160000003";
    expect(createBusinessNodeCode(localDeviceId, "organization", [], [nextOrganizationCode])).toBe("34020000002160000004");
    expect(businessNodeErrors([root, { ...organization, code: nextOrganizationCode }], [], [], [nextOrganizationCode])).toContain(
      "节点国标编号与本平台、上级或已共享资源编号冲突"
    );
  });

  it("checks the GB node type in digits 11 through 13", () => {
    expect(businessNodeErrors([{ ...root, code: "34020000002160000001" }], [], [])).toContain(
      "节点国标编号必须为 20 位数字，且第 11–13 位类型码与节点一致（根 215、组织 216）"
    );
    expect(businessNodeErrors([{ ...root, code: "34020000002150000001" }], [], [])).toEqual([]);
  });

  it("rejects malformed roots, cycles, invalid names and bindings outside shared channels", () => {
    expect(businessNodeErrors([root, organization], [{ sourceChannelId: 31, nodeCode: organization.code }], [channel])).toEqual(
      []
    );
    const cycle = [
      root,
      { ...organization, parentCode: "34020000002160000003" },
      { ...organization, code: "34020000002160000003", parentCode: organization.code }
    ];
    expect(businessNodeErrors(cycle, [], [channel])).toContain("业务分组存在循环或孤立节点");
    expect(businessNodeErrors([{ ...root, name: " " }], [], [])).toContain("节点名称不能为空");
    expect(businessNodeErrors([root, organization], [{ sourceChannelId: 99, nodeCode: organization.code }], [channel])).toContain(
      "只能挂载当前上级已共享的有效通道"
    );
  });

  it("finds a node and all descendants without making cycles recurse forever", () => {
    const cyclic: BusinessNode[] = [
      root,
      organization,
      { ...organization, code: "34020000002160000003", parentCode: organization.code }
    ];
    expect(descendantCodes(cyclic, organization.code)).toEqual(new Set([organization.code, "34020000002160000003"]));
    const bindings: BusinessBinding[] = [];
    expect(bindings).toEqual([]);
  });

  it("returns a complete path and search matches with their ancestor paths", () => {
    const otherOrganization: BusinessNode = {
      ...organization,
      code: "34020000002160000004",
      name: "同名组织",
      parentCode: organization.code
    };
    const nodes = [root, organization, childOrganization, otherOrganization];
    expect(businessNodePath(nodes, childOrganization.code)).toEqual([root, organization, childOrganization]);
    expect(searchBusinessNodeCodes(nodes, "子组织")).toEqual(new Set([root.code, organization.code, childOrganization.code]));
    expect(searchBusinessNodeCodes(nodes, "同名组织")).toEqual(new Set([root.code, organization.code, otherOrganization.code]));
  });

  it("adds the node code to same-name sibling paths so each organization remains identifiable", () => {
    const first = { ...organization, name: "运营" };
    const second = { ...first, code: "34020000002160000004" };
    const nodes = [root, first, second];
    expect(businessNodeLabel(nodes, first)).toBe(`运营 · ${first.code}`);
    expect(businessNodeLabel(nodes, second)).toBe(`运营 · ${second.code}`);
  });

  it("counts active shared channels, valid bindings, direct and inclusive organization totals", () => {
    const nodes = [root, organization, childOrganization, { ...organization, code: "34020000002160000004", name: "B" }];
    const channels = [1, 2, 3, 4, 5, 6, 7, 8, 9].map(id => makeChannel(id));
    channels[8] = { ...channels[8], active: false };
    const bindings: BusinessBinding[] = [
      { sourceChannelId: 1, nodeCode: organization.code },
      { sourceChannelId: 2, nodeCode: organization.code },
      { sourceChannelId: 3, nodeCode: childOrganization.code },
      { sourceChannelId: 4, nodeCode: childOrganization.code },
      { sourceChannelId: 5, nodeCode: childOrganization.code }
    ];

    const counts = businessGroupCounts(nodes, bindings, channels);
    expect(counts).toEqual({
      sharedChannels: 8,
      groupedChannels: 5,
      ungroupedChannels: 3,
      directByNode: new Map([
        [root.code, 0],
        [organization.code, 2],
        [childOrganization.code, 3],
        ["34020000002160000004", 0]
      ]),
      inclusiveByNode: new Map([
        [root.code, 5],
        [organization.code, 5],
        [childOrganization.code, 3],
        ["34020000002160000004", 0]
      ])
    });
    expect(ungroupedChannels(nodes, bindings, channels).map(item => item.sourceChannelId)).toEqual([6, 7, 8]);
  });

  it("batch assigns unique valid channels to one organization and preserves unrelated snapshot data", () => {
    const otherOrganization = { ...organization, code: "34020000002160000004", name: "B" };
    const channels = [makeChannel(31), makeChannel(32), makeChannel(33)];
    const existing: BusinessBinding[] = [
      { sourceChannelId: 32, nodeCode: otherOrganization.code },
      { sourceChannelId: 33, nodeCode: otherOrganization.code }
    ];
    const assigned = assignBusinessBindings(
      [root, organization, otherOrganization],
      existing,
      channels,
      [31, 31, 32],
      organization.code
    );

    expect(assigned).toEqual({
      bindings: [
        { sourceChannelId: 33, nodeCode: otherOrganization.code },
        { sourceChannelId: 31, nodeCode: organization.code },
        { sourceChannelId: 32, nodeCode: organization.code }
      ],
      errors: []
    });
    expect(channels).toEqual([makeChannel(31), makeChannel(32), makeChannel(33)]);
  });

  it("does not partially assign invalid targets or channels", () => {
    const channels = [makeChannel(31), { ...makeChannel(32), active: false }];
    const bindings = [{ sourceChannelId: 31, nodeCode: organization.code }];
    expect(assignBusinessBindings([root, organization], bindings, channels, [31], root.code)).toEqual({
      bindings,
      errors: ["通道只能挂载到虚拟组织节点"]
    });
    expect(assignBusinessBindings([root, organization], bindings, channels, [31, 99, 32], organization.code)).toEqual({
      bindings,
      errors: ["只能挂载当前上级已共享的有效通道"]
    });
  });

  it("removes selected bindings while leaving channel projections unchanged", () => {
    const bindings = [
      { sourceChannelId: 31, nodeCode: organization.code },
      { sourceChannelId: 32, nodeCode: organization.code },
      { sourceChannelId: 33, nodeCode: childOrganization.code }
    ];
    expect(removeBusinessBindings(bindings, [31, 32, 32])).toEqual([{ sourceChannelId: 33, nodeCode: childOrganization.code }]);
  });

  it("compares business-group drafts by semantic content, not array enumeration order", () => {
    const nodes = [root, organization, childOrganization];
    const bindings = [{ sourceChannelId: 31, nodeCode: organization.code }];
    const baseline = { nodes, bindings };
    expect(isBusinessGroupDirty(baseline, { nodes: [...nodes].reverse(), bindings: [...bindings] })).toBe(false);
    expect(isBusinessGroupDirty(baseline, { nodes: nodes.map(node => ({ ...node, sort: node.sort + 1 })), bindings })).toBe(true);
    expect(
      isBusinessGroupDirty(baseline, {
        nodes: nodes.map(node => (node.code === organization.code ? { ...node, name: "已改名" } : node)),
        bindings
      })
    ).toBe(true);
    expect(
      isBusinessGroupDirty(baseline, {
        nodes: nodes.map(node => (node.code === childOrganization.code ? { ...node, parentCode: root.code } : node)),
        bindings
      })
    ).toBe(true);
    expect(isBusinessGroupDirty(baseline, { nodes, bindings: [{ sourceChannelId: 31, nodeCode: childOrganization.code }] })).toBe(
      true
    );
  });
});
