<template>
  <div class="snow-page cascade-page">
    <div class="snow-inner uvp-page-shell-flat cascade-shell">
      <a-alert v-if="!canView" type="warning" class="cascade-alert">
        无权查看国标级联，请联系管理员分配查看权限。
      </a-alert>

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
              <a-button class="uvp-page-action-btn uvp-refresh-btn" :loading="loading" :title="`自动刷新倒计时 ${autoRefreshCountdown} 秒`" @click="refresh">
                <template #icon><RefreshCw :size="15" /></template>
                刷新 <span class="refresh-countdown">{{ autoRefreshCountdown }}s</span>
              </a-button>
              <a-button v-if="canManage" class="uvp-page-action-btn uvp-create-btn" type="primary" @click="openCreate">
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
          :loading="loading"
          :bordered="false"
          :pagination="false"
          :scroll="{ x: '100%', minWidth: 1850 }"
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
                <div class="entity-cell"><span>{{ record.host }}:{{ record.port }}</span><small>{{ record.transport }}</small></div>
              </template>
            </a-table-column>
            <a-table-column title="本平台身份" :width="218">
              <template #cell="{ record }">
                <div class="entity-cell"><code>{{ record.localDeviceId }}</code><small>{{ record.localSipIp }}:{{ record.localSipPort }}</small></div>
              </template>
            </a-table-column>
            <a-table-column title="共享通道" :width="190">
              <template #cell="{ record }">
                <a-link class="shared-channels-link" @click="openSharedDevices(record)">
                  <ListVideo :size="14" />
                  <span>{{ sharedSummary(record.id) }}</span>
                </a-link>
              </template>
            </a-table-column>
            <a-table-column title="运行状态" :width="150">
              <template #cell="{ record }">
                <a-tooltip :content="cascadePresentation(record).detail">
                  <a-tag :color="cascadePresentation(record).color">{{ cascadePresentation(record).label }}</a-tag>
                </a-tooltip>
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
                <div class="entity-cell"><span>GB/T {{ record.effectiveVersion || '2016' }}</span><small>{{ profileLabel(record.profileOverride) }}</small></div>
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
            <a-table-column title="操作" :width="290" align="center" :fixed="isMobile ? '' : 'right'">
              <template #cell="{ record }">
                <div class="uvp-table-actions cascade-actions">
                  <a-link v-if="canManage" class="uvp-table-action uvp-table-action--edit" @click="openEdit(record)"><Settings2 :size="14" />编辑</a-link>
                  <a-link v-if="canShare" class="uvp-table-action uvp-table-action--assign" @click="openShare(record)"><Share2 :size="14" />共享</a-link>
                  <a-tooltip content="把当前共享给该上级的设备/通道目录主动报送一遍">
                    <a-link v-if="canShare" class="uvp-table-action uvp-table-action--sync" :disabled="!record.enabled" :loading="actionId === record.id" @click="pushCatalog(record)"><Send :size="14" />推送</a-link>
                  </a-tooltip>
                  <a-link v-if="canManage" class="uvp-table-action uvp-table-action--delete" @click="confirmDelete(record)"><Trash2 :size="14" />删除</a-link>
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
        <section class="form-section">
          <header><Building2 :size="17" /><h3>上级平台</h3></header>
          <div class="form-grid">
            <a-form-item label="平台名称" required :validate-status="fieldErrors.name ? 'error' : ''" :help="fieldErrors.name">
              <a-input v-model="form.name" maxlength="128" allow-clear @blur="touched.name = true" />
            </a-form-item>
            <a-form-item label="上级平台 ID" required :validate-status="fieldErrors.upstreamServerId ? 'error' : ''" :help="fieldErrors.upstreamServerId">
              <a-input v-model="form.upstreamServerId" maxlength="20" allow-clear @input="updateUpstreamDomain" @blur="touched.upstreamServerId = true">
                <template #suffix><s-counter-suffix :value="form.upstreamServerId.length" :total="20" /></template>
              </a-input>
            </a-form-item>
            <a-form-item label="上级域" required :validate-status="fieldErrors.upstreamDomain ? 'error' : ''" :help="fieldErrors.upstreamDomain">
              <a-input v-model="form.upstreamDomain" allow-clear @blur="touched.upstreamDomain = true" />
            </a-form-item>
            <a-form-item label="上级地址" required :validate-status="fieldErrors.host ? 'error' : ''" :help="fieldErrors.host">
              <a-input v-model="form.host" allow-clear @blur="touched.host = true" />
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
          </div>
        </section>

        <section class="form-section">
          <header><Fingerprint :size="17" /><h3>本平台身份</h3></header>
          <div class="form-grid">
            <a-form-item label="本平台设备 ID" required :validate-status="fieldErrors.localDeviceId ? 'error' : ''" :help="fieldErrors.localDeviceId">
              <a-input v-model="form.localDeviceId" maxlength="20" allow-clear @blur="touched.localDeviceId = true">
                <template #suffix><s-counter-suffix :value="form.localDeviceId.length" :total="20" /></template>
              </a-input>
            </a-form-item>
            <a-form-item label="本平台域" required :validate-status="fieldErrors.localDomain ? 'error' : ''" :help="fieldErrors.localDomain">
              <a-input v-model="form.localDomain" allow-clear @blur="touched.localDomain = true" />
            </a-form-item>
            <a-form-item label="本地 SIP 地址" required :validate-status="fieldErrors.localSipIp ? 'error' : ''" :help="fieldErrors.localSipIp">
              <a-select v-model="form.localSipIp" :loading="networkLoading" allow-search allow-create placeholder="选择本机网卡或输入宣告地址" @blur="touched.localSipIp = true">
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
            <a-form-item label="媒体宣告地址" :validate-status="fieldErrors.mediaAdvertiseIp ? 'error' : ''" :help="fieldErrors.mediaAdvertiseIp">
              <a-input v-model="form.mediaAdvertiseIp" allow-clear @blur="touched.mediaAdvertiseIp = true" />
            </a-form-item>
            <a-form-item label="认证用户名"><a-input v-model="form.authUsername" allow-clear /></a-form-item>
            <a-form-item label="认证密码">
              <a-alert v-if="editing?.credentialNeedsReset" type="warning">原认证密码无法读取，请重新填写上级平台提供的密码后保存。</a-alert>
              <s-password-field v-model="form.password" :placeholder="editing?.hasPassword ? '留空则保持原密码' : '未配置可留空'" />
            </a-form-item>
          </div>
        </section>

        <section class="form-section">
          <header><SlidersHorizontal :size="17" /><h3>注册与能力</h3></header>
          <div class="form-grid">
            <a-form-item label="协议版本"><a-select v-model="form.profileOverride"><a-option value="auto">自动协商</a-option><a-option value="2016">固定 2016</a-option><a-option value="2022">固定 2022</a-option></a-select></a-form-item>
            <a-form-item label="字符集覆盖"><a-select v-model="form.charsetOverride" allow-clear><a-option value="GB2312">GB2312</a-option><a-option value="UTF-8">UTF-8</a-option></a-select></a-form-item>
            <a-form-item label="注册有效期（秒）"><s-number-field ref="registerExpiresField" v-model="form.registerExpires" :min="60" :max="86400" required /></a-form-item>
            <a-form-item label="心跳间隔（秒）"><s-number-field ref="keepaliveIntervalField" v-model="form.keepaliveInterval" :min="5" :max="3600" required /></a-form-item>
            <a-form-item label="目录批量大小"><s-number-field ref="catalogBatchSizeField" v-model="form.catalogBatchSize" :min="1" :max="1000" required /></a-form-item>
            <a-form-item label="最大并发流"><s-number-field ref="maxStreamsField" v-model="form.maxStreams" :min="1" :max="10000" required /></a-form-item>
          </div>
          <div class="switch-grid">
            <label><span>发布平台目录</span><a-switch v-model="form.publishPlatform" /></label>
            <label><span>发布行政区划</span><a-switch v-model="form.publishCivil" /></label>
            <label><span>发布业务分组</span><a-switch v-model="form.publishGroup" /></label>
            <label><span>允许云台控制</span><a-switch v-model="form.ptzEnabled" /></label>
            <label><span>保存后启用</span><a-switch v-model="form.enabled" /></label>
          </div>
        </section>
      </a-form>
      <template #footer>
        <div class="dialog-actions">
          <a-button :disabled="saving" @click="editorVisible = false">取消</a-button>
          <a-button type="primary" :loading="saving" @click="savePlatform">保存</a-button>
        </div>
      </template>
    </a-modal>

    <a-modal
      v-model:visible="sharedDevicesVisible"
      :title="`已共享通道 · ${sharedDevicesPlatform?.name || ''}`"
      width="min(780px, calc(100vw - 24px))"
      modal-class="uvp-system-dialog shared-devices-dialog"
      unmount-on-close
    >
      <a-spin :loading="sharedDevicesLoading" style="width: 100%">
        <a-alert v-if="sharedDevicesError" type="error">{{ sharedDevicesError }} <a-link @click="openSharedDevices(sharedDevicesPlatform!)">重试</a-link></a-alert>
        <a-table v-else :data="sharedChannelRows" row-key="sourceChannelId" :pagination="{ pageSize: 10 }" :bordered="false">
          <template #columns>
            <a-table-column title="通道名称" :width="180">
              <template #cell="{ record }">{{ record.name || record.publishedChannelId }}</template>
            </a-table-column>
            <a-table-column title="共享通道编号" :width="200">
              <template #cell="{ record }"><code>{{ record.publishedChannelId }}</code></template>
            </a-table-column>
            <a-table-column title="所属设备">
              <template #cell="{ record }">{{ record.deviceName || "-" }}</template>
            </a-table-column>
            <a-table-column v-if="canShare" title="操作" :width="80" align="center">
              <template #cell="{ record }">
                <a-tooltip content="停止向该上级平台共享此通道">
                  <a-link
                    class="uvp-table-action uvp-table-action--delete"
                    :disabled="sharedDevicesRemovingId !== null"
                    :loading="sharedDevicesRemovingId === record.sourceChannelId"
                    @click="removeSharedChannel(record)"
                  >移除</a-link>
                </a-tooltip>
              </template>
            </a-table-column>
          </template>
          <template #empty><a-empty description="尚未共享通道" /></template>
        </a-table>
      </a-spin>
    </a-modal>

    <a-modal
      v-model:visible="shareVisible"
      width="min(1080px, calc(100vw - 24px))"
      modal-class="uvp-system-dialog cascade-share-dialog"
      :mask-closable="!shareSaving"
      :closable="!shareSaving"
      :esc-to-close="!shareSaving"
      unmount-on-close
    >
      <template #title>
        <div class="share-dialog-title"><Share2 :size="17" /><span>共享通道</span><small>{{ sharingPlatform?.name }}</small></div>
      </template>
      <a-spin :loading="shareLoading" class="share-workspace">
        <div class="share-toolbar">
          <a-input-search
            v-model="channelKeyword"
            allow-clear
            placeholder="通道名称 / 国标编号"
            class="share-resource-search"
            @search="queryShareChannels"
          />
        </div>
        <a-table
          :selected-keys="selectedChannelIds"
          class="share-channel-table share-table"
          row-key="id"
          :data="channelRows"
          :loading="channelLoading"
          :bordered="false"
          :row-selection="{ type: 'checkbox', showCheckedAll: true }"
          :pagination="channelPagination"
          :scroll="{ x: '100%', minWidth: 940, y: 360 }"
          @page-change="handleChannelPageChange"
          @page-size-change="handleChannelPageSizeChange"
          @update:selected-keys="handleChannelSelectionChange"
        >
          <template #columns>
            <a-table-column title="通道名称" :width="210">
              <template #cell="{ record }"><strong class="share-channel-name">{{ record.name || record.channelId }}</strong></template>
            </a-table-column>
            <a-table-column title="通道国标编号" :width="210">
              <template #cell="{ record }"><code class="share-channel-code">{{ record.channelId }}</code></template>
            </a-table-column>
            <a-table-column title="所属设备" :width="250">
              <template #cell="{ record }">
                <div class="entity-cell"><span>{{ deviceNameByCode(record.deviceId) }}</span><code>{{ record.deviceId }}</code></div>
              </template>
            </a-table-column>
            <a-table-column title="厂商" :width="110">
              <template #cell="{ record }">{{ record.manufacturer || "-" }}</template>
            </a-table-column>
            <a-table-column title="状态" :width="90" align="center">
              <template #cell="{ record }"><a-tag :color="record.status === 1 ? 'green' : 'gray'">{{ record.status === 1 ? "在线" : "离线" }}</a-tag></template>
            </a-table-column>
            <a-table-column title="允许云台" :width="100" align="center">
              <template #cell="{ record }">
                <a-switch
                  size="small"
                  :model-value="channelPTZAllowed(record.id)"
                  :disabled="!sharingPlatform?.ptzEnabled || !selectedChannelIds.includes(record.id)"
                  @change="(value: boolean | string | number) => setChannelPTZAllowed(record.id, Boolean(value))"
                />
              </template>
            </a-table-column>
          </template>
          <template #empty><a-empty description="暂无符合条件的通道" /></template>
        </a-table>
        <a-alert v-if="shareError" type="error" class="cascade-alert">{{ shareError }}</a-alert>
      </a-spin>
      <template #footer>
        <div class="share-dialog-footer">
          <span>已选 {{ selectedChannelIds.length }} 个通道</span>
          <div class="dialog-actions">
            <a-button :disabled="shareSaving" @click="shareVisible = false">取消</a-button>
            <a-button type="primary" :loading="shareSaving" @click="saveShares">保存共享</a-button>
          </div>
        </div>
      </template>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import dayjs from "dayjs";
