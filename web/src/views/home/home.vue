<template>
  <div class="snow-page">
    <main class="home-page" aria-label="GB28181 视频平台首页">
      <section class="home-overview" aria-label="平台概览">
        <div class="overview-copy">
          <p class="overview-kicker">GB28181 Operations</p>
          <h1>视频接入与流媒体运行总览</h1>
          <p>集中观察设备注册、通道在线、实时点播、ZLMediaKit 节点与协议告警。</p>
        </div>
        <div class="overview-status" aria-label="核心运行状态">
          <span class="status-pill status-pill--ok">SIP 信令正常</span>
          <span class="status-pill">6 个流媒体节点</span>
          <span class="status-pill status-pill--warn">32 条今日告警</span>
        </div>
      </section>

      <section class="stat-grid" aria-label="核心指标">
        <article class="stat-card" v-for="card in statCards" :key="card.label">
          <div class="stat-card__icon" aria-hidden="true">
            <img :src="card.icon" :alt="card.label" />
          </div>
          <div class="stat-card__content">
            <div class="stat-card__label">{{ card.label }}</div>
            <div class="stat-card__value">
              {{ card.value }}
              <span v-if="card.rateBadge" class="rate-badge">{{ card.rateBadge }}</span>
            </div>
            <div class="stat-card__extra">
              <template v-if="card.extra">
                <span :class="card.extraType">{{ card.extra }}</span>
              </template>
              <template v-if="card.split">
                正常 <span class="ok">{{ card.split.ok }}</span> / 异常 <span class="danger">{{ card.split.err }}</span>
              </template>
            </div>
          </div>
        </article>
      </section>

      <section class="dashboard-grid">
        <div class="dashboard-main">
          <SipDashboardCard />

          <div class="chart-grid">
            <article class="uvp-panel chart-panel">
              <header class="panel-header">
                <div>
                  <h2>SIP 注册趋势</h2>
                  <p>REGISTER 成功、失败和未注册设备走势</p>
                </div>
                <select class="panel-select" aria-label="SIP 注册趋势时间范围">
                  <option>近 24 小时</option>
                  <option>近 7 天</option>
                </select>
              </header>
              <LineChart :series="sipSeries" :labels="hours" />
            </article>

            <article class="uvp-panel chart-panel">
              <header class="panel-header">
                <div>
                  <h2>播放请求趋势</h2>
                  <p>INVITE 呼叫与播放链路成功率</p>
                </div>
                <select class="panel-select" aria-label="播放请求趋势时间范围">
                  <option>近 24 小时</option>
                  <option>近 7 天</option>
                </select>
              </header>
              <LineChart :series="playSeries" :labels="hours" />
            </article>
          </div>

          <article class="uvp-panel">
            <header class="panel-header">
              <div>
                <h2>媒体服务器健康状态</h2>
                <p>ZLMediaKit 节点资源、流数与 RTP 端口占用</p>
              </div>
            </header>
            <div class="table-scroll">
              <table class="native-data-table">
                <thead>
                  <tr>
                    <th>节点名称</th>
                    <th>状态</th>
                    <th>CPU</th>
                    <th>内存</th>
                    <th>流数</th>
                    <th>RTP端口</th>
                    <th>上行带宽</th>
                    <th>下行带宽</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="row in mediaServers" :key="row.name">
                    <td>{{ row.name }}</td>
                    <td><span class="status-dot status-dot--ok">正常</span></td>
                    <td>
                      <span class="metric-cell">
                        {{ row.cpu }}%
                        <span class="progress-bar"><span class="fill fill--ok" :style="{ width: row.cpu + '%' }" /></span>
                      </span>
                    </td>
                    <td>
                      <span class="metric-cell">
                        {{ row.mem }}%
                        <span class="progress-bar"><span class="fill fill--brand" :style="{ width: row.mem + '%' }" /></span>
                      </span>
                    </td>
                    <td>{{ row.streams }}</td>
                    <td>{{ row.rtp }}</td>
                    <td>{{ row.up }}</td>
                    <td>{{ row.down }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </article>

          <article class="uvp-panel">
            <header class="panel-header">
              <div>
                <h2>最近事件</h2>
                <p>注册、目录、点播和通道状态事件</p>
              </div>
            </header>
            <div class="event-toolbar" aria-label="事件筛选">
              <select aria-label="事件类型">
                <option>全部事件</option>
                <option>GB28181注册</option>
                <option>通道离线</option>
                <option>GB28181呼叫</option>
                <option>播放失败</option>
              </select>
              <select aria-label="事件级别">
                <option>全部级别</option>
                <option>信息</option>
                <option>告警</option>
                <option>错误</option>
              </select>
              <select aria-label="事件时间范围">
                <option>近 24 小时</option>
                <option>近 7 天</option>
                <option>近 30 天</option>
              </select>
              <button class="icon-button" type="button" aria-label="刷新事件">
                <icon-refresh />
              </button>
            </div>
            <div class="table-scroll">
              <table class="native-data-table">
                <thead>
                  <tr>
                    <th>时间</th>
                    <th>级别</th>
                    <th>事件类型</th>
                    <th>事件内容</th>
                    <th>来源</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="ev in recentEvents" :key="ev.time">
                    <td>{{ ev.time }}</td>
                    <td><span class="tag" :class="`tag--${ev.levelClass}`">{{ ev.level }}</span></td>
                    <td>{{ ev.type }}</td>
                    <td>{{ ev.content }}</td>
                    <td>{{ ev.source }}</td>
                    <td><button class="link-button" type="button">详情</button></td>
                  </tr>
                </tbody>
              </table>
            </div>
          </article>
        </div>

        <aside class="dashboard-side" aria-label="侧栏监控">
          <article class="uvp-panel">
            <header class="panel-header panel-header--inline">
              <div>
                <h2>异常设备排行</h2>
                <p>按异常持续时间排序</p>
              </div>
              <button class="text-button" type="button">更多</button>
            </header>
            <div class="rank-list">
              <div class="rank-row rank-row--head" aria-hidden="true">
                <span>排名</span>
                <span>设备</span>
                <span>原因</span>
                <span>时长</span>
              </div>
              <div class="rank-row" v-for="(item, idx) in abnormalDevices" :key="item.id">
                <span class="rank-num" :class="rankClass(idx)">{{ idx + 1 }}</span>
                <span class="rank-device">
                  <strong>{{ item.name }}</strong>
                  <em>{{ item.id }}</em>
                </span>
                <span class="reason-tag" :class="`reason-tag--${item.reasonClass}`">{{ item.reason }}</span>
                <span class="rank-duration">{{ item.duration }}</span>
              </div>
            </div>
          </article>

          <article class="uvp-panel">
            <header class="panel-header panel-header--inline">
              <div>
                <h2>告警统计</h2>
                <p>近 24 小时</p>
              </div>
              <select class="panel-select" aria-label="告警统计时间范围">
                <option>近 24 小时</option>
                <option>近 7 天</option>
              </select>
            </header>
            <div class="alarm-wrap">
              <DonutChart :data="alarmData" total="32" totalLabel="告警总数" />
              <div class="alarm-legend">
                <div class="alarm-legend__item" v-for="item in alarmData" :key="item.name">
                  <span class="legend-dot" :style="{ background: item.color }" aria-hidden="true" />
                  <span class="legend-name">{{ item.name }}</span>
                  <strong>{{ item.count }}</strong>
                  <span>{{ item.pct }}</span>
                </div>
              </div>
            </div>
          </article>

          <article class="uvp-panel uptime-panel">
            <header class="panel-header">
              <div>
                <h2>系统运行时长</h2>
                <p>v2.3.0 · 2025-05-06</p>
              </div>
            </header>
            <div class="uptime-clock" aria-label="系统已运行 15 天 6 小时 42 分钟 18 秒">
              <span><strong>15</strong>天</span>
              <span><strong>6</strong>小时</span>
              <span><strong>42</strong>分钟</span>
              <span><strong>18</strong>秒</span>
            </div>
          </article>
        </aside>
      </section>
    </main>
  </div>
</template>

<script setup lang="ts">
import LineChart from "./components/line-chart.vue";
import DonutChart from "./components/donut-chart.vue";
import SipDashboardCard from "./components/sip-dashboard/index.vue";

import iconDevices from "@/assets/img/icon-devices.svg";
import iconOnline from "@/assets/img/icon-online.svg";
import iconChannels from "@/assets/img/icon-channels.svg";
import iconPlaying from "@/assets/img/icon-playing.svg";
import iconServers from "@/assets/img/icon-servers.svg";
import iconAlarm from "@/assets/img/icon-alarm.svg";

defineOptions({ name: "Home" });

const statCards = [
  { label: "设备总数", value: "1,248", icon: iconDevices, extra: "较昨日 +3", extraType: "up" },
  { label: "在线设备", value: "1,032", icon: iconOnline, rateBadge: "在线率 82.69%", extra: "较昨日 +18", extraType: "up" },
  { label: "通道总数", value: "6,732", icon: iconChannels, extra: "较昨日 +57", extraType: "up" },
  { label: "正在播放", value: "465", icon: iconPlaying, extra: "较昨日 +23", extraType: "up" },
  { label: "流媒体节点", value: "6", icon: iconServers, split: { ok: 6, err: 0 } },
  { label: "今日告警", value: "32", icon: iconAlarm, extra: "较昨日 +8", extraType: "down" }
];

const hours = ["14:00", "16:00", "18:00", "20:00", "22:00", "00:00", "02:00", "04:00", "06:00", "08:00", "10:00", "12:00"];

const sipSeries = [
  { label: "注册成功", color: "var(--uvp-brand)", data: [680, 720, 580, 420, 280, 180, 120, 150, 380, 850, 1100, 1250] },
  { label: "注册失败", color: "var(--uvp-danger)", data: [35, 42, 28, 18, 12, 8, 5, 6, 22, 48, 65, 72] },
  { label: "未注册", color: "var(--uvp-text-tertiary)", data: [120, 135, 150, 180, 200, 210, 215, 205, 160, 95, 60, 45] }
];

const playSeries = [
  { label: "请求总数", color: "var(--uvp-brand)", data: [450, 520, 380, 220, 120, 65, 40, 55, 280, 580, 780, 920] },
  { label: "成功次数", color: "var(--uvp-brand-cyan)", data: [435, 502, 365, 210, 115, 60, 38, 52, 268, 560, 755, 895] },
  { label: "失败次数", color: "var(--uvp-danger)", data: [15, 18, 15, 10, 5, 5, 2, 3, 12, 20, 25, 25] }
];

const mediaServers = [
  { name: "zlm-01(主)", cpu: 23, mem: 45, streams: 128, rtp: "256/1000", up: "320 Mbps", down: "1.2 Gbps" },
  { name: "zlm-02", cpu: 18, mem: 38, streams: 96, rtp: "192/1000", up: "240 Mbps", down: "960 Mbps" },
  { name: "zlm-03", cpu: 31, mem: 52, streams: 156, rtp: "312/1000", up: "380 Mbps", down: "1.5 Gbps" },
  { name: "zlm-04", cpu: 12, mem: 28, streams: 64, rtp: "128/1000", up: "160 Mbps", down: "640 Mbps" },
  { name: "zlm-05", cpu: 8, mem: 22, streams: 32, rtp: "64/1000", up: "80 Mbps", down: "320 Mbps" },
  { name: "zlm-06", cpu: 15, mem: 35, streams: 85, rtp: "170/1000", up: "200 Mbps", down: "800 Mbps" }
];

const recentEvents = [
  { time: "2026-04-29 10:23:15", level: "信息", levelClass: "info", type: "GB28181注册", content: "设备 340200...0001 注册成功", source: "前端球机-1" },
  { time: "2026-04-29 10:20:08", level: "告警", levelClass: "error", type: "通道离线", content: "通道 340200...0015_01 心跳超时", source: "高速卡口-3" },
  { time: "2026-04-29 10:18:42", level: "信息", levelClass: "info", type: "GB28181呼叫", content: "INVITE 呼叫成功，建立 RTP 推流", source: "枪机-2" },
  { time: "2026-04-29 10:15:30", level: "告警", levelClass: "warning", type: "播放失败", content: "INVITE 超时，设备未响应 200 OK", source: "单元门口机-5" },
  { time: "2026-04-29 10:12:55", level: "信息", levelClass: "info", type: "GB28181注册", content: "设备 340200...0023 注册成功", source: "人脸抓拍机-2" }
];

const abnormalDevices = [
  { id: "34020000001320000001", name: "前端球机-1", reason: "心跳超时", reasonClass: "red", duration: "2小时35分" },
  { id: "34020000001320000015", name: "高速卡口-3", reason: "Catalog失败", reasonClass: "orange", duration: "1小时48分" },
  { id: "34020000001320000023", name: "人脸抓拍机-2", reason: "RTP无流", reasonClass: "orange", duration: "1小时12分" },
  { id: "34020000001320000037", name: "单元门口机-5", reason: "INVITE超时", reasonClass: "orange", duration: "58分" },
  { id: "34020000001320000042", name: "枪机-8", reason: "心跳超时", reasonClass: "red", duration: "47分" }
];

const alarmData = [
  { name: "心跳超时", count: 12, pct: "37.50%", color: "var(--uvp-danger)" },
  { name: "Catalog失败", count: 7, pct: "21.88%", color: "var(--uvp-warning)" },
  { name: "RTP无流", count: 6, pct: "18.75%", color: "#f59e0b" },
  { name: "INVITE超时", count: 4, pct: "12.50%", color: "var(--uvp-brand)" },
  { name: "其他", count: 3, pct: "9.38%", color: "var(--uvp-brand-cyan)" }
];

const rankClass = (idx: number) => {
  if (idx === 0) return "rank-num--top1";
  if (idx === 1) return "rank-num--top2";
  if (idx === 2) return "rank-num--top3";
  return "rank-num--normal";
};
</script>

<style lang="scss" scoped>
.home-page {
  display: flex;
  flex-direction: column;
  gap: 20px;
  min-height: 100%;
}

.home-overview {
  display: flex;
  gap: 24px;
  align-items: flex-end;
  justify-content: space-between;
  padding: 22px 24px;
  background:
    radial-gradient(circle at 0% 0%, rgb(37 99 235 / 8%), transparent 32%),
    var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: var(--uvp-panel-radius);
  box-shadow: var(--uvp-panel-shadow);
}

.overview-copy {
  min-width: 0;

  h1 {
    margin: 4px 0 8px;
    font-size: 24px;
    font-weight: 650;
    line-height: 1.28;
    color: var(--uvp-text-primary);
  }

  p {
    margin: 0;
    font-size: 13px;
    color: var(--uvp-text-tertiary);
  }
}

.overview-kicker {
  font-size: 12px;
  font-weight: 700;
  color: var(--uvp-brand);
  text-transform: uppercase;
}

.overview-status {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  justify-content: flex-end;
}

.status-pill {
  display: inline-flex;
  align-items: center;
  min-height: 32px;
  padding: 0 12px;
  font-size: 12px;
  font-weight: 600;
  color: var(--uvp-text-secondary);
  white-space: nowrap;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 999px;
}

.status-pill--ok {
  color: var(--uvp-brand-cyan);
  background: rgb(15 170 166 / 10%);
  border-color: rgb(15 170 166 / 18%);
}

.status-pill--warn {
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
  border-color: var(--uvp-warning-border);
}

.stat-grid {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: 12px;
}

.stat-card,
.uvp-panel {
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: var(--uvp-panel-radius);
  box-shadow: var(--uvp-panel-shadow);
}

.stat-card {
  display: flex;
  gap: 14px;
  align-items: center;
  min-width: 0;
  min-height: 108px;
  padding: 18px;
}

.stat-card__icon {
  display: flex;
  flex: 0 0 44px;
  align-items: center;
  justify-content: center;
  width: 44px;
  height: 44px;
  background: var(--uvp-brand-soft);
  border-radius: 12px;

  img {
    width: 32px;
    height: 32px;
    object-fit: contain;
  }
}

.stat-card__content {
  min-width: 0;
}

.stat-card__label,
.stat-card__extra {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}

.stat-card__value {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  margin: 5px 0 4px;
  font-size: 26px;
  font-weight: 700;
  line-height: 1.1;
  color: var(--uvp-text-primary);
}

.rate-badge {
  padding: 3px 8px;
  font-size: 12px;
  font-weight: 600;
  color: var(--uvp-brand-cyan);
  background: rgb(15 170 166 / 10%);
  border-radius: 999px;
}

.up,
.ok {
  color: var(--uvp-brand-cyan);
}

.down,
.danger {
  color: var(--uvp-danger);
}

.dashboard-grid {
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(320px, 0.92fr);
  gap: 16px;
  align-items: start;
}

.dashboard-main,
.dashboard-side {
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-width: 0;
}

.chart-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.uvp-panel {
  min-width: 0;
  padding: 18px;
}

.chart-panel {
  overflow: hidden;
}

.panel-header {
  display: flex;
  gap: 16px;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 14px;

  h2 {
    margin: 0;
    font-size: 15px;
    font-weight: 650;
    color: var(--uvp-text-primary);
  }

  p {
    margin: 4px 0 0;
    font-size: 12px;
    color: var(--uvp-text-tertiary);
  }
}

.panel-header--inline {
  align-items: center;
}

.panel-select,
.event-toolbar select {
  min-width: 116px;
  height: 34px;
  padding: 0 28px 0 10px;
  font-size: 12px;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: var(--uvp-search-control-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
  outline: none;
  transition: border-color 0.2s ease, box-shadow 0.2s ease;

  &:focus-visible {
    border-color: var(--uvp-brand);
    box-shadow: 0 0 0 3px rgb(37 99 235 / 12%);
  }
}

.table-scroll {
  width: 100%;
  overflow-x: auto;
}

.native-data-table {
  width: 100%;
  min-width: 780px;
  border-collapse: separate;
  border-spacing: 0;

  th {
    padding: 11px 12px;
    font-size: 12px;
    font-weight: 600;
    color: var(--uvp-text-secondary);
    text-align: left;
    white-space: nowrap;
    background: var(--uvp-table-header-bg);
    border-bottom: 1px solid var(--uvp-panel-border);
  }

  td {
    padding: 12px;
    font-size: 13px;
    color: var(--uvp-text-primary);
    white-space: nowrap;
    background: var(--uvp-table-row-bg);
    border-bottom: 1px solid var(--uvp-panel-border);
  }

  tr:hover td {
    background: var(--uvp-table-row-hover-bg);
  }
}

.metric-cell {
  display: inline-flex;
  gap: 8px;
  align-items: center;
}

.status-dot {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  font-size: 12px;

  &::before {
    flex: 0 0 7px;
    width: 7px;
    height: 7px;
    content: "";
    border-radius: 50%;
  }
}

.status-dot--ok::before {
  background: var(--uvp-brand-cyan);
}

.progress-bar {
  display: inline-flex;
  width: 68px;
  height: 6px;
  overflow: hidden;
  background: var(--uvp-list-toolbar-bg);
  border-radius: 999px;
}

.fill {
  height: 100%;
  border-radius: inherit;
}

.fill--ok {
  background: var(--uvp-brand-cyan);
}

.fill--brand {
  background: var(--uvp-brand);
}

.event-toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
  margin-bottom: 14px;
  padding: 12px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 12px;
}

.icon-button,
.text-button,
.link-button {
  cursor: pointer;
  transition: color 0.2s ease, border-color 0.2s ease, background-color 0.2s ease, box-shadow 0.2s ease;

  &:focus-visible {
    outline: none;
    box-shadow: 0 0 0 3px rgb(37 99 235 / 12%);
  }
}

.icon-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  margin-left: auto;
  color: var(--uvp-text-secondary);
  background: var(--uvp-search-control-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;

  &:hover {
    color: var(--uvp-brand);
    border-color: var(--uvp-brand);
  }
}

.text-button,
.link-button {
  padding: 0;
  color: var(--uvp-brand);
  background: transparent;
  border: 0;
}

.text-button {
  min-height: 32px;
  font-size: 13px;
}

.link-button {
  min-height: 28px;
  font-size: 13px;
}

.tag,
.reason-tag {
  display: inline-flex;
  align-items: center;
  min-height: 24px;
  padding: 0 8px;
  font-size: 12px;
  border-radius: 999px;
}

.tag--info {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
}

.tag--warning,
.reason-tag--orange {
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
  border: 1px solid var(--uvp-warning-border);
}

.tag--error,
.reason-tag--red {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
  border: 1px solid var(--uvp-danger-border);
}

.rank-list {
  display: flex;
  flex-direction: column;
}

.rank-row {
  display: grid;
  grid-template-columns: 42px minmax(0, 1fr) 92px 76px;
  gap: 10px;
  align-items: center;
  min-height: 54px;
  border-bottom: 1px solid var(--uvp-panel-border);

  &:last-child {
    border-bottom: 0;
  }
}

.rank-row--head {
  min-height: 32px;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}

.rank-num {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 24px;
  font-size: 12px;
  font-weight: 700;
  border-radius: 999px;
}

.rank-num--top1 {
  color: #fff;
  background: var(--uvp-danger);
}

.rank-num--top2 {
  color: #fff;
  background: var(--uvp-warning);
}

.rank-num--top3 {
  color: #fff;
  background: #f59e0b;
}

.rank-num--normal {
  color: var(--uvp-text-tertiary);
  background: var(--uvp-list-toolbar-bg);
}

.rank-device {
  min-width: 0;

  strong,
  em {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  strong {
    font-size: 13px;
    color: var(--uvp-text-primary);
  }

  em {
    margin-top: 2px;
    font-size: 11px;
    font-style: normal;
    color: var(--uvp-text-tertiary);
  }
}

.rank-duration {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
  text-align: right;
}

.alarm-wrap {
  display: flex;
  gap: 18px;
  align-items: center;
}

.alarm-legend {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 9px;
  min-width: 0;
}

.alarm-legend__item {
  display: grid;
  grid-template-columns: 10px minmax(0, 1fr) auto auto;
  gap: 8px;
  align-items: center;
  font-size: 12px;
  color: var(--uvp-text-tertiary);

  strong {
    color: var(--uvp-text-primary);
  }
}

.legend-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.legend-name {
  overflow: hidden;
  color: var(--uvp-text-secondary);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.uptime-panel {
  background:
    linear-gradient(135deg, rgb(37 99 235 / 8%), transparent 42%),
    var(--uvp-panel-bg);
}

.uptime-clock {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;

  span {
    display: flex;
    flex-direction: column;
    gap: 4px;
    align-items: center;
    justify-content: center;
    min-height: 70px;
    font-size: 12px;
    color: var(--uvp-text-tertiary);
    background: var(--uvp-list-toolbar-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 12px;
  }

  strong {
    font-size: 26px;
    line-height: 1;
    color: var(--uvp-text-primary);
  }
}

:deep(.sip-card) {
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: var(--uvp-panel-radius);
  box-shadow: var(--uvp-panel-shadow);
}

@media (max-width: 1440px) {
  .stat-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .dashboard-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}

@media (max-width: 1024px) {
  .chart-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 768px) {
  .home-page {
    gap: 16px;
  }

  .home-overview {
    align-items: flex-start;
    padding: 18px;
  }

  .home-overview,
  .alarm-wrap {
    flex-direction: column;
  }

  .overview-status {
    justify-content: flex-start;
  }

  .stat-grid {
    grid-template-columns: 1fr;
  }

  .stat-card,
  .uvp-panel {
    padding: 16px;
  }

  .rank-row {
    grid-template-columns: 34px minmax(0, 1fr);
  }

  .rank-row--head {
    display: none;
  }

  .reason-tag,
  .rank-duration {
    grid-column: 2;
    justify-self: start;
  }

  .uptime-clock {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
