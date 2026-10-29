<template>
  <div class="snow-page cascade-page">
    <div class="snow-inner uvp-page-shell-flat cascade-shell">
      <a-alert v-if="!canView" type="warning" class="cascade-alert"> 无权查看国标级联，请联系管理员分配查看权限。 </a-alert>

      <template v-else>
        <section class="cascade-summary" aria-label="级联平台摘要">
          <article class="cascade-summary-card">
            <span class="cascade-summary-card__icon is-brand"><Building2 :size="19" /></span>
            <div class="cascade-summary-card__content">
              <strong>{{ platforms.length }}</strong>
              <span>平台总数</span>
            </div>
          </article>
          <article class="cascade-summary-card">
            <span class="cascade-summary-card__icon is-enabled"><CircleCheck :size="19" /></span>
            <div class="cascade-summary-card__content">
              <strong>{{ summary.enabled }}</strong>
              <span>已启用</span>
            </div>
          </article>
          <article class="cascade-summary-card">
            <span class="cascade-summary-card__icon is-online"><RadioTower :size="19" /></span>
            <div class="cascade-summary-card__content">
              <strong class="is-online">{{ summary.online }}</strong>
              <span>在线</span>
            </div>
          </article>
          <article class="cascade-summary-card">
            <span class="cascade-summary-card__icon is-attention"><TriangleAlert :size="19" /></span>
            <div class="cascade-summary-card__content">
              <strong :class="{ 'is-attention': summary.attention > 0 }">{{ summary.attention }}</strong>
              <span>需关注</span>
            </div>
          </article>
        </section>

        <a-alert v-if="errorMessage" type="error" class="cascade-alert">{{ errorMessage }}</a-alert>

        <s-layout-search class="cascade-search-panel">
          <template #fields>
            <a-input-search v-model="keyword" allow-clear placeholder="平台名称 / 编码 / 地址" class="cascade-search" />
            <div class="cascade-status-filter" :class="{ 'is-all': statusFilter === 'all' }">
              <a-select v-model="statusFilter">
                <a-option value="all">全部状态</a-option>
                <a-option value="online">在线</a-option>
                <a-option value="offline">离线</a-option>
                <a-option value="disabled">已停用</a-option>
              </a-select>
            </div>
          </template>
          <template #actions>
            <div class="cascade-search-actions">
              <a-button
                class="uvp-page-action-btn uvp-refresh-btn"
                :title="`自动刷新倒计时 ${autoRefreshCountdown} 秒`"
                @click="refresh"
              >
                <template #icon><RefreshCw :size="15" /></template>
                刷新 <span class="refresh-countdown">{{ autoRefreshCountdown }}s</span>
              </a-button>
              <a-button class="uvp-page-action-btn uvp-create-btn" type="primary" :disabled="!canManage" @click="openCreate">
                <template #icon><Plus :size="15" /></template>
                新增上级
              </a-button>
            </div>
          </template>
        </s-layout-search>

        <a-table
          class="uvp-data-table cascade-table"
          row-key="id"
          :data="filteredPlatforms"
          :bordered="false"
          :pagination="false"
          :scroll="{ x: '100%', minWidth: 2130 }"
        >
          <template #columns>
            <a-table-column title="上级平台" :width="220">
              <template #cell="{ record }">
                <div class="entity-cell">
                  <strong>{{ record.name }}</strong>
                  <code>{{ record.upstreamServerId }}</code>
                  <a-tag v-if="record.credentialNeedsReset" color="orange">需重新填写认证密码</a-tag>
                </div>
              </template>
            </a-table-column>
            <a-table-column title="上级地址" :width="190">
              <template #cell="{ record }">
                <div class="entity-cell">
                  <span>{{ record.host }}:{{ record.port }}</span
                  ><small>{{ record.transport }}</small>
                </div>
              </template>
            </a-table-column>
            <a-table-column title="本平台身份" :width="218">
              <template #cell="{ record }">
                <div class="entity-cell">
                  <code>{{ record.localDeviceId }}</code
                  ><small>{{ record.localSipIp }}:{{ record.localSipPort }}</small>
                </div>
              </template>
            </a-table-column>
            <a-table-column title="共享通道" :width="190">
              <template #cell="{ record }">
                <a-link class="shared-channels-link" :disabled="!canView" @click="openSharedDirectory(record)">
                  <ListVideo :size="14" />
                  <span>{{ sharedSummary(record.id) }}</span>
                </a-link>
              </template>
            </a-table-column>
            <a-table-column title="运行状态" :width="150">
              <template #cell="{ record }">
                <a-tooltip :content="cascadeStatus(record).detail">
                  <a-tag :color="cascadeStatus(record).color">{{ cascadeStatus(record).label }}</a-tag>
                </a-tooltip>
              </template>
            </a-table-column>
            <a-table-column title="上级订阅" :width="280">
              <template #cell="{ record }">
                <UpstreamSubscriptions :subscriptions="record.subscriptions" :stale="Boolean(errorMessage)" />
              </template>
            </a-table-column>
            <a-table-column title="启用" :width="86" align="center">
              <template #cell="{ record }">
                <a-tooltip :content="record.enabled ? '已启用，向上级保持注册；关闭即注销' : '已停用，开启后向上级发起注册'">
                  <a-switch
                    size="small"
                    :model-value="record.enabled"
                    :loading="actionId === record.id"
                    :disabled="!canEnable"
                    @change="(value: boolean | string | number) => toggleEnabled(record, Boolean(value))"
                  />
                </a-tooltip>
              </template>
            </a-table-column>
            <a-table-column title="注册 / 心跳周期（秒）" :width="170">
              <template #cell="{ record }">{{ cascadeCycleLabel(record) }}</template>
            </a-table-column>
            <a-table-column title="协议" :width="112">
              <template #cell="{ record }">
                <div class="entity-cell">
                  <span>GB/T {{ record.effectiveVersion || "2016" }}</span
                  ><small>{{ profileLabel(record.profileOverride) }}</small>
                </div>
              </template>
            </a-table-column>
            <a-table-column title="最近注册" :width="180">
              <template #cell="{ record }">{{ formatRelative(record.registerAt) }}</template>
            </a-table-column>
            <a-table-column title="最近心跳" :width="180">
              <template #cell="{ record }">{{ formatRelative(record.heartbeatAt) }}</template>
            </a-table-column>
            <a-table-column title="创建时间" :width="180">
              <template #cell="{ record }">{{ formatRelative(record.createdAt) }}</template>
            </a-table-column>
            <a-table-column title="修改时间" :width="180">
              <template #cell="{ record }">{{ formatRelative(record.updatedAt) }}</template>
            </a-table-column>
            <a-table-column title="操作" :width="340" align="center" :fixed="isMobile ? '' : 'right'">
              <template #cell="{ record }">
                <div class="uvp-table-actions cascade-actions">
                  <a-link
                    class="uvp-table-action uvp-table-action--assign"
                    data-action="shared-directory"
                    :disabled="!canView"
                    @click="openSharedDirectory(record)"
                    ><Building2 :size="14" />共享与目录</a-link
                  >
                  <a-link class="uvp-table-action uvp-table-action--edit" :disabled="!canManage" @click="openEdit(record)"
                    ><Settings2 :size="14" />编辑</a-link
                  >
                  <a-tooltip content="把当前共享给该上级的设备/通道目录主动报送一遍">
                    <a-link
                      class="uvp-table-action uvp-table-action--sync"
                      :disabled="!canShare || !record.enabled"
                      :loading="actionId === record.id"
                      @click="pushCatalog(record)"
                      ><Send :size="14" />推送</a-link
                    >
                  </a-tooltip>
                  <a-link class="uvp-table-action uvp-table-action--delete" :disabled="!canManage" @click="confirmDelete(record)"
                    ><Trash2 :size="14" />删除</a-link
                  >
                </div>
              </template>
            </a-table-column>
          </template>
          <template #empty><a-empty description="暂无符合条件的上级平台" /></template>
        </a-table>
      </template>
    </div>

    <a-modal
      v-model:visible="editorVisible"
      width="min(820px, calc(100vw - 24px))"
      modal-class="uvp-system-dialog cascade-platform-dialog"
      :mask-closable="!saving"
      :closable="!saving"
      :esc-to-close="!saving"
      unmount-on-close
    >
      <template #title>{{ editing ? "编辑上级平台" : "新增上级平台" }}</template>
      <a-form :model="form" layout="vertical" class="cascade-form">
        <a-steps :current="editorStep" class="cascade-editor-steps">
          <a-step title="上级平台" /><a-step title="本平台身份" /><a-step title="注册与协议" /><a-step
            title="目录与发布"
          /><a-step title="完成确认" />
        </a-steps>
        <section v-show="editorStep === 1" class="form-section">
          <header>
            <Building2 :size="17" />
            <h3>上级平台</h3>
          </header>
          <div class="form-grid">
            <a-form-item label="平台名称" required :validate-status="fieldErrors.name ? 'error' : ''" :help="fieldErrors.name">
              <a-input v-model="form.name" maxlength="128" allow-clear @blur="touched.name = true" />
            </a-form-item>
            <a-form-item
              label="上级平台 ID"
              required
              :validate-status="fieldErrors.upstreamServerId ? 'error' : ''"
              :help="fieldErrors.upstreamServerId"
            >
              <a-input
                v-model="form.upstreamServerId"
                maxlength="20"
                allow-clear
                @input="updateUpstreamDomain"
                @blur="touched.upstreamServerId = true"
              >
                <template #suffix><s-counter-suffix :value="form.upstreamServerId.length" :total="20" /></template>
              </a-input>
            </a-form-item>
            <a-form-item
              label="上级域"
              required
              :validate-status="fieldErrors.upstreamDomain ? 'error' : ''"
              :help="fieldErrors.upstreamDomain"
            >
              <a-input v-model="form.upstreamDomain" maxlength="10" allow-clear @blur="touched.upstreamDomain = true">
                <template #suffix><s-counter-suffix :value="form.upstreamDomain.length" :total="10" /></template>
              </a-input>
            </a-form-item>
            <a-form-item label="上级地址" required :validate-status="fieldErrors.host ? 'error' : ''" :help="fieldErrors.host">
              <a-input v-model="form.host" placeholder="请输入 IPv4 或 IPv6 地址" allow-clear @blur="touched.host = true" />
            </a-form-item>
            <a-form-item label="上级端口" required>
              <s-number-field ref="portField" v-model="form.port" :min="1" :max="65535" required />
            </a-form-item>
            <a-form-item label="传输协议" required>
              <a-radio-group v-model="form.transport" type="button">
                <a-radio value="UDP">UDP</a-radio>
                <a-radio value="TCP">TCP</a-radio>
              </a-radio-group>
            </a-form-item>
            <a-form-item label="认证用户名">
              <a-input v-model="form.authUsername" maxlength="20" allow-clear>
                <template #suffix><s-counter-suffix :value="form.authUsername.length" :total="20" /></template>
              </a-input>
            </a-form-item>
            <a-form-item label="认证密码">
              <a-alert v-if="editing?.credentialNeedsReset" type="warning"
                >原认证密码无法读取，请重新填写上级平台提供的密码后保存。</a-alert
              >
              <a-input-password
                v-model="form.password"
                allow-clear
                autocomplete="new-password"
                :placeholder="editing?.hasPassword ? '留空则保持原密码' : '未配置可留空'"
              />
            </a-form-item>
          </div>
        </section>

        <section v-show="editorStep === 2" class="form-section">
          <header>
            <Fingerprint :size="17" />
            <h3>本平台身份</h3>
          </header>
          <div class="form-grid">
            <a-form-item
              label="本平台设备 ID"
              required
              :validate-status="fieldErrors.localDeviceId ? 'error' : ''"
              :help="fieldErrors.localDeviceId"
            >
              <a-input v-model="form.localDeviceId" maxlength="20" allow-clear @blur="touched.localDeviceId = true">
                <template #suffix><s-counter-suffix :value="form.localDeviceId.length" :total="20" /></template>
              </a-input>
            </a-form-item>
            <a-form-item
              label="本平台域"
              required
              :validate-status="fieldErrors.localDomain ? 'error' : ''"
              :help="fieldErrors.localDomain"
            >
              <a-input v-model="form.localDomain" maxlength="10" allow-clear @blur="touched.localDomain = true">
                <template #suffix><s-counter-suffix :value="form.localDomain.length" :total="10" /></template>
              </a-input>
            </a-form-item>
            <a-form-item
              label="本地 SIP 地址"
              required
              :validate-status="fieldErrors.localSipIp ? 'error' : ''"
              :help="fieldErrors.localSipIp"
            >
              <a-select
                v-model="form.localSipIp"
                :loading="networkLoading"
                allow-search
                allow-create
                placeholder="选择本机网卡或输入宣告地址"
                @blur="touched.localSipIp = true"
              >
                <a-option v-for="item in localSipAddresses" :key="item.ip" :value="item.ip" :label="item.ip">
                  {{ item.ip }} <span v-if="item.interfaceName">（{{ item.interfaceName }}）</span>
                  <a-tag v-if="item.recommended" size="small" color="green">推荐</a-tag>
                  <a-tag v-if="item.virtual" size="small">虚拟网卡</a-tag>
                </a-option>
              </a-select>
            </a-form-item>
            <a-form-item label="本地 SIP 端口" required>
              <s-number-field ref="localSipPortField" v-model="form.localSipPort" :min="1" :max="65535" required />
            </a-form-item>
            <a-form-item
              label="媒体宣告地址"
              :validate-status="fieldErrors.mediaAdvertiseIp ? 'error' : ''"
              :help="fieldErrors.mediaAdvertiseIp"
            >
              <a-input v-model="form.mediaAdvertiseIp" allow-clear @blur="touched.mediaAdvertiseIp = true" />
            </a-form-item>
          </div>
        </section>

        <section v-show="editorStep === 3" class="form-section">
          <header>
            <SlidersHorizontal :size="17" />
            <h3>注册与协议</h3>
          </header>
          <div class="form-grid">
            <a-form-item label="协议版本"
              ><a-select v-model="form.profileOverride"
                ><a-option value="auto">自动协商</a-option><a-option value="2016">固定 2016</a-option
                ><a-option value="2022">固定 2022</a-option></a-select
              ></a-form-item
            >
            <a-form-item label="字符集" help="跟随协议版本时：2016 使用 GB2312，2022 使用 GB18030。">
              <a-select v-model="form.charsetOverride">
                <a-option value="">跟随协议版本（默认）</a-option>
                <a-option value="GB2312">GB2312</a-option>
                <a-option value="GB18030">GB18030</a-option>
                <a-option value="UTF-8">UTF-8（兼容扩展）</a-option>
              </a-select>
            </a-form-item>
            <a-form-item label="注册有效期（秒）"
              ><s-number-field ref="registerExpiresField" v-model="form.registerExpires" :min="3600" :max="86400" required
            /></a-form-item>
            <a-form-item label="心跳间隔（秒）"
              ><s-number-field ref="keepaliveIntervalField" v-model="form.keepaliveInterval" :min="5" :max="3600" required
            /></a-form-item>
          </div>
        </section>

        <section v-show="editorStep === 4" class="form-section">
          <header>
            <SlidersHorizontal :size="17" />
            <h3>目录与发布</h3>
          </header>
          <div class="form-grid">
            <a-form-item label="每批目录条数" required>
              <a-select v-model="form.catalogBatchSize">
                <a-option
                  v-if="editing && !CASCADE_CATALOG_BATCH_SIZES.includes(editing.catalogBatchSize)"
                  :value="editing.catalogBatchSize"
                  disabled
                >
                  {{ editing.catalogBatchSize }} 条（原配置）
                </a-option>
                <a-option v-for="size in CASCADE_CATALOG_BATCH_SIZES" :key="size" :value="size">{{ size }} 条</a-option>
              </a-select>
            </a-form-item>
            <a-form-item label="最大并发流" help="0 表示不限制并发流。"
              ><s-number-field ref="maxStreamsField" v-model="form.maxStreams" :min="0" :max="10000" required
            /></a-form-item>
          </div>
          <div class="switch-grid">
            <label><span>发布平台目录</span><a-switch v-model="form.publishPlatform" /></label>
            <label><span>发布行政区划</span><a-switch v-model="form.publishCivil" /></label>
            <label>
              <span>共享方式</span>
              <a-tag size="small" :color="form.publishGroup ? 'blue' : 'gray'">{{
                form.publishGroup ? "按照目录共享" : "按照通道共享"
              }}</a-tag>
            </label>
            <label><span>允许云台控制</span><a-switch v-model="form.ptzEnabled" /></label>
            <label title="允许上级对已共享通道执行录像启停、布撤防和报警复位"
              ><span>允许录像/报警控制</span><a-switch v-model="form.deviceControlEnabled"
            /></label>
            <label title="独立授权设备重启与存储卡格式化；默认关闭"
              ><span>允许维护类控制</span><a-switch v-model="form.maintenanceEnabled"
            /></label>
            <label title="允许上级修改已共享通道及其所属设备的国标配置"
              ><span>允许设备配置</span><a-switch v-model="form.configEnabled"
            /></label>
            <label title="允许上级通过共享通道升级其所属整机；默认关闭"
              ><span>允许设备升级</span><a-switch v-model="form.upgradeEnabled"
            /></label>
            <label><span>自动推送目录</span><a-switch v-model="form.autoPushCatalog" /></label>
            <label><span>允许录像查询与回放</span><a-switch v-model="form.recordingEnabled" /></label>
            <label><span>RTCP 保活</span><a-switch v-model="form.rtcpKeepalive" /></label>
            <label><span>启用级联</span><a-switch v-model="form.enabled" /></label>
          </div>
          <a-alert type="info" class="cascade-publish-group-hint">
            共享方式请在“共享与目录”页面切换并保存；目录共享需先建立业务根和虚拟组织，未归类通道仍保留共享。
          </a-alert>
        </section>
        <section v-show="editorStep === 5" class="cascade-confirm">
          <div class="cascade-confirm-heading">
            <div>
              <h3>核对平台配置</h3>
              <p>请核对以下信息，如需调整可返回对应步骤修改。</p>
            </div>
            <a-tag :color="form.enabled ? 'green' : 'gray'">{{ form.enabled ? "保存后启用" : "保存后停用" }}</a-tag>
          </div>
          <div class="cascade-confirm-grid">
            <article class="cascade-confirm-card">
              <header>
                <h4><Building2 :size="16" />上级平台</h4>
                <a-link :disabled="saving" @click="editorStep = 1">修改</a-link>
              </header>
              <dl>
                <div>
                  <dt>平台名称</dt>
                  <dd>{{ form.name }}</dd>
                </div>
                <div>
                  <dt>上级平台 ID</dt>
                  <dd class="is-code">{{ form.upstreamServerId }}</dd>
                </div>
                <div>
                  <dt>上级域</dt>
                  <dd class="is-code">{{ form.upstreamDomain }}</dd>
                </div>
                <div>
                  <dt>上级地址</dt>
                  <dd class="is-code">{{ form.host }}:{{ form.port }}</dd>
                </div>
                <div>
                  <dt>传输协议</dt>
                  <dd>{{ form.transport }}</dd>
                </div>
                <div>
                  <dt>认证用户名</dt>
                  <dd class="is-code">{{ form.authUsername || form.localDeviceId }}</dd>
                </div>
                <div>
                  <dt>认证密码</dt>
                  <dd>{{ form.password ? "已填写" : editing?.hasPassword ? "保留原密码" : "未配置" }}</dd>
                </div>
              </dl>
            </article>
            <article class="cascade-confirm-card">
              <header>
                <h4><Fingerprint :size="16" />本平台身份</h4>
                <a-link :disabled="saving" @click="editorStep = 2">修改</a-link>
              </header>
              <dl>
                <div>
                  <dt>本平台设备 ID</dt>
                  <dd class="is-code">{{ form.localDeviceId }}</dd>
                </div>
                <div>
                  <dt>本平台域</dt>
                  <dd class="is-code">{{ form.localDomain }}</dd>
                </div>
                <div>
                  <dt>本地 SIP 地址</dt>
                  <dd class="is-code">{{ form.localSipIp }}</dd>
                </div>
                <div>
                  <dt>本地 SIP 端口</dt>
                  <dd>{{ form.localSipPort }}</dd>
                </div>
                <div>
                  <dt>媒体宣告地址</dt>
                  <dd :class="{ 'is-code': form.mediaAdvertiseIp }">{{ form.mediaAdvertiseIp || "跟随媒体服务配置" }}</dd>
                </div>
              </dl>
            </article>
            <article class="cascade-confirm-card">
              <header>
                <h4><SlidersHorizontal :size="16" />注册与协议</h4>
                <a-link :disabled="saving" @click="editorStep = 3">修改</a-link>
              </header>
              <dl>
                <div>
                  <dt>协议版本</dt>
                  <dd>{{ form.profileOverride === "auto" ? "自动协商" : `GB/T 28181-${form.profileOverride}` }}</dd>
                </div>
                <div>
                  <dt>字符集</dt>
                  <dd>{{ form.charsetOverride || "跟随协议版本" }}</dd>
                </div>
                <div>
                  <dt>注册有效期</dt>
                  <dd>{{ form.registerExpires }} 秒</dd>
                </div>
                <div>
                  <dt>心跳间隔</dt>
                  <dd>{{ form.keepaliveInterval }} 秒</dd>
                </div>
              </dl>
            </article>
            <article class="cascade-confirm-card">
              <header>
                <h4><SlidersHorizontal :size="16" />目录与发布</h4>
                <a-link :disabled="saving" @click="editorStep = 4">修改</a-link>
              </header>
              <dl>
                <div>
                  <dt>每批目录条数</dt>
                  <dd>{{ form.catalogBatchSize }} 条 / 批</dd>
                </div>
                <div>
                  <dt>最大并发流</dt>
                  <dd>{{ form.maxStreams === 0 ? "不限制" : `${form.maxStreams} 路` }}</dd>
                </div>
                <div>
                  <dt>发布平台目录</dt>
                  <dd>
                    <a-tag size="small" :color="form.publishPlatform ? 'blue' : 'gray'">{{
                      form.publishPlatform ? "发布" : "不发布"
                    }}</a-tag>
                  </dd>
                </div>
                <div>
                  <dt>发布行政区划</dt>
                  <dd>
                    <a-tag size="small" :color="form.publishCivil ? 'blue' : 'gray'">{{
                      form.publishCivil ? "发布" : "不发布"
                    }}</a-tag>
                  </dd>
                </div>
                <div>
                  <dt>发布业务分组</dt>
                  <dd>
                    <a-tag size="small" :color="form.publishGroup ? 'blue' : 'gray'">{{
                      form.publishGroup ? "发布" : "不发布"
                    }}</a-tag>
                  </dd>
                </div>
                <div>
                  <dt>云台控制</dt>
                  <dd>
                    <a-tag size="small" :color="form.ptzEnabled ? 'blue' : 'gray'">{{ form.ptzEnabled ? "允许" : "禁止" }}</a-tag>
                  </dd>
                </div>
                <div>
                  <dt>允许录像/报警控制</dt>
                  <dd>
                    <a-tag size="small" :color="form.deviceControlEnabled ? 'blue' : 'gray'">{{
                      form.deviceControlEnabled ? "允许" : "禁止"
                    }}</a-tag>
                  </dd>
                </div>
                <div>
                  <dt>允许维护类控制</dt>
                  <dd>
                    <a-tag size="small" :color="form.maintenanceEnabled ? 'blue' : 'gray'">{{
                      form.maintenanceEnabled ? "允许" : "禁止"
                    }}</a-tag>
                  </dd>
                </div>
                <div>
                  <dt>允许设备配置</dt>
                  <dd>
                    <a-tag size="small" :color="form.configEnabled ? 'blue' : 'gray'">{{
                      form.configEnabled ? "允许" : "禁止"
                    }}</a-tag>
                  </dd>
                </div>
                <div>
                  <dt>允许设备升级</dt>
                  <dd>
                    <a-tag size="small" :color="form.upgradeEnabled ? 'blue' : 'gray'">{{
                      form.upgradeEnabled ? "允许" : "禁止"
                    }}</a-tag>
                  </dd>
                </div>
                <div>
                  <dt>自动推送目录</dt>
                  <dd>
                    <a-tag size="small" :color="form.autoPushCatalog ? 'blue' : 'gray'">{{
                      form.autoPushCatalog ? "开启" : "关闭"
                    }}</a-tag>
                  </dd>
                </div>
                <div>
                  <dt>允许录像查询与回放</dt>
                  <dd>
                    <a-tag size="small" :color="form.recordingEnabled ? 'blue' : 'gray'">{{
                      form.recordingEnabled ? "允许" : "禁止"
                    }}</a-tag>
                  </dd>
                </div>
                <div>
                  <dt>RTCP 保活</dt>
                  <dd>
                    <a-tag size="small" :color="form.rtcpKeepalive ? 'blue' : 'gray'">{{
                      form.rtcpKeepalive ? "开启" : "关闭"
                    }}</a-tag>
                  </dd>
                </div>
              </dl>
            </article>
          </div>
          <a-alert :type="form.enabled ? 'success' : 'info'">{{
            form.enabled ? "保存后启用级联，并向上级发起注册。" : "保存后级联保持停用，不会向上级注册。"
          }}</a-alert>
          <p class="cascade-confirm-hint">保存后可到平台列表的“共享与目录”配置通道。</p>
        </section>
      </a-form>
      <template #footer>
        <div class="dialog-actions">
          <a-button :disabled="saving" @click="editorVisible = false">取消</a-button>
          <a-button v-if="editorStep > 1" :disabled="saving" @click="editorStep--">上一步</a-button>
          <a-button v-if="editorStep < 5" type="primary" @click="nextEditorStep">下一步</a-button>
          <a-button v-else type="primary" :loading="saving" @click="savePlatform">确认保存</a-button>
        </div>
      </template>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { Message, Modal } from "@arco-design/web-vue";