import { Building2, CircleCheck, Fingerprint, ListVideo, Plus, RadioTower, RefreshCw, Send, Settings2, Share2, SlidersHorizontal, Trash2, TriangleAlert } from "lucide-vue-next";
import {
  createCascadePlatform,
  deleteCascadePlatform,
  fetchSipNetworkInterfaces,
  fetchSipSetupStatus,
  getCascadeShares,
  listCascadePlatforms,
  pushCascadeCatalog,
  replaceCascadeShares,
  setCascadePlatformEnabled,
  updateCascadePlatform,
  type CascadeChannelProjection,
  type CascadeDeviceProjection,
  type CascadePlatform,
  type CascadePlatformInput,
  type CascadeShares,
  type GbChannel,
  type GbDevice,
  type SipNetworkAddress,
  type SipConfigSummary
} from "@/api/gb28181";
import {
  listChannels as listChannelPage,
  listDevices as listDevicePage,
  type ChannelVO,
  type DeviceVO
} from "../device-mgmt/api";
import { useUserStoreHook } from "@/store/modules/user";
import { useDevicesSize } from "@/hooks/useDevicesSize";
import SCounterSuffix from "@/components/s-counter-suffix/index.vue";
import SNumberField from "@/components/s-number-field/index.vue";
import SPasswordField from "@/components/s-password-field/index.vue";
import { cascadeCycleLabel, cascadeFormFieldErrors, cascadeLocalIdentityDefaults, cascadePresentation, defaultCascadePlatform, resolveChannelPTZAllowed, validGbId, validateCascadePlatform } from "./cascadeState";

