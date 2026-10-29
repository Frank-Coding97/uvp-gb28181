<template>
  <section v-if="visible" class="business-group-manager" aria-label="共享与目录工作区">
    <a-spin :loading="loading" class="business-group-content">
      <a-alert v-if="!canWrite" type="info" class="business-group-readonly">当前账号只有查看权限，共享与目录为只读。</a-alert>
      <a-alert v-if="loadError" type="error" class="business-group-alert">
        {{ loadError }}
        <a-button size="mini" data-action="retry-load" @click="load">重试</a-button>
      </a-alert>

      <template v-else-if="snapshot">
        <div class="business-group-toolbar" aria-label="共享方式操作">
          <a-tabs
            class="uvp-system-tabs business-group-sharing-tabs"
            aria-label="共享方式"
            :active-key="sharingMode"
            @change="selectSharingMode($event)"
          >
            <a-tab-pane key="channel" :disabled="saving || channelPickerVisible">
              <template #title>
                <span class="business-group-sharing-tab-title" data-action="channel-tab">
                  <Video :size="16" aria-hidden="true" />按照通道共享
                </span>
              </template>
            </a-tab-pane>
            <a-tab-pane key="directory" :disabled="saving || channelPickerVisible">
              <template #title>
                <span class="business-group-sharing-tab-title" data-action="directory-tab">
                  <FolderTree :size="16" aria-hidden="true" />按照目录共享
                </span>
              </template>
            </a-tab-pane>
          </a-tabs>
          <div class="business-group-platform-actions">
            <a-button data-action="close-manager" :disabled="saving || channelPickerVisible" @click="beginExit('close')"
              >返回</a-button
            >
            <a-button
              data-action="push-catalog"
              class="uvp-refresh-btn"
              :disabled="!canWrite || !platformEnabled || channelPickerVisible"
              @click="beginExit('push')"
              >推送目录</a-button
            >
            <a-button
              type="primary"
              data-action="save"
              :loading="saving"
              :disabled="
                !canWrite ||
                !snapshot ||
                loading ||
                saving ||
                channelPickerVisible ||
                revisionConflict ||
                Boolean(loadError) ||
                (modeChanged && !canManage)
              "
              @click="save"
              >保存修改</a-button
            >
          </div>
        </div>
        <div
          id="sharing-mode-panel"
          aria-label="共享资源"
          class="business-group-workspace"
          :class="{ 'is-channel-mode': sharingMode === 'channel' }"
        >
          <aside
            v-show="sharingMode === 'directory'"
            :class="{ 'is-mode-hidden': sharingMode !== 'directory' }"
            class="business-group-tree-panel uvp-tree-panel"
            aria-label="资源范围与业务目录"
          >
            <!--
              左侧只留业务分组树（2026-10-14 重构，老板要求）。
              ⛔ 原先树上方还有两块，都已删除：
                ① 「资源范围」（全部共享 / 未分组）—— 老板明确"直接删掉"，范围改由点树节点决定；
                ② 「业务目录」标题 + 一排工具按钮（新建目录 / 新建虚拟组织 / 编辑组织 / 删除空节点）
                   —— 这些动作全部挪到**树节点自己身上**（hover 出现，右键菜单保留），
                      左侧不再单占一行。相关死代码（`UNASSIGNED_NODE` / `visibleUnassignedCount`）已一并清掉。
            -->
            <div v-if="!rootNode" class="business-group-tree-empty">
              <p class="business-group-tree-empty__title">还没有业务分组</p>
              <p class="business-group-tree-empty__hint">
                按国标规范先建立一个业务分组根节点，才能把共享通道挂进去、并推送给上级平台。
              </p>
              <a-button
                type="primary"
                data-action="create-root"
                :disabled="!canWrite || saving || channelPickerVisible"
                @click="openCreateRootDialog"
                >新建业务分组</a-button
              >
              <p v-if="!canWrite" class="business-group-tree-empty__hint">当前账号只有查看权限。</p>
            </div>
            <!--
              搜索框（2026-10-15 对齐部门树）：补一颗放大镜前缀。
              ⛔ 前缀图标只是观感，**不要**把它当成"有搜索范围"的暗示 —— 过滤在本组件内做（`treeKeyword`）。
            -->
            <a-input
              v-if="nodes.length"
              v-model="treeKeyword"
              aria-label="搜索组织"
              allow-clear
              placeholder="搜索目录名称 / 编号"
            >
              <template #prefix><icon-search /></template>
            </a-input>
            <a-tree
              v-if="nodes.length"
              class="business-group-tree uvp-tree-panel__body"
              aria-label="业务目录"
              :data="treeData"
              :selected-keys="selectedNode ? [selectedNode.code] : []"
              :disabled="channelPickerVisible"
              v-model:expanded-keys="expandedNodeCodes"
              :show-line="true"
              :block-node="true"
              @select="selectTreeNode"
            >
              <template #title="node">
                <a-dropdown
                  trigger="contextMenu"
                  position="bl"
                  :popup-visible="contextMenuCode === node.code"
                  @popup-visible-change="setNodeMenu(node.code, $event)"
                >
                  <span
                    class="business-group-tree-title"
                    :data-code="node.code"
                    title="右键操作 · Shift+F10"
                    tabindex="0"
                    @keydown.shift.f10.prevent="setNodeMenu(node.code, true)"
                    @click.stop="selectScope(node.code)"
                  >
                    <!--
                      节点图标（2026-10-14 老板要求「在节点上加上对应的图标」）。
                      ⛔ 只能写在 `#title` 里，**不能**用 Arco 的 `#icon` 插槽：那个插槽只收到
                        `nodeStatus`（loading / checked / selected / expanded / isLeaf），
                        **拿不到 kind / code**，而业务根（215）和虚拟组织（216）都可能"有子节点或没子节点"，
                        靠 isLeaf 根本分不出这两类 ⇒ 两类节点会显示成同一个图标。
                      ⛔ 图标是纯装饰：`aria-hidden` 让读屏跳过，名称本身跟在后面。
                    -->
                    <component
                      :is="NODE_ICONS[nodeIconName(node)]"
                      class="uvp-tree-node-icon business-group-tree-icon"
                      :class="`business-group-tree-icon--${node.kind}`"
                      :size="15"
                      :stroke-width="2"
                      aria-hidden="true"
                      data-testid="tree-node-icon"
                      :data-icon="nodeIconName(node)"
                    /><span class="business-group-tree-name">{{ nodeLabel(node) || "未命名节点" }}</span
                    ><a-tag size="small">{{
                      node.kind === "group" ? descendantBindingCount(node.code) : bindingCount(node.code)
                    }}</a-tag>
                    <!--
                      行内操作按钮（2026-10-14）：**常显**在每一行行尾。
                      ⛔ 老板明确否掉了"悬停才显形"的方案 —— 藏起来用户根本不知道树上有操作，
                        右键菜单又本来就不显眼；两个入口都藏着等于没有入口。
                      ⛔ 三个按钮一律 `@click.stop`，否则会连带着触发外层 span 的 `selectScope`（范围会被切走）。
                    -->
                    <span class="business-group-tree-buttons">
                      <a-button
                        class="business-group-tree-button"
                        size="mini"
                        type="text"
                        data-action="add-organization"
                        :title="`新建${node.kind === 'group' ? '虚拟组织' : '下级组织'}`"
                        :aria-label="`在 ${nodeLabel(node)} 下新建下级组织`"
                        :disabled="!canWrite || saving || channelPickerVisible"
                        @click.stop="openCreateChildDialog(node)"
                        ><icon-plus
                      /></a-button>
                      <a-button
                        class="business-group-tree-button"
                        size="mini"
                        type="text"
                        data-action="edit-organization"
                        title="编辑节点信息"
                        :aria-label="`编辑 ${nodeLabel(node)}`"
                        :disabled="saving || channelPickerVisible"
                        @click.stop="openEditNodeDialog(node)"
                        ><icon-edit
                      /></a-button>
                      <a-button
                        class="business-group-tree-button"
                        size="mini"
                        type="text"
                        status="danger"
                        :data-action="node.kind === 'group' ? 'delete-root' : 'delete-node'"
                        :title="nodeDeleteReason(node) || '删除空节点'"
                        :aria-label="`删除 ${nodeLabel(node)}`"
                        :disabled="!canWrite || saving || channelPickerVisible || Boolean(nodeDeleteReason(node))"
                        @click.stop="requestDeleteNode(node)"
                        ><icon-delete
                      /></a-button>
                    </span>
                  </span>
                  <template #content>
                    <a-doption
                      data-action="context-add-organization"
                      :disabled="!canWrite || saving || channelPickerVisible"
                      @click.stop="
                        contextMenuCode = '';
                        openCreateChildDialog(node);
                      "
                      >新建{{ node.kind === "group" ? "虚拟组织" : "下级组织" }}</a-doption
                    >
                    <a-doption
                      data-action="context-edit-organization"
                      :disabled="saving || channelPickerVisible"
                      @click.stop="
                        contextMenuCode = '';
                        openEditNodeDialog(node);
                      "
                      >编辑{{ node.kind === "group" ? "业务根" : "组织" }}</a-doption
                    >
                    <a-doption
                      data-action="context-delete-node"
                      :disabled="!canWrite || saving || channelPickerVisible || Boolean(nodeDeleteReason(node))"
                      :title="nodeDeleteReason(node) || '删除空节点'"
                      @click.stop="
                        contextMenuCode = '';
                        requestDeleteNode(node);
                      "
                      >删除空{{ node.kind === "group" ? "根" : "组织" }}</a-doption
                    >
                  </template>
                </a-dropdown>
              </template>
              <!--
                展开箭头统一成 ChevronDown（与 `device-assignment` / `account` 的部门树对齐）。
                Arco 默认那个小三角在这套 UI 里已经没人用了，同级的三棵树箭头形状必须一样。
                ⭐ Arco 自己会转它：折叠 -90°（指右）、展开 0°（指下），不用我们写 transform。
              -->
              <template #switcher-icon>
                <ChevronDown class="arco-tree-node-switcher-icon" :size="13" :stroke-width="2.2" />
              </template>
            </a-tree>
          </aside>

          <!--
            右侧**永远**只显示"当前选中节点下已经添加的通道"（2026-10-14 老板确认）。
            ⛔ 别再在这里内嵌通道选择器：原来目录模式是把选择器塞在这块面板里、直接顶掉通道列表，
              老板的要求是「点添加 → 弹出弹窗 → 选通道 → 加入节点」，选择器只活在弹窗里（见下方 `a-modal`）。
          -->
          <section class="business-group-detail uvp-list-workspace" :class="{ 'is-directory': sharingMode === 'directory' }">
            <header v-if="sharingMode === 'directory'" class="business-group-panel-heading">
              <div>
                <strong>{{ currentScopeTitle }}</strong
                ><span
                  v-if="selectedNode && selectedNode.kind === 'organization'"
                  class="business-group-path"
                  data-testid="current-node-path"
                  >{{ nodePath(selectedNode.code) }} · 直属通道</span
                ><span
                  v-else-if="selectedNode && selectedNode.kind === 'group'"
                  class="business-group-path"
                  data-testid="current-node-path"
                  >{{ nodePath(selectedNode.code) }} · 目录概览</span
                ><span v-else>{{ currentScopeChannels.length }} 路通道</span>
              </div>
            </header>

            <!--
                节点的名称 / 国标 ID / 上级组织编辑区，2026-10-14 已整体挪进弹窗
                （入口 = 树节点上的铅笔按钮 / 右键「编辑」），右侧只剩共享通道列表本身。
              -->

            <s-layout-search
              ><template #fields>
                <a-input
                  v-model="channelKeyword"
                  aria-label="搜索通道"
                  allow-clear
                  placeholder="通道名称 / 编号"
                  :style="{ width: sharingMode === 'directory' ? '160px' : '260px', maxWidth: '100%' }"
                  @input="onChannelSearch"
                />
                <a-select
                  v-model="channelStatusFilter"
                  aria-label="状态筛选"
                  :style="{ width: sharingMode === 'directory' ? '110px' : '140px', maxWidth: '100%' }"
                  @change="onChannelSearch"
                >
                  <a-option value="all">全部状态</a-option>
                  <a-option value="online">在线</a-option>
                  <a-option value="offline">离线</a-option>
                  <a-option value="unknown">未知</a-option>
                </a-select> </template
              ><template v-if="sharingMode === 'channel'" #actions>
                <a-button type="primary" @click="onChannelSearch">查询</a-button></template
              ><template #extra>
                <div class="business-group-node-actions">
                  <template v-if="sharingMode === 'directory'">
                    <a-button
                      data-action="move-selected"
                      :disabled="!canWrite || saving || !selectedChannelIds.length || !organizationNodes.length"
                      @click="prepareMove(selectedChannelIds)"
                      >移动到组织</a-button
                    >
                    <a-button
                      data-action="move-out-selected"
                      :disabled="!canWrite || saving || !selectedBoundCount"
                      @click="moveOut(selectedChannelIds)"
                      >移出组织</a-button
                    >
                  </template>
                  <a-button
                    v-else
                    size="small"
                    status="danger"
                    data-action="request-unshare-selected"
                    :disabled="!canWrite || saving || !selectedChannelIds.length"
                    @click="requestUnshare(selectedChannelIds)"
                    >批量移除<template v-if="selectedChannelIds.length"
                      >（{{ selectedChannelIds.length }} 路）</template
                    ></a-button
                  >
                  <a-button
                    v-if="sharingMode === 'channel' || selectedNode?.kind === 'organization'"
                    type="primary"
                    size="small"
                    data-action="add-channels"
                    :disabled="!canWrite || saving"
                    @click="openChannelPicker"
                    >{{ sharingMode === "channel" ? "添加共享" : "添加通道" }}</a-button
                  >
                </div></template
              ></s-layout-search
            >
            <!--
                ⛔⛔ 2026-10-14 老板要求：这根「批量编排」工具条（`s-layout-tools`）**已整块删除**，
                  里面的批量控件全部搬进了上面的筛选区（见 `#extra`）。
                  ⛔ 别再在这里加回任何工具条：那会重新变成"筛选区和表格之间多插一个区域"。
            -->

            <a-table
              class="uvp-data-table business-group-channel-table"
              data-testid="resource-table"
              aria-label="共享通道列表"
              row-key="sourceChannelId"
              :data="currentPageRows"
              :bordered="false"
              :pagination="false"
              :scroll="{ x: 1000, y: '100%' }"
              :row-class="channelRowClass"
            >
              <template #columns>
                <!--
                    勾选列 2026-10-14 起**两种模式都可用**：按通道共享也要能多选后批量移除（原来只有目录模式有）。
                    表头即「本页全选」（半选态由 pageSelectionState 给 indeterminate）——
                    原来那颗「选择本页」按钮已删：同一语义不留两份入口。
                  -->
                <a-table-column title="" :width="46"
                  ><template #title>
                    <a-checkbox
                      data-action="select-page"
                      aria-label="全选本页通道"
                      :model-value="pageSelectionState === 'all'"
                      :indeterminate="pageSelectionState === 'partial'"
                      :disabled="!canWrite || saving || !currentPageRows.length"
                      @update:model-value="toggleVisibleSelection(currentPageRows)"
                    /> </template
                  ><template #cell="{ record: row }">
                    <a-checkbox
                      :aria-label="`${row.name || '通道'}选择`"
                      :model-value="selectedChannelIds.includes(row.sourceChannelId)"
                      :disabled="!canWrite || saving || unshareChannelIds.includes(row.sourceChannelId)"
                      @update:model-value="toggleChannelSelection(row.sourceChannelId)"
                    /> </template
                ></a-table-column>
                <a-table-column title="通道" :width="170"
                  ><template #cell="{ record: row }">
                    <span class="business-group-channel-name"
                      ><strong>{{ row.name || "未命名通道" }}</strong
                      ><small>{{
                        pendingShareChannelIds.includes(row.sourceChannelId)
                          ? "待共享"
                          : unshareChannelIds.includes(row.sourceChannelId)
                            ? "待取消共享"
                            : row.active === false
                              ? "共享已失效"
                              : "已共享"
                      }}</small></span
                    >
                  </template></a-table-column
                >
                <a-table-column title="源编号 / 发布编号" :width="240"
                  ><template #cell="{ record: row }">
                    <code
                      ><span>源 {{ sourceChannelNumber(row) }}</span
                      ><span
                        >发布
                        {{ pendingShareChannelIds.includes(row.sourceChannelId) ? "保存后生成" : row.publishedChannelId }}</span
                      ></code
                    >
                  </template></a-table-column
                >
                <a-table-column title="业务归属" :width="190"
                  ><template #cell="{ record: row }">{{ channelBindingPath(row.sourceChannelId) }}</template></a-table-column
                >
                <!--
                    在线状态：文案走 `device_status` 字典（与设备管理 / 录像计划分配弹窗同一把尺子）。
                    ⛔ 读不到状态的通道显示「未知」，**不当作离线**（见 channelOnlineStatus）。
                    云台权限列 2026-10-14 移除：云台改走统一控制入口，这里不再逐通道开关。
                  -->
                <a-table-column title="在线状态" :width="100" align="center"
                  ><template #cell="{ record: row }"
                    ><a-badge :status="channelOnlineStatus(row)" :text="channelOnlineLabel(row)" /></template
                ></a-table-column>
                <a-table-column title="操作" :width="190"
                  ><template #cell="{ record: row }">
                    <div class="business-group-row-actions uvp-table-actions">
                      <a-link
                        v-if="
                          sharingMode === 'directory' &&
                          bindingNodeCode(row.sourceChannelId) &&
                          (!selectedNode ||
                            selectedNode.kind !== 'organization' ||
                            bindingNodeCode(row.sourceChannelId) === selectedNode.code)
                        "
                        class="uvp-table-action"
                        data-action="remove-binding"
                        :disabled="!canWrite || saving"
                        @click="moveOut([row.sourceChannelId])"
                        >移出组织</a-link
                      ><a-link
                        class="uvp-table-action"
                        status="danger"
                        data-action="request-unshare"
                        :disabled="!canWrite || saving"
                        @click="requestUnshare([row.sourceChannelId])"
                        >{{ sharingMode === "channel" ? "移除" : "取消共享" }}</a-link
                      >
                    </div>
                  </template></a-table-column
                >
              </template>
            </a-table>
            <a-pagination
              class="business-group-pagination"
              :current="channelPage"
              :total="filteredCurrentScopeChannels.length"
              :page-size="20"
              :show-total="true"
              @change="channelPage = $event"
            />
          </section>
        </div>
      </template>
    </a-spin>

    <a-alert v-if="validationError" type="error" class="business-group-alert">{{ validationError }}</a-alert>
    <a-alert v-if="saveError" type="error" class="business-group-alert">
      {{ saveError }}
      <a-button
        v-if="resourceSyncPending"
        size="mini"
        data-action="retry-resource-sync"
        :disabled="!canWrite || saving || revisionConflict"
        @click="save"
        >{{ dirty ? "保存修改并重试同步" : "重试资源同步" }}</a-button
      >
      <template v-if="revisionConflict"
        ><a-button size="mini" data-action="reload-latest" @click="requestReloadLatest">重新读取最新目录</a-button></template
      >
    </a-alert>

    <!--
      新建 / 编辑业务分组节点的弹窗（2026-10-14）。
      ⛔ 国标 ID **预填但不锁死**（老板拍板）：默认按国标规则算好一个唯一编号，用户可改，
        边输边校验（20 位数字 / 第 11–13 位类型码 215|216 / 不与本平台·上级·已共享编号冲突）。
      ⛔ 编辑已有节点时 ID **只读** —— 这个编号可能已经推给上级平台了，改了等于换了一个节点。
      ⛔⛔ `modal-class="uvp-system-dialog"` **不能省**（2026-10-15 老板截图为证："这个弹窗样式不对"）：
        本仓面板底色 / 边框 / 圆角 16px、头尾的内边距与分隔线、页脚 10px 间距、暗色下的控件底色，
        **全部挂在 `.uvp-system-dialog` 上**（见 `style/model/uvp-ui-language.scss`）。
        漏了它就是一个裸 Arco 弹窗 —— 跟旁边每一个弹窗都"不像一家"。
        同级那颗「选择共享通道」弹窗一直带着它，别只修一半。
    -->
    <a-modal
      v-model:visible="nodeDialog.visible"
      :title="nodeDialogTitle"
      :closable="!saving"
      :mask-closable="false"
      modal-class="uvp-system-dialog business-group-node-dialog"
      unmount-on-close
    >
      <div class="business-group-node-form">
        <label>
          <span>分组名称 <small>最多 128 个字符</small></span>
          <a-input
            v-model="nodeDialog.name"
            data-testid="node-name-input"
            aria-label="分组名称"
            :maxlength="128"
            :disabled="!canWrite || saving"
            placeholder="例如：东区前端点位"
          />
        </label>
        <label>
          <span
            >国标 ID
            <small>{{ nodeDialog.mode === "edit" ? "已用于对外发布，不可修改" : "按国标规范自动生成，可修改" }}</small></span
          >
          <a-input
            v-model="nodeDialog.code"
            data-testid="node-code-input"
            aria-label="国标 ID"
            :maxlength="20"
            :readonly="nodeDialog.mode === 'edit'"
            :disabled="!canWrite || saving"
          />
        </label>
        <!--
          「上级组织」2026-10-15 起用 **树选择器**（老板：换成树组件分层级的那种）。
          ⛔ 不再用扁平 `a-select` + 「父 / 子」拼接串：候选本身就是一棵目录树，
            层级一深，拼出来的路径又长又难认（下拉框还要横向截断）。
          ⭐ 默认全展开（`defaultExpandAll`）：候选通常只有几层，摊开比"点了才知道有下级"好找；
            组织多了靠 `allow-search` 搜名字 / 编号。
        -->
        <label v-if="nodeDialog.kind === 'organization'">
          <span>上级组织</span>
          <a-tree-select
            v-model="nodeDialog.parentCode"
            data-testid="node-parent-select"
            aria-label="上级组织"
            :disabled="!canWrite || saving"
            :data="nodeDialogParentTree"
            :field-names="{ key: 'code', title: 'label', children: 'children' }"
            :tree-props="{ defaultExpandAll: true }"
            allow-search
            placeholder="选择上级组织"
          />
        </label>
        <p v-if="nodeDialogError" class="business-group-node-form__error" role="alert">{{ nodeDialogError }}</p>
      </div>
      <!--
        用**显式 footer** 而不是 modal 自带的 ok/cancel：
        ① 单测里 `a-modal` 被 stub 成只渲染 title + 默认插槽，自带 footer 根本点不到；
        ② 这里需要"校验没过就置灰确定键"以及两套文案（创建 / 保存），显式写更好控。
        ⛔ 同步依赖 `src/test/setup.ts` 里 a-modal 桩渲染 `footer` 插槽。
      -->
      <template #footer>
        <a-button data-action="cancel-node-dialog" :disabled="saving" @click="nodeDialog.visible = false">取消</a-button>
        <a-button
          type="primary"
          data-action="confirm-node-dialog"
          :disabled="!canWrite || saving || Boolean(nodeDialogError)"
          @click="submitNodeDialog"
          >{{ nodeDialog.mode === "edit" ? "保存" : "创建" }}</a-button
        >
      </template>
    </a-modal>

    <a-modal
      v-model:visible="moveDialog.visible"
      title="移动到组织"
      :closable="!saving"
      :mask-closable="false"
      modal-class="uvp-system-dialog business-group-move-dialog"
      unmount-on-close
    >
      <div class="business-group-node-form">
        <p>将选中的 {{ moveDialog.channelIds.length }} 路通道移入目标组织，替换原有归属并保持共享。</p>
        <label>
          <span>目标组织</span>
          <a-select v-model="moveDialog.targetCode" aria-label="移动目标组织" allow-search placeholder="选择目标组织">
            <a-option v-for="node in organizationNodes" :key="node.code" :value="node.code">
              {{ nodePath(node.code) }}
            </a-option>
          </a-select>
        </label>
      </div>
      <template #footer>
        <a-button data-action="cancel-move-dialog" :disabled="saving" @click="moveDialog.visible = false">取消</a-button>
        <a-button
          type="primary"
          data-action="confirm-move-dialog"
          :disabled="!canWrite || saving || !moveDialog.targetCode"
          @click="submitMove"
          >确认移动</a-button
        >
      </template>
    </a-modal>

    <!--
      选择通道的弹窗：**两种模式共用这一颗**（2026-10-14 合并）。
      - 「按通道共享」：把本地通道加进本上级的共享列表（`pickerTargetCode` 为空 = 不归属组织）；
      - 「按目录共享」：把通道加进**当前选中的虚拟组织**，加完右侧自动切到该组织、直接看到结果。
      ⛔ 目录模式原来是把选择器**内嵌**在右侧面板里（会顶掉通道列表）⇒ 老板明确要求
        「点添加 → 弹出弹窗 → 选通道 → 加入节点」，所以内嵌那份已删、只留这一颗弹窗。
      ⛔ 标题别塞完整路径：Arco 的 `.arco-modal-title` 是 `nowrap + ellipsis`，长路径会被截掉；
        目标组织由选择器自己那行「目标组织」显示（见 `BusinessChannelPicker`）。
    -->
    <a-modal
      v-if="channelPickerVisible"
      :visible="true"
      :title="sharingMode === 'channel' ? '选择共享通道' : '添加通道'"
      width="min(1000px, calc(100vw - 32px))"
      :footer="false"
      :mask-closable="false"
      modal-class="uvp-system-dialog business-share-channel-dialog"
      :data-testid="sharingMode === 'channel' ? 'share-channel-dialog' : 'add-channel-dialog'"
      @cancel="closeChannelPicker"
    >
      <BusinessChannelPicker
        :platform-name="platformName"
        :target-path="pickerTargetCode ? nodePath(pickerTargetCode) : '未分组共享'"
        :target-code="pickerTargetCode"
        :channels="channels"
        :bindings="bindings"
        :nodes="nodes"
        :disabled="saving"
        @apply="applyChannelPicker"
        @cancel="closeChannelPicker"
      />
    </a-modal>
  </section>
