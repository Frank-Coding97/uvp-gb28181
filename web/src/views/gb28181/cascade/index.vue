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
            <div class="cascade-status-filter">
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
              <a-button class="uvp-page-action-btn uvp-refresh-btn" :loading="loading" @click="refresh">
                <template #icon><RefreshCw :size="15" /></template>
                刷新
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
          :scroll="{ x: '100%', minWidth: 1260 }"
        >
          <template #columns>
            <a-table-column title="上级平台" :width="220">
              <template #cell="{ record }">
                <div class="entity-cell">
                  <strong>{{ record.name }}</strong>
                  <code>{{ record.upstreamServerId }}</code>
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
            <a-table-column title="协议" :width="112">
              <template #cell="{ record }">
                <div class="entity-cell"><span>GB/T {{ record.effectiveVersion || '2016' }}</span><small>{{ profileLabel(record.profileOverride) }}</small></div>
              </template>
            </a-table-column>
            <a-table-column title="运行状态" :width="150">
              <template #cell="{ record }">
                <a-tooltip :content="cascadePresentation(record).detail">
                  <a-tag :color="cascadePresentation(record).color">{{ cascadePresentation(record).label }}</a-tag>
                </a-tooltip>
              </template>
            </a-table-column>
            <a-table-column title="注册 / 心跳" :width="172">
              <template #cell="{ record }">
                <div class="entity-cell"><span>{{ registrationLabel(record.registration) }} / {{ heartbeatLabel(record.heartbeat) }}</span><small>{{ formatRelative(record.heartbeatAt || record.registerAt) }}</small></div>
              </template>
            </a-table-column>
            <a-table-column title="共享" :width="108">
              <template #cell="{ record }"><span>{{ record.projectionRevision ? `版本 ${record.projectionRevision}` : "未配置" }}</span></template>
            </a-table-column>
            <a-table-column title="操作" :width="250" align="center" :fixed="isMobile ? '' : 'right'">
              <template #cell="{ record }">
                <div class="uvp-table-actions cascade-actions">
                  <a-link v-if="canManage" @click="openEdit(record)"><Settings2 :size="14" />编辑</a-link>
                  <a-link v-if="canShare" @click="openShare(record)"><Share2 :size="14" />共享</a-link>
                  <a-link v-if="canReconnect && record.enabled" :loading="actionId === record.id" @click="reconnect(record)"><RotateCw :size="14" />重连</a-link>
                  <a-dropdown trigger="click">
                    <a-link><MoreHorizontal :size="16" /></a-link>
                    <template #content>
                      <a-doption v-if="canEnable" @click="toggleEnabled(record)">{{ record.enabled ? "停用" : "启用" }}</a-doption>
                      <a-doption v-if="canManage" class="cascade-danger" @click="confirmDelete(record)">删除</a-doption>
                    </template>
                  </a-dropdown>
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
            <a-form-item label="平台名称" required><a-input v-model="form.name" maxlength="128" /></a-form-item>
            <a-form-item label="上级平台 ID" required><a-input v-model="form.upstreamServerId" maxlength="20" /></a-form-item>
            <a-form-item label="上级域" required><a-input v-model="form.upstreamDomain" /></a-form-item>
            <a-form-item label="上级地址" required><a-input v-model="form.host" /></a-form-item>
            <a-form-item label="上级端口" required><a-input-number v-model="form.port" :min="1" :max="65535" /></a-form-item>
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
            <a-form-item label="本平台设备 ID" required><a-input v-model="form.localDeviceId" maxlength="20" /></a-form-item>
            <a-form-item label="本平台域" required><a-input v-model="form.localDomain" /></a-form-item>
            <a-form-item label="本地 SIP 地址" required>
              <a-select v-model="form.localSipIp" :loading="networkLoading" allow-search allow-create placeholder="选择本机网卡或输入宣告地址">
                <a-option v-for="item in localSipAddresses" :key="item.ip" :value="item.ip" :label="item.ip">
                  {{ item.ip }} <span v-if="item.interfaceName">（{{ item.interfaceName }}）</span>
                  <a-tag v-if="item.recommended" size="small" color="green">推荐</a-tag>
                  <a-tag v-if="item.virtual" size="small">虚拟网卡</a-tag>
                </a-option>
              </a-select>
            </a-form-item>
            <a-form-item label="本地 SIP 端口" required><a-input-number v-model="form.localSipPort" :min="1" :max="65535" /></a-form-item>
            <a-form-item label="媒体宣告地址"><a-input v-model="form.mediaAdvertiseIp" allow-clear /></a-form-item>
            <a-form-item label="认证用户名"><a-input v-model="form.authUsername" allow-clear /></a-form-item>
            <a-form-item label="认证密码">
              <a-input-password v-model="form.password" allow-clear :placeholder="editing?.hasPassword ? '留空则保持原密码' : '未配置可留空'" />
            </a-form-item>
          </div>
        </section>

        <section class="form-section">
          <header><SlidersHorizontal :size="17" /><h3>注册与能力</h3></header>
          <div class="form-grid">
            <a-form-item label="协议版本"><a-select v-model="form.profileOverride"><a-option value="auto">自动协商</a-option><a-option value="2016">固定 2016</a-option><a-option value="2022">固定 2022</a-option></a-select></a-form-item>
            <a-form-item label="字符集覆盖"><a-select v-model="form.charsetOverride" allow-clear><a-option value="GB2312">GB2312</a-option><a-option value="UTF-8">UTF-8</a-option></a-select></a-form-item>
            <a-form-item label="注册有效期（秒）"><a-input-number v-model="form.registerExpires" :min="60" :max="86400" /></a-form-item>
            <a-form-item label="心跳间隔（秒）"><a-input-number v-model="form.keepaliveInterval" :min="5" :max="3600" /></a-form-item>
            <a-form-item label="目录批量大小"><a-input-number v-model="form.catalogBatchSize" :min="1" :max="1000" /></a-form-item>
            <a-form-item label="最大并发流"><a-input-number v-model="form.maxStreams" :min="1" :max="10000" /></a-form-item>
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
          <a-radio-group v-model="shareMode" type="button" size="small" class="share-mode-switch" @change="switchShareMode">
            <a-radio value="device">按设备</a-radio>
            <a-radio value="channel">按通道</a-radio>
          </a-radio-group>
          <span class="share-mode-hint">{{ shareMode === "device" ? "设备模式下会将该设备的全部通道加入共享" : "通道模式下仅共享勾选通道" }}</span>
          <a-input-search
            v-if="shareMode === 'device'"
            v-model="deviceKeyword"
            allow-clear
            placeholder="设备名称 / 国标编号"
            class="share-resource-search"
            @search="queryShareDevices"
          />
          <a-input-search
            v-else
            v-model="channelKeyword"
            allow-clear
            placeholder="通道名称 / 国标编号"
            class="share-resource-search"
            @search="queryShareChannels"
          />
        </div>
        <a-table
          v-if="shareMode === 'device'"
          :selected-keys="selectedDeviceKeys"
          class="share-device-table share-table"
          row-key="id"
          :data="devices"
          :loading="channelLoading"
          :bordered="false"
          :row-selection="{ type: 'checkbox', showCheckedAll: true }"
          :pagination="devicePagination"
          :scroll="{ x: '100%', minWidth: 850, y: 360 }"
          @page-change="handleDevicePageChange"
          @page-size-change="handleDevicePageSizeChange"
          @update:selected-keys="handleDeviceSelectionChange"
        >
          <template #columns>
            <a-table-column title="设备名称" :width="220">
              <template #cell="{ record }"><strong class="share-channel-name">{{ record.name || record.deviceId }}</strong></template>
            </a-table-column>
            <a-table-column title="设备国标编号" :width="230">
              <template #cell="{ record }"><code class="share-channel-code">{{ record.deviceId }}</code></template>
            </a-table-column>
            <a-table-column title="厂商" :width="150">
              <template #cell="{ record }">{{ record.manufacturer || "-" }}</template>
            </a-table-column>
            <a-table-column title="通道数" :width="100" align="center">
              <template #cell="{ record }">{{ record.channelCount ?? 0 }}</template>
            </a-table-column>
            <a-table-column title="在线通道" :width="110" align="center">
              <template #cell="{ record }">{{ record.channelOnlineCount ?? 0 }}</template>
            </a-table-column>
            <a-table-column title="状态" :width="90" align="center">
              <template #cell="{ record }"><a-tag :color="record.status === 1 ? 'green' : 'gray'">{{ record.status === 1 ? "在线" : "离线" }}</a-tag></template>
            </a-table-column>
          </template>
          <template #empty><a-empty description="暂无符合条件的设备" /></template>
        </a-table>
        <a-table
          v-else
          :selected-keys="selectedChannelIds"
          class="share-channel-table share-table"
          row-key="id"
          :data="channelRows"
          :loading="channelLoading"
          :bordered="false"
          :row-selection="{ type: 'checkbox', showCheckedAll: true }"
          :pagination="channelPagination"
          :scroll="{ x: '100%', minWidth: 850, y: 360 }"
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
          </template>
          <template #empty><a-empty description="暂无符合条件的通道" /></template>
        </a-table>
        <a-alert v-if="shareError" type="error" class="cascade-alert">{{ shareError }}</a-alert>
      </a-spin>
      <template #footer>
        <div class="share-dialog-footer">
          <span>已选 {{ selectedChannelIds.length }} 个通道，覆盖 {{ selectedDeviceIds.length }} 个设备目录</span>
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
import { computed, onMounted, reactive, ref } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import dayjs from "dayjs";
import { Building2, CircleCheck, Fingerprint, MoreHorizontal, Plus, RadioTower, RefreshCw, RotateCw, Settings2, Share2, SlidersHorizontal, TriangleAlert } from "lucide-vue-next";
import {
  createCascadePlatform,
  deleteCascadePlatform,
  fetchSipNetworkInterfaces,
  fetchSipSetupStatus,
  getCascadeShares,
  listCascadePlatforms,
  listChannels,
  reconnectCascadePlatform,
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
import { cascadeLocalIdentityDefaults, cascadePresentation, defaultCascadePlatform, heartbeatLabel, registrationLabel, uniquePublishedGbId, validGbId, validateCascadePlatform } from "./cascadeState";

type ShareMode = "device" | "channel";
type ChannelOption = (GbChannel | ChannelVO) & { sourceDeviceId: number };
type ShareDevice = GbDevice & Partial<DeviceVO>;

const userStore = useUserStoreHook();
const { isMobile } = useDevicesSize();
const hasPermission = (permission: string) => userStore.account.permissions.includes("*:*:*") || userStore.account.permissions.includes(permission);
const canView = computed(() => hasPermission("gb28181:cascade:view"));
const canManage = computed(() => hasPermission("gb28181:cascade:manage"));
const canEnable = computed(() => hasPermission("gb28181:cascade:enable"));
const canShare = computed(() => hasPermission("gb28181:cascade:share"));
const canReconnect = computed(() => hasPermission("gb28181:cascade:reconnect"));

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

async function refresh() {
  if (!canView.value) return;
  loading.value = true;
  errorMessage.value = "";
  try {
    const response: any = await listCascadePlatforms();
    platforms.value = response?.list || response?.data?.list || [];
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

async function openCreate() {
  void loadLocalSipAddresses();
  if (!localSipConfig.value) await loadLocalSipConfig();
  editing.value = null;
  Object.assign(form, defaultCascadePlatform(), cascadeLocalIdentityDefaults(localSipConfig.value || undefined));
  editorVisible.value = true;
}

function openEdit(platform: CascadePlatform) {
  void loadLocalSipAddresses();
  editing.value = platform;
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
    authUsername: platform.authUsername || "",
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

async function toggleEnabled(platform: CascadePlatform) {
  actionId.value = platform.id;
  try {
    await setCascadePlatformEnabled(platform.id, !platform.enabled, platform.configRevision);
    Message.success(platform.enabled ? "上级平台已停用" : "上级平台已启用");
    await refresh();
  } finally {
    actionId.value = null;
  }
}

async function reconnect(platform: CascadePlatform) {
  actionId.value = platform.id;
  try {
    await reconnectCascadePlatform(platform.id);
    Message.success("已发起重新注册");
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
const devices = ref<ShareDevice[]>([]);
const deviceDirectory = reactive(new Map<number, ShareDevice>());
const channelsByDevice = reactive(new Map<number, ChannelOption[]>());
const channelRows = ref<ChannelOption[]>([]);
const selectedChannelIds = ref<number[]>([]);
const shareMode = ref<ShareMode>("channel");
const deviceKeyword = ref("");
const channelKeyword = ref("");
const devicePage = reactive({ page: 1, pageSize: 10, total: 0 });
const channelPage = reactive({ page: 1, pageSize: 10, total: 0 });
const devicePagination = computed(() => ({ current: devicePage.page, pageSize: devicePage.pageSize, total: devicePage.total, showTotal: true, showPageSize: true, pageSizeOptions: [10, 20, 50] }));
const channelPagination = computed(() => ({ current: channelPage.page, pageSize: channelPage.pageSize, total: channelPage.total, showTotal: true, showPageSize: true, pageSizeOptions: [10, 20, 50] }));
const channelSourceDeviceIds = computed(() => {
  const sourceDeviceIds = new Map<number, number>();
  const projectionDevices = new Map((shares.value?.devices || []).map(device => [device.id, device.sourceDeviceId]));
  (shares.value?.channels || []).forEach(channel => {
    const sourceDeviceId = channel.sourceDeviceId || projectionDevices.get(channel.deviceProjectionId);
    if (sourceDeviceId) sourceDeviceIds.set(channel.sourceChannelId, sourceDeviceId);
  });
  channelsByDevice.forEach(channels => channels.forEach(channel => sourceDeviceIds.set(channel.id, channel.sourceDeviceId)));
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
const selectedDeviceKeys = computed(() => devices.value.filter(device => {
  const channels = channelsByDevice.get(device.id);
  return Boolean(channels?.length && channels.every(channel => selectedChannelIds.value.includes(channel.id)));
}).map(device => device.id));

function unwrapPage<T>(response: any): T {
  return response?.data?.data || response?.data || response;
}

function deviceNameByCode(deviceCode: string) {
  return [...deviceDirectory.values()].find(device => device.deviceId === deviceCode)?.name || deviceCode || "-";
}

async function loadShareDevices(page = 1) {
  const response: any = await listDevicePage({ page, pageSize: devicePage.pageSize, q: deviceKeyword.value.trim() || undefined });
  const payload = unwrapPage<{ list?: ShareDevice[]; total?: number }>(response) || {};
  devices.value = payload.list || [];
  devices.value.forEach(device => deviceDirectory.set(device.id, device));
  devicePage.page = page;
  devicePage.total = Number(payload.total || 0);
  if (shareMode.value === "device") {
    const selectedIds = new Set(selectedDeviceIds.value);
    await Promise.all(devices.value.filter(device => selectedIds.has(device.id)).map(loadAllDeviceChannels));
  }
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

async function switchShareMode(mode: string | number | boolean) {
  shareMode.value = String(mode) as ShareMode;
  if (shareMode.value === "device") await loadShareDevices(1);
  else await loadShareChannels(1);
}

function queryShareDevices() {
  void loadShareDevices(1);
}

function queryShareChannels() {
  void loadShareChannels(1);
}

function handleDevicePageChange(page: number) {
  void loadShareDevices(page);
}

function handleDevicePageSizeChange(pageSize: number) {
  devicePage.pageSize = pageSize;
  void loadShareDevices(1);
}

function handleChannelPageChange(page: number) {
  void loadShareChannels(page);
}

function handleChannelPageSizeChange(pageSize: number) {
  channelPage.pageSize = pageSize;
  void loadShareChannels(1);
}

async function loadAllDeviceChannels(device: ShareDevice) {
  if (channelsByDevice.has(device.id)) return channelsByDevice.get(device.id) || [];
  const response: any = await listChannels(device.deviceId);
  const list: GbChannel[] = response?.data?.list || response?.list || [];
  const channels = list.map(channel => ({ ...channel, sourceDeviceId: device.id }));
  channelsByDevice.set(device.id, channels);
  return channels;
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
  await Promise.all(channelRows.value.filter(channel => next.has(channel.id)).map(resolveChannelSourceDevice));
}

async function handleDeviceSelectionChange(keys: Array<string | number>) {
  const previous = new Set(selectedDeviceKeys.value);
  const next = new Set(keys.map(Number));
  const selected = new Set(selectedChannelIds.value);
  const changedDevices = devices.value.filter(device => previous.has(device.id) !== next.has(device.id));
  channelLoading.value = true;
  try {
    const channelGroups = await Promise.all(changedDevices.map(loadAllDeviceChannels));
    channelGroups.forEach((channels, index) => {
      const isSelected = next.has(changedDevices[index].id);
      channels.forEach(channel => isSelected ? selected.add(channel.id) : selected.delete(channel.id));
    });
    selectedChannelIds.value = [...selected];
  } finally {
    channelLoading.value = false;
  }
}

async function openShare(platform: CascadePlatform) {
  sharingPlatform.value = platform;
  shareVisible.value = true;
  shareLoading.value = true;
  shareError.value = "";
  deviceKeyword.value = "";
  channelKeyword.value = "";
  shareMode.value = "channel";
  devicePage.page = 1;
  channelPage.page = 1;
  deviceDirectory.clear();
  channelsByDevice.clear();
  channelRows.value = [];
  try {
    const projectionResponse: any = await getCascadeShares(platform.id);
    shares.value = projectionResponse?.data || projectionResponse;
    selectedChannelIds.value = (shares.value?.channels || []).filter(item => item.active !== false).map(item => item.sourceChannelId);
    await Promise.all([loadShareDevices(1), loadShareChannels(1)]);
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
    const allLoadedChannels = [...channelsByDevice.values()].flat().concat(channelRows.value);
    const loadedChannelMap = new Map(allLoadedChannels.map(item => [item.id, item]));
    const requiredDeviceIds = new Set(selectedDeviceIds.value);
    const deviceProjection: CascadeDeviceProjection[] = [...requiredDeviceIds].map(id => {
      const source = deviceDirectory.get(id);
      const existing = existingDevices.get(id);
      return { sourceDeviceId: id, publishedDeviceId: existing?.publishedDeviceId || source?.deviceId || "", name: existing?.name || source?.name || source?.deviceId || "" };
    });
    const usedPublishedIds = new Set(deviceProjection.map(item => item.publishedDeviceId).filter(Boolean));
    const channelProjection: CascadeChannelProjection[] = selectedChannelIds.value.map(id => {
      const source = loadedChannelMap.get(id);
      const existing = existingChannels.get(id);
      const publishedChannelId = uniquePublishedGbId(existing?.publishedChannelId || source?.channelId || "", id, usedPublishedIds);
      return {
        sourceDeviceId: source?.sourceDeviceId || existing?.sourceDeviceId || 0,
        sourceChannelId: id,
        publishedChannelId,
        name: existing?.name || source?.name || source?.channelId || "",
        parentOverride: existing?.parentOverride || "",
        ptzAllowed: existing?.ptzAllowed || false
      };
    });
    if (channelProjection.some(item => !item.sourceDeviceId)) {
      throw new Error("部分通道无法关联所属设备，请刷新后重新选择。");
    }
    if (deviceProjection.some(item => !validGbId(item.publishedDeviceId)) || channelProjection.some(item => !validGbId(item.publishedChannelId))) {
      throw new Error("共享资源存在非 20 位国标编码，请先修正设备或通道编码。");
    }
    const response: any = await replaceCascadeShares(sharingPlatform.value.id, { scope: "all", devices: deviceProjection, channels: channelProjection });
    shares.value = response?.data || response;
    Message.success(`共享已更新：${deviceProjection.length} 个设备，${channelProjection.length} 个通道`);
    shareVisible.value = false;
    await refresh();
  } catch (error: any) {
    shareError.value = error?.message || "共享保存失败，服务端未应用本次选择。";
  } finally {
    shareSaving.value = false;
  }
}

function profileLabel(value: string) { return value === "auto" ? "自动协商" : `固定 ${value}`; }
function formatRelative(value?: string | null) { return value ? dayjs(value).format("MM-DD HH:mm:ss") : "暂无记录"; }

onMounted(() => {
  refresh();
  loadLocalSipConfig();
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
.cascade-status-filter :deep(.arco-select) { width: 100%; }
.cascade-table { min-height: 260px; }
.entity-cell { display: flex; min-width: 0; flex-direction: column; gap: 4px; }
.entity-cell strong, .entity-cell span { overflow: hidden; color: var(--color-text-1); text-overflow: ellipsis; white-space: nowrap; }
.entity-cell code, .entity-cell small { overflow: hidden; color: var(--color-text-3); font-size: 12px; letter-spacing: 0; text-overflow: ellipsis; white-space: nowrap; }
.cascade-actions :deep(.arco-link) { display: inline-flex; align-items: center; gap: 4px; font-size: 13px; }
.cascade-danger { color: rgb(var(--red-6)); }
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
.share-mode-switch { flex: 0 0 auto; }
.share-mode-switch :deep(.arco-radio-button) { min-width: 84px; text-align: center; }
.share-mode-hint { min-width: 0; color: var(--color-text-3); font-size: 12px; }
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
  .share-mode-hint { display: flex; align-items: center; }
  .share-resource-search { width: min(100%, 300px); margin-left: auto; }
}

@media (max-width: 560px) {
  .cascade-search-actions { width: 100%; align-items: stretch; }
  .cascade-search-actions :deep(.arco-btn) { flex: 1; }
  .cascade-search, .cascade-status-filter { width: 100%; flex-basis: 100%; }
  .form-grid, .switch-grid { grid-template-columns: 1fr; }
  .cascade-summary-card { min-height: 70px; padding: 12px; }
  .share-mode-switch, .share-resource-search { width: 100%; margin-left: 0; }
  .share-mode-switch :deep(.arco-radio-button) { min-width: 0; flex: 1; }
  .share-mode-hint { width: 100%; }
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