import { deriveDomain } from "../sip/sipSetupRules";

type ChannelOption = (GbChannel | ChannelVO) & { sourceDeviceId: number };
type ShareDevice = GbDevice & Partial<DeviceVO>;

const userStore = useUserStoreHook();
const { isMobile } = useDevicesSize();
const hasPermission = (permission: string) => userStore.account.permissions.includes("*:*:*") || userStore.account.permissions.includes(permission);
const canView = computed(() => hasPermission("gb28181:cascade:view"));
const canManage = computed(() => hasPermission("gb28181:cascade:manage"));
const canEnable = computed(() => hasPermission("gb28181:cascade:enable"));
const canShare = computed(() => hasPermission("gb28181:cascade:share"));

const platforms = ref<CascadePlatform[]>([]);
const loading = ref(false);
const saving = ref(false);
const actionId = ref<number | null>(null);
const errorMessage = ref("");
const keyword = ref("");
const statusFilter = ref("all");
const editorVisible = ref(false);
const editing = ref<CascadePlatform | null>(null);
const form = reactive(defaultCascadePlatform());
const touched = reactive<Record<string, boolean>>({});
const fieldErrors = computed(() => cascadeFormFieldErrors(form, touched));
type NumberFieldInstance = InstanceType<typeof SNumberField>;
const portField = ref<NumberFieldInstance | null>(null);
const localSipPortField = ref<NumberFieldInstance | null>(null);
const registerExpiresField = ref<NumberFieldInstance | null>(null);
const keepaliveIntervalField = ref<NumberFieldInstance | null>(null);
const catalogBatchSizeField = ref<NumberFieldInstance | null>(null);
const maxStreamsField = ref<NumberFieldInstance | null>(null);
const numberFields = computed(() => [portField, localSipPortField, registerExpiresField, keepaliveIntervalField, catalogBatchSizeField, maxStreamsField]);
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
    const statusMatch = statusFilter.value === "all" || (statusFilter.value === "disabled" ? !item.enabled : item.enabled && item.overall === statusFilter.value);
    const keywordMatch = !q || [item.name, item.upstreamServerId, item.host, item.localDeviceId].some(value => String(value || "").toLowerCase().includes(q));
    return statusMatch && keywordMatch;
  });
});