</template>

<script setup lang="ts">
import SLayoutSearch from "@/components/s-layout-search/index.vue";
import { computed, h, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { IconDelete, IconEdit, IconPlus, IconSearch } from "@arco-design/web-vue/es/icon";
import { ChevronDown, Folder, FolderOpen, FolderTree, Video } from "lucide-vue-next";
import {
  Modal as AModal,
  Table as ATable,
  TableColumn as ATableColumn,
  Tree as ATree,
  Pagination as APagination,
  Tabs as ATabs,
  TabPane as ATabPane,
  Checkbox as ACheckbox,
  Button as AButton,
  Message,
  Modal
} from "@arco-design/web-vue";
import {
  getCascadeBusinessGroup,
  getCascadePlatform,
  pushCascadeCatalog,
  replaceCascadeBusinessGroup,
  type BusinessBinding,
  type BusinessNode,
  type CascadeBusinessGroup,
  type CascadeChannelProjection,
  type CascadePlatform
} from "@/api/gb28181";
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
  searchBusinessNodeCodes
} from "./businessGroupState";
import BusinessChannelPicker from "./BusinessChannelPicker.vue";
import { getDevice, listChannels as listChannelPage, type ChannelVO } from "@/views/gb28181/device-mgmt/api";
import { useDeviceStatusLabel } from "../useDeviceStatusDict";

