<script setup lang="ts">
import { computed, reactive, ref } from "vue";
import { Modal, Message } from "@arco-design/web-vue";
import {
  Activity,
  ArrowDown,
  ArrowDownLeft,
  ArrowDownRight,
  ArrowLeft,
  ArrowRight,
  ArrowUp,
  ArrowUpLeft,
  ArrowUpRight,
  Camera,
  Check,
  ChevronRight,
  Compass,
  Download,
  Focus,
  HardDrive,
  Image as ImageIcon,
  Maximize2,
  Mic,
  Minus,
  Pause,
  Play,
  Plus,
  RadioTower,
  RefreshCw,
  RotateCcw,
  Save,
  Settings,
  ShieldCheck,
  Square,
  Trash2,
  Video,
  Volume2,
  X
} from "@lucide/vue";
import { previewTabs } from "./components/workbenchPreview";

const visible = ref(true);
const active = ref("image");
const selectedGroups = reactive(Object.fromEntries(previewTabs.map(tab => [tab.key, tab.groups[0].key])));
const tab = computed(() => previewTabs.find(item => item.key === active.value)!);
const group = computed(() => tab.value.groups.find(item => item.key === selectedGroups[active.value])!);
const initial = Object.fromEntries(previewTabs.flatMap(t => t.groups.flatMap(g => g.fields.map(f => [f.key, f.value]))));
const values = reactive<Record<string, any>>({ ...initial });
const baseline = reactive<Record<string, any>>({ ...initial });
const dirtyCount = computed(() => group.value.fields.filter(f => values[f.key] !== baseline[f.key]).length);
const iconMap = {
  ptz: Compass,
  image: ImageIcon,
  encoding: Video,
  record: HardDrive,
  alarm: ShieldCheck,
  device: Settings,
  probe: Activity
};
const stream = ref("主码流");
const version = ref("2022");
const protocol = ref("WS-FLV");
const paused = ref(false);
const fullscreen = ref(false);
const talk = ref(false);
const talkMode = ref("双向对讲");
const sound = ref(false);
const home = ref(true);
const homeDelay = ref(60);
const homePreset = ref("全景位置");
const recording = ref(false);
const armed = ref(true);
const cruise = ref(false);
const selectedPreset = ref(1);
const presets = ref(["全景位置", "入口通道", "收银区域", "设备机柜"]);
const presetName = ref("");
const schedule = ref([true, true, true, true, true, false, false]);
const scheduleStart = ref("08:00:00");
const scheduleEnd = ref("18:00:00");
const gallery = ref<number[]>([]);
const stateText = ref("参数已读取");
const events = ref([{ time: "12:36:20", action: "读取设备参数", result: "本地样例" }]);
const directionIcons = [
  ArrowUpLeft,
  ArrowUp,
  ArrowUpRight,
  ArrowLeft,
  Square,
  ArrowRight,
  ArrowDownLeft,
  ArrowDown,
  ArrowDownRight
];
const directions = ["左上", "上", "右上", "左", "停止", "右", "左下", "下", "右下"];
const moving = ref("");
const currentTime = () => new Date().toLocaleTimeString("zh-CN", { hour12: false });
function action(name: string) {
  stateText.value = `预览：${name}`;
  events.value.unshift({ time: currentTime(), action: name, result: "本地预览" });
  events.value = events.value.slice(0, 8);
}
function apply() {
  group.value.fields.forEach(f => {
    baseline[f.key] = values[f.key];
  });
  action(`已保存${group.value.label}`);
}
function reset() {
  group.value.fields.forEach(f => {
    values[f.key] = baseline[f.key];
  });
  action("已撤销本组修改");
}
function confirmAction(name: string) {
  Modal.confirm({
    title: name,
    content: "此处为视觉预览，确认后只更新本地操作记录，不会控制真实设备。",
    okText: "确认预览",
    onOk: () => action(name)
  });
}
function addPreset() {
  if (!presetName.value.trim()) return Message.warning("请输入预置位名称");
  presets.value.push(presetName.value.trim());
  presetName.value = "";
  action("保存当前位置");
}
function capture() {
  gallery.value.unshift(Date.now());
  gallery.value = gallery.value.slice(0, 4);
  action("设备抓拍");
}
function moveOverlay(event: PointerEvent) {
  if (active.value !== "image") return;
  const bounds = (event.currentTarget as HTMLElement).getBoundingClientRect();
  const x = Math.round(((event.clientX - bounds.left) / bounds.width) * 1920);
  const y = Math.round(((event.clientY - bounds.top) / bounds.height) * 1080);
  if (group.value.key === "osd") {
    values.textX = x;
    values.textY = y;
  }
  if (group.value.key === "mask") {
    values.maskX = Math.min(x, 1440);
    values.maskY = Math.min(y, 780);
    values.maskRight = values.maskX + 480;
    values.maskBottom = values.maskY + 300;
  }
}
function close() {
  const dirty = Object.keys(values).some(key => values[key] !== baseline[key]);
  if (dirty)
    Modal.confirm({
      title: "关闭工作台？",
      content: "有尚未应用的修改，关闭后仍保留在本次预览中。",
      onOk: () => {
        visible.value = false;
      }
    });
  else visible.value = false;
}
</script>

