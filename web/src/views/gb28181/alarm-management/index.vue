<template>
  <div class="snow-page alarm-management-page">
    <div class="snow-inner uvp-page-shell-flat">
      <a-alert v-if="!canView" class="alarm-state" type="warning">无权查看告警，请联系管理员分配告警查看权限。</a-alert>

      <template v-else>
        <header class="alarm-page-header">
          <div>
            <h2>告警管理</h2>
            <p>集中查看权限范围内的设备告警；平台接收时间与设备告警时间分开展示。</p>
          </div>
          <a-button data-testid="alarm-refresh" :loading="loading" @click="refresh">
            <template #icon><RefreshCw :size="15" /></template>
            刷新
          </a-button>
        </header>

        <s-layout-search>
          <template #fields>
            <a-select
              v-model="form.deviceId"
              data-testid="device-filter"
              placeholder="设备名称 / 国标编码"
              allow-clear
              allow-search
              :loading="deviceLoading"
              :filter-option="false"
              style="width: 210px"
              @search="searchDevices"
            >
              <a-option v-for="device in deviceOptions" :key="device.id" :value="device.id">
                {{ device.label }}
              </a-option>
            </a-select>
            <a-input
              v-model="form.sourceCode"
              data-testid="source-code"
              placeholder="来源编码"
              allow-clear
              style="width: 176px"
              @press-enter="query"
            />
            <a-range-picker
              v-model="form.alarmRange"
              data-testid="alarm-range"
              show-time
              value-format="YYYY-MM-DDTHH:mm:ssZ"
              allow-clear
              style="width: 330px"
            />
            <a-select v-model="form.priority" placeholder="告警级别" allow-clear style="width: 126px">
              <a-option v-for="option in priorityOptions" :key="option.value" :value="option.value">{{ option.label }}</a-option>
            </a-select>
            <a-select v-model="form.method" placeholder="告警方法" allow-clear style="width: 138px">
              <a-option v-for="option in methodOptions" :key="option.value" :value="option.value">{{ option.label }}</a-option>
            </a-select>
            <a-select v-model="form.alarmType" placeholder="告警类型" allow-clear style="width: 126px">
              <a-option v-for="value in alarmTypeOptions" :key="value" :value="value">类型 {{ value }}</a-option>
            </a-select>
            <a-input
              v-model="form.keyword"
              data-testid="keyword"
              placeholder="描述 / 设备 / 通道关键词"
              allow-clear
              style="width: 220px"
              @press-enter="query"
            />
          </template>
          <template #actions>
            <a-button type="primary" data-testid="alarm-query" @click="query">
              <template #icon><Search :size="15" /></template>
              查询
            </a-button>
            <a-button data-testid="alarm-reset" @click="reset">
              <template #icon><RotateCcw :size="15" /></template>
              重置
            </a-button>
          </template>
        </s-layout-search>

        <a-alert v-if="errorMessage && !loading" class="alarm-state" type="error" closable @close="errorMessage = ''">
          {{ errorMessage }}
        </a-alert>

        <a-table
          v-else
          v-model:selected-keys="selectedKeys"
          class="uvp-data-table"
          data-testid="alarm-table"
          row-key="id"
          :data="alarms"
          :bordered="false"
          :loading="loading"
          :pagination="pagination"
          :row-selection="{ type: 'checkbox', showCheckedAll: true }"
          :scroll="tableScroll"
          @page-change="handlePageChange"
          @page-size-change="handlePageSizeChange"
        >
          <template #columns>
            <a-table-column title="平台接收时间" :width="176">
              <template #cell="{ record }">{{ formatDateTime(record.receivedAt) }}</template>
            </a-table-column>
            <a-table-column title="设备告警时间" :width="176">
              <template #cell="{ record }">{{ formatDateTime(record.alarmTime) }}</template>
            </a-table-column>
            <a-table-column title="设备" :width="188">
              <template #cell="{ record }">
                <div class="alarm-entity-cell">
                  <span>{{ displayAlarmEntityName(record.device) }}</span>
                  <small>{{ record.device.code }}</small>
                </div>
              </template>
            </a-table-column>
            <a-table-column title="来源" :width="188">
              <template #cell="{ record }">
                <div class="alarm-entity-cell">
                  <span>{{ displayAlarmEntityName(record.channel, record.sourceCode || "未知来源") }}</span>
                  <small>{{ record.sourceCode || "—" }}</small>
                </div>
              </template>
            </a-table-column>
            <a-table-column title="级别" :width="112">
              <template #cell="{ record }"><a-tag>{{ enumText(record.priority) }}</a-tag></template>
            </a-table-column>
            <a-table-column title="方法" :width="132">
              <template #cell="{ record }">{{ enumText(record.method) }}</template>
            </a-table-column>
            <a-table-column title="类型" :width="190">
              <template #cell="{ record }">{{ enumText(record.alarmType) }}</template>
            </a-table-column>
            <a-table-column title="告警描述" :width="260" :ellipsis="true" :tooltip="true">
              <template #cell="{ record }">{{ record.description || "—" }}</template>
            </a-table-column>
            <a-table-column title="操作" :width="104" align="center" :fixed="isMobile ? '' : 'right'">
              <template #cell="{ record }">
                <div class="uvp-table-actions">
                  <a-link class="uvp-table-action uvp-table-action--detail" @click="openDetail(record.id)">详情</a-link>
                </div>
              </template>
            </a-table-column>
          </template>
          <template #empty>
            <a-empty description="当前筛选条件下暂无告警" />
          </template>
        </a-table>
      </template>
    </div>
  </div>

  <AlarmDetailDrawer
    v-model:visible="detailVisible"
    :alarm-id="detailAlarmId"
    :can-delete="canDelete"
  />
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { RefreshCw, RotateCcw, Search } from "@lucide/vue";
import { useDevicesSize } from "@/hooks/useDevicesSize";
import useGlobalProperties from "@/hooks/useGlobalProperties";
import { useUserStoreHook } from "@/store/modules/user";
import { listDevices, type DeviceVO } from "../device-mgmt/api";
import AlarmDetailDrawer from "./components/AlarmDetailDrawer.vue";
import { displayAlarmEntityName, mayDeleteAlarms, mayViewAlarms, normalizeAlarmQuery } from "./alarmState";
import { listAlarms, type AlarmEnumValue, type AlarmListItem, type AlarmQuery } from "./api";