const props = defineProps<{
  visible: boolean;
  platformId: number;
  platformName: string;
  localDeviceId: string;
  canWrite: boolean;
  canManage?: boolean;
  platform?: CascadePlatform;
  refreshKey?: number;
}>();
const emit = defineEmits<{
  "update:visible": [value: boolean];
  dirtyChange: [dirty: boolean];
  saved: [snapshot: CascadeBusinessGroup];
  publishGroupUpdated: [platform: CascadePlatform];
}>();

const loading = ref(false);
const saving = ref(false);
const loadError = ref("");
const saveError = ref("");
const revisionConflict = ref(false);
const validationError = ref("");
const snapshot = ref<CascadeBusinessGroup | null>(null);
const nodes = ref<BusinessNode[]>([]);
const bindings = ref<BusinessBinding[]>([]);
const channels = ref<CascadeBusinessGroup["channels"]>([]);
const baselineChannels = ref<CascadeBusinessGroup["channels"]>([]);
const selectedNodeCode = ref("");
const sharingMode = ref<"channel" | "directory">("channel");
const directoryScopeCode = ref("");
const treeKeyword = ref("");
const channelKeyword = ref("");
type ChannelStatusFilter = "all" | "online" | "offline" | "unknown";
const channelStatusFilter = ref<ChannelStatusFilter>("all");
const channelStatusById = ref<Record<number, number>>({});
const channelPage = ref(1);
const pickerTargetCode = ref("");
const selectedChannelIds = ref<number[]>([]);
const channelPickerVisible = ref(false);
/**
 * 「离开前有未保存修改」的目标动作 —— 由 `confirmExit()` 这颗 confirm 弹窗挂起，
 * `onOk`（保存并离开）/ `onCancel`（继续编辑）时再取出来继续跑。
 */
const pendingExit = ref<"close" | "push" | "route" | "">("");
const moveDialog = reactive({ visible: false, targetCode: "", channelIds: [] as number[] });
const contextMenuCode = ref("");
const expandedNodeCodes = ref<string[]>([]);
const publishGroup = ref(props.platform?.publishGroup ?? false);
const configRevision = ref(props.platform?.configRevision ?? 0);
const platformState = ref<CascadePlatform | null>(props.platform || null);
const modeChanged = computed(() => (sharingMode.value === "directory") !== publishGroup.value);
const resourceSyncPending = ref(false);
const platformEnabled = ref(props.platform?.enabled ?? false);
let exitResolver: ((allowed: boolean) => void) | null = null;
let baselineNodes: BusinessNode[] = [];
let baselineBindings: BusinessBinding[] = [];
let loadSequence = 0;
/**
 * 通道模式（以及目录模式还没有根节点时）的"范围"标识 —— 它**不是**一个真实的树节点。
 * ⛔ 原先还有个 `UNASSIGNED_NODE`（未分组），2026-10-14 随左侧范围入口一起删掉了，
 *   不要再加回来：范围完全由"点哪个树节点"决定。
 */
const ALL_SHARED_NODE = "__all_shared__";

const selectedNode = computed(() => nodes.value.find(node => node.code === selectedNodeCode.value));
/**
 * 业务分组根节点：国标规定**有且只有一个**。
 * 空树或只有脏数据（没有 215 根）时为 undefined ⇒ 左侧面板走空态引导（"新建业务分组"）。
 */
const rootNode = computed(() => nodes.value.find(node => node.kind === "group"));
const organizationNodes = computed(() => nodes.value.filter(node => node.kind === "organization"));
const counts = computed(() => businessGroupCounts(nodes.value, bindings.value, channels.value));
const currentChannelIds = computed(() => new Set(channels.value.map(channel => channel.sourceChannelId)));
const pendingShareChannelIds = computed(() =>
  channels.value
    .filter(channel => {
      const original = baselineChannels.value.find(candidate => candidate.sourceChannelId === channel.sourceChannelId);
      return channel.active !== false && (!original || original.active === false);
    })
    .map(channel => channel.sourceChannelId)
);
const unshareChannelIds = computed(() =>
  baselineChannels.value
    .filter(channel => channel.active !== false && !currentChannelIds.value.has(channel.sourceChannelId))
    .map(channel => channel.sourceChannelId)
);
const resourceChanges = computed(() => ({
  shareChannelIds: [...pendingShareChannelIds.value],
  unshareChannelIds: [...unshareChannelIds.value],
  // 新增共享没有逐通道权限编辑入口；不要把草稿默认值当成显式权限变更，
  // 由后端按保存时的平台 PTZ 开关决定新投影的默认权限。
  channelPermissions: []
}));
const dirty = computed(
  () =>
    isBusinessGroupDirty(
      { nodes: baselineNodes, bindings: baselineBindings },
      { nodes: nodes.value, bindings: bindings.value }
    ) ||
    resourceChanges.value.shareChannelIds.length > 0 ||
    resourceChanges.value.unshareChannelIds.length > 0 ||
    resourceChanges.value.channelPermissions.length > 0 ||
    modeChanged.value
);