import dayjs from "dayjs";
import {
  Building2,
  CircleCheck,
  Fingerprint,
  ListVideo,
  Plus,
  RadioTower,
  RefreshCw,
  Send,
  Settings2,
  SlidersHorizontal,
  Trash2,
  TriangleAlert
} from "lucide-vue-next";
import {
  createCascadePlatform,
  deleteCascadePlatform,
  fetchSipNetworkInterfaces,
  fetchSipSetupStatus,
  getCascadeShares,
  listCascadePlatforms,
  pushCascadeCatalog,
  setCascadePlatformEnabled,
  updateCascadePlatform,
  type CascadePlatform,
  type CascadePlatformInput,
  type CascadeShares,
  type SipNetworkAddress,
  type SipConfigSummary
} from "@/api/gb28181";
import { hasPermission } from "@/hooks/usePermission";
import { useDevicesSize } from "@/hooks/useDevicesSize";
import SCounterSuffix from "@/components/s-counter-suffix/index.vue";
import SNumberField from "@/components/s-number-field/index.vue";
import { useDictLabelMap } from "@/hooks/useDictOptions";
import {
  CASCADE_CATALOG_BATCH_SIZES,
  CASCADE_REGISTER_STATE_LABEL_FALLBACK,
  DICT_CODE_CASCADE_REGISTER_STATE,
  cascadeCycleLabel,
  cascadeFormFieldErrors,
  cascadeLocalIdentityDefaults,
  cascadePresentation,
  cascadeRuntimeSyncPending,
  defaultCascadePlatform,
  validGbId,
  validateCascadeStep,
  validateCascadePlatform
} from "./cascadeState";