<template>
  <div class="workbench-preview-page">
    <a-button v-if="!visible" type="primary" @click="visible = true">打开播放控制台</a-button>
    <a-modal
      :visible="visible"
      :width="fullscreen ? 'calc(100vw - 16px)' : 'min(1560px, calc(100vw - 40px))'"
      :footer="false"
      :closable="false"
      :mask-closable="false"
      :esc-to-close="false"
      modal-class="uvp-system-dialog workbench-preview-modal"
      :body-style="{ padding: '0' }"
    >
      <template #title>
        <div class="wb-title">
          <span class="wb-logo"><RadioTower :size="21" /></span>
          <div>
            <strong>播放控制台</strong><small>后置摄像头 <span>·</span> 34020000001320000010</small>
          </div>
          <a-tag size="small" color="orange">视觉预览</a-tag>
          <div class="wb-title-actions">
            <span class="wb-online"><i></i>{{ paused ? "已暂停" : "预览画面" }}</span>
            <a-tooltip content="调整窗口大小"
              ><a-button size="small" @click="fullscreen = !fullscreen"
                ><template #icon><Maximize2 :size="15" /></template></a-button
            ></a-tooltip>
            <a-button size="small" status="danger" @click="close"
              ><template #icon><X :size="14" /></template>关闭</a-button
            >
          </div>
        </div>
      </template>

      <div class="wb-layout">
        <nav class="wb-nav" role="tablist" aria-label="设备工作区">
          <div class="wb-nav-title">工作区</div>
          <button
            v-for="item in previewTabs"
            :key="item.key"
            role="tab"
            :aria-selected="active === item.key"
            :class="{ selected: active === item.key }"
            @click="active = item.key"
          >
            <component :is="iconMap[item.key as keyof typeof iconMap]" :size="16" /><span>{{ item.label }}</span
            ><ChevronRight :size="13" class="wb-nav-arrow" />
          </button>
          <div class="wb-nav-spacer"></div>
          <div class="wb-nav-foot">
            <span>协议版本</span
            ><a-select v-model="version" size="small" :options="['2022', '2016']" aria-label="国标版本" /><small
              >当前通道 · 设备端</small
            >
          </div>
        </nav>

        <div class="wb-grid">
          <section class="wb-stage">
            <div class="wb-video" :class="{ 'is-editing': active === 'image' }" @pointerdown="moveOverlay">
              <img
                src="/workbench-preview-frame.png"
                alt="后置摄像头预览画面"
                :style="{
                  transform:
                    values.mirrorMode === '左右翻转'
                      ? 'scaleX(-1)'
                      : values.mirrorMode === '上下翻转'
                        ? 'scaleY(-1)'
                        : values.mirrorMode === '中心镜像'
                          ? 'scale(-1)'
                          : undefined
                }"
              />
              <span class="wb-source">静态样例 <span>·</span> {{ stream }}</span>
              <template v-if="active === 'image'">
                <span
                  v-if="group.key === 'osd' && values.textOn"
                  class="wb-osd"
                  :style="{
                    left: `${Math.min((values.textX / 1920) * 100, 73)}%`,
                    top: `${Math.min((values.textY / 1080) * 100, 86)}%`
                  }"
                  >{{ values.osdText }}<Focus :size="14"
                /></span>
                <div
                  v-if="group.key === 'mask' && values.maskOn"
                  class="wb-mask"
                  :style="{
                    left: `${(values.maskX / 1920) * 100}%`,
                    top: `${(values.maskY / 1080) * 100}%`,
                    width: `${(Math.max(0, values.maskRight - values.maskX) / 1920) * 100}%`,
                    height: `${(Math.max(0, values.maskBottom - values.maskY) / 1080) * 100}%`
                  }"
                >
                  <span>{{ values.maskRegion }}</span>
                </div>
              </template>
              <div v-if="paused" class="wb-paused"><Pause :size="32" /></div>
              <div class="wb-video-tools">
                <a-tooltip :content="paused ? '继续预览' : '暂停预览'"
                  ><a-button type="text" @pointerdown.stop @click="paused = !paused"
                    ><template #icon><component :is="paused ? Play : Pause" :size="17" /></template></a-button
                ></a-tooltip>
                <span>H.264 <span>·</span> 1920 × 1080 <span>·</span> 25 fps</span>
                <span class="wb-video-spacer"></span>
                <a-tooltip content="声音"
                  ><a-button type="text" :class="{ 'is-on': sound }" @pointerdown.stop @click="sound = !sound"
                    ><template #icon><Volume2 :size="17" /></template></a-button
                ></a-tooltip>
                <a-tooltip content="画面截图"
                  ><a-button type="text" @pointerdown.stop @click="capture"
                    ><template #icon><Camera :size="17" /></template></a-button
                ></a-tooltip>
              </div>
            </div>
            <div class="wb-streambar">
              <a-select v-model="stream" size="small" :options="['主码流', '子码流 1']" aria-label="码流" />
              <a-select
                v-model="protocol"
                size="small"
                :options="['WS-FLV', 'HTTP-FLV', 'HLS', 'WebRTC']"
                aria-label="播放协议"
              />
              <span class="wb-stream-status"><i></i>TCP 被动 <span>·</span> 4.09 Mb/s</span>
              <a-tooltip content="重新连接"
                ><a-button size="small" @click="action('重新连接')"
                  ><template #icon><RefreshCw :size="14" /></template></a-button
              ></a-tooltip>
            </div>
          </section>

          <aside class="wb-side">
            <header class="wb-section-heading">
              <div>
                <h3>{{ tab.label }}</h3>
                <p>{{ tab.subtitle }}</p>
              </div>
              <a-tag size="small">{{ version }}</a-tag>
            </header>
            <a-radio-group v-model="selectedGroups[active]" type="button" size="small" class="wb-subtabs"
              ><a-radio v-for="item in tab.groups" :key="item.key" :value="item.key">{{ item.label }}</a-radio></a-radio-group
            >
            <div class="wb-side-content">
              <template v-if="active === 'ptz' && group.key === 'direction'">
                <div class="wb-ptz-area">
                  <div class="wb-direction">
                    <a-tooltip v-for="(Icon, index) in directionIcons" :key="index" :content="directions[index]"
                      ><a-button
                        :class="{ 'wb-center': index === 4 }"
                        @pointerdown="moving = directions[index]"
                        @pointerup="moving = ''"
                        @pointerleave="moving = ''"
                        @click="action(`云台${directions[index]}`)"
                        ><template #icon><component :is="Icon" :size="20" /></template></a-button
                    ></a-tooltip>
                  </div>
                  <div class="wb-lens">
                    <label v-for="name in ['变倍', '聚焦', '光圈']" :key="name"
                      ><span>{{ name }}</span
                      ><a-button-group
                        ><a-button size="small" @click="action(`${name}减小`)"
                          ><template #icon><Minus :size="14" /></template></a-button
                        ><a-button size="small" @click="action(`${name}增大`)"
                          ><template #icon><Plus :size="14" /></template></a-button></a-button-group
                    ></label>
                  </div>
                </div>
                <span class="wb-motion-status">{{ moving ? `云台向${moving}移动` : "云台已停止" }}</span>
              </template>
              <a-form
                :model="values"
                layout="horizontal"
                :label-col-props="{ span: 9 }"
                :wrapper-col-props="{ span: 15 }"
                size="small"
              >
                <a-form-item v-for="field in group.fields" :key="field.key" :label="field.label" :field="field.key">
                  <a-select v-if="field.kind === 'select'" v-model="values[field.key]" :options="field.options" />
                  <a-input-number
                    v-else-if="field.kind === 'number'"
                    v-model="values[field.key]"
                    :min="field.min"
                    :max="field.max"
                    ><template v-if="field.unit" #suffix>{{ field.unit }}</template></a-input-number
                  >
                  <a-switch v-else-if="field.kind === 'switch'" v-model="values[field.key]" size="small" />
                  <a-input v-else v-model="values[field.key]" allow-clear />
                </a-form-item>
              </a-form>
              <template v-if="active === 'ptz'">
                <div v-if="group.key === 'scan'" class="wb-inline-actions">
                  <a-button size="small" @click="action('设置扫描左边界')">左边界</a-button
                  ><a-button size="small" @click="action('设置扫描右边界')">右边界</a-button
                  ><a-button size="small" type="primary" @click="action('启动扫描')"
                    ><template #icon><Play :size="13" /></template>扫描</a-button
                  >
                </div>
                <div v-if="group.key === 'precise'" class="wb-inline-actions">
                  <a-button size="small" @click="action('读取当前位置')">读取位置</a-button
                  ><a-button type="primary" size="small" @click="action('移动到指定位置')">定位</a-button>
                </div>
                <div class="wb-side-divider"></div>
                <h4>语音通信</h4>
                <div class="wb-talk">
                  <a-select v-model="talkMode" :options="['双向对讲', '语音广播']" size="small" /><a-button
                    :type="talk ? 'outline' : 'primary'"
                    size="small"
                    @click="
                      talk = !talk;
                      action(talk ? '开始对讲' : '结束对讲');
                    "
                    ><template #icon><Mic :size="14" /></template>{{ talk ? "结束" : "开始" }}</a-button
                  >
                </div>
              </template>
              <template v-if="active === 'image'"
                ><div class="wb-side-divider"></div>
                <div class="wb-meta-line"><span>配置画布</span><strong>1920 × 1080</strong></div>
                <div class="wb-meta-line"><span>作用范围</span><strong>当前通道 · 设备端</strong></div></template
              >
              <template v-if="active === 'encoding'"
                ><div class="wb-side-divider"></div>
                <div class="wb-meta-line">
                  <span>当前配置</span><strong>{{ stream }}</strong>
                </div>
                <div v-if="group.key === 'range'" class="wb-capabilities">
                  <a-tag v-for="item in ['H.264', 'H.265', '1080P', '720P', '1–60 fps', 'CBR / VBR']" :key="item" size="small">{{
                    item
                  }}</a-tag>
                </div>
                <a-button size="small" long @click="action('请求关键帧')"
                  ><template #icon><RefreshCw :size="13" /></template>请求关键帧</a-button
                ></template
              >
              <template v-if="active === 'record'"
                ><div class="wb-side-divider"></div>
                <div class="wb-meta-line">
                  <span>设备录像</span
                  ><a-tag :color="recording ? 'red' : 'gray'" size="small">{{ recording ? "录制中" : "未录制" }}</a-tag>
                </div>
                <a-button
                  long
                  size="small"
                  :status="recording ? 'danger' : 'normal'"
                  @click="
                    recording = !recording;
                    action(recording ? '开始设备录像' : '停止设备录像');
                  "
                  ><template #icon><component :is="recording ? Square : Video" :size="14" /></template
                  >{{ recording ? "停止录像" : "开始录像" }}</a-button
                ></template
              >
              <template v-if="active === 'alarm'"
                ><div class="wb-side-divider"></div>
                <div class="wb-meta-line">
                  <span>当前状态</span
                  ><a-tag :color="armed ? 'green' : 'gray'" size="small">{{ armed ? "已布防" : "已撤防" }}</a-tag>
                </div>
                <div class="wb-inline-actions">
                  <a-button
                    type="primary"
                    size="small"
                    @click="
                      armed = !armed;
                      action(armed ? '布防' : '撤防');
                    "
                    >{{ armed ? "撤防" : "布防" }}</a-button
                  ><a-button size="small" @click="action('报警复位')">报警复位</a-button>
                </div></template
              >
              <template v-if="active === 'device'"
                ><div class="wb-side-divider"></div>
                <a-button v-if="group.key === 'snapshot'" long type="primary" size="small" @click="capture"
                  ><template #icon><Camera :size="14" /></template>立即抓拍</a-button
                ><a-button
                  v-else-if="group.key === 'upgrade'"
                  long
                  type="primary"
                  size="small"
                  :disabled="!values.firmware"
                  @click="confirmAction('开始固件升级')"
                  >开始升级</a-button
                ><a-button v-else long size="small" status="danger" @click="confirmAction('重启设备')"
                  ><template #icon><RefreshCw :size="14" /></template>重启设备</a-button
                ></template
              >
              <template v-if="active === 'probe'"
                ><div class="wb-probe-summary">
                  <div
                    v-for="(value, label) in {
                      视频码率: '4.09 Mb/s',
                      帧率: '25 fps',
                      关键帧间隔: '2.0 s',
                      音频编码: 'G.711A',
                      'RTP 丢包': '0.00%',
                      当前观看: '2 人'
                    }"
                    :key="label"
                  >
                    <span>{{ label }}</span
                    ><strong>{{ value }}</strong>
                  </div>
                </div>
                <a-button type="primary" long size="small" @click="action('完成采样诊断')"
                  ><template #icon><Activity :size="14" /></template>开始检测</a-button
                ></template
              >
            </div>
            <footer class="wb-side-footer">
              <span :class="{ changed: dirtyCount }">{{ dirtyCount ? `${dirtyCount} 项待应用` : "无待应用修改" }}</span
              ><a-tooltip content="撤销本组修改"
                ><a-button size="small" :disabled="!dirtyCount" @click="reset"
                  ><template #icon><RotateCcw :size="14" /></template></a-button></a-tooltip
              ><a-button size="small" @click="action('读取当前配置')"
                ><template #icon><RefreshCw :size="13" /></template>读取</a-button
              ><a-button type="primary" size="small" :disabled="!dirtyCount" @click="apply"
                ><template #icon><Check :size="14" /></template>应用</a-button
              >
            </footer>
          </aside>

          <section class="wb-bottom">
            <template v-if="active === 'ptz'">
              <div class="wb-bottom-three">
                <section>
                  <header>
                    <h4>
                      预置位 <a-tag size="small">{{ presets.length }}</a-tag>
                    </h4>
                    <a-tooltip content="同步设备预置位"
                      ><a-button size="mini" @click="action('同步预置位')"
                        ><template #icon><RefreshCw :size="13" /></template></a-button
                    ></a-tooltip>
                  </header>
                  <div class="wb-presets">
                    <a-button
                      v-for="(name, index) in presets"
                      :key="index"
                      size="small"
                      :type="selectedPreset === index + 1 ? 'outline' : 'secondary'"
                      @click="
                        selectedPreset = index + 1;
                        action(`调用${name}`);
                      "
                      ><span>{{ String(index + 1).padStart(2, "0") }}</span
                      >{{ name }}</a-button
                    >
                  </div>
                  <a-input-group class="wb-preset-add"
                    ><a-input
                      v-model="presetName"
                      size="small"
                      placeholder="保存当前位置为预置位"
                      @press-enter="addPreset" /><a-button size="small" @click="addPreset"
                      ><template #icon><Plus :size="14" /></template></a-button
                  ></a-input-group>
                </section>
                <section>
                  <header>
                    <h4>巡航轨迹</h4>
                    <a-tag size="small" :color="cruise ? 'green' : 'gray'">{{ cruise ? "巡航中" : "已停止" }}</a-tag>
                  </header>
                  <a-select default-value="日间巡航" :options="['日间巡航', '夜间巡航']" size="small" />
                  <div class="wb-cruise-path">
                    <span>01 全景</span><ChevronRight :size="13" /><span>02 入口</span><ChevronRight :size="13" /><span
                      >03 收银</span
                    >
                  </div>
                  <div class="wb-inline-actions">
                    <a-button
                      size="small"
                      type="primary"
                      @click="
                        cruise = !cruise;
                        action(cruise ? '开始巡航' : '停止巡航');
                      "
                      ><template #icon><component :is="cruise ? Square : Play" :size="13" /></template
                      >{{ cruise ? "停止" : "开始巡航" }}</a-button
                    ><a-button size="small" @click="action('编辑巡航轨迹')">编辑轨迹</a-button>
                  </div>
                </section>
                <section>
                  <header>
                    <h4>看守位</h4>
                    <a-switch v-model="home" size="small" />
                  </header>
                  <div class="wb-compact-field">
                    <span>归位位置</span><a-select v-model="homePreset" :options="presets" size="small" />
                  </div>
                  <div class="wb-compact-field">
                    <span>空闲等待</span
                    ><a-input-number v-model="homeDelay" :min="10" :max="3600" size="small"
                      ><template #suffix>秒</template></a-input-number
                    >
                  </div>
                  <a-button size="small" @click="action('保存看守位')"
                    ><template #icon><Save :size="13" /></template>保存看守位</a-button
                  >
                </section>
              </div>
            </template>
            <template v-else-if="active === 'image'">
              <header>
                <h4>
                  {{ group.key === "mask" ? "遮挡区域" : group.key === "mirror" ? "画面方向" : "叠加内容"
                  }}<span class="wb-heading-note">当前通道</span>
                </h4>
                <a-button v-if="group.key === 'osd'" size="small" @click="action('新增叠加文字')"
                  ><template #icon><Plus :size="13" /></template>添加文字</a-button
                ><a-button
                  v-if="group.key === 'mask'"
                  size="small"
                  @click="values.maskRegion = `区域 ${Math.min(4, Number(values.maskRegion.slice(-1)) + 1)}`"
                  ><template #icon><Plus :size="13" /></template>添加区域</a-button
                >
              </header>
              <div v-if="group.key === 'mirror'" class="wb-mirror-options">
                <a-button
                  v-for="mode in ['不镜像', '左右翻转', '上下翻转', '中心镜像']"
                  :key="mode"
                  :type="values.mirrorMode === mode ? 'outline' : 'secondary'"
                  @click="values.mirrorMode = mode"
                  ><template #icon><ImageIcon :size="18" /></template>{{ mode }}</a-button
                >
              </div>
              <div v-else class="wb-data-table">
                <div class="wb-table-head"><span>内容</span><span>位置 / 范围</span><span>显示</span><span>操作</span></div>
                <div>
                  <span>{{ group.key === "mask" ? values.maskRegion : "日期时间" }}</span
                  ><span>{{
                    group.key === "mask"
                      ? `${values.maskX}, ${values.maskY} — ${values.maskRight}, ${values.maskBottom}`
                      : "24, 24"
                  }}</span
                  ><a-switch v-if="group.key === 'mask'" v-model="values.maskOn" size="small" /><a-switch
                    v-else
                    v-model="values.timeOn"
                    size="small"
                  /><a-button size="mini" type="text" @click="action('选中画面区域')"
                    ><template #icon><Focus :size="14" /></template
                  ></a-button>
                </div>
                <div v-if="group.key === 'osd'">
                  <a-input v-model="values.osdText" size="small" /><span>{{ values.textX }}, {{ values.textY }}</span
                  ><a-switch v-model="values.textOn" size="small" /><a-button
                    type="text"
                    size="mini"
                    status="danger"
                    @click="
                      values.osdText = '';
                      values.textOn = false;
                    "
                    ><template #icon><Trash2 :size="14" /></template
                  ></a-button>
                </div>
              </div>
              <div class="wb-bottom-note">
                <span><Check :size="13" />设备端叠加</span><span>画布 1920 × 1080</span
                ><span>{{ group.key === "mask" ? "最多 4 个区域" : "最多 8 条文字" }}</span>
              </div>
            </template>
            <template v-else-if="active === 'encoding'">
              <header>
                <h4>码流概览</h4>
                <a-tag size="small" color="green">设备参数已读取</a-tag>
              </header>
              <div class="wb-data-table wb-encoding-table">
                <div class="wb-table-head"><span>码流</span><span>编码 / 分辨率</span><span>帧率</span><span>码率</span></div>
                <div
                  v-for="name in ['主码流', '子码流 1']"
                  :key="name"
                  :class="{ 'wb-selected-row': stream === name }"
                  @click="stream = name"
                >
                  <strong>{{ name }}</strong
                  ><span>{{ name === "主码流" ? `${values.codec} / ${values.resolution}` : "H.264 / 1280 × 720" }}</span
                  ><span>{{ name === "主码流" ? values.fps : 15 }} fps</span
                  ><span>{{ name === "主码流" ? values.bitrate : 1024 }} kb/s</span>
                </div>
              </div>
              <div class="wb-bottom-note">
                <span><Activity :size="13" />实测：H.264 · 1080P · 25 fps · 4.09 Mb/s</span
                ><a-button type="text" size="mini" @click="active = 'probe'">查看探针<ChevronRight :size="12" /></a-button>
              </div>
            </template>
            <template v-else-if="active === 'record'">
              <header>
                <h4>{{ group.key === "storage" ? "存储介质" : "每周录像计划" }}</h4>
                <a-button
                  size="small"
                  @click="group.key === 'storage' ? action('刷新存储卡') : (schedule = schedule.map(() => true))"
                  ><template #icon><component :is="group.key === 'storage' ? RefreshCw : Plus" :size="13" /></template
                  >{{ group.key === "storage" ? "刷新" : "应用到每天" }}</a-button
                >
              </header>
              <template v-if="group.key === 'storage'"
                ><div class="wb-storage">
                  <HardDrive :size="30" />
                  <div>
                    <strong>SD 卡 1 <a-tag color="green" size="small">正常</a-tag></strong
                    ><a-progress :percent="0.62" :show-text="false" /><small>已用 79.4 GB / 总计 128 GB</small>
                  </div>
                  <a-button status="danger" size="small" @click="confirmAction('格式化存储卡')">格式化</a-button>
                </div></template
              >
              <template v-else
                ><div class="wb-week">
                  <button
                    v-for="(day, index) in ['周一', '周二', '周三', '周四', '周五', '周六', '周日']"
                    :key="day"
                    :class="{ enabled: schedule[index] }"
                    @click="schedule[index] = !schedule[index]"
                  >
                    <span>{{ day }}</span
                    ><i></i><small>{{ schedule[index] ? "08–18" : "关闭" }}</small>
                  </button>
                </div>
                <div class="wb-schedule-times">
                  <a-time-picker v-model="scheduleStart" size="small" format="HH:mm:ss" value-format="HH:mm:ss" /><span>至</span
                  ><a-time-picker v-model="scheduleEnd" size="small" format="HH:mm:ss" value-format="HH:mm:ss" /><a-button
                    size="small"
                    @click="action('保存录像计划')"
                    ><template #icon><Check :size="13" /></template>保存计划</a-button
                  >
                </div></template
              >
            </template>
            <template v-else-if="active === 'alarm'"
              ><header>
                <h4>最近报警</h4>
                <a-tag size="small">当前通道</a-tag>
              </header>
              <div class="wb-data-table">
                <div class="wb-table-head"><span>事件</span><span>时间</span><span>状态</span><span>操作</span></div>
                <div v-for="(name, index) in ['移动侦测', '区域入侵']" :key="name">
                  <strong>{{ name }}</strong
                  ><span>09-19 12:{{ 30 - index }}:08</span
                  ><a-tag size="small" :color="index ? 'gray' : 'orange'">{{ index ? "已恢复" : "待处理" }}</a-tag
                  ><a-button type="text" size="mini" @click="action(`复位${name}`)">复位</a-button>
                </div>
              </div></template
            >
            <template v-else-if="active === 'device'">
              <header>
                <h4>{{ group.key === "snapshot" ? "抓拍结果" : group.key === "upgrade" ? "升级任务" : "设备信息" }}</h4>
                <a-tag size="small">GB/T 28181-{{ version }}</a-tag>
              </header>
              <div v-if="group.key === 'snapshot'" class="wb-gallery">
                <div v-for="id in gallery" :key="id">
                  <img src="/workbench-preview-frame.png" alt="抓拍预览样例" /><small>{{
                    new Date(id).toLocaleTimeString()
                  }}</small>
                </div>
                <div v-if="!gallery.length" class="wb-empty"><Camera :size="24" /><span>暂无抓拍结果</span></div>
              </div>
              <div v-else-if="group.key === 'upgrade'" class="wb-upgrade">
                <Download :size="27" />
                <div>
                  <strong>当前固件 v2.4.0</strong>
                  <p>暂无进行中的升级任务</p>
                </div>
                <a-tag size="small">设备在线</a-tag>
              </div>
              <div v-else class="wb-device-facts">
                <div
                  v-for="(value, label) in {
                    设备厂商: 'UVP',
                    设备型号: 'GB28181 Camera',
                    固件版本: 'v2.4.0',
                    通道数量: '2',
                    传输方式: 'TCP 被动',
                    最近心跳: '12:36:20'
                  }"
                  :key="label"
                >
                  <span>{{ label }}</span
                  ><strong>{{ value }}</strong>
                </div>
              </div>
            </template>
            <template v-else
              ><header>
                <h4>媒体链路</h4>
                <a-tag color="green" size="small">链路正常</a-tag>
              </header>
              <div class="wb-pipeline">
                <div v-for="step in ['设备在线', 'SIP 会话', 'RTP 接收', '媒体注册', '浏览器解码']" :key="step">
                  <Check :size="15" /><span>{{ step }}</span>
                </div>
              </div>
              <div class="wb-sparkline"><i v-for="n in 48" :key="n" :style="{ height: `${30 + ((n * 17) % 45)}%` }"></i></div>
              <div class="wb-bottom-note"><span>码率趋势</span><span>最近 60 秒 · 样例数据</span></div></template
            >
          </section>
        </div>
      </div>
      <footer class="wb-statusbar">
        <span><Check :size="13" />{{ stateText }}</span
        ><span class="wb-status-events"
          ><a-popover title="操作记录" position="top"
            ><a-button size="mini" type="text"
              >操作记录 <span>{{ events.length }}</span></a-button
            ><template #content
              ><div class="wb-event-log">
                <div v-for="(event, index) in events" :key="index">
                  <span>{{ event.time }}</span
                  ><strong>{{ event.action }}</strong
                  ><small>{{ event.result }}</small>
                </div>
              </div></template
            ></a-popover
          ></span
        ><span>通道配置 <span>·</span> {{ version }} <span>·</span> 本地预览</span>
      </footer>
    </a-modal>
  </div>