const treeRows = computed(() => {
  const result: Array<{ node: BusinessNode; depth: number }> = [];
  const visited = new Set<string>();
  const append = (parentCode: string, depth: number) => {
    nodes.value
      .filter(node => node.parentCode === parentCode && !visited.has(node.code))
      .sort((left, right) => left.sort - right.sort || left.code.localeCompare(right.code))
      .forEach(node => {
        visited.add(node.code);
        result.push({ node, depth });
        append(node.code, depth + 1);
      });
  };
  append("", 0);
  nodes.value
    .filter(node => !visited.has(node.code))
    .forEach(node => {
      visited.add(node.code);
      result.push({ node, depth: 0 });
      append(node.code, 1);
    });
  return result;
});
const visibleTreeRows = computed(() => {
  const visibleCodes = searchBusinessNodeCodes(nodes.value, treeKeyword.value);
  return treeRows.value.filter(row => visibleCodes.has(row.node.code));
});
function selectTreeNode(keys: (string | number)[]) {
  if (keys.length) selectScope(String(keys[0]));
}
function channelRowClass(row: CascadeChannelProjection) {
  return `channel-row-${row.sourceChannelId}`;
}
type BusinessTreeNode = BusinessNode & { key: string; title: string; children: BusinessTreeNode[] };
const treeData = computed(() => {
  const entries = new Map(
    visibleTreeRows.value.map(({ node }) => [
      node.code,
      { ...node, key: node.code, title: nodeLabel(node), children: [] as BusinessTreeNode[] }
    ])
  );
  const roots: BusinessTreeNode[] = [];
  entries.forEach(node => {
    const parent = entries.get(node.parentCode);
    if (parent) parent.children.push(node);
    else roots.push(node);
  });
  return roots;
});
const allSharedRows = computed(() => channels.value.filter(channel => channel.active !== false));
const currentScopeChannels = computed(() => {
  if (selectedNodeCode.value === ALL_SHARED_NODE) return allSharedRows.value;
  if (selectedNode.value?.kind === "organization") {
    const ids = new Set(
      bindings.value.filter(binding => binding.nodeCode === selectedNodeCode.value).map(binding => binding.sourceChannelId)
    );
    return channels.value.filter(channel => ids.has(channel.sourceChannelId));
  }
  if (selectedNode.value?.kind === "group") {
    const descendantNodeCodes = descendantCodes(nodes.value, selectedNodeCode.value);
    const ids = new Set(
      bindings.value.filter(binding => descendantNodeCodes.has(binding.nodeCode)).map(binding => binding.sourceChannelId)
    );
    return channels.value.filter(channel => ids.has(channel.sourceChannelId));
  }
  return allSharedRows.value;
});
const searchedCurrentScopeChannels = computed(() => filterChannels(currentScopeChannels.value, channelKeyword.value));
const filteredCurrentScopeChannels = computed(() => {
  const searched = searchedCurrentScopeChannels.value;
  if (channelStatusFilter.value === "all") return searched;
  return searched.filter(row => {
    const status = channelStatusById.value[row.sourceChannelId];
    if (channelStatusFilter.value === "online") return status === 1;
    if (channelStatusFilter.value === "offline") return status === 0;
    return status !== 1 && status !== 0;
  });
});
const currentPageRows = computed(() =>
  filteredCurrentScopeChannels.value.slice((channelPage.value - 1) * 20, channelPage.value * 20)
);

/**
 * 通道在线状态（表格「在线状态」列）。
 *
 * ⛔ 共享快照里**没有**状态字段，设备管理接口也**只支持按设备筛**（没有"按 id 批量查"这一支），
 * 所以只能把每行归到一个**来源设备编号**上、把该设备的通道清单取回来按通道 id 建索引，
 * 每个设备只取一次并缓存。
 * ⛔ 纯展示：读不到就是「未知」，既不当作离线，也**绝不能**因为查状态失败影响共享/保存流程。
 */
const STATUS_PAGE_SIZE = 200; // 通道列表接口允许的最大页大小
const STATUS_PAGE_LIMIT = 5; // 单设备最多取 5 页（1000 路），防呆
/** 设备主键 → 设备编号（已保存的行只带主键，回查一次拿编号并缓存）。 */
const deviceCodeById = ref<Record<number, string>>({});
const pendingStatusCodes = new Set<string>();
/** ⛔ 只有**取全**了某设备的通道清单才登记；取一半失败不登记，下次翻页/重开会再试。 */
const loadedStatusCodes = new Set<string>();
const channelStatusLabel = useDeviceStatusLabel("未知");

function mergeChannelStatuses(list: ChannelVO[]) {
  const next = { ...channelStatusById.value };
  list.forEach(channel => {
    next[channel.id] = channel.status;
  });
  channelStatusById.value = next;
}

/**
 * 把一行共享通道归到「来源设备编号」上。
 * ⛔⛔ 两条来源**都要认**（2026-10-14 实测踩过，只认 ① 会让草稿行永远显示「未知」）：
 *   ① 已保存的行：只有设备主键 `sourceDeviceId`，先回查拿编号；
 *   ② 刚「添加共享」还没保存的草稿行：`sourceDeviceId` 恒为 0，
 *      **只有编号**（见 `BusinessChannelPicker.toProjection` 的 `sourceDeviceCode`）。
 */
async function resolveRowDeviceCode(row: CascadeChannelProjection) {
  if (row.sourceDeviceCode) return row.sourceDeviceCode;
  if (!(row.sourceDeviceId > 0)) return "";
  const cached = deviceCodeById.value[row.sourceDeviceId];
  if (cached) return cached;
  const response = await getDevice(row.sourceDeviceId);
  const code = response.data?.deviceId || "";
  if (code) deviceCodeById.value = { ...deviceCodeById.value, [row.sourceDeviceId]: code };
  return code;
}

async function ensureChannelStatuses(rows: CascadeChannelProjection[]) {
  if (!rows.length) return;
  const resolved = await Promise.all(rows.map(row => resolveRowDeviceCode(row).catch(() => "")));
  const codes = [...new Set(resolved.filter(Boolean))].filter(
    code => !loadedStatusCodes.has(code) && !pendingStatusCodes.has(code)
  );
  if (!codes.length) return;
  codes.forEach(code => pendingStatusCodes.add(code));
  await Promise.all(
    codes.map(async code => {
      try {
        for (let page = 1; page <= STATUS_PAGE_LIMIT; page += 1) {
          const response = await listChannelPage({ deviceId: code, page, pageSize: STATUS_PAGE_SIZE });
          if (response.code !== 0) return;
          const list = response.data?.list || [];
          mergeChannelStatuses(list);
          if (list.length < STATUS_PAGE_SIZE) {
            loadedStatusCodes.add(code);
            return;
          }
        }
      } catch {
        // 读不到状态不算"已加载"：翻页或重新打开时会再试一次
      } finally {
        pendingStatusCodes.delete(code);
      }
    })
  );
}

watch(
  () =>
    searchedCurrentScopeChannels.value
      .map(row => `${row.sourceChannelId}:${row.sourceDeviceId}:${row.sourceDeviceCode || ""}`)
      .join(","),
  () => void ensureChannelStatuses(searchedCurrentScopeChannels.value),
  { immediate: true }
);

const currentScopeTitle = computed(() =>
  selectedNodeCode.value === ALL_SHARED_NODE
    ? "全部共享"
    : selectedNode.value?.kind === "group"
      ? "业务目录概览"
      : selectedNode.value?.name || "全部共享"
);
const selectedBoundCount = computed(() => selectedChannelIds.value.filter(id => Boolean(bindingNodeCode(id))).length);
/**
 * 勾选列表头那颗复选框的三态：本页全选中 / 部分选中（半选）/ 未选中。
 * ⛔ 只按**当前页**算 —— 与 `toggleVisibleSelection(currentPageRows)` 的语义严格对齐，
 *   别拿 `filteredCurrentScopeChannels` 算（那是"全部筛选结果"、跨页的语义）。
 *   ⭐ 2026-10-15 起页面上**只剩这一个选范围入口**（旧的「全选筛选结果」按钮已删），
 *   这个"表头只管本页"的口径就更不能含糊了，否则勾了表头却少选/多选了都不知道。
 */
const pageSelectionState = computed<"none" | "partial" | "all">(() => {
  const rows = currentPageRows.value;
  if (!rows.length) return "none";
  const selected = rows.filter(row => selectedChannelIds.value.includes(row.sourceChannelId)).length;
  if (selected === 0) return "none";
  return selected === rows.length ? "all" : "partial";
});
watch(
  () => [props.visible, props.platformId, props.refreshKey] as const,
  ([visible]) => {
    if (visible) void load();
    else reset();
  },
  { immediate: true }
);
watch(dirty, value => emit("dirtyChange", value), { immediate: true });
watch([sharingMode, saving, channelPickerVisible, treeKeyword], () => {
  contextMenuCode.value = "";
});

function unwrap<T>(response: any): T {
  return (response?.data?.data || response?.data || response) as T;
}

function requestErrorMessage(error: any): string {
  return String(error?.response?.data?.message || error?.response?.data?.msg || error?.message || "");
}

function reset() {
  loadSequence++;
  snapshot.value = null;
  nodes.value = [];
  bindings.value = [];
  channels.value = [];
  baselineChannels.value = [];
  selectedNodeCode.value = "";
  baselineNodes = [];
  baselineBindings = [];
  selectedChannelIds.value = [];
  moveDialog.visible = false;
  channelStatusFilter.value = "all";
  loadError.value = "";
  saveError.value = "";
  sharingMode.value = "channel";
  directoryScopeCode.value = "";
  resourceSyncPending.value = false;
  revisionConflict.value = false;
  validationError.value = "";
  loading.value = false;
  pendingExit.value = "";
  channelPickerVisible.value = false;
  // 关闭/重开时把「设备清单已取全」的记号清掉，下次打开重新拉一次在线状态（否则会一直沿用它第一次的快照）。
  loadedStatusCodes.clear();
  pendingStatusCodes.clear();
  publishGroup.value = props.platform?.publishGroup ?? false;
  configRevision.value = props.platform?.configRevision ?? 0;
  platformState.value = props.platform || null;
}