const sharedSnapshots = reactive(new Map<number, CascadeShares>());
const sharedDevicesVisible = ref(false);
const sharedDevicesLoading = ref(false);
const sharedDevicesRemovingId = ref<number | null>(null);
const sharedDevicesError = ref("");
const sharedDevicesPlatform = ref<CascadePlatform | null>(null);
const sharedChannelRows = ref<Array<CascadeChannelProjection & { deviceName: string }>>([]);
function sharedSummary(id: number) {
  const snapshot = sharedSnapshots.get(id);
  if (!snapshot) return "查看已共享通道";
  const channels = snapshot.channels.filter(item => item.active !== false);
  return channels.length ? `${channels.length} 个通道` : "未共享";
}
async function loadSharedSnapshot(id: number) {
  const response: any = await getCascadeShares(id);
  const snapshot: CascadeShares = response?.data || response;
  sharedSnapshots.set(id, snapshot);
  return snapshot;
}
async function openSharedDevices(platform: CascadePlatform) {
  sharedDevicesPlatform.value = platform;
  sharedDevicesVisible.value = true;
  sharedDevicesLoading.value = true;
  sharedDevicesError.value = "";
  sharedChannelRows.value = [];
  try {
    const snapshot = await loadSharedSnapshot(platform.id);
    if (sharedDevicesPlatform.value?.id !== platform.id) return;
    const deviceNames = new Map(snapshot.devices.filter(item => item.active !== false).map(device => [device.sourceDeviceId, device.name]));
    sharedChannelRows.value = snapshot.channels.filter(item => item.active !== false).map(channel => ({
      ...channel,
      deviceName: deviceNames.get(channel.sourceDeviceId) || ""
    }));
  } catch {
    if (sharedDevicesPlatform.value?.id === platform.id) sharedDevicesError.value = "已共享设备读取失败，请重试。";
  } finally {
    if (sharedDevicesPlatform.value?.id === platform.id) sharedDevicesLoading.value = false;
  }
}