import { deriveDomain } from "../sip/sipSetupRules";
import UpstreamSubscriptions from "./UpstreamSubscriptions.vue";

const { isMobile } = useDevicesSize();
const router = useRouter();
const canView = computed(() => hasPermission("gb28181:cascade:view"));
const canManage = computed(() => hasPermission("gb28181:cascade:manage"));
const canEnable = computed(() => hasPermission("gb28181:cascade:enable"));
const canShare = computed(() => hasPermission("gb28181:cascade:share"));
function openSharedDirectory(platform: CascadePlatform) {
  void router.push(`/gb28181/cascade/${platform.id}/resources`);
}
/** 上级平台的注册状态文案（`cascade_register_state` 字典；色/说明仍在代码里）。 */
const cascadeRegisterLabels = useDictLabelMap(DICT_CODE_CASCADE_REGISTER_STATE, CASCADE_REGISTER_STATE_LABEL_FALLBACK);
function cascadeStatus(platform: Parameters<typeof cascadePresentation>[0]) {
  return cascadePresentation(platform, cascadeRegisterLabels.value);
}

const platforms = ref<CascadePlatform[]>([]);
const loading = ref(false);
const saving = ref(false);
const actionId = ref<number | null>(null);
const errorMessage = ref("");
const keyword = ref("");
const statusFilter = ref("all");
const editorVisible = ref(false);
const editorStep = ref(1);
function nextEditorStep() {
  const errors = validateCascadeStep(form, editorStep.value, editing.value?.catalogBatchSize);
  const numericError = stepNumberFields(editorStep.value)
    .map(field => field.value?.error || "")
    .find(Boolean);
  if (errors.length || numericError) {
    touchCascadeStep(editorStep.value);
    Message.warning(errors[0] || numericError!);
    return;
  }
  editorStep.value++;
}
const editing = ref<CascadePlatform | null>(null);
const form = reactive(defaultCascadePlatform());
const touched = reactive<Record<string, boolean>>({});
const fieldErrors = computed(() => cascadeFormFieldErrors(form, touched));
type NumberFieldInstance = InstanceType<typeof SNumberField>;
const portField = ref<NumberFieldInstance | null>(null);
const localSipPortField = ref<NumberFieldInstance | null>(null);
const registerExpiresField = ref<NumberFieldInstance | null>(null);
const keepaliveIntervalField = ref<NumberFieldInstance | null>(null);
const maxStreamsField = ref<NumberFieldInstance | null>(null);
function stepNumberFields(step: number) {
  if (step === 1) return [portField];
  if (step === 2) return [localSipPortField];
  if (step === 3) return [registerExpiresField, keepaliveIntervalField];
  if (step === 4) return [maxStreamsField];
  return [];
}
function touchCascadeStep(step: number) {
  const fields =
    step === 1
      ? ["name", "upstreamServerId", "upstreamDomain", "host"]
      : step === 2
        ? ["localDeviceId", "localDomain", "localSipIp", "mediaAdvertiseIp"]
        : [];
  fields.forEach(field => (touched[field] = true));
}
const localSipConfig = ref<SipConfigSummary | null>(null);
const localSipAddresses = ref<SipNetworkAddress[]>([]);
const networkLoading = ref(false);
async function loadLocalSipAddresses() {
  networkLoading.value = true;
  try {
    const response = await fetchSipNetworkInterfaces();
    if (response.code !== 0 || response.data.scanStatus === "failed") throw new Error("network scan failed");
    localSipAddresses.value = response.data.items.filter(item => !item.loopback && !item.listenOnly && item.ip !== "0.0.0.0");
  } catch {
    localSipAddresses.value = [];
    Message.warning("网卡列表读取失败，可手动输入本地 SIP 地址");
  } finally {
    networkLoading.value = false;
  }
}
let localSipConfigRequest: Promise<void> | null = null;

