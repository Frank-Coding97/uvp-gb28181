<template>
  <div class="snow-page cascade-page">
    <div class="snow-inner uvp-page-shell-flat cascade-shell">
      <a-alert v-if="!canView" type="warning" class="cascade-alert">
        无权查看国标级联，请联系管理员分配查看权限。
      </a-alert>

      <template v-else>
        <header class="cascade-header">
          <div>
            <h2>国标级联</h2>
            <p>管理 UVP 作为下级平台向上级平台的注册、资源共享和运行状态。</p>
          </div>
          <div class="cascade-header__actions">
            <a-button :loading="loading" @click="refresh">
              <template #icon><RefreshCw :size="15" /></template>
              刷新
            </a-button>
            <a-button v-if="canManage" type="primary" @click="openCreate">
              <template #icon><Plus :size="15" /></template>
              新增上级
            </a-button>
          </div>
        </header>

        <div class="cascade-summary" aria-label="级联平台摘要">
          <div class="cascade-summary__item"><span>平台总数</span><strong>{{ platforms.length }}</strong></div>
          <div class="cascade-summary__item"><span>已启用</span><strong>{{ summary.enabled }}</strong></div>
          <div class="cascade-summary__item"><span>在线</span><strong class="is-success">{{ summary.online }}</strong></div>
          <div class="cascade-summary__item"><span>需关注</span><strong :class="{ 'is-warning': summary.attention > 0 }">{{ summary.attention }}</strong></div>
        </div>

        <a-alert v-if="errorMessage" type="error" class="cascade-alert">{{ errorMessage }}</a-alert>

        <div class="cascade-toolbar">
          <a-input v-model="keyword" allow-clear placeholder="平台名称 / 编码 / 地址" class="cascade-search">
            <template #prefix><Search :size="15" /></template>
          </a-input>
          <a-select v-model="statusFilter" class="cascade-status-filter">
            <a-option value="all">全部状态</a-option>
            <a-option value="online">在线</a-option>
            <a-option value="offline">离线</a-option>
            <a-option value="disabled">已停用</a-option>
          </a-select>
        </div>

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

    <a-drawer v-model:visible="editorVisible" :width="isMobile ? '100%' : 760" :footer="false" unmount-on-close>
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
            <a-form-item label="本地 SIP 地址" required><a-input v-model="form.localSipIp" placeholder="监听或宣告 IP" /></a-form-item>
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

        <div class="drawer-actions">
          <a-button @click="editorVisible = false">取消</a-button>
          <a-button type="primary" :loading="saving" @click="savePlatform">保存</a-button>
        </div>
      </a-form>
    </a-drawer>

    <a-drawer v-model:visible="shareVisible" :width="isMobile ? '100%' : 880" :footer="false" unmount-on-close>
      <template #title>共享资源 · {{ sharingPlatform?.name }}</template>
      <a-spin :loading="shareLoading" class="share-workspace">
        <div class="share-summary">
          <span>已选设备 <strong>{{ selectedDeviceIds.length }}</strong></span>
          <span>已选通道 <strong>{{ selectedChannelIds.length }}</strong></span>
          <span>投影版本 <strong>{{ shares?.revision || 0 }}</strong></span>
        </div>
        <div class="share-grid">
          <section class="share-panel">
            <header><h3>设备</h3><small>共享通道时会自动包含所属设备</small></header>
            <a-input v-model="deviceKeyword" allow-clear placeholder="搜索设备" />
            <a-checkbox-group v-model="selectedDeviceIds" class="share-list">
              <a-checkbox v-for="device in filteredDevices" :key="device.id" :value="device.id">
                <span class="share-option"><strong>{{ device.name || device.deviceId }}</strong><code>{{ device.deviceId }}</code></span>
              </a-checkbox>
            </a-checkbox-group>
          </section>
          <section class="share-panel">
            <header><h3>通道</h3><small>选择设备后加载通道</small></header>
            <a-select v-model="activeSourceDeviceId" placeholder="选择设备" allow-search @change="loadDeviceChannels">
              <a-option v-for="device in devices" :key="device.id" :value="device.id">{{ device.name || device.deviceId }}</a-option>
            </a-select>
            <a-checkbox-group v-model="selectedChannelIds" class="share-list">
              <a-checkbox v-for="channel in activeChannels" :key="channel.id" :value="channel.id">
                <span class="share-option"><strong>{{ channel.name || channel.channelId }}</strong><code>{{ channel.channelId }}</code></span>
              </a-checkbox>
            </a-checkbox-group>
            <a-empty v-if="activeSourceDeviceId && !activeChannels.length && !channelLoading" description="该设备暂无通道" />
            <a-spin v-if="channelLoading" :loading="true" />
          </section>
        </div>
        <a-alert v-if="shareError" type="error" class="cascade-alert">{{ shareError }}</a-alert>
        <div class="drawer-actions">
          <a-button @click="shareVisible = false">取消</a-button>
          <a-button type="primary" :loading="shareSaving" @click="saveShares">保存共享</a-button>
        </div>
      </a-spin>
    </a-drawer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import dayjs from "dayjs";