function removeSharedChannel(row: CascadeChannelProjection & { deviceName: string }) {
  const platform = sharedDevicesPlatform.value;
  if (!platform) return;
  Modal.warning({
    title: "移除共享通道",
    content: `确定停止向“${platform.name}”共享通道“${row.name || row.publishedChannelId}”吗？`,
    hideCancel: false,
    onOk: async () => {
      sharedDevicesRemovingId.value = row.sourceChannelId;
      sharedDevicesError.value = "";
      try {
        const snapshot = sharedSnapshots.get(platform.id);
        const devices = snapshot?.devices || [];
        const projectionIdBySource = new Map(devices.map(item => [item.sourceDeviceId, item.id]));
        const remaining = (snapshot?.channels || []).filter(item => item.active !== false && item.sourceChannelId !== row.sourceChannelId);
        const keepSourceIds = new Set(
          remaining
            .map(item => item.sourceDeviceId || projectionIdBySource.get(item.deviceProjectionId ?? 0))
            .filter((id): id is number => Boolean(id))
        );
        const response: any = await replaceCascadeShares(platform.id, {
          scope: "all",
          devices: devices.filter(item => item.active !== false && keepSourceIds.has(item.sourceDeviceId)),
          channels: remaining,
          expectedProjectionRevision: snapshot?.revision ?? 0
        });
        sharedSnapshots.set(platform.id, response?.data || response);
        Message.success(`已停止共享通道“${row.name || row.publishedChannelId}”`);
        await refresh();
        await openSharedDevices(platform);
      } catch (error: any) {
        const message = String(error?.message || "");
        sharedDevicesError.value = message.includes("revision conflict")
          ? "共享已被其他操作更新，请关闭弹窗后重新打开再移除。"
          : (error?.message || "移除共享通道失败，请稍后重试。");
      } finally {
        sharedDevicesRemovingId.value = null;
      }
    }
  });
}

async function refresh() {
  if (!canView.value) return;
  resetAutoRefreshCountdown();
  loading.value = true;
  errorMessage.value = "";
  try {
    const response: any = await listCascadePlatforms();
    platforms.value = response?.list || response?.data?.list || [];
    sharedSnapshots.clear();
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
          advertiseIp = addresses.find(item => item.recommended && !item.loopback && !item.listenOnly)?.ip
            || addresses.find(item => !item.loopback && !item.listenOnly)?.ip
            || "";
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
    charsetOverride: platform.charsetOverride || "GB2312",
    registerExpires: platform.registerExpires,
    keepaliveInterval: platform.keepaliveInterval,
    transport: platform.transport,
    catalogBatchSize: platform.catalogBatchSize,
    publishPlatform: platform.publishPlatform,
    publishCivil: platform.publishCivil,
    publishGroup: platform.publishGroup,
    maxStreams: platform.maxStreams,
    ptzEnabled: platform.ptzEnabled,
    enabled: platform.enabled,
    password: ""
  });
  editorVisible.value = true;
}

function buildInput(): CascadePlatformInput {
  const data: any = { ...form };
  if (!data.password) delete data.password;
  return data;
}

async function savePlatform() {
  const numberFieldError = numberFields.value.map(field => field.value?.error || "").find(Boolean);
  if (numberFieldError) {
    Message.warning(numberFieldError);
    return;
  }
  const fieldError = Object.values(cascadeFormFieldErrors(form, Object.fromEntries(Object.keys(touched).map(key => [key, true])))).find(Boolean);
  if (fieldError) {
    Object.keys(fieldErrors.value).forEach(key => (touched[key] = true));
    Message.warning(fieldError);
    return;
  }
  const errors = validateCascadePlatform(form);
  if (errors.length) return Message.warning(errors[0]);
  saving.value = true;
  try {
    if (editing.value) await updateCascadePlatform(editing.value.id, buildInput(), editing.value.configRevision);
    else await createCascadePlatform(buildInput());
    Message.success(editing.value ? "上级平台已更新" : "上级平台已创建");
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
    await setCascadePlatformEnabled(platform.id, next, platform.configRevision);
    Message.success(next ? "上级平台已启用，正在向上级注册" : "上级平台已停用，已向上级注销");
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
      await deleteCascadePlatform(platform.id);
      Message.success("上级平台已删除");
      await refresh();
    }
  });
}

const shareVisible = ref(false);
const shareLoading = ref(false);
const shareSaving = ref(false);
const channelLoading = ref(false);
const shareError = ref("");
const sharingPlatform = ref<CascadePlatform | null>(null);
const shares = ref<CascadeShares | null>(null);
const deviceDirectory = reactive(new Map<number, ShareDevice>());
const channelRows = ref<ChannelOption[]>([]);
const selectedChannelIds = ref<number[]>([]);
const channelPTZPermissions = reactive(new Map<number, boolean>());
const channelKeyword = ref("");
const channelPage = reactive({ page: 1, pageSize: 10, total: 0 });
const channelPagination = computed(() => ({ current: channelPage.page, pageSize: channelPage.pageSize, total: channelPage.total, showTotal: true, showPageSize: true, pageSizeOptions: [10, 20, 50] }));
const channelSourceDeviceIds = computed(() => {
  const sourceDeviceIds = new Map<number, number>();
  const projectionDevices = new Map((shares.value?.devices || []).map(device => [device.id, device.sourceDeviceId]));
  (shares.value?.channels || []).forEach(channel => {
    const sourceDeviceId = channel.sourceDeviceId || projectionDevices.get(channel.deviceProjectionId);
    if (sourceDeviceId) sourceDeviceIds.set(channel.sourceChannelId, sourceDeviceId);
  });
  channelRows.value.forEach(channel => {
    if (channel.sourceDeviceId) sourceDeviceIds.set(channel.id, channel.sourceDeviceId);
  });
  return sourceDeviceIds;
});
const selectedDeviceIds = computed(() => {
  const deviceIds = new Set<number>();
  selectedChannelIds.value.forEach(channelId => {
    const sourceDeviceId = channelSourceDeviceIds.value.get(channelId);
    if (sourceDeviceId) deviceIds.add(sourceDeviceId);
  });
  return [...deviceIds];
});