const summary = computed(() => ({
  enabled: platforms.value.filter(item => item.enabled).length,
  online: platforms.value.filter(item => item.overall === "online").length,
  attention: platforms.value.filter(item => item.enabled && item.overall !== "online").length
}));

const filteredPlatforms = computed(() => {
  const q = keyword.value.trim().toLowerCase();
  return platforms.value.filter(item => {
    const statusMatch =
      statusFilter.value === "all" ||
      (statusFilter.value === "disabled" ? !item.enabled : item.enabled && item.overall === statusFilter.value);
    const keywordMatch =
      !q ||
      [item.name, item.upstreamServerId, item.host, item.localDeviceId].some(value =>
        String(value || "")
          .toLowerCase()
          .includes(q)
      );
    return statusMatch && keywordMatch;
  });
});

const sharedSnapshots = reactive(new Map<number, CascadeShares>());
function sharedSummary(id: number) {
  const snapshot = sharedSnapshots.get(id);
  if (!snapshot) return "查看共享与目录";
  const channels = snapshot.channels.filter(item => item.active !== false);
  return channels.length ? `${channels.length} 个通道` : "未共享";
}
async function loadSharedSnapshot(id: number) {
  const response: any = await getCascadeShares(id);
  const snapshot: CascadeShares = response?.data || response;
  sharedSnapshots.set(id, snapshot);
  return snapshot;
}