async function load() {
  const requestSequence = ++loadSequence;
  loading.value = true;
  loadError.value = "";
  saveError.value = "";
  resourceSyncPending.value = false;
  revisionConflict.value = false;
  validationError.value = "";
  snapshot.value = null;
  try {
    const [groupResponse, platformResponse] = await Promise.all([
      getCascadeBusinessGroup(props.platformId),
      getCascadePlatform(props.platformId)
    ]);
    const result = unwrap<CascadeBusinessGroup>(groupResponse);
    const platform = unwrap<CascadePlatform>(platformResponse);
    if (!props.visible || requestSequence !== loadSequence || result.platformId !== props.platformId) return;
    snapshot.value = result;
    nodes.value = result.nodes.map(node => ({ ...node }));
    expandedNodeCodes.value = nodes.value.map(node => node.code);
    bindings.value = result.bindings.map(binding => ({ ...binding }));
    channels.value = result.channels.map(channel => ({ ...channel }));
    baselineChannels.value = result.channels.map(channel => ({ ...channel }));
    baselineNodes = nodes.value.map(node => ({ ...node }));
    baselineBindings = bindings.value.map(binding => ({ ...binding }));
    selectedNodeCode.value =
      selectedNodeCode.value === ALL_SHARED_NODE || nodes.value.some(node => node.code === selectedNodeCode.value)
        ? selectedNodeCode.value || ALL_SHARED_NODE
        : ALL_SHARED_NODE;
    selectedChannelIds.value = [];
    channelPage.value = 1;
    publishGroup.value = result.publishGroup ?? platform.publishGroup;
    configRevision.value = result.configRevision ?? platform.configRevision;
    platformState.value = { ...platform, publishGroup: publishGroup.value, configRevision: configRevision.value };
    sharingMode.value = publishGroup.value ? "directory" : "channel";
    directoryScopeCode.value = nodes.value.find(node => node.kind === "group")?.code || ALL_SHARED_NODE;
    selectedNodeCode.value = sharingMode.value === "directory" ? directoryScopeCode.value : ALL_SHARED_NODE;
    platformEnabled.value = platform.enabled;
    emit("publishGroupUpdated", platformState.value);
  } catch (error: any) {
    if (props.visible && requestSequence === loadSequence) {
      const message = requestErrorMessage(error);
      loadError.value = message ? `业务分组读取失败：${message}` : "业务分组读取失败，请重试。";
    }
  } finally {
    if (requestSequence === loadSequence) loading.value = false;
  }
}

/**
 * 新建 / 编辑业务分组节点的弹窗（2026-10-14 重构）。
 *
 * ⛔ 国标 ID **预填但不锁死**（老板拍板）：默认按国标规则算出唯一编号，用户可改，
 *   边输边校验 —— 20 位数字；第 11–13 位必须是 `215`（业务根）/ `216`（虚拟组织）类型码；
 *   不能与本平台 / 上级平台 / 其它节点 / 已发布通道编号冲突。
 *   ⭐ 为什么不干脆全自动：上级平台对新节点编号有时有约定（指定号段），锁死就没法适配。
 * ⛔ 编辑已有节点时 ID **只读**：这个编号可能已经推送给上级平台了，改了等于换了一个节点。
 */
const nodeDialog = reactive({
  visible: false,
  mode: "create" as "create" | "edit",
  kind: "group" as BusinessNode["kind"],
  parentCode: "",
  targetCode: "",
  name: "",
  code: ""
});

const nodeDialogTitle = computed(() =>
  nodeDialog.mode === "edit"
    ? nodeDialog.kind === "group"
      ? "编辑业务分组"
      : "编辑虚拟组织"
    : nodeDialog.kind === "group"
      ? "新建业务分组"
      : "新建虚拟组织"
);

/** 弹窗里的实时校验，返回空串即通过。⛔ 提交时必须再走一遍 —— 不能只靠按钮禁用来兜。 */
const nodeDialogError = computed(() => {
  const name = nodeDialog.name.trim();
  if (!name) return "分组名称不能为空。";
  if (name.length > 128) return "分组名称最多 128 个字符。";
  const code = nodeDialog.code.trim();
  if (!/^\d{20}$/.test(code)) return "国标 ID 必须是 20 位数字。";
  const expectedType = nodeDialog.kind === "group" ? "215" : "216";
  if (code.slice(10, 13) !== expectedType) {
    return `国标 ID 第 11–13 位必须是 ${expectedType}（${nodeDialog.kind === "group" ? "业务根" : "虚拟组织"}类型码）。`;
  }
  if (nodes.value.some(node => node.code === code && node.code !== nodeDialog.targetCode)) return "该编号已被其它节点占用。";
  if (reservedCodes().includes(code)) return "该编号与本平台、上级平台或已共享通道编号冲突。";
  if (nodeDialog.kind === "organization" && !nodes.value.some(node => node.code === nodeDialog.parentCode)) {
    return "请选择上级组织。";
  }
  return "";
});

/** 上级组织选择器里的一个树节点（弹窗上 `a-tree-select` 的 `field-names` 就是照这个形状配的）。 */
type ParentOption = { code: string; label: string; children: ParentOption[] };

/**
 * 弹窗里「上级组织」的**分层**可选项（2026-10-15 老板要求：换成树组件分层级的那种）。
 *
 * ⛔ 可见集合与旧版扁平下拉**完全一致**：编辑时剔除「自己 + 全部后代」（`allowedParents`）——
 *   漏了这一步就能把一棵子树挂到自己的后代底下，形成环、把目录树写坏。
 * ⛔ 排序口径照抄左侧目录树（`sort` → 编号）：同一个组织在左右两处的位置必须一致。
 * ⭐ 父节点不在可见集合里就**当根处理**（本场景被剔的是一整棵子树、不会留下孤儿，
 *   这里只是兜底 —— 万一以后改了剔除口径，宁可让它升到顶层，也别整支消失）。
 */
const nodeDialogParentTree = computed<ParentOption[]>(() => {
  const editing = nodeDialog.targetCode ? nodes.value.find(node => node.code === nodeDialog.targetCode) : undefined;
  const allowed = editing ? allowedParents(editing) : nodes.value;
  const sortOf = new Map(allowed.map(node => [node.code, node.sort]));
  const entries = new Map(
    allowed.map(node => [
      node.code,
      { parentCode: node.parentCode, option: { code: node.code, label: nodeLabel(node), children: [] as ParentOption[] } }
    ])
  );
  const roots: ParentOption[] = [];
  entries.forEach(entry => {
    const parent = entries.get(entry.parentCode);
    (parent ? parent.option.children : roots).push(entry.option);
  });
  const sortByTreeOrder = (list: ParentOption[]) => {
    list.sort(
      (left, right) => (sortOf.get(left.code) ?? 0) - (sortOf.get(right.code) ?? 0) || left.code.localeCompare(right.code)
    );
    list.forEach(option => sortByTreeOrder(option.children));
  };
  sortByTreeOrder(roots);
  return roots;
});

function openNodeDialog(seed: {
  mode: "create" | "edit";
  kind: BusinessNode["kind"];
  parentCode: string;
  targetCode?: string;
  name?: string;
  code?: string;
}) {
  const suggested = seed.code || createBusinessNodeCode(props.localDeviceId, seed.kind, nodes.value, reservedCodes());
  if (!suggested) {
    validationError.value =
      seed.kind === "group"
        ? "无法根据本平台设备 ID 生成业务根编码，请先检查本平台身份。"
        : "无法生成虚拟组织编码，请检查本平台身份或已用编号。";
    return;
  }
  Object.assign(nodeDialog, {
    visible: true,
    mode: seed.mode,
    kind: seed.kind,
    parentCode: seed.parentCode,
    targetCode: seed.targetCode || "",
    name: seed.name ?? (seed.kind === "group" ? "新建业务分组" : "新建虚拟组织"),
    code: suggested
  });
  validationError.value = "";
  saveError.value = "";
}

function openCreateRootDialog() {
  if (!props.canWrite || saving.value || channelPickerVisible.value || rootNode.value) return;
  openNodeDialog({ mode: "create", kind: "group", parentCode: "" });
}

function openCreateChildDialog(node: BusinessNode) {
  if (!props.canWrite || saving.value || channelPickerVisible.value) return;
  openNodeDialog({ mode: "create", kind: "organization", parentCode: node.code });
}

function openEditNodeDialog(node: BusinessNode) {
  if (saving.value || channelPickerVisible.value) return;
  openNodeDialog({
    mode: "edit",
    kind: node.kind,
    parentCode: node.parentCode,
    targetCode: node.code,
    name: node.name,
    code: node.code
  });
}

function submitNodeDialog() {
  if (!props.canWrite || saving.value || nodeDialogError.value) return;
  const name = nodeDialog.name.trim();
  const code = nodeDialog.code.trim();
  if (nodeDialog.mode === "edit") {
    const node = nodes.value.find(item => item.code === nodeDialog.targetCode);
    if (!node) return;
    node.name = name;
    if (node.kind === "organization") node.parentCode = nodeDialog.parentCode;
  } else if (nodeDialog.kind === "group") {
    if (rootNode.value) return; // 国标：业务根有且只有一个
    nodes.value.push({ code, name, kind: "group", parentCode: "", sort: 0 });
    expandedNodeCodes.value.push(code);
  } else {
    const parent = nodes.value.find(item => item.code === nodeDialog.parentCode);
    if (!parent) return;
    if (!expandedNodeCodes.value.includes(parent.code)) expandedNodeCodes.value.push(parent.code);
    nodes.value.push({
      code,
      name,
      kind: "organization",
      parentCode: parent.code,
      sort: nodes.value.filter(item => item.parentCode === parent.code).length
    });
  }
  selectedNodeCode.value = code;
  nodeDialog.visible = false;
  validationError.value = "";
  saveError.value = "";
}

/** 树节点上的删除入口：拦一道"能不能删"再落到二次确认（确认条复用 `deleteNode`）。 */
function requestDeleteNode(node: BusinessNode) {
  deleteNode(node.code);
}

function allowedParents(node: BusinessNode) {
  const excluded = descendantCodes(nodes.value, node.code);
  return nodes.value.filter(candidate => !excluded.has(candidate.code));
}

function nodeHasContents(code: string) {
  return nodes.value.some(node => node.parentCode === code) || bindings.value.some(binding => binding.nodeCode === code);
}

function nodeDeleteReason(node: BusinessNode): string {
  if (node.kind === "group" && publishGroup.value) return "请先切换为通道共享并保存。";
  return nodeHasContents(node.code) ? "请先移动下级组织或直属通道。" : "";
}

function setNodeMenu(code: string, visible: boolean) {
  if (!visible) {
    if (contextMenuCode.value === code) contextMenuCode.value = "";
    return;
  }
  if (saving.value || channelPickerVisible.value || sharingMode.value !== "directory") return;
  selectScope(code);
  contextMenuCode.value = code;
}

/**
 * 删除组织节点 —— 走 confirm 弹窗二次确认（2026-10-15 起页面内不再有内嵌确认条）。
 * 业务根（215）被禁止删除：它承载整棵共享目录树，`publishGroup` 开着时还会对外发布。
 */