function unwrapPage<T>(response: any): T {
  return response?.data?.data || response?.data || response;
}

function deviceNameByCode(deviceCode: string) {
  return [...deviceDirectory.values()].find(device => device.deviceId === deviceCode)?.name || deviceCode || "-";
}

function channelPTZAllowed(channelId: number) {
  return channelPTZPermissions.get(channelId) || false;
}

function setChannelPTZAllowed(channelId: number, allowed: boolean) {
  channelPTZPermissions.set(channelId, allowed);
}

function ensureChannelPTZPermission(channelId: number) {
  if (channelPTZPermissions.has(channelId)) return;
  channelPTZPermissions.set(channelId, resolveChannelPTZAllowed(undefined, Boolean(sharingPlatform.value?.ptzEnabled)));
}

// 只用于给「所属设备」列和保存时的设备投影补名称,不参与勾选交互。
async function loadShareDevices() {
  const response: any = await listDevicePage({ page: 1, pageSize: 50 });
  const payload = unwrapPage<{ list?: ShareDevice[] }>(response) || {};
  (payload.list || []).forEach(device => deviceDirectory.set(device.id, device));
}

async function loadShareChannels(page = 1) {
  channelLoading.value = true;
  try {
    const response: any = await listChannelPage({ page, pageSize: channelPage.pageSize, q: channelKeyword.value.trim() || undefined });
    const payload = unwrapPage<{ list?: ChannelVO[]; total?: number }>(response) || {};
    channelRows.value = (payload.list || []).map(channel => ({
      ...channel,
      sourceDeviceId: channelSourceDeviceIds.value.get(channel.id) || 0
    }));
    channelPage.page = page;
    channelPage.total = Number(payload.total || 0);
  } finally {
    channelLoading.value = false;
  }
}

function queryShareChannels() {
  void loadShareChannels(1);
}

function handleChannelPageChange(page: number) {
  void loadShareChannels(page);
}

function handleChannelPageSizeChange(pageSize: number) {
  channelPage.pageSize = pageSize;
  void loadShareChannels(1);
}

async function resolveChannelSourceDevice(channel: ChannelOption) {
  if (channel.sourceDeviceId) return channel.sourceDeviceId;
  const existing = (shares.value?.channels || []).find(item => item.sourceChannelId === channel.id);
  if (existing?.sourceDeviceId) {
    channel.sourceDeviceId = existing.sourceDeviceId;
    return channel.sourceDeviceId;
  }
  const response: any = await listDevicePage({ page: 1, pageSize: 1, q: channel.deviceId });
  const payload = unwrapPage<{ list?: ShareDevice[] }>(response) || {};
  const device = payload.list?.find(item => item.deviceId === channel.deviceId);
  if (device) {
    deviceDirectory.set(device.id, device);
    channel.sourceDeviceId = device.id;
  }
  return channel.sourceDeviceId;
}

async function handleChannelSelectionChange(keys: Array<string | number>) {
  const currentIds = new Set(channelRows.value.map(channel => channel.id));
  const next = new Set(selectedChannelIds.value.filter(id => !currentIds.has(id)));
  keys.forEach(key => next.add(Number(key)));
  selectedChannelIds.value = [...next];
  selectedChannelIds.value.forEach(ensureChannelPTZPermission);
  await Promise.all(channelRows.value.filter(channel => next.has(channel.id)).map(resolveChannelSourceDevice));
}

async function openShare(platform: CascadePlatform) {
  sharingPlatform.value = platform;
  shareVisible.value = true;
  shareLoading.value = true;
  shareError.value = "";
  channelKeyword.value = "";
  channelPage.page = 1;
  deviceDirectory.clear();
  channelPTZPermissions.clear();
  channelRows.value = [];
  try {
    const projectionResponse: any = await getCascadeShares(platform.id);
    shares.value = projectionResponse?.data || projectionResponse;
    selectedChannelIds.value = (shares.value?.channels || []).filter(item => item.active !== false).map(item => item.sourceChannelId);
    (shares.value?.channels || []).forEach(item => channelPTZPermissions.set(item.sourceChannelId, item.ptzAllowed));
    await Promise.all([loadShareDevices(), loadShareChannels(1)]);
  } catch {
    shareError.value = "共享资源加载失败，请稍后重试。";
  } finally {
    shareLoading.value = false;
  }
}