async function refresh() {
  if (!canView.value || loading.value) return;
  resetAutoRefreshCountdown();
  loading.value = true;
  errorMessage.value = "";
  try {
    const response: any = await listCascadePlatforms();
    platforms.value = response?.list || response?.data?.list || [];
    const platformIds = new Set(platforms.value.map(platform => platform.id));
    for (const id of sharedSnapshots.keys()) {
      if (!platformIds.has(id)) sharedSnapshots.delete(id);
    }
    await Promise.allSettled(platforms.value.map(platform => loadSharedSnapshot(platform.id)));
  } catch {
    errorMessage.value = "国标级联平台加载失败，请检查服务状态后重试。";
  } finally {
    loading.value = false;
  }
}

async function loadLocalSipConfig() {
  if (localSipConfigRequest) return localSipConfigRequest;
  localSipConfigRequest = (async () => {
    try {
      const response = await fetchSipSetupStatus();
      if (response.code !== 0 || !response.data?.config) return;
      const config = response.data.config;
      let advertiseIp = config.advertiseIp;
      if (!advertiseIp && config.listenIp === "0.0.0.0") {
        const networkResponse = await fetchSipNetworkInterfaces();
        if (networkResponse.code === 0) {
          const addresses = networkResponse.data?.items || [];
          // 优先序与平台接入向导一致:推荐且非虚拟 > 非虚拟 > 推荐 > 任意具体地址。
          // ⛔ "非虚拟"要比"推荐"靠前:后端靠网卡名猜,认不出某些虚拟网卡是常态。
          const usable = (item: (typeof addresses)[number]) => !item.loopback && !item.listenOnly;
          advertiseIp =
            addresses.find(item => usable(item) && item.recommended && !item.virtual)?.ip ||
            addresses.find(item => usable(item) && !item.virtual)?.ip ||
            addresses.find(item => usable(item) && item.recommended)?.ip ||
            addresses.find(usable)?.ip ||
            "";
        }
      }
      localSipConfig.value = { ...config, advertiseIp };
    } catch {
      // 本级 SIP 配置读取失败时保留手工填写能力,不阻塞新增平台。
    }
  })()
    .catch(() => undefined)
    .finally(() => {
      localSipConfigRequest = null;
    });
  return localSipConfigRequest;
}