</template>

<style scoped>
.workbench-preview-page {
  display: grid;
  place-items: center;
  min-height: 100vh;
  background: var(--uvp-shell-bg);
}
.wb-title {
  display: flex;
  gap: 12px;
  align-items: center;
  width: 100%;
  text-align: left;
}
.wb-logo {
  display: grid;
  place-items: center;
  width: 36px;
  height: 36px;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-radius: 8px;
}
.wb-title strong {
  font-size: 15px;
}
.wb-title small {
  display: block;
  margin-top: 4px;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.wb-title small span {
  margin: 0 5px;
}
.wb-title-actions {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-left: auto;
}
.wb-online {
  display: flex;
  gap: 6px;
  align-items: center;
  margin-right: 10px;
  font-size: 12px;
  color: var(--uvp-brand-cyan);
}
.wb-online i,
.wb-stream-status i {
  width: 6px;
  height: 6px;
  background: #12aa86;
  border-radius: 50%;
}
.wb-layout {
  display: grid;
  grid-template-columns: 142px minmax(0, 1fr);
  min-height: 0;
}
.wb-nav {
  display: flex;
  flex-direction: column;
  min-height: 760px;
  padding: 16px 10px;
  background: var(--uvp-list-toolbar-bg);
  border-right: 1px solid var(--uvp-dialog-border);
}
.wb-nav-title {
  padding: 0 10px 12px;
  font-size: 11px;
  font-weight: 600;
  color: var(--uvp-text-tertiary);
  letter-spacing: 0.04em;
}
.wb-nav button {
  position: relative;
  display: flex;
  gap: 9px;
  align-items: center;
  width: 100%;
  min-height: 42px;
  padding: 0 10px;
  font: inherit;
  font-size: 12px;
  color: var(--uvp-text-secondary);
  text-align: left;
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: 5px;
}
.wb-nav button:hover {
  background: var(--uvp-dialog-bg);
}
.wb-nav button.selected {
  font-weight: 600;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
}
.wb-nav button.selected::before {
  position: absolute;
  top: 8px;
  bottom: 8px;
  left: -10px;
  width: 3px;
  content: "";
  background: var(--uvp-brand);
  border-radius: 0 2px 2px 0;
}
.wb-nav-arrow {
  margin-left: auto;
  opacity: 0.35;
}
.wb-nav button.selected .wb-nav-arrow {
  opacity: 1;
}
.wb-nav-spacer {
  flex: 1;
}
.wb-nav-foot {
  display: grid;
  gap: 7px;
  padding: 12px 8px 0;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
  border-top: 1px solid var(--uvp-dialog-border);
}
.wb-nav-foot :deep(.arco-select) {
  width: 100%;
}
.wb-nav-foot small {
  font-size: 10px;
  line-height: 1.5;
}
.wb-grid {
  display: grid;
  grid-template-rows: auto auto;
  grid-template-columns: minmax(0, 1fr) 362px;
  gap: 0 20px;
  align-items: start;
  padding: 16px 20px 0;
}
.wb-stage {
  min-width: 0;
}
.wb-video {
  position: relative;
  height: clamp(260px, calc(100dvh - 420px), 540px);
  overflow: hidden;
  background: #10161d;
  border-radius: 6px 6px 0 0;
}
.wb-video > img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: contain;
}
.wb-video.is-editing {
  cursor: crosshair;
}
.wb-source {
  position: absolute;
  top: 12px;
  left: 12px;
  padding: 4px 8px;
  font-size: 11px;
  color: #ffffff;
  background: #1f2937;
  border-radius: 3px;
}
.wb-source span {
  padding: 0 5px;
  color: #a9b5c5;
}
.wb-osd {
  position: absolute;
  display: flex;
  gap: 10px;
  align-items: center;
  max-width: 72%;
  padding: 5px 8px;
  overflow: hidden;
  font-size: 12px;
  color: #ffffff;
  white-space: nowrap;
  background: #102735;
  border: 1px dashed #65d2fb;
}
.wb-mask {
  position: absolute;
  box-sizing: border-box;
  padding: 8px;
  font-size: 12px;
  color: #ffffff;
  background: #202a35;
  border: 2px solid #50b9ff;
}
.wb-paused {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  color: white;
}
.wb-video-tools {
  position: absolute;
  right: 0;
  bottom: 0;
  left: 0;
  display: flex;
  gap: 10px;
  align-items: center;
  height: 36px;
  padding: 0 8px;
  font-size: 11px;
  color: #cbd5e1;
  background: #17212c;
}
.wb-video-tools :deep(.arco-btn) {
  color: white !important;
  background: transparent !important;
  border-color: transparent !important;
}
.wb-video-tools > span > span {
  padding: 0 5px;
  color: #77879b;
}
.wb-video-spacer {
  flex: 1;
}
.wb-video-tools .is-on {
  background: #2563eb;
}
.wb-streambar {
  display: flex;
  gap: 8px;
  align-items: center;
  padding: 9px 10px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-dialog-border);
  border-top: 0;
  border-radius: 0 0 6px 6px;
}
.wb-streambar :deep(.arco-select) {
  width: 108px;
}
.wb-stream-status {
  display: flex;
  gap: 7px;
  align-items: center;
  margin-left: auto;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.wb-side {
  display: flex;
  flex-direction: column;
  grid-row: 1 / 3;
  grid-column: 2;
  align-self: stretch;
  min-width: 0;
  padding-left: 18px;
  border-left: 1px solid var(--uvp-dialog-border);
}
.wb-section-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 14px;
}
.wb-section-heading h3 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
}
.wb-section-heading p {
  margin: 5px 0 0;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.wb-subtabs {
  display: flex;
  flex-wrap: wrap;
  gap: 3px;
  width: 100%;
  margin-bottom: 18px;
}
:deep(.wb-subtabs .arco-radio-button) {
  flex: 1;
  padding: 0 8px;
  font-size: 12px;
  text-align: center;
  white-space: nowrap;
}
.wb-side-content {
  flex: 1;
  min-height: 250px;
}
.wb-side :deep(.arco-form-item) {
  margin-bottom: 16px;
}
.wb-side :deep(.arco-form-item-label-col) {
  padding-right: 12px;
}
.wb-side :deep(.arco-form-item-label) {
  font-size: 12px;
  color: var(--uvp-text-secondary);
}
.wb-side :deep(.arco-input-number) {
  width: 100%;
}
.wb-side :deep(.arco-input-suffix) {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.wb-side-divider {
  margin: 20px 0 16px;
  border-top: 1px solid var(--uvp-dialog-border);
}
.wb-side h4,
.wb-bottom h4 {
  display: flex;
  gap: 8px;
  align-items: center;
  margin: 0;
  font-size: 12px;
  font-weight: 600;
}
.wb-side h4 {
  margin-bottom: 12px;
}
.wb-meta-line {
  display: flex;
  gap: 12px;
  justify-content: space-between;
  margin: 12px 0;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.wb-meta-line strong {
  font-weight: 400;
  color: var(--uvp-text-secondary);
}
.wb-side-footer {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
  padding: 12px 0;
  margin-top: 18px;
  border-top: 1px solid var(--uvp-dialog-border);
}
.wb-side-footer > span {
  margin-right: auto;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.wb-side-footer > span.changed {
  color: var(--uvp-warning);
}
.wb-bottom {
  box-sizing: border-box;
  min-width: 0;
  min-height: 195px;
  padding: 16px 0 12px;
}
.wb-bottom header {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: space-between;
  min-height: 28px;
  margin-bottom: 10px;
}
.wb-heading-note {
  margin-left: 5px;
  font-size: 11px;
  font-weight: 400;
  color: var(--uvp-text-tertiary);
}
.wb-bottom-three {
  display: grid;
  grid-template-columns: 1.1fr 1fr 1fr;
  gap: 16px;
}
.wb-bottom-three > section {
  min-width: 0;
}
.wb-bottom-three > section + section {
  padding-left: 16px;
  border-left: 1px solid var(--uvp-dialog-border);
}
.wb-presets {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 6px;
}
.wb-presets :deep(.arco-btn) {
  justify-content: flex-start;
  padding: 0 6px;
  overflow: hidden;
  font-size: 11px;
}
.wb-presets :deep(.arco-btn) span {
  margin-right: 4px;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
}
.wb-preset-add {
  display: flex;
  width: 100%;
  margin-top: 10px;
}
.wb-preset-add :deep(.arco-input-wrapper) {
  flex: 1;
  min-width: 0;
}
.wb-cruise-path {
  display: flex;
  gap: 3px;
  align-items: center;
  padding: 18px 0;
  font-size: 10px;
  color: var(--uvp-text-secondary);
  white-space: nowrap;
}
.wb-inline-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.wb-compact-field {
  display: flex;
  gap: 8px;
  align-items: center;
  margin: 0 0 10px;
  font-size: 11px;
}
.wb-compact-field > span {
  flex-shrink: 0;
  width: 50px;
}
.wb-compact-field :deep(.arco-select),
.wb-compact-field :deep(.arco-input-number) {
  flex: 1;
  min-width: 0;
}
.wb-data-table > div {
  display: grid;
  grid-template-columns: minmax(100px, 1.6fr) minmax(100px, 1.6fr) 65px 48px;
  gap: 12px;
  align-items: center;
  min-height: 40px;
  padding: 0 10px;
  font-size: 12px;
  border-bottom: 1px solid var(--uvp-dialog-border);
}
.wb-data-table > div > span {
  min-width: 0;
}
.wb-data-table .wb-table-head {
  min-height: 30px;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-table-header-bg);
}
.wb-data-table strong {
  font-weight: 500;
}
.wb-data-table :deep(.arco-switch) {
  justify-self: start;
}
.wb-data-table :deep(.arco-tag) {
  justify-self: start;
}
.wb-encoding-table > div {
  grid-template-columns: 1fr 1.8fr 0.7fr 0.8fr;
}
.wb-encoding-table > div:not(:first-child) {
  cursor: pointer;
}
.wb-selected-row {
  background: var(--uvp-brand-soft);
}
.wb-bottom-note {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
  justify-content: space-between;
  padding-top: 12px;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.wb-bottom-note > span {
  display: flex;
  gap: 5px;
  align-items: center;
}
.wb-bottom-note > span:first-child {
  color: var(--uvp-brand-cyan);
}
.wb-mirror-options {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 10px;
  padding: 20px 0;
}
.wb-mirror-options :deep(.arco-btn) {
  display: flex;
  flex-direction: column;
  gap: 7px;
  height: 60px;
}
.wb-ptz-area {
  display: flex;
  gap: 28px;
  align-items: center;
  justify-content: center;
  margin: 4px 0 6px;
}
.wb-direction {
  display: grid;
  grid-template-columns: repeat(3, 42px);
  gap: 4px;
}
.wb-direction :deep(.arco-btn) {
  height: 38px;
  padding: 0;
  color: var(--uvp-brand);
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-dialog-border);
}
.wb-direction :deep(.arco-btn.wb-center) {
  background: var(--uvp-brand-soft);
}
.wb-lens {
  display: grid;
  gap: 14px;
}
.wb-lens label {
  display: flex;
  gap: 8px;
  align-items: center;
  font-size: 12px;
  color: var(--uvp-text-secondary);
}
.wb-motion-status {
  display: block;
  margin: 10px 0 18px;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  text-align: center;
}
.wb-talk {
  display: flex;
  gap: 8px;
}
.wb-talk :deep(.arco-select) {
  flex: 1;
}
.wb-capabilities {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin: 14px 0;
}
.wb-week {
  display: grid;
  grid-template-columns: repeat(7, 1fr);
  gap: 8px;
}
.wb-week button {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 10px;
  font-size: 11px;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-dialog-border);
  border-radius: 4px;
}
.wb-week i {
  display: block;
  height: 7px;
  background: var(--uvp-dialog-border);
  border-radius: 2px;
}
.wb-week .enabled i {
  background: #66a6f5;
}
.wb-week small {
  font-size: 10px;
  color: var(--uvp-text-tertiary);
}
.wb-schedule-times {
  display: flex;
  gap: 10px;
  align-items: center;
  margin-top: 12px;
  font-size: 11px;
}
.wb-schedule-times :deep(.arco-picker) {
  width: 125px;
}
.wb-storage {
  display: flex;
  gap: 18px;
  align-items: center;
  padding: 20px 10px;
}
.wb-storage > div {
  flex: 1;
}
.wb-storage strong {
  display: flex;
  gap: 10px;
  margin-bottom: 10px;
  font-size: 12px;
}
.wb-storage small {
  color: var(--uvp-text-tertiary);
}
.wb-device-facts {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 20px;
  padding: 12px 0;
}
.wb-device-facts > div {
  display: flex;
  flex-direction: column;
  gap: 7px;
  font-size: 12px;
}
.wb-device-facts span {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.wb-device-facts strong {
  font-weight: 500;
}
.wb-gallery {
  display: flex;
  gap: 12px;
  min-height: 130px;
}
.wb-gallery > div {
  width: 150px;
}
.wb-gallery img {
  width: 100%;
  aspect-ratio: 16/9;
  object-fit: cover;
  border-radius: 4px;
}
.wb-gallery small {
  display: block;
  margin-top: 6px;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.wb-gallery .wb-empty {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: center;
  width: 100%;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.wb-upgrade {
  display: flex;
  gap: 18px;
  align-items: center;
  padding: 22px 10px;
}
.wb-upgrade strong {
  font-size: 13px;
}
.wb-upgrade p {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.wb-probe-summary {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 20px;
  margin: 6px 0 24px;
}
.wb-probe-summary > div {
  display: flex;
  flex-direction: column;
  gap: 7px;
}
.wb-probe-summary span {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.wb-probe-summary strong {
  font-size: 19px;
  font-weight: 500;
}
.wb-pipeline {
  display: flex;
  gap: 8px;
  justify-content: space-between;
  padding: 4px 0 12px;
}
.wb-pipeline > div {
  display: flex;
  gap: 5px;
  align-items: center;
  font-size: 11px;
  color: var(--uvp-brand-cyan);
}
.wb-sparkline {
  display: flex;
  gap: 4px;
  align-items: end;
  height: 68px;
  border-bottom: 1px solid var(--uvp-dialog-border);
}
.wb-sparkline i {
  flex: 1;
  background: #6bbeb1;
  border-radius: 2px 2px 0 0;
}
.wb-statusbar {
  display: flex;
  gap: 16px;
  align-items: center;
  padding: 8px 20px;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-dialog-footer-bg);
  border-top: 1px solid var(--uvp-dialog-border);
}
.wb-statusbar > span:first-child {
  display: flex;
  gap: 5px;
  align-items: center;
  color: var(--uvp-brand-cyan);
}
.wb-status-events {
  margin-left: auto;
}
.wb-statusbar > span:last-child > span {
  margin: 0 6px;
}
.wb-event-log > div {
  display: flex;
  gap: 16px;
  align-items: center;
  padding: 8px 0;
  font-size: 12px;
}
.wb-event-log strong {
  font-weight: 500;
}
.wb-event-log small {
  color: var(--uvp-text-tertiary);
}

@media (width >= 1600px) {
  .wb-grid {
    grid-template-columns: minmax(0, 1fr) 390px;
  }
}

@media (width <= 1200px) {
  .wb-layout {
    grid-template-columns: 124px minmax(0, 1fr);
  }
  .wb-grid {
    grid-template-columns: minmax(0, 1fr) 330px;
    gap: 0 14px;
    padding: 14px 16px 0;
  }
  .wb-bottom-three {
    gap: 10px;
  }
  .wb-bottom-three > section + section {
    padding-left: 10px;
  }
  .wb-presets {
    grid-template-columns: 1fr;
  }
  .wb-cruise-path {
    flex-wrap: wrap;
    white-space: normal;
  }
  .wb-stream-status {
    font-size: 10px;
  }
  .wb-side {
    padding-left: 14px;
  }
}

@media (width <= 850px) {
  .wb-layout {
    grid-template-columns: 1fr;
  }
  .wb-nav {
    flex-direction: row;
    align-items: center;
    min-height: 0;
    padding: 8px 10px;
    overflow-x: auto;
    border-right: 0;
    border-bottom: 1px solid var(--uvp-dialog-border);
  }
  .wb-nav-title,
  .wb-nav-spacer,
  .wb-nav-foot {
    display: none;
  }
  .wb-nav button {
    flex: 0 0 auto;
    width: auto;
    min-height: 34px;
    padding: 0 10px;
  }
  .wb-nav button.selected::before {
    inset: auto 8px 0;
    width: auto;
    height: 2px;
  }
  .wb-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .wb-side {
    grid-row: 3;
    grid-column: 1;
    padding: 16px 0 0;
    border-top: 1px solid var(--uvp-dialog-border);
    border-left: 0;
  }
  .wb-bottom {
    grid-row: 2;
  }
  .wb-side-content {
    min-height: 0;
  }
  .wb-title .arco-tag {
    display: none;
  }
  .wb-title small {
    font-size: 10px;
  }
  .wb-online {
    display: none;
  }
  .wb-bottom-three {
    grid-template-columns: 1fr;
  }
  .wb-bottom-three > section + section {
    padding: 12px 0 0;
    border-top: 1px solid var(--uvp-dialog-border);
    border-left: 0;
  }
  .wb-presets {
    grid-template-columns: 1fr 1fr;
  }
  .wb-stream-status {
    display: none;
  }
  .wb-streambar > button {
    margin-left: auto;
  }
  .wb-statusbar > span:last-child {
    display: none;
  }
  .wb-data-table > div {
    grid-template-columns: minmax(70px, 1fr) minmax(70px, 1fr) 50px 36px;
    gap: 6px;
    padding: 0 5px;
    font-size: 10px;
  }
  .wb-week {
    gap: 3px;
  }
  .wb-week button {
    padding: 8px 4px;
  }
  .wb-schedule-times {
    flex-wrap: wrap;
  }
  .wb-mirror-options {
    grid-template-columns: 1fr 1fr;
  }
  .wb-device-facts {
    grid-template-columns: 1fr 1fr;
  }
  .wb-pipeline {
    flex-wrap: wrap;
  }
  .wb-title {
    gap: 7px;
  }
  .wb-logo {
    display: none;
  }
  .wb-title-actions {
    gap: 5px;
  }
}
</style>

<style>
.workbench-preview-modal {
  top: 0 !important;
  margin: 18px auto !important;
  color: var(--uvp-text-primary);
}
.workbench-preview-modal .arco-modal-header {
  height: auto !important;
  padding: 14px 20px !important;
  background: var(--uvp-dialog-bg) !important;
}
.workbench-preview-modal .arco-modal-title {
  width: 100%;
}
.workbench-preview-modal .arco-modal-body {
  max-height: calc(100dvh - 112px);
  padding: 0 !important;
  overflow: auto;
}
.workbench-preview-modal * {
  box-sizing: border-box;
  letter-spacing: 0;
}

@media (width <= 850px) {
  .workbench-preview-modal {
    width: calc(100vw - 16px) !important;
    margin: 8px auto !important;
  }
  .workbench-preview-modal .arco-modal-header {
    padding: 12px !important;
  }
}
</style>