async function saveShares() {
  if (!sharingPlatform.value) return;
  shareSaving.value = true;
  shareError.value = "";
  try {
    const existingDevices = new Map((shares.value?.devices || []).map(item => [item.sourceDeviceId, item]));
    const existingChannels = new Map((shares.value?.channels || []).map(item => [item.sourceChannelId, item]));
    const allLoadedChannels = [...channelRows.value];
    const loadedChannelMap = new Map<number, ChannelOption>();
    for (const channel of allLoadedChannels) {
      const previous = loadedChannelMap.get(channel.id);
      loadedChannelMap.set(channel.id, {
        ...channel,
        sourceDeviceId: channel.sourceDeviceId || previous?.sourceDeviceId || channelSourceDeviceIds.value.get(channel.id) || 0
      });
    }
    const requiredDeviceIds = new Set(selectedDeviceIds.value);
    const deviceProjection: CascadeDeviceProjection[] = [...requiredDeviceIds].map(id => {
      const source = deviceDirectory.get(id);
      const existing = existingDevices.get(id);
      return { sourceDeviceId: id, publishedDeviceId: existing?.publishedDeviceId || source?.deviceId || "", name: existing?.name || source?.name || source?.deviceId || "" };
    });
    const channelProjection: CascadeChannelProjection[] = selectedChannelIds.value.map(id => {
      const source = loadedChannelMap.get(id);
      const existing = existingChannels.get(id);
      const publishedChannelId = source?.channelId || existing?.publishedChannelId || "";
      return {
        sourceDeviceId: source?.sourceDeviceId || existing?.sourceDeviceId || 0,
        sourceChannelId: id,
        publishedChannelId,
        name: existing?.name || source?.name || source?.channelId || "",
        parentOverride: existing?.parentOverride || "",
        ptzAllowed: resolveChannelPTZAllowed(channelPTZPermissions.get(id) ?? existing?.ptzAllowed, Boolean(sharingPlatform.value?.ptzEnabled))
      };
    });
    if (channelProjection.some(item => !item.sourceDeviceId)) {
      throw new Error("部分通道无法关联所属设备，请刷新后重新选择。");
    }
    if (deviceProjection.some(item => !validGbId(item.publishedDeviceId)) || channelProjection.some(item => !validGbId(item.publishedChannelId))) {
      throw new Error("共享资源存在非 20 位国标编码，请先修正设备或通道编码。");
    }
    // 乐观锁:必须回带打开弹窗时加载的投影修订号,否则从第二次保存起必然 409。
    const response: any = await replaceCascadeShares(sharingPlatform.value.id, {
      scope: "all",
      devices: deviceProjection,
      channels: channelProjection,
      expectedProjectionRevision: shares.value?.revision ?? 0
    });
    shares.value = response?.data || response;
    Message.success(`共享已更新：${deviceProjection.length} 个设备，${channelProjection.length} 个通道`);
    shareVisible.value = false;
    await refresh();
  } catch (error: any) {
    const message = String(error?.message || "");
    shareError.value = message.includes("revision conflict")
      ? "共享已被其他操作更新，请关闭弹窗后重新打开再保存。"
      : (error?.message || "共享保存失败，服务端未应用本次选择。");
  } finally {
    shareSaving.value = false;
  }
}

function profileLabel(value: string) { return value === "auto" ? "自动协商" : `固定 ${value}`; }
function formatRelative(value?: string | null) { return value ? dayjs(value).format("YYYY-MM-DD HH:mm:ss") : "暂无记录"; }

const AUTO_REFRESH_INTERVAL_SECONDS = 10;
const autoRefreshCountdown = ref(AUTO_REFRESH_INTERVAL_SECONDS);
let refreshCountdownTimer: number | null = null;