function updateUpstreamDomain(value: string) {
  if (validGbId(value)) form.upstreamDomain = deriveDomain(value);
}

async function openCreate() {
  void loadLocalSipAddresses();
  if (!localSipConfig.value) await loadLocalSipConfig();
  editing.value = null;
  Object.keys(touched).forEach(key => delete touched[key]);
  Object.assign(form, defaultCascadePlatform(), cascadeLocalIdentityDefaults(localSipConfig.value || undefined));
  editorStep.value = 1;
  editorVisible.value = true;
}

function openEdit(platform: CascadePlatform) {
  void loadLocalSipAddresses();
  editing.value = platform;
  Object.keys(touched).forEach(key => delete touched[key]);
  Object.assign(form, defaultCascadePlatform(), {
    name: platform.name,
    upstreamServerId: platform.upstreamServerId,
    upstreamDomain: platform.upstreamDomain,
    host: platform.host,
    port: platform.port,
    localDeviceId: platform.localDeviceId,
    localDomain: platform.localDomain,
    localSipIp: platform.localSipIp,
    localSipPort: platform.localSipPort,
    mediaAdvertiseIp: platform.mediaAdvertiseIp || "",
    authUsername: platform.authUsername || platform.localDeviceId,
    profileOverride: platform.profileOverride,
    charsetOverride: platform.charsetOverride || "",
    registerExpires: platform.registerExpires,
    keepaliveInterval: platform.keepaliveInterval,
    transport: platform.transport,
    catalogBatchSize: platform.catalogBatchSize,
    publishPlatform: platform.publishPlatform,
    publishCivil: platform.publishCivil,
    publishGroup: platform.publishGroup,
    maxStreams: platform.maxStreams,
    ptzEnabled: platform.ptzEnabled,
    deviceControlEnabled: platform.deviceControlEnabled ?? false,
    maintenanceEnabled: platform.maintenanceEnabled ?? false,
    autoPushCatalog: platform.autoPushCatalog ?? false,
    configEnabled: platform.configEnabled ?? false,
    upgradeEnabled: platform.upgradeEnabled ?? false,
    recordingEnabled: platform.recordingEnabled ?? false,
    rtcpKeepalive: platform.rtcpKeepalive ?? false,
    enabled: platform.enabled,
    password: ""
  });
  editorStep.value = 1;
  editorVisible.value = true;
}