interface DeviceOption {
  id: number;
  label: string;
}

const { isMobile } = useDevicesSize();
const proxy = useGlobalProperties();
const userStore = useUserStoreHook();
const canView = computed(() => mayViewAlarms(userStore.account.permissions));
const canDelete = computed(() => mayDeleteAlarms(userStore.account.permissions));

const form = reactive({
  deviceId: undefined as number | undefined,
  sourceCode: "",
  alarmRange: [] as string[],
  priority: undefined as number | undefined,
  method: undefined as number | undefined,
  alarmType: undefined as number | undefined,
  keyword: ""
});
const alarms = ref<AlarmListItem[]>([]);
const selectedKeys = ref<string[]>([]);
const loading = ref(false);
const errorMessage = ref("");
const deviceLoading = ref(false);
const deviceOptions = ref<DeviceOption[]>([]);
const detailVisible = ref(false);
const detailAlarmId = ref<string | null>(null);
const pagination = reactive({
  current: 1,
  pageSize: 20,
  total: 0,
  showTotal: true,
  showJumper: true,
  showPageSize: true,
  pageSizeOptions: [10, 20, 50, 100]
});
const tableScroll = computed(() => ({ x: "100%", minWidth: 1526, ...(alarms.value.length ? { y: "100%" } : {}) }));

const priorityOptions = [1, 2, 3, 4].map(value => ({ value, label: `${["一", "二", "三", "四"][value - 1]}级警情` }));
const methodOptions = [
  { value: 1, label: "电话报警" },
  { value: 2, label: "设备报警" },
  { value: 3, label: "短信报警" },
  { value: 4, label: "GPS 报警" },
  { value: 5, label: "视频报警" },
  { value: 6, label: "设备故障" },
  { value: 7, label: "其他报警" }
];
const alarmTypeOptions = Array.from({ length: 13 }, (_, index) => index + 1);

let listRequestToken = 0;
let deviceRequestToken = 0;