function deleteNode(code: string) {
  if (!props.canWrite || nodeHasContents(code)) return;
  if (nodes.value.find(node => node.code === code)?.kind === "group" && publishGroup.value) {
    validationError.value = "发布开启时不能删除业务根，请先关闭业务分组发布。";
    return;
  }
  Modal.confirm({
    title: "删除组织",
    content: `确认删除空组织“${nodePath(code)}”？`,
    okText: "确认删除",
    cancelText: "取消",
    okButtonProps: { status: "danger" },
    onOk: () => removeNode(code)
  });
}

function removeNode(code: string) {
  nodes.value = nodes.value.filter(node => node.code !== code);
  selectedNodeCode.value = ALL_SHARED_NODE;
  validationError.value = "";
}

function bindingCount(code: string) {
  return counts.value.directByNode.get(code) || 0;
}

function descendantBindingCount(code: string) {
  return counts.value.inclusiveByNode.get(code) || 0;
}

function nodePath(code: string) {
  return (
    businessNodePath(nodes.value, code)
      .map(node => businessNodeLabel(nodes.value, node))
      .filter(Boolean)
      .join(" / ") || "未分组通道"
  );
}

function nodeLabel(node: BusinessNode) {
  return businessNodeLabel(nodes.value, node);
}

/**
 * 图标名 → 组件。`data-icon` 用的也是这个键（`folder-tree` / `folder-open` / `folder`），
 * 测试直接断它，不依赖 lucide 自己往 svg 上写的类名。
 */
const NODE_ICONS = { "folder-tree": FolderTree, "folder-open": FolderOpen, folder: Folder };

/**
 * 树节点图标（2026-10-14 老板要求「在节点上加上对应的图标」）。
 *
 * ⭐ 语义：业务根（国标 215）= 整棵共享目录树的**根**，固定用 `FolderTree`，不随展开变化；
 *   虚拟组织（国标 216）= 下级**目录**，折叠 `Folder` / 展开 `FolderOpen`。
 *   （正好对应"业务分组根 / 虚拟组织"这组国标概念，图标一眼能分出层次。）
 *
 * ⛔ 图标只能落在 `#title` 插槽里，**不能**改用 Arco 的 `#icon` 插槽：那个插槽只收到
 *   `nodeStatus`（loading / checked / selected / expanded / isLeaf），**拿不到 kind / code**；
 *   而业务根和虚拟组织都可能是"有子节点或者没子节点"，靠 isLeaf 会把两类显示成同一个图标。
 */
function nodeIconName(node: BusinessNode): keyof typeof NODE_ICONS {
  if (node.kind === "group") return "folder-tree";
  return expandedNodeCodes.value.includes(node.code) ? "folder-open" : "folder";
}

function reservedCodes() {
  return [
    props.localDeviceId,
    props.platform?.upstreamServerId || "",
    ...channels.value.map(channel => channel.publishedChannelId)
  ];
}

function sourceChannelId(channelId: number) {
  return `通道记录 #${channelId}`;
}

function bindingNodeCode(channelId: number) {
  return bindings.value.find(binding => binding.sourceChannelId === channelId)?.nodeCode || "";
}

function sourceChannelNumber(channel: CascadeChannelProjection) {
  return pendingShareChannelIds.value.includes(channel.sourceChannelId)
    ? channel.publishedChannelId || "暂无源编号"
    : sourceChannelId(channel.sourceChannelId);
}

function channelBindingPath(channelId: number) {
  const code = bindingNodeCode(channelId);
  return code ? nodePath(code) : "未分组";
}

/** 在线状态列的语义色：只有"确定在线"才给绿色，离线/未知一律灰（颜色不撒谎，文案才是判据）。 */
function channelOnlineStatus(row: CascadeChannelProjection) {
  return channelStatusById.value[row.sourceChannelId] === 1 ? ("success" as const) : ("normal" as const);
}

/** 在线状态文案：`1`=在线 / `0`=离线 / 取不到=「未知」（按 `device_status` 字典出词）。 */
function channelOnlineLabel(row: CascadeChannelProjection) {
  return channelStatusLabel(channelStatusById.value[row.sourceChannelId]);
}

function selectSharingMode(mode: string | number) {
  if (mode !== "channel" && mode !== "directory") return;
  if (saving.value || channelPickerVisible.value) return;
  if (sharingMode.value === "directory") directoryScopeCode.value = selectedNodeCode.value;
  sharingMode.value = mode;
  selectScope(
    mode === "channel"
      ? ALL_SHARED_NODE
      : nodes.value.some(node => node.code === directoryScopeCode.value)
        ? directoryScopeCode.value
        : nodes.value.find(node => node.kind === "group")?.code || ALL_SHARED_NODE
  );
  validationError.value = "";
}

function selectScope(code: string) {
  contextMenuCode.value = "";
  selectedNodeCode.value = code;
  selectedChannelIds.value = [];
  channelPage.value = 1;
  moveDialog.visible = false;
}

function filterChannels(source: CascadeChannelProjection[], query: string) {
  const normalized = query.trim().toLocaleLowerCase();
  if (!normalized) return source;
  return source.filter(item =>
    `${item.name} ${item.sourceChannelId} ${item.publishedChannelId}`.toLocaleLowerCase().includes(normalized)
  );
}

function onChannelSearch() {
  channelPage.value = 1;
  selectedChannelIds.value = [];
  Message.info("搜索条件已变更，已清除之前的通道选择。");
}

function toggleChannelSelection(channelId: number) {
  selectedChannelIds.value = selectedChannelIds.value.includes(channelId)
    ? selectedChannelIds.value.filter(id => id !== channelId)
    : [...selectedChannelIds.value, channelId];
}

function toggleVisibleSelection(rows: CascadeChannelProjection[]) {
  const ids = rows.map(item => item.sourceChannelId);
  const allSelected = ids.length > 0 && ids.every(id => selectedChannelIds.value.includes(id));
  selectedChannelIds.value = allSelected
    ? selectedChannelIds.value.filter(id => !ids.includes(id))
    : [...new Set([...selectedChannelIds.value, ...ids])];
}

function openChannelPicker() {
  if (!props.canWrite || (sharingMode.value === "directory" && selectedNode.value?.kind !== "organization")) {
    if (sharingMode.value === "directory") validationError.value = "按照目录共享时，请先选择虚拟组织作为通道归属。";
    return;
  }
  pickerTargetCode.value = selectedNode.value?.kind === "organization" ? selectedNode.value.code : "";
  channelPickerVisible.value = true;
}

function closeChannelPicker() {
  channelPickerVisible.value = false;
}

function applyChannelPicker(rows: CascadeChannelProjection[]) {
  if (!props.canWrite || saving.value || !channelPickerVisible.value) return;
  const replacements = new Map(rows.map(row => [row.sourceChannelId, { ...row, active: true }]));
  const additions = rows.filter(row => {
    const existing = channels.value.find(channel => channel.sourceChannelId === row.sourceChannelId);
    return !existing || existing.active === false;
  });
  const replacedIds = new Set(additions.map(row => row.sourceChannelId));
  const nextChannels = [
    ...channels.value.filter(channel => !replacedIds.has(channel.sourceChannelId)),
    ...additions.map(row => replacements.get(row.sourceChannelId)!)
  ];
  let nextBindings = bindings.value;
  if (pickerTargetCode.value) {
    const result = assignBusinessBindings(
      nodes.value,
      bindings.value,
      nextChannels,
      rows.map(row => row.sourceChannelId),
      pickerTargetCode.value
    );
    if (result.errors.length) {
      validationError.value = result.errors[0];
      return;
    }
    nextBindings = result.bindings;
  }
  channels.value = nextChannels;
  bindings.value = nextBindings;
  selectedNodeCode.value = pickerTargetCode.value || ALL_SHARED_NODE;
  selectedChannelIds.value = [];
  channelPickerVisible.value = false;
  validationError.value = "";
  saveError.value = "";
}

/**
 * 批量移动：弹窗选择并确认目标后更新草稿，保存时再提交。
 * ⛔ 归属落库前必须过 `assignBusinessBindings` 校验：它可能因为"目标是业务根"之类的规则报错，
 *   报错时只把错误挂到页面顶部，**不能**把 `bindings` 改坏。
 */
function prepareMove(channelIds: number[]) {
  if (!channelIds.length || !props.canWrite || saving.value || !organizationNodes.value.length) return;
  moveDialog.channelIds = [...channelIds];
  moveDialog.targetCode = "";
  moveDialog.visible = true;
}

function submitMove() {
  if (!props.canWrite || saving.value || !moveDialog.targetCode) return;
  const result = assignBusinessBindings(
    nodes.value,
    bindings.value,
    channels.value,
    moveDialog.channelIds,
    moveDialog.targetCode
  );
  if (result.errors.length) {
    validationError.value = result.errors[0];
    return;
  }
  bindings.value = result.bindings;
  selectedNodeCode.value = moveDialog.targetCode;
  selectedChannelIds.value = [];
  moveDialog.visible = false;
  validationError.value = "";
  saveError.value = "";
}

function moveOut(channelIds: number[]) {
  if (!props.canWrite || !channelIds.length) return;
  bindings.value = removeBusinessBindings(bindings.value, channelIds);
  selectedChannelIds.value = [];
  validationError.value = "";
  saveError.value = "";
}

/**
 * 取消共享 —— confirm 确认后**只改草稿**（真正停止共享要等保存），所以确认键文案明说"加入草稿"。
 */
function requestUnshare(channelIds: number[]) {
  if (!props.canWrite) return;
  const activeIds = new Set(channels.value.map(channel => channel.sourceChannelId));
  const ids = [...new Set(channelIds)].filter(id => activeIds.has(id) && !unshareChannelIds.value.includes(id));
  if (!ids.length) return;
  Modal.confirm({
    title: "取消共享",
    content: `保存后停止向该上级共享 ${ids.length} 路通道，并移除其业务归属。`,
    okText: "加入取消共享草稿",
    cancelText: "保留共享",
    okButtonProps: { status: "danger" },
    onOk: () => confirmUnshare(ids)
  });
}

function confirmUnshare(ids: number[]) {
  const target = new Set(ids);
  bindings.value = bindings.value.filter(binding => !target.has(binding.sourceChannelId));
  channels.value = channels.value.filter(channel => !target.has(channel.sourceChannelId));
  selectedChannelIds.value = selectedChannelIds.value.filter(id => !target.has(id));
  validationError.value = "";
}

/**
 * 「有未保存修改」confirm 弹窗（2026-10-15 老板要求：页面内嵌的确认条一律改成 confirm 组件）。
 *
 * 离开时可继续编辑、放弃草稿，或保存后继续原动作。
 */