function buildInput(): CascadePlatformInput {
  const data: any = { ...form };
  if (!data.password) delete data.password;
  return data;
}

async function savePlatform() {
  for (let step = 1; step <= 4; step++) {
    const errors = validateCascadeStep(form, step, editing.value?.catalogBatchSize);
    const numericError = stepNumberFields(step)
      .map(field => field.value?.error || "")
      .find(Boolean);
    if (errors.length || numericError) {
      editorStep.value = step;
      touchCascadeStep(step);
      Message.warning(errors[0] || numericError!);
      return;
    }
  }
  const fieldError = Object.values(
    cascadeFormFieldErrors(form, Object.fromEntries(Object.keys(touched).map(key => [key, true])))
  ).find(Boolean);
  if (fieldError) {
    Object.keys(fieldErrors.value).forEach(key => (touched[key] = true));
    Message.warning(fieldError);
    return;
  }
  const errors = validateCascadePlatform(form);
  if (errors.length) {
    editorStep.value = errors[0].includes("本平台") || errors[0].includes("本地") ? 2 : 1;
    Message.warning(errors[0]);
    return;
  }
  saving.value = true;
  try {
    const response: any = editing.value
      ? await updateCascadePlatform(editing.value.id, buildInput(), editing.value.configRevision)
      : await createCascadePlatform(buildInput());
    if (cascadeRuntimeSyncPending(response)) {
      Message.warning(response.warning || "配置已保存，但运行时尚未同步；请稍后重试");
    } else {
      Message.success(
        form.enabled
          ? editing.value
            ? "上级平台已保存并启用，正在向上级注册；通道请到“共享与目录”配置"
            : "上级平台已创建并启用，正在向上级注册"
          : editing.value
            ? "上级平台已保存，当前未启用；通道请到“共享与目录”配置"
            : "上级平台已创建，当前未启用"
      );
    }
    editorVisible.value = false;
    await refresh();
  } finally {
    saving.value = false;
  }
}

async function pushCatalog(platform: CascadePlatform) {
  if (!platform.enabled) {
    Message.warning("上级平台已停用，启用后才能推送目录");
    return;
  }
  actionId.value = platform.id;
  try {
    const response: any = await pushCascadeCatalog(platform.id);
    const payload = response?.data || response || {};
    Message.success(`目录已推送：${payload.items ?? 0} 个通道，分 ${payload.batches ?? 0} 批发送`);
  } catch (error: any) {
    Message.error(error?.message || "目录推送失败，请确认上级链路后重试");
  } finally {
    actionId.value = null;
  }
}

async function toggleEnabled(platform: CascadePlatform, next: boolean) {
  if (next === platform.enabled) return;
  actionId.value = platform.id;
  try {
    const response: any = await setCascadePlatformEnabled(platform.id, next, platform.configRevision);
    if (cascadeRuntimeSyncPending(response)) {
      Message.warning(next ? "平台已启用，但运行时尚未完成注册同步" : "平台已停用，但运行时尚未完成注销同步");
    } else {
      Message.success(next ? "上级平台已启用，正在向上级注册" : "上级平台已停用，已向上级注销");
    }
    await refresh();
  } catch (error: any) {
    Message.error(error?.message || "启停操作失败，请稍后重试");
    await refresh();
  } finally {
    actionId.value = null;
  }
}

function confirmDelete(platform: CascadePlatform) {
  Modal.warning({
    title: "删除上级平台",
    content: `确定删除“${platform.name}”吗？存在活动媒体会话时服务端会拒绝删除。`,
    hideCancel: false,
    onOk: async () => {
      const response = await deleteCascadePlatform(platform.id);
      if (response.runtimeSynced === false) {
        Message.warning(response.deleted ? "上级平台已删除，但运行时尚未同步" : "上级平台已停用，但删除尚未完成");
      } else if (!response.deleted) {
        Message.warning("上级平台删除尚未完成，请刷新后重试");
      } else {
        Message.success("上级平台已删除");
      }
      await refresh();
    }
  });
}

function profileLabel(value: string) {
  return value === "auto" ? "自动协商" : `固定 ${value}`;
}
function formatRelative(value?: string | null) {
  return value ? dayjs(value).format("YYYY-MM-DD HH:mm:ss") : "暂无记录";
}

const AUTO_REFRESH_INTERVAL_SECONDS = 10;
const autoRefreshCountdown = ref(AUTO_REFRESH_INTERVAL_SECONDS);
let refreshCountdownTimer: number | null = null;

function resetAutoRefreshCountdown() {
  autoRefreshCountdown.value = AUTO_REFRESH_INTERVAL_SECONDS;
}

function startAutoRefresh() {
  if (refreshCountdownTimer !== null) return;
  resetAutoRefreshCountdown();
  refreshCountdownTimer = window.setInterval(() => {
    if (autoRefreshCountdown.value <= 1) {
      resetAutoRefreshCountdown();
      void refresh();
    } else {
      autoRefreshCountdown.value -= 1;
    }
  }, 1000);
}

function stopAutoRefresh() {
  if (refreshCountdownTimer !== null) {
    clearInterval(refreshCountdownTimer);
    refreshCountdownTimer = null;
  }
}

onMounted(() => {
  refresh();
  loadLocalSipConfig();
  startAutoRefresh();
});

onUnmounted(() => {
  stopAutoRefresh();
});
</script>