import { Building2, Fingerprint, MoreHorizontal, Plus, RefreshCw, RotateCw, Search, Settings2, Share2, SlidersHorizontal } from "lucide-vue-next";
import {
  createCascadePlatform,
  deleteCascadePlatform,
  getCascadeShares,
  listCascadePlatforms,
  listChannels,
  listDevices,
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
  type GbDevice
} from "@/api/gb28181";
import { useUserStoreHook } from "@/store/modules/user";
import { useDevicesSize } from "@/hooks/useDevicesSize";
import { cascadePresentation, defaultCascadePlatform, heartbeatLabel, registrationLabel, validGbId, validateCascadePlatform } from "./cascadeState";

type ChannelOption = GbChannel & { sourceDeviceId: number };

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

function openCreate() {
  editing.value = null;
  Object.assign(form, defaultCascadePlatform());
  editorVisible.value = true;
}

function openEdit(platform: CascadePlatform) {
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
const devices = ref<GbDevice[]>([]);
const channelsByDevice = reactive(new Map<number, ChannelOption[]>());
const selectedDeviceIds = ref<number[]>([]);
const selectedChannelIds = ref<number[]>([]);
const activeSourceDeviceId = ref<number | undefined>();
const deviceKeyword = ref("");

const filteredDevices = computed(() => {
  const q = deviceKeyword.value.trim().toLowerCase();
  return devices.value.filter(device => !q || [device.name, device.deviceId].some(value => String(value || "").toLowerCase().includes(q)));
});
const activeChannels = computed(() => activeSourceDeviceId.value ? channelsByDevice.get(activeSourceDeviceId.value) || [] : []);

async function openShare(platform: CascadePlatform) {
  sharingPlatform.value = platform;
  shareVisible.value = true;
  shareLoading.value = true;
  shareError.value = "";
  channelsByDevice.clear();
  try {
    const [projectionResponse, deviceResponse]: any[] = await Promise.all([
      getCascadeShares(platform.id),
      listDevices({ page: 1, pageSize: 200 })
    ]);
    shares.value = projectionResponse?.data || projectionResponse;
    devices.value = deviceResponse?.data?.list || deviceResponse?.list || [];
    selectedDeviceIds.value = (shares.value?.devices || []).filter(item => item.active !== false).map(item => item.sourceDeviceId);
    selectedChannelIds.value = (shares.value?.channels || []).filter(item => item.active !== false).map(item => item.sourceChannelId);
    activeSourceDeviceId.value = selectedDeviceIds.value[0] || devices.value[0]?.id;
    if (activeSourceDeviceId.value) await loadDeviceChannels(activeSourceDeviceId.value);
  } catch {
    shareError.value = "共享资源加载失败，请稍后重试。";
  } finally {
    shareLoading.value = false;
  }
}

async function loadDeviceChannels(value?: string | number) {
  const sourceDeviceId = Number(value || activeSourceDeviceId.value);
  if (!sourceDeviceId || channelsByDevice.has(sourceDeviceId)) return;
  const device = devices.value.find(item => item.id === sourceDeviceId);
  if (!device) return;
  channelLoading.value = true;
  try {
    const response: any = await listChannels(device.deviceId);
    const list: GbChannel[] = response?.data?.list || response?.list || [];
    channelsByDevice.set(sourceDeviceId, list.map(channel => ({ ...channel, sourceDeviceId })));
  } finally {
    channelLoading.value = false;
  }
}

async function saveShares() {
  if (!sharingPlatform.value) return;
  shareSaving.value = true;
  shareError.value = "";
  try {
    const existingDevices = new Map((shares.value?.devices || []).map(item => [item.sourceDeviceId, item]));
    const existingChannels = new Map((shares.value?.channels || []).map(item => [item.sourceChannelId, item]));
    const allLoadedChannels = [...channelsByDevice.values()].flat();
    const loadedChannelMap = new Map(allLoadedChannels.map(item => [item.id, item]));
    const requiredDeviceIds = new Set(selectedDeviceIds.value);
    selectedChannelIds.value.forEach(id => {
      const sourceDeviceId = loadedChannelMap.get(id)?.sourceDeviceId || existingChannels.get(id)?.sourceDeviceId;
      if (sourceDeviceId) requiredDeviceIds.add(sourceDeviceId);
    });
    const deviceProjection: CascadeDeviceProjection[] = [...requiredDeviceIds].map(id => {
      const source = devices.value.find(item => item.id === id);
      const existing = existingDevices.get(id);
      return { sourceDeviceId: id, publishedDeviceId: existing?.publishedDeviceId || source?.deviceId || "", name: existing?.name || source?.name || source?.deviceId || "" };
    });
    const channelProjection: CascadeChannelProjection[] = selectedChannelIds.value.map(id => {
      const source = loadedChannelMap.get(id);
      const existing = existingChannels.get(id);
      return {
        sourceDeviceId: source?.sourceDeviceId || existing?.sourceDeviceId || 0,
        sourceChannelId: id,
        publishedChannelId: existing?.publishedChannelId || source?.channelId || "",
        name: existing?.name || source?.name || source?.channelId || "",
        parentOverride: existing?.parentOverride || "",
        ptzAllowed: existing?.ptzAllowed || false
      };
    });
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

onMounted(refresh);
</script>

<style scoped lang="scss">
.cascade-page { min-height: 100%; }
.cascade-shell { display: flex; min-height: 100%; flex-direction: column; gap: 14px; padding: 18px 20px; }
.cascade-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 18px; }
.cascade-header h2 { margin: 0; color: var(--color-text-1); font-size: 20px; letter-spacing: 0; }
.cascade-header p { margin: 5px 0 0; color: var(--color-text-3); font-size: 13px; }
.cascade-header__actions, .drawer-actions, .cascade-actions { display: flex; align-items: center; gap: 8px; }
.cascade-summary { display: grid; grid-template-columns: repeat(4, minmax(130px, 1fr)); border-block: 1px solid var(--color-border-2); background: var(--color-fill-1); }
.cascade-summary__item { display: flex; min-height: 62px; align-items: center; justify-content: space-between; padding: 0 18px; border-right: 1px solid var(--color-border-2); }
.cascade-summary__item:last-child { border-right: 0; }
.cascade-summary__item span { color: var(--color-text-3); font-size: 13px; }
.cascade-summary__item strong { color: var(--color-text-1); font-size: 22px; font-weight: 600; }
.cascade-summary__item .is-success { color: rgb(var(--green-6)); }
.cascade-summary__item .is-warning { color: rgb(var(--orange-6)); }
.cascade-toolbar { display: flex; align-items: center; gap: 10px; }
.cascade-search { width: 280px; }
.cascade-status-filter { width: 132px; }
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
.form-section h3, .share-panel h3 { margin: 0; color: var(--color-text-1); font-size: 15px; letter-spacing: 0; }
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 16px; }
.form-grid :deep(.arco-input-number), .form-grid :deep(.arco-select) { width: 100%; }
.switch-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px 16px; }
.switch-grid label { display: flex; min-height: 36px; align-items: center; justify-content: space-between; padding: 0 10px; background: var(--color-fill-1); }
.drawer-actions { justify-content: flex-end; padding-top: 4px; }
.share-workspace { display: block; }
.share-summary { display: flex; gap: 20px; margin-bottom: 14px; padding: 10px 12px; background: var(--color-fill-1); color: var(--color-text-3); }
.share-summary strong { margin-left: 4px; color: var(--color-text-1); }
.share-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; min-height: 420px; }
.share-panel { display: flex; min-width: 0; flex-direction: column; gap: 10px; padding: 14px; border: 1px solid var(--color-border-2); border-radius: 6px; }
.share-panel header { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; }
.share-panel header small { color: var(--color-text-3); }
.share-list { display: flex; max-height: 360px; flex-direction: column; gap: 2px; overflow: auto; }
.share-list :deep(.arco-checkbox) { width: 100%; margin: 0; padding: 8px; }
.share-list :deep(.arco-checkbox:hover) { background: var(--color-fill-2); }
.share-option { display: flex; min-width: 0; flex-direction: column; gap: 3px; }
.share-option strong, .share-option code { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.share-option code { color: var(--color-text-3); font-size: 12px; letter-spacing: 0; }

@media (max-width: 900px) {
  .cascade-shell { padding: 14px 12px; }
  .cascade-header { align-items: stretch; flex-direction: column; }
  .cascade-header__actions { justify-content: flex-end; }
  .cascade-summary { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .cascade-summary__item:nth-child(2) { border-right: 0; }
  .cascade-summary__item:nth-child(-n + 2) { border-bottom: 1px solid var(--color-border-2); }
  .share-grid { grid-template-columns: 1fr; }
}

@media (max-width: 560px) {
  .cascade-header__actions, .cascade-toolbar { align-items: stretch; flex-direction: column; }
  .cascade-search, .cascade-status-filter { width: 100%; }
  .form-grid, .switch-grid { grid-template-columns: 1fr; }
  .cascade-summary__item { min-height: 54px; padding: 0 12px; }
}
</style>