function resetAutoRefreshCountdown() { autoRefreshCountdown.value = AUTO_REFRESH_INTERVAL_SECONDS; }

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
.cascade-page { min-height: 100%; }
.cascade-shell { display: flex; min-height: 100%; flex-direction: column; gap: 14px; }
.cascade-search-actions, .cascade-actions { display: flex; align-items: center; gap: 8px; }
.cascade-summary { display: grid; grid-template-columns: repeat(4, minmax(130px, 1fr)); gap: 12px; }
.cascade-summary-card { display: flex; min-width: 0; min-height: 78px; align-items: center; gap: 12px; padding: 14px 16px; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); box-shadow: var(--uvp-panel-shadow); }
.cascade-summary-card__icon { display: inline-flex; width: 40px; height: 40px; flex: 0 0 40px; align-items: center; justify-content: center; border-radius: 10px; }
.cascade-summary-card__icon.is-brand { color: var(--uvp-brand); background: var(--uvp-brand-soft); }
.cascade-summary-card__icon.is-enabled { color: var(--uvp-brand-strong); background: var(--uvp-brand-soft); }
.cascade-summary-card__icon.is-online { color: var(--uvp-brand-cyan); background: color-mix(in srgb, var(--uvp-brand-cyan) 12%, transparent); }
.cascade-summary-card__icon.is-attention { color: var(--uvp-warning); background: var(--uvp-warning-soft); }
.cascade-summary-card__content { display: flex; min-width: 0; flex-direction: column; gap: 3px; }
.cascade-summary-card__content strong { color: var(--uvp-text-primary); font-size: 24px; font-weight: 600; line-height: 1; }
.cascade-summary-card__content strong.is-online { color: var(--uvp-brand-cyan); }
.cascade-summary-card__content strong.is-attention { color: var(--uvp-warning); }
.cascade-summary-card__content span { color: var(--uvp-text-tertiary); font-size: 12px; }
.cascade-search-panel { margin-bottom: 0; }
.cascade-search { flex: 0 0 280px; width: 280px; }
.cascade-status-filter { flex: 0 0 148px; width: 148px; }
.cascade-status-filter :deep(.arco-select) {
  width: 100%;
  box-sizing: border-box;
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
.cascade-table { min-height: 260px; }
.entity-cell { display: flex; min-width: 0; flex-direction: column; gap: 4px; }
.entity-cell strong, .entity-cell span { overflow: hidden; color: var(--color-text-1); text-overflow: ellipsis; white-space: nowrap; }
.entity-cell code, .entity-cell small { overflow: hidden; color: var(--color-text-3); font-size: 12px; letter-spacing: 0; text-overflow: ellipsis; white-space: nowrap; }
.cascade-actions :deep(.arco-link) { display: inline-flex; align-items: center; gap: 4px; font-size: 13px; }
.refresh-countdown { color: var(--uvp-text-tertiary, var(--color-text-3)); font-variant-numeric: tabular-nums; }
.shared-channels-link { display: inline-flex; max-width: 100%; align-items: center; gap: 5px; color: rgb(var(--link-6)); }
.shared-channels-link span { overflow: hidden; color: inherit; text-decoration: underline; text-underline-offset: 3px; text-overflow: ellipsis; white-space: nowrap; }
.cascade-table :deep(.shared-channels-link:hover span) { color: rgb(var(--link-6)); text-decoration-thickness: 2px; }
.cascade-alert { flex: 0 0 auto; }
.cascade-form { display: flex; flex-direction: column; gap: 14px; }
.form-section { padding: 0 0 14px; border-bottom: 1px solid var(--color-border-2); }
.form-section > header { display: flex; align-items: center; gap: 8px; margin-bottom: 12px; color: var(--color-text-2); }
.form-section h3 { margin: 0; color: var(--color-text-1); font-size: 15px; letter-spacing: 0; }
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 16px; }
.form-grid :deep(.arco-input-number), .form-grid :deep(.arco-select) { width: 100%; }
.switch-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px 16px; }
.switch-grid label { display: flex; min-height: 36px; align-items: center; justify-content: space-between; padding: 0 10px; background: var(--color-fill-1); }
.dialog-actions { display: flex; width: 100%; align-items: center; justify-content: flex-end; gap: 8px; }
.share-workspace { display: block; }
.share-dialog-title { display: flex; min-width: 0; align-items: center; gap: 8px; }
.share-dialog-title > span { color: var(--color-text-1); font-weight: 500; }
.share-dialog-title small { overflow: hidden; margin-left: 4px; color: var(--color-text-3); font-size: 12px; font-weight: 400; text-overflow: ellipsis; white-space: nowrap; }
.share-toolbar { display: flex; align-items: center; gap: 10px; margin-bottom: 14px; }
.share-resource-search { width: 280px; margin-left: auto; }
.share-table { min-height: 454px; }
.share-table :deep(.arco-table-th) { background: var(--color-fill-1); }
.share-table :deep(.arco-table-cell) { white-space: nowrap; }
.share-channel-name { display: block; overflow: hidden; color: var(--color-text-1); font-weight: 500; text-overflow: ellipsis; }
.share-channel-code { color: var(--color-text-2); font-size: 12px; letter-spacing: 0; }
.share-dialog-footer { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: 14px; color: var(--color-text-3); font-size: 13px; }
.share-dialog-footer > span { min-width: 0; }
.share-dialog-footer .dialog-actions { width: auto; flex: 0 0 auto; }

@media (max-width: 900px) {
  .cascade-summary { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .share-toolbar { align-items: stretch; flex-wrap: wrap; }
  .share-resource-search { width: min(100%, 300px); margin-left: auto; }
}

@media (max-width: 560px) {
  .cascade-search-actions { width: 100%; align-items: stretch; }
  .cascade-search-actions :deep(.arco-btn) { flex: 1; }
  .cascade-search, .cascade-status-filter { width: 100%; flex-basis: 100%; }
  .form-grid, .switch-grid { grid-template-columns: 1fr; }
  .cascade-summary-card { min-height: 70px; padding: 12px; }
  .share-resource-search { width: 100%; margin-left: 0; }
  .share-dialog-footer { align-items: stretch; flex-direction: column; }
  .share-dialog-footer .dialog-actions { width: 100%; }
}
</style>

<style lang="scss">
.cascade-platform-dialog .arco-modal-body {
  max-height: calc(100dvh - 260px);
  overflow-y: auto;
  overscroll-behavior: contain;
}

.cascade-share-dialog .arco-modal-body {
  max-height: calc(100dvh - 230px);
  overflow-y: auto;
  overscroll-behavior: contain;
}

.shared-devices-dialog .arco-modal-body {
  max-height: calc(100dvh - 220px);
  overflow-y: auto;
  overscroll-behavior: contain;
}

@media (max-width: 560px) {
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