<style scoped lang="scss">
.cascade-page {
  min-height: 100%;
}
.cascade-shell {
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-height: 100%;
}
.cascade-search-actions,
.cascade-actions {
  display: flex;
  gap: 8px;
  align-items: center;
}
.cascade-summary {
  display: grid;
  grid-template-columns: repeat(4, minmax(130px, 1fr));
  gap: 12px;
}
.cascade-summary-card {
  display: flex;
  gap: 12px;
  align-items: center;
  min-width: 0;
  min-height: 78px;
  padding: 14px 16px;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: var(--uvp-panel-radius);
  box-shadow: var(--uvp-panel-shadow);
}
.cascade-summary-card__icon {
  display: inline-flex;
  flex: 0 0 40px;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  border-radius: 10px;
}
.cascade-summary-card__icon.is-brand {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
}
.cascade-summary-card__icon.is-enabled {
  color: var(--uvp-brand-strong);
  background: var(--uvp-brand-soft);
}
.cascade-summary-card__icon.is-online {
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 12%, transparent);
}
.cascade-summary-card__icon.is-attention {
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
}
.cascade-summary-card__content {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
}
.cascade-summary-card__content strong {
  font-size: 24px;
  font-weight: 600;
  line-height: 1;
  color: var(--uvp-text-primary);
}
.cascade-summary-card__content strong.is-online {
  color: var(--uvp-brand-cyan);
}
.cascade-summary-card__content strong.is-attention {
  color: var(--uvp-warning);
}
.cascade-summary-card__content span {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.cascade-search-panel {
  margin-bottom: 0;
}
.cascade-search {
  flex: 0 0 280px;
  width: 280px;
}
.cascade-status-filter {
  flex: 0 0 148px;
  width: 148px;
}
.cascade-status-filter :deep(.arco-select) {
  box-sizing: border-box;
  width: 100%;
  background: var(--uvp-search-control-bg) !important;
  border: 1px solid var(--uvp-search-secondary-btn-border) !important;
  border-radius: 10px !important;
  box-shadow: var(--uvp-search-control-shadow) !important;
}
.cascade-status-filter :deep(.arco-select-view-focus) {
  border-color: var(--uvp-brand) !important;
  box-shadow: var(--uvp-search-control-focus-shadow) !important;
}
.cascade-status-filter.is-all :deep(.arco-select-view-value) {
  color: var(--uvp-text-tertiary) !important;
}
.cascade-table {
  min-height: 260px;
}
.entity-cell {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}
.entity-cell strong,
.entity-cell span {
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--color-text-1);
  white-space: nowrap;
}
.entity-cell code,
.entity-cell small {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 12px;
  color: var(--color-text-3);
  letter-spacing: 0;
  white-space: nowrap;
}
.cascade-actions :deep(.arco-link) {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  font-size: 13px;
}
.refresh-countdown {
  font-variant-numeric: tabular-nums;
  color: var(--uvp-text-tertiary, var(--color-text-3));
}
.shared-channels-link {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  max-width: 100%;
  color: rgb(var(--link-6));
}
.shared-channels-link span {
  overflow: hidden;
  text-overflow: ellipsis;
  color: inherit;
  white-space: nowrap;
  text-decoration: underline;
  text-underline-offset: 3px;
}
.cascade-table :deep(.shared-channels-link:hover span) {
  color: rgb(var(--link-6));
  text-decoration-thickness: 2px;
}
.cascade-alert {
  flex: 0 0 auto;
}
.cascade-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.form-section {
  padding: 0 0 14px;
  border-bottom: 1px solid var(--color-border-2);
}
.form-section > header {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 12px;
  color: var(--color-text-2);
}
.form-section h3 {
  margin: 0;
  font-size: 15px;
  color: var(--color-text-1);
  letter-spacing: 0;
}
.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 16px;
}
.form-grid :deep(.arco-input-number),
.form-grid :deep(.arco-select) {
  width: 100%;
}
.switch-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px 16px;
}
.switch-grid label {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 36px;
  padding: 0 10px;
}
.cascade-confirm {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.cascade-confirm-heading,
.cascade-confirm-card > header {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
}
.cascade-confirm-heading h3 {
  margin: 0;
  font-size: 16px;
  color: var(--color-text-1);
}
.cascade-confirm-heading p,
.cascade-confirm-hint {
  margin: 4px 0 0;
  font-size: 12px;
  line-height: 20px;
  color: var(--color-text-3);
}
.cascade-confirm-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
.cascade-confirm-card {
  min-width: 0;
  padding: 14px 16px;
  background: var(--color-fill-1);
  border: 1px solid var(--color-border-2);
  border-radius: 8px;
}
.cascade-confirm-card > header {
  padding-bottom: 10px;
  margin-bottom: 10px;
  border-bottom: 1px solid var(--color-border-2);
}
.cascade-confirm-card h4 {
  display: flex;
  gap: 8px;
  align-items: center;
  margin: 0;
  font-size: 14px;
  color: var(--color-text-1);
}
.cascade-confirm-card h4 svg {
  color: var(--color-text-3);
}
.cascade-confirm-card dl {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin: 0;
}
.cascade-confirm-card dl > div {
  display: grid;
  grid-template-columns: 104px minmax(0, 1fr);
  gap: 8px;
  font-size: 13px;
  line-height: 22px;
}
.cascade-confirm-card dt {
  color: var(--color-text-3);
}
.cascade-confirm-card dd {
  min-width: 0;
  margin: 0;
  color: var(--color-text-1);
  overflow-wrap: anywhere;
}
.cascade-confirm-card .is-code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}

@media (width <= 680px) {
  .cascade-confirm-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
.dialog-actions {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: flex-end;
  width: 100%;
}

@media (width <= 900px) {
  .cascade-summary {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (width <= 560px) {
  .cascade-search-actions {
    align-items: stretch;
    width: 100%;
  }
  .cascade-search-actions :deep(.arco-btn) {
    flex: 1;
  }
  .cascade-search,
  .cascade-status-filter {
    flex-basis: 100%;
    width: 100%;
  }
  .form-grid,
  .switch-grid {
    grid-template-columns: 1fr;
  }
  .cascade-summary-card {
    min-height: 70px;
    padding: 12px;
  }
}
</style>

<style lang="scss">
.cascade-platform-dialog .arco-modal-body {
  max-height: calc(100dvh - 260px);
  overflow-y: auto;
  overscroll-behavior: contain;
}

@media (width <= 560px) {
  .cascade-platform-dialog .arco-modal-header {
    padding-inline: 16px !important;
  }

  .cascade-platform-dialog .arco-modal-body {
    padding: 18px 16px !important;
  }

  .cascade-platform-dialog .arco-modal-footer {
    padding-inline: 16px !important;
  }
}
</style>