function confirmExit(action: "close" | "push" | "route") {
  pendingExit.value = action;
  const dialog = Modal.confirm({
    title: "有未保存修改",
    width: "min(520px, calc(100vw - 32px))",
    content: "当前有未保存修改。可以保存后离开、放弃本次修改，或继续编辑。",
    okText: "保存并离开",
    cancelText: "继续编辑",
    onOk: () => saveAndExit(),
    onCancel: () => abortExit(),
    footer: () => [
      h(
        AButton,
        {
          onClick: () => {
            dialog.close();
            abortExit();
          }
        },
        () => "继续编辑"
      ),
      h(
        AButton,
        {
          type: "outline",
          status: "warning",
          "data-action": "discard-and-leave",
          onClick: () => {
            dialog.close();
            discardAndExit();
          }
        },
        () => "放弃并离开"
      ),
      h(
        AButton,
        {
          type: "primary",
          onClick: () => {
            dialog.close();
            void saveAndExit();
          }
        },
        () => "保存并离开"
      )
    ]
  });
}

function discardAndExit() {
  const action = pendingExit.value;
  nodes.value = baselineNodes.map(node => ({ ...node }));
  bindings.value = baselineBindings.map(binding => ({ ...binding }));
  channels.value = baselineChannels.value.map(channel => ({ ...channel }));
  sharingMode.value = publishGroup.value ? "directory" : "channel";
  directoryScopeCode.value = nodes.value.find(node => node.kind === "group")?.code || ALL_SHARED_NODE;
  selectScope(sharingMode.value === "directory" ? directoryScopeCode.value : ALL_SHARED_NODE);
  validationError.value = "";
  if (!resourceSyncPending.value) saveError.value = "";
  continuePendingAction(action);
}

function beginExit(action: "close" | "push") {
  if (saving.value || channelPickerVisible.value) return;
  if (dirty.value) {
    confirmExit(action);
    return;
  }
  continuePendingAction(action);
}

function requestExit(): Promise<boolean> {
  if (saving.value || channelPickerVisible.value) return Promise.resolve(false);
  if (!dirty.value) return Promise.resolve(true);
  return new Promise(resolve => {
    exitResolver = resolve;
    confirmExit("route");
  });
}

/** 用户在弹窗里选了"继续编辑"（或点了 ×）⇒ 什么都不做地留下。 */
function abortExit() {
  if (pendingExit.value === "route") exitResolver?.(false);
  exitResolver = null;
  pendingExit.value = "";
}

function continuePendingAction(action: "close" | "push" | "route" | "") {
  pendingExit.value = "";
  if (action === "close") emit("update:visible", false);
  else if (action === "push") void confirmPushCatalog();
  else if (action === "route") exitResolver?.(true);
  exitResolver = null;
}

async function saveAndExit() {
  // 保存失败就留在页面上（错误已显示在页面顶部），此时**不能**清掉 pendingExit 之外的草稿状态
  if (!(await save())) {
    pendingExit.value = "";
    exitResolver = null;
    return;
  }
  continuePendingAction(pendingExit.value);
}

async function confirmPushCatalog() {
  if (!platformEnabled.value) {
    Message.warning("上级平台已停用，启用后才能推送目录。");
    return;
  }
  Modal.confirm({
    title: "推送上级平台目录",
    content: `${publishGroup.value ? "将推送该上级的完整目录。" : "业务分组发布已关闭，本次不会包含业务分组。"}此操作不会确认上级已展示。`,
    okText: "推送目录",
    cancelText: "取消",
    onOk: async () => {
      try {
        const response: any = await pushCascadeCatalog(props.platformId);
        const result = unwrap<{ items: number; batches: number }>(response);
        Message.success(`目录推送请求已完成：${result.items ?? 0} 项，${result.batches ?? 0} 批。此结果不代表上级已展示。`);
      } catch (error: any) {
        Message.error(`目录推送失败：${requestErrorMessage(error) || "请稍后重试"}`);
      }
    }
  });
}

async function save(): Promise<boolean> {
  if (!snapshot.value || !props.canWrite || saving.value || revisionConflict.value) return false;
  const targetPlatformId = props.platformId;
  const requestSequence = loadSequence;
  const errors = businessNodeErrors(nodes.value, bindings.value, channels.value, reservedCodes());
  if (errors.length) {
    validationError.value = errors[0];
    return false;
  }
  if (modeChanged.value && !props.canManage) {
    validationError.value = "当前账号没有切换共享方式的管理权限。";
    return false;
  }
  if (sharingMode.value === "directory") {
    if (!nodes.value.some(node => node.kind === "group")) {
      validationError.value = "按照目录共享前，请先新建业务根和虚拟组织。";
      return false;
    }
  }
  saving.value = true;
  saveError.value = "";
  validationError.value = "";
  try {
    const result = unwrap<CascadeBusinessGroup>(
      await replaceCascadeBusinessGroup(targetPlatformId, {
        nodes: nodes.value.map(node => ({ ...node })),
        bindings: bindings.value.map(binding => ({ ...binding })),
        expectedProjectionRevision: snapshot.value.revision,
        expectedConfigRevision: configRevision.value,
        publishGroup: sharingMode.value === "directory",
        resourceChanges: { ...resourceChanges.value }
      })
    );
    if (
      !props.visible ||
      requestSequence !== loadSequence ||
      targetPlatformId !== props.platformId ||
      result.platformId !== targetPlatformId
    )
      return false;
    snapshot.value = result;
    publishGroup.value = result.publishGroup ?? publishGroup.value;
    configRevision.value = result.configRevision ?? configRevision.value;
    if (platformState.value) {
      platformState.value = {
        ...platformState.value,
        publishGroup: publishGroup.value,
        configRevision: configRevision.value
      };
      emit("publishGroupUpdated", platformState.value);
    }
    sharingMode.value = publishGroup.value ? "directory" : "channel";
    nodes.value = result.nodes.map(node => ({ ...node }));
    bindings.value = result.bindings.map(binding => ({ ...binding }));
    channels.value = result.channels.map(channel => ({ ...channel }));
    baselineChannels.value = result.channels.map(channel => ({ ...channel }));
    baselineNodes = nodes.value.map(node => ({ ...node }));
    baselineBindings = bindings.value.map(binding => ({ ...binding }));
    selectedNodeCode.value =
      selectedNodeCode.value === ALL_SHARED_NODE || nodes.value.some(node => node.code === selectedNodeCode.value)
        ? selectedNodeCode.value
        : ALL_SHARED_NODE;
    emit("saved", result);
    resourceSyncPending.value = result.runtimeSynced === false;
    if (resourceSyncPending.value) {
      saveError.value = `共享与目录已保存，但运行时同步失败：${result.warning || "请稍后重试资源同步"}`;
      // 数据已经存下了，但运行时没同步上 ⇒ 视为"没能干净地离开"，把 route 守卫退回 false 并留在页面上
      abortExit();
      return false;
    }
    Message.success("共享与目录配置已保存；可按需推送平台目录。");
    return true;
  } catch (error: any) {
    if (!props.visible || requestSequence !== loadSequence || targetPlatformId !== props.platformId) return false;
    const message = requestErrorMessage(error);
    const isConflict =
      error?.response?.status === 409 || message.toLowerCase().includes("revision conflict") || message.includes("资源状态冲突");
    if (isConflict) {
      revisionConflict.value = true;
      saveError.value = "数据已被其他操作修改，当前草稿已保留。请重新读取最新目录后再决定如何调整。";
    } else if (message) {
      saveError.value = `${message}（当前草稿已保留）`;
    } else {
      saveError.value = "共享与目录保存失败，当前草稿已保留。";
    }
    return false;
  } finally {
    saving.value = false;
  }
}

/** 版本冲突时工具栏那颗 `重新读取最新目录` —— 会丢草稿，所以先 confirm。 */
function requestReloadLatest() {
  Modal.confirm({
    title: "重新读取最新目录",
    content: "重新读取将放弃当前草稿，是否继续？",
    okText: "放弃草稿并重载",
    cancelText: "继续编辑",
    okButtonProps: { status: "warning" },
    onOk: () => reloadLatest()
  });
}

async function reloadLatest() {
  const selectedCode = selectedNodeCode.value;
  await load();
  if (nodes.value.some(node => node.code === selectedCode)) selectedNodeCode.value = selectedCode;
}

function allowBrowserLeave(event: BeforeUnloadEvent) {
  if (!dirty.value) return;
  event.preventDefault();
  event.returnValue = "";
}

onMounted(() => window.addEventListener("beforeunload", allowBrowserLeave));
onUnmounted(() => window.removeEventListener("beforeunload", allowBrowserLeave));
defineExpose({ requestExit });
</script>

<style scoped>
.business-group-manager,
.business-group-content {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
}
.business-group-content {
  width: 100%;
}
.business-group-toolbar {
  display: flex;
  flex: none;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px;
  margin-bottom: 12px;
  background: var(--color-fill-1);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 10px;
}
.business-group-sharing-tabs {
  flex: none;
  width: auto;
}
.business-group-sharing-tab-title {
  display: inline-flex;
  gap: 6px;
  align-items: center;
}
.business-group-sharing-tabs :deep(.arco-tabs-nav) {
  margin-bottom: 0;
}
.business-group-sharing-tabs :deep(.arco-tabs-content) {
  display: none;
}
.business-group-platform-actions,
.business-group-tree-actions,
.business-group-node-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}
.business-group-platform-actions .arco-btn {
  min-width: 88px;
  height: 40px;
  padding: 0 15px;
  border-radius: 8px;
  transition:
    background-color 0.18s ease,
    border-color 0.18s ease,
    box-shadow 0.18s ease,
    color 0.18s ease;
}

/**
 * ⛔ 网格只给了列模板、行是隐式 `auto` ⇒ 行还是按**内容**高度算（明细面板撑多高就多高），
 *   下面的定高链到这里会再次断掉。显式写 `minmax(0, 1fr)`：行吃满容器，且允许收缩到 0。
 *   （`1fr` 的最小值默认也是 auto，只写 `1fr` 照样被内容顶开，必须 `minmax(0, …)`。）
 */
.business-group-workspace {
  display: grid;
  flex: 1;
  grid-template-rows: minmax(0, 1fr);
  grid-template-columns: 340px minmax(0, 1fr);
  gap: 16px;
  min-height: 430px;
}
.business-group-workspace.is-channel-mode {
  grid-template-columns: minmax(0, 1fr);
}
.business-group-tree-panel.is-mode-hidden {
  display: none;
}
.business-group-tree-panel {
  min-width: 0;
  padding: 14px;
}
.business-group-detail {
  display: flex;
  flex-direction: column;
  min-width: 0;

  /** ⛔ 网格子项默认 `min-height: auto`，会被表格内容顶高、把定高链顶断；与上方 grid-template-rows 配套。 */
  min-height: 0;
  padding: 0;
}

