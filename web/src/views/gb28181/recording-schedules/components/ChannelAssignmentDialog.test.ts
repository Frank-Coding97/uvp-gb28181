import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(
  resolve(process.cwd(), "src/views/gb28181/recording-schedules/components/ChannelAssignmentDialog.vue"),
  "utf8"
);

describe("ChannelAssignmentDialog high fidelity workflow", () => {
  it("uses a system dialog instead of a drawer", () => {
    expect(source).toContain("<a-modal");
    expect(source).toContain('modal-class="uvp-system-dialog channel-assignment-dialog"');
    expect(source).toContain("align-center");
    expect(source).toContain("height: 'min(760px, calc(100vh - 48px))'");
    expect(source).toContain("display: 'flex'");
    expect(source).toContain("flex: '1 1 auto'");
    expect(source).toContain("minHeight: 0");
    expect(source).not.toContain("<a-drawer");
  });

  /**
   * 回归护栏：弹窗弹出时**只能**有表格表体一条竖向滚动条。
   *
   * 实测（修复前，1440x900 / 1280x720 / 1280x577 三档）有两条多余滚动条：
   *   ① `.arco-modal-wrapper`：Arco `align-center` 靠 `white-space:nowrap` +
   *      高度 100% 的 `::after` 做垂直居中，`inline-block` 弹窗把 wrapper 的
   *      scrollHeight 撑到 1108px（视口仅 577px）⇒ 全屏右侧一条大滚动条；
   *   ② `.arco-modal-body`：`overflowY:auto` 让 777px 内容挤在 406px 里滚
   *      ⇒ 弹窗内一条大滚动条，表格被压到只剩表头。
   */
  it("keeps the only vertical scrollbar inside the table body", () => {
    // 弹窗 body 不许滚（内联 body-style 与 CSS 双保险）
    expect(source).toContain("overflow: 'hidden'");
    expect(source).not.toContain("overflowY: 'auto'");

    // 全屏 wrapper 禁滚：必须是非 scoped 的全局样式，用 :has() 命中装载本弹窗的那个 wrapper
    expect(source).toContain(".arco-modal-wrapper:has(> .channel-assignment-dialog)");
    expect(source).toMatch(/\.arco-modal-wrapper:has\(> \.channel-assignment-dialog\)\s*{[^}]*overflow:\s*hidden;/s);

    // body 自身必须是纵向 flex 容器：全局给弹窗 body 的 padding 是 content-box，
    // body 若仍是 display:block，内部内容只能按内容高度收缩（637px 的 body 只分到 486px）。
    expect(source).toMatch(
      /\.channel-assignment-dialog :deep\(\.arco-modal-body\)\s*{[^}]*display:\s*flex;[^}]*flex-direction:\s*column;[^}]*overflow:\s*hidden;/s
    );

    // 表格区整段参与伸缩，内部固定块不参与
    expect(source).toMatch(/\.assignment-channel-section\s*{[^}]*flex:\s*1 1 auto;[^}]*min-height:\s*0;/s);
    // 表体是唯一允许滚动的地方，且要压过 Arco 落下的内联 max-height
    expect(source).toMatch(
      /\.assignment-channel-table :deep\(\.arco-table-body\)\s*{[^}]*flex:\s*1 1 auto;[^}]*max-height:\s*none\s*!important;/s
    );
    // 不能再用固定 min-height 撑表格：矮视口下会反向撑破 body（实测被腰斩到 48px）
    expect(source).not.toMatch(/\.assignment-channel-table\s*{[^}]*min-height:\s*340px/s);
  });

  /**
   * 回归护栏：`scroll.y` 是「触发 Arco 拆分表头/表体」的开关，不是表体高度。
   * 写死正常数字会把表体钉死、写字符串会被 Arco 落成内联 `height:100%`，
   * 两者都会压掉 flex 分配（实测表格被压回 140px = 表头+一行+分页的自然高度）。
   */
  it("keeps scroll.y as a non-empty placeholder so Arco splits header from body", () => {
    expect(source).toContain(':scroll="{ x: 860, y: 1 }"');
    expect(source).toContain(':scroll="{ x: 900, y: 1 }"');
    expect(source).not.toContain("y: 300");
    expect(source).not.toContain("y: '100%'");
  });

  it("locks the plan inherited from the ledger row and keeps the footer summary as the only impact hint", () => {
    expect(source).toContain("分配录像计划");
    expect(source).toContain("当前录像计划");
    expect(source).toContain("selectedPlan.name");
    expect(source).not.toContain("选择录像计划");
    expect(source).not.toContain('v-model="selectedPlanId"');
    expect(source).toContain("确认分配");
    expect(source).toContain("当前计划已停用，不能新增分配");
    expect(source).toContain("!selectedPlan?.enabled");
    // footer 摘要已说明影响（“将为 N 台设备下当前有权限的通道分配…”），
    // 底部那条重复的 a-alert 提示已按产品要求移除，把纵向空间让给表格。
    expect(source).toContain("footerSummary");
    expect(source).not.toContain("选择后，通道录像模式将切换为");
    expect(source).not.toContain("assignment-impact");
  });

  /**
   * 回归护栏：设备档表格列 = 设备名称 / 设备 ID / 在线状态三列，
   * 「设备名称 + 编码」两行合一已拆开，「分配说明」列已删除。
   * ⚠️ 只对**设备档**生效；通道档是另一张表（通道/所属设备/在线状态/分配状态），
   * 不要用全文件 not.toContain 去卡通道档的列名。
   */
  it("splits the device column into name and id and drops the assignment-description column", () => {
    const deviceTable = source.slice(
      source.indexOf("<a-table\n          v-if=\"selectionScope === 'device'\""),
      source.indexOf("<a-table\n          v-else")
    );
    // 三列都在
    expect(deviceTable).toContain('title="设备名称"');
    expect(deviceTable).toContain('title="设备 ID"');
    expect(deviceTable).toContain('title="在线状态"');
    // 旧的合并列与说明列都不在了
    expect(deviceTable).not.toContain('title="设备"');
    expect(deviceTable).not.toContain('title="分配说明"');
    expect(deviceTable).not.toContain("确认后应用到该设备下当前有权限的全部通道");
    // ID 列用独立的 entity-code 类（字号与名称同级，不再是 10px 副行）
    expect(deviceTable).toContain('class="entity-code"');
    expect(source).toMatch(/\.entity-cell small\.entity-code\s*{[^}]*font-size:\s*12px;/s);
    // 通道档不受影响
    expect(source).toContain('title="所属设备"');
  });

  /**
   * 回归护栏：通道档表格列 = 通道名称 / 通道 ID / 所属设备 / 在线状态，
   * 「通道名称 + 编码」两行合一已拆开，「分配状态」列已删除。
   */
  it("splits the channel column into name and id and drops the binding-status column", () => {
    const channelTable = source.slice(
      source.indexOf("<a-table\n          v-else"),
      source.indexOf("</a-table>", source.indexOf("<a-table\n          v-else"))
    );
    expect(channelTable).toContain('title="通道名称"');
    expect(channelTable).toContain('title="通道 ID"');
    expect(channelTable).toContain('title="所属设备"');
    expect(channelTable).toContain('title="在线状态"');
    expect(channelTable).not.toContain('title="通道"');
    expect(channelTable).not.toContain('title="分配状态"');
    expect(channelTable).not.toContain("已有录像计划");
    // ID 列同样走 entity-code（12px，与名称同级）
    expect(channelTable).toContain('class="entity-code"');
  });

  /**
   * 回归护栏（方案B：提交前就告知「已被占用」）。
   * 四件事必须同时成立，缺一个就退回"提交后才被拒"的旧体验：
   *   ① 按行禁用勾选 —— ⛔ Arco **只认数据行的 `record.disabled`**，
   *      `rowSelection.disabled`（哪怕写成按行函数）实测**完全无效**
   *      （`table-operation-td.js`: `"disabled": Boolean(props.record.disabled)`）；
   *   ② 整行置灰 + 名称后给出「已被录像计划占用」文案；
   *   ③ 提交后把 conflict 单独计数提示，而不是笼统说"M 项未分配"。
   */
  it("disables and greys out candidates already bound to a plan", () => {
    // ① 按行禁用：注入 record.disabled，并把它喂给 :data
    expect(source).toContain("function rowDisabled(record: AssignmentOption): boolean");
    expect(source).toContain("return !!record.bound;");
    expect(source).toContain("disabled: rowDisabled(item)");
    expect(source).toContain(':data="assignmentRows"');
    // ⛔ 不得回退成 rowSelection.disabled（写了也不生效，属假修复）
    expect(source).not.toContain("rowSelectionDisabled");
    expect(source).not.toMatch(/rowSelection[\s\S]{0,120}disabled:/);
    // 两张表都接上
    expect(source).toContain(':row-selection="deviceRowSelection"');
    expect(source).toContain(':row-selection="channelRowSelection"');
    // ② 置灰 + 文案
    expect(source).toContain(':row-class="rowClass"');
    expect(source).toContain('return record.bound ? "row-bounded" : "";');
    expect(source).toContain("row-bounded");
    expect(source).toContain("已被录像计划占用");
    expect(source).toMatch(/\.bound-hint\s*{[^}]*}/);
    // ③ conflict 单独计数
    expect(source).toContain('item.status === "conflict"');
    expect(source).toContain("已被其他录像计划占用");
  });

  /**
   * 回归护栏：弹窗必须**垂直居中**（上下留白对称）。
   * Arco 的 `align-center` 用「white-space:nowrap + 高度100% 的 ::after」这套
   * inline-block 技巧做居中；一旦给 wrapper 加 `overflow:hidden`（消掉全屏滚动条），
   * 它会退化成「贴顶对齐」：实测 top=0 而下方空 138px。
   * 详情弹窗同样贴顶 ⇒ 是 Arco 官方实现的共性问题，不是本组件独有。
   * 这里锁定改用 flex 居中，且 white-space 必须一起改回 normal。
   */
  it("centers the dialog vertically instead of sticking to the top", () => {
    const rule = source.match(/\.arco-modal-wrapper:has\(> \.channel-assignment-dialog\)\s*{([^}]*)}/s)?.[1] ?? "";
    expect(rule).toMatch(/display:\s*flex;/);
    expect(rule).toMatch(/align-items:\s*center;/);
    expect(rule).toMatch(/justify-content:\s*center;/);
    expect(rule).toMatch(/white-space:\s*normal;/);
    expect(rule).toMatch(/overflow:\s*hidden;/);
  });

  it("supports device and channel scopes without loading a device dropdown", () => {
    expect(source).toContain("按设备");
    expect(source).toContain("按通道");
    expect(source).toContain("selectionScope");
    expect(source).toContain("输入设备名称或国标编码");
    expect(source).toContain("输入通道名称、编码或所属设备");
    expect(source).not.toContain("deviceFilter");
    expect(source).not.toContain('placeholder="所属设备"');
  });

  it("places the online-state dropdown in the search toolbar", () => {
    const filters = source.slice(
      source.indexOf('<div class="assignment-filters">'),
      source.indexOf('<div v-if="selectedTargetCount" class="selection-actions">')
    );
    expect(filters).toContain('<a-select v-model="onlineFilter"');
    expect(filters).toContain(':options="onlineFilterOptions"');
    expect(source).toContain("仅显示在线通道");
    expect(source).not.toContain('<a-checkbox v-model="onlineOnly">');
  });

  it("paginates the assignment ledger and keeps only the clear-selection action", () => {
    expect(source).toContain(':pagination="assignmentPagination"');
    expect(source).toContain('@page-change="handlePageChange"');
    expect(source).toContain('@page-size-change="handlePageSizeChange"');
    // 保留「清空已选」这个功能性操作（含已选数量），但那句
    // 「全选仅作用于当前页，已选结果跨页保留」说明文字已按产品要求移除。
    expect(source).toContain("clearSelection");
    expect(source).toContain("清空已选");
    expect(source).not.toContain("全选仅作用于当前页");
    expect(source).not.toContain("跨页保留");
    // 只允许样式注释里提到旧类名，模板与样式规则本身都不能再有它。
    expect(source).not.toMatch(/class="selection-policy"/);
    expect(source).not.toMatch(/^\.selection-policy\s*\{/m);
  });

  it("loads assignment candidates and confirms selection through real APIs", () => {
    expect(source).toContain("listRecordingPlanDevices");
    expect(source).toContain("listRecordingPlanChannels");
    expect(source).toContain("assignRecordingPlan");
    expect(source).not.toContain("const devices:");
    expect(source).not.toContain("const channels:");
  });
});