function currentQuery(): AlarmQuery {
  return normalizeAlarmQuery({
    page: pagination.current,
    pageSize: pagination.pageSize,
    ...(form.deviceId !== undefined ? { deviceId: form.deviceId } : {}),
    sourceCode: form.sourceCode,
    ...(form.alarmRange.length ? { alarmFrom: form.alarmRange[0], alarmTo: form.alarmRange[1] } : {}),
    ...(form.priority !== undefined ? { priority: form.priority } : {}),
    ...(form.method !== undefined ? { method: form.method } : {}),
    ...(form.alarmType !== undefined ? { alarmType: form.alarmType } : {}),
    keyword: form.keyword
  });
}

async function loadAlarms() {
  if (!canView.value) return;
  const token = ++listRequestToken;
  loading.value = true;
  errorMessage.value = "";
  try {
    const response = await listAlarms(currentQuery());
    if (token !== listRequestToken) return;
    alarms.value = response.data.list ?? [];
    pagination.total = response.data.total ?? 0;
    pagination.current = response.data.page ?? pagination.current;
    pagination.pageSize = response.data.pageSize ?? pagination.pageSize;
  } catch (error) {
    if (token !== listRequestToken) return;
    alarms.value = [];
    pagination.total = 0;
    errorMessage.value = "查询告警失败，请稍后重试";
    console.error(error);
  } finally {
    if (token === listRequestToken) loading.value = false;
  }
}

function query() {
  pagination.current = 1;
  selectedKeys.value = [];
  loadAlarms();
}

function reset() {
  Object.assign(form, {
    deviceId: undefined,
    sourceCode: "",
    alarmRange: [],
    priority: undefined,
    method: undefined,
    alarmType: undefined,
    keyword: ""
  });
  pagination.current = 1;
  selectedKeys.value = [];
  loadAlarms();
}

function refresh() {
  loadAlarms();
}

function openDetail(id: string) {
  detailAlarmId.value = id;
  detailVisible.value = true;
}

function handlePageChange(page: number) {
  pagination.current = page;
  selectedKeys.value = [];
  loadAlarms();
}

function handlePageSizeChange(pageSize: number) {
  pagination.current = 1;
  pagination.pageSize = pageSize;
  selectedKeys.value = [];
  loadAlarms();
}

async function searchDevices(keyword: string) {
  const token = ++deviceRequestToken;
  deviceLoading.value = true;
  try {
    const response = await listDevices({ q: keyword.trim() || undefined, page: 1, pageSize: 20, sort: "id desc" });
    if (token !== deviceRequestToken) return;
    deviceOptions.value = (response.data.list ?? []).map((device: DeviceVO) => ({
      id: device.id,
      label: `${device.alias || device.name || device.deviceId} · ${device.deviceId}`
    }));
  } catch {
    if (token === deviceRequestToken) proxy.$message.error("加载设备选项失败");
  } finally {
    if (token === deviceRequestToken) deviceLoading.value = false;
  }
}

function formatDateTime(value: string | null | undefined): string {
  if (!value) return "未知";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "未知" : date.toLocaleString("zh-CN", { hour12: false });
}

function enumText(value: AlarmEnumValue): string {
  return value.label || (value.value === null ? "未知" : `未知(${value.value})`);
}

onMounted(() => {
  if (canView.value) {
    loadAlarms();
    searchDevices("");
  }
});
</script>

<style scoped lang="scss">
.alarm-management-page {
  color: var(--uvp-text-primary);
}

.alarm-page-header {
  display: flex;
  gap: 16px;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 14px;
}

.alarm-page-header h2 {
  margin: 0;
  font-size: 18px;
  font-weight: 650;
}

.alarm-page-header p {
  margin: 4px 0 0;
  color: var(--uvp-text-secondary);
  font-size: 13px;
}

.alarm-state {
  margin-bottom: 12px;
}

.alarm-entity-cell {
  display: flex;
  flex-direction: column;
  min-width: 0;
  line-height: 1.35;
}

.alarm-entity-cell span,
.alarm-entity-cell small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.alarm-entity-cell small {
  margin-top: 2px;
  color: var(--uvp-text-tertiary);
  font-size: 11px;
}

@media (max-width: 768px) {
  .alarm-page-header {
    align-items: center;
  }

  .alarm-page-header p {
    display: none;
  }
}

@media (prefers-reduced-motion: reduce) {
  .alarm-management-page * {
    scroll-behavior: auto !important;
  }
}
</style>