/** 空态：还没有业务分组根节点 —— 左侧整块让位给这句引导 + 一颗主按钮。 */
.business-group-tree-empty {
  display: flex;
  flex-direction: column;
  gap: 10px;
  align-items: center;
  padding: 16px 14px;
  margin: auto 0;
  text-align: center;
  background: var(--uvp-panel-bg);
  border: 1px dashed var(--uvp-panel-border);
  border-radius: 10px;
}
.business-group-tree-empty__title {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  color: var(--uvp-text-primary);
}
.business-group-tree-empty__hint {
  margin: 0;
  font-size: 12px;
  line-height: 1.6;
  color: var(--uvp-text-tertiary);
}
.business-group-tree {
  max-height: 55vh;
  margin-top: 12px;
  overflow: auto;
}

/**
 * 行背景左边缘与搜索框左边缘对齐（2026-10-15 老板要求「参照设备列表那棵树，左边和输入框对齐」）。
 *
 * ⛔ 问题出在 Arco 的行结构：`[缩进 indent][展开箭头 switcher][标题块 title]` 是**三个平级兄弟**，
 *   而 `block-node` 让 title 撑满整行 ⇒ 高亮背景块只能从**箭头右边**开始，
 *   比搜索框左边缘整整右了 23px（真机 1440：搜索框 308 / 根节点 title 331）。
 *   设备权限工作台那棵树没开 `block-node`、背景只有内容宽，所以看不出这个毛病；
 *   我们这棵必须保留整行背景（三颗行内按钮要钉在行尾），只能反过来把背景块**拉出去**。
 *
 * ⛔ 两条声明必须成对，且**和恒为 2px**（现在是 `-27px + 29px`；原态是 Arco 自带的 `-4px + 6px`）：
 *   `margin-left` 把背景块左移 23px 去贴搜索框，`padding-left` 再把内容补回同样的 23px ⇒
 *   图标 / 名字 / 计数 / 按钮**一个像素都不动**（真机实测图标恒在 337，改前改后一致）。
 *   只改其中一条 = 整行内容跟着漂（试过不加 padding-left：图标从 337 跑到 314）。
 *
 * ⛔ `.arco-tree-node-switcher` 那条**不是**可选的观感修饰：背景块左移之后正好压住箭头，
 *   而 title 在 DOM 里排在 switcher 后面（同层绘制在后 = 盖在上面）⇒ 不加 `position: relative; z-index`
 *   的话，点在箭头上的鼠标事件会被 title 吃掉，**展开 / 折叠直接点不动**，只剩点标题选中。
 */
.business-group-tree :deep(.arco-tree-node-title) {
  padding-left: 29px;
  margin-left: -27px;
}
.business-group-tree :deep(.arco-tree-node-switcher) {
  position: relative;
  z-index: 1;
}

/**
 * ⛔ 想让三颗按钮**统一钉在行尾**，必须把 Arco 自己套的两层容器都松开 —— 少一层都不行：
 *   ① `.arco-tree-node-title-text`：`#title` 插槽外面还有这一层，默认只有内容那么宽 ⇒
 *      我们的 `width: 100%` 只能拿到**它**的宽度，于是每行各按名字长短给自己对齐，
 *      按钮左一个右一个（真机量到 groupRight = 492 / 503 / 561）。
 *      给它 `flex: 1` 才会吃满整行，配合按钮那侧的 `margin-left: auto` 贴到行尾。
 *   ② `.arco-tree-node-title-block`：`flex: 1` 却没配 `min-width: 0` ⇒ 它的自动最小尺寸 =
 *      内容的 min-content = 名字整行宽（`white-space: nowrap`，`overflow: hidden` 改不了**固有宽度**）
 *      ⇒ 名字一长整行撑出面板、按钮被推到可视区外（真机量到 `rightGap = -237px`，按钮完全看不见）。
 *   两条都补上，才是「名字压出省略号 + 按钮钉在行尾」。（`:deep(.arco-tree-*)` 在本仓有先例：
 *   `device-assignment` / `select-department`。）
 */
.business-group-tree :deep(.arco-tree-node-title-block),
.business-group-tree :deep(.arco-tree-node-title-text) {
  min-width: 0;
}
.business-group-tree :deep(.arco-tree-node-title-text) {
  flex: 1;
}

/**
 * ⛔ 别删 `min-width: 0`：它是名字能被压出省略号的前提（见上面 ①②）。
 */
.business-group-tree-title {
  display: flex;
  gap: 8px;
  align-items: center;
  width: 100%;
  min-width: 0;
}

/**
 * 节点图标（2026-10-14）：业务根 = 橙，虚拟组织 = 青。
 * 两个颜色直接沿用 `device-assignment` 部门树的 `--branch` / `--leaf`，同级三棵树保持同一套观感。
 * ⛔ **不要**给这里加 `:deep()`：图标是同一个模板渲染出的元素，scoped 属性本来就落在 svg 上，
 *   套 `:deep()` 反倒把特异性抬上去、压过下面那条"悬停/选中走品牌色"的规则。
 * ⛔ 颜色必须比全局 `.uvp-tree-node-icon`（默认灰）特异性高，否则两类节点全是灰的。
 */
.business-group-tree-icon {
  flex: none;
}
.business-group-tree-icon--group {
  color: #c47a18;
}
.business-group-tree-icon--organization {
  color: #0f8b83;
}

/** 悬停 / 选中统一走品牌色 —— 与全局 `.uvp-tree-node-icon` 的三态保持一致（这里只是把特异性补回来）。 */
.business-group-tree :deep(.arco-tree-node-title:hover .business-group-tree-icon),
.business-group-tree :deep(.arco-tree-node-selected .business-group-tree-icon) {
  color: var(--uvp-brand-strong);
}

/**
 * 暗色：两个色在深底上偏闷，各提亮一档。
 * ⛔ 必须有这两条 —— 全局暗色兜底 `body[arco-theme="dark"] .uvp-tree-node-icon` 会把两个修饰类一起盖成灰；
 *   同时它们的特异性要**低于**上面那条悬停/选中规则，暗色下选中才仍然会变品牌色。
 * ⛔⛔ `:global(...)` 必须把**整段选择器**包进去。只让它包住 `body[arco-theme="dark"]`、后面接着写
 *   `.foo :deep(.bar) { … }`（本仓另外几个面板就是这么写的），会被 scoped 编译器**悄悄截断**成
 *   `body[arco-theme="dark"] { … }` —— 后半截选择器整个丢掉，规则与声明全挂在 `<body>` 上
 *   （真机 `document.styleSheets` 里就是这条），看起来"写了暗色适配"、实际一个像素都不生效，
 *   而且**不会报任何错**（2026-10-14 真机抓到的）。
 */
:global(body[arco-theme="dark"] .business-group-tree .business-group-tree-icon--group) {
  color: #e0a355;
}
:global(body[arco-theme="dark"] .business-group-tree .business-group-tree-icon--organization) {
  color: #3fb3a8;
}
.business-group-tree-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/** 计数标签不参与伸缩：否则名字一长，先被挤扁的是它，数字会折行。 */
.business-group-tree-title > .arco-tag {
  flex: none;
}

/**
 * 树节点上的行内操作按钮：**常显**（2026-10-14 老板要求「需要本身就显示不要悬浮出现」）。
 * ⛔ 别再改回「`opacity: 0` + `:hover` 显形」那套：藏起来的按钮等于告诉用户"这一行没有操作"，
 *   而树的右键菜单本来就不显眼，两个入口都藏着就没人找得到。
 * ⛔ `margin-left: auto` 把它推到行尾、`flex: none` 防止名称一长就被挤没。
 * ⚠️ 名字能不能压出省略号**不取决于这里**（别以为写了 `flex: none` 就万事大吉）——
 *   真正卡住宽度的是上面 `title-block` 缺的那个 `min-width: 0`。
 */
.business-group-tree-buttons {
  display: inline-flex;
  flex: none;
  gap: 2px;
  align-items: center;
  padding-left: 6px;
  margin-left: auto;
}
.business-group-tree-button {
  flex: none;
  padding: 0 4px;
}
.business-group-panel-heading {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}
.business-group-panel-heading > div:first-child {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
}
.business-group-panel-heading strong {
  font-size: 15px;
}
.business-group-panel-heading span {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
  overflow-wrap: anywhere;
}

/** 新建 / 编辑节点的弹窗表单：竖排「标签 + 输入」，校验不过时最下面补一行红字。 */
.business-group-node-form {
  display: grid;
  gap: 16px;
}
.business-group-node-form label {
  display: grid;
  gap: 8px;
  min-width: 0;
}
.business-group-node-form label > span {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  font-size: 13px;
}
.business-group-node-form small {
  color: var(--uvp-text-tertiary);
}
.business-group-node-form__error {
  margin: 0;
  font-size: 12px;
  color: var(--uvp-danger);
}

/* 目录模式缩窄筛选控件，为直接展示的批量操作留出空间。 */
.business-group-detail.is-directory :deep(.uvp-search-panel__surface) {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}
.business-group-detail.is-directory :deep(.uvp-search-panel__main) {
  flex: 0 1 auto;
  min-width: 0;
}
.business-group-detail.is-directory :deep(.uvp-search-panel__fields) {
  flex-wrap: nowrap;
  gap: 8px;
}
.business-group-detail.is-directory :deep(.uvp-search-panel__fields > .arco-input-wrapper) {
  min-width: 120px;
}
.business-group-detail.is-directory :deep(.uvp-search-panel__extra) {
  flex: none;
  padding-top: 0;
  margin-left: auto;
}

/**
 * ⛔ 竖向滚动条必须落在**表格自己**身上（`:scroll="{ y: '100%' }"`），不是外层容器：
 *   外层 `overflow: auto` 时滚动条贴在面板边上，而且**表头会跟着一起滚走**。
 *   y 传字符串时 Arco 是给表格根节点 `height: 100%`（数字形式是给表体 max-height），
 *   所以根节点保持 flex 定高、`overflow: hidden`，再让内层容器吃满高度即可 —— 
 *   这与播放日志页（`.playback-log-table`）的写法完全一致。
 */
.business-group-channel-table {
  flex: 1;
  min-height: 200px;
  overflow: hidden;
}
.business-group-channel-table :deep(.arco-table-container) {
  height: 100%;
}
.business-group-channel-name,
.business-group-channel-table code {
  display: grid;
  gap: 4px;
}
.business-group-channel-name small {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.business-group-channel-table code {
  font-size: 12px;
  color: var(--uvp-text-secondary);
}
.business-group-pagination {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  margin-top: 12px;
}
.business-group-alert,
.business-group-readonly {
  margin-bottom: 12px;
}

@media (width <= 700px) {
  .business-group-toolbar {
    flex-direction: column;
    align-items: stretch;
  }
  .business-group-workspace {
    grid-template-columns: minmax(0, 1fr);
    min-height: 0;
  }
  .business-group-tree {
    max-height: 240px;
  }
  .business-group-pagination {
    justify-content: flex-start;
  }
}
</style>
