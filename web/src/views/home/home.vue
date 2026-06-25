<template>
  <div class="dashboard">
    <!-- 6 Stat Cards -->
    <div class="stat-row">
      <div class="stat-card" v-for="card in statCards" :key="card.label">
        <div class="stat-card-icon">
          <img :src="card.icon" :alt="card.label" />
        </div>
        <div class="stat-card-content">
          <div class="stat-card-label">{{ card.label }}</div>
          <div class="stat-card-value">
            {{ card.value }}
            <span v-if="card.rateBadge" class="rate-badge">{{ card.rateBadge }}</span>
          </div>
          <div class="stat-card-extra">
            <template v-if="card.extra">
              <span :class="card.extraType">{{ card.extra }}</span>
            </template>
            <template v-if="card.split">
              正常
              <span style="color: var(--success); font-weight: 500">{{ card.split.ok }}</span>
              / 异常
              <span style="color: var(--danger); font-weight: 500">{{ card.split.err }}</span>
            </template>
          </div>
        </div>
      </div>
    </div>

    <!-- Two-column waterfall layout -->
    <div class="waterfall">
      <!-- Left column (2/3) -->
      <div class="col-left">
        <!-- 2 charts -->
        <div class="charts-row">
          <div class="panel">
            <div class="panel-header">
              <span class="panel-title">SIP 注册趋势</span>
              <select class="time-select">
                <option>近 24 小时</option>
                <option>近 7 天</option>
              </select>
            </div>
            <LineChart :series="sipSeries" :labels="hours" />
          </div>
          <div class="panel">
            <div class="panel-header">
              <span class="panel-title">播放请求趋势</span>
              <select class="time-select">
                <option>近 24 小时</option>
                <option>近 7 天</option>
              </select>
            </div>
            <LineChart :series="playSeries" :labels="hours" />
          </div>
        </div>

        <!-- Media Server Health -->
        <div class="panel">
          <div class="panel-header">
            <span class="panel-title">媒体服务器健康状态(ZLMediaKit)</span>
          </div>
          <table class="data-table">
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
                <td><span class="status-dot green">正常</span></td>
                <td>
                  {{ row.cpu }}%
                  <span class="progress-bar"><span class="fill green" :style="{ width: row.cpu + '%' }" /></span>
                </td>
                <td>
                  {{ row.mem }}%
                  <span class="progress-bar"><span class="fill blue" :style="{ width: row.mem + '%' }" /></span>
                </td>
                <td>{{ row.streams }}</td>
                <td>{{ row.rtp }}</td>
                <td>{{ row.up }}</td>
                <td>{{ row.down }}</td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- Recent Events -->
        <div class="panel">
          <div class="panel-header">
            <span class="panel-title">最近事件</span>
          </div>
          <div class="event-filter">
            <select>
              <option>全部事件</option>
              <option>GB28181注册</option>
              <option>通道离线</option>
              <option>GB28181呼叫</option>
              <option>播放失败</option>
            </select>
            <select>
              <option>全部级别</option>
              <option>信息</option>
              <option>告警</option>
              <option>错误</option>
            </select>
            <select>
              <option>近 24 小时</option>
              <option>近 7 天</option>
              <option>近 30 天</option>
            </select>
            <span class="spacer" />
            <button class="refresh-btn" title="刷新">↻</button>
          </div>
          <table class="data-table">
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
                <td><span class="tag" :class="ev.levelClass">{{ ev.level }}</span></td>
                <td>{{ ev.type }}</td>
                <td>{{ ev.content }}</td>
                <td>{{ ev.source }}</td>
                <td><button class="link-btn">详情</button></td>
              </tr>
            </tbody>
          </table>
          <div class="pagination">
            <span class="total">共 256 条</span>
            <span class="page-btn disabled">&lt;</span>
            <span class="page-btn active">1</span>
            <span class="page-btn">2</span>
            <span class="page-btn">3</span>
            <span class="dots">...</span>
            <span class="page-btn">26</span>
            <span class="page-btn">&gt;</span>
            <select class="page-size">
              <option>10 条/页</option>
              <option>20 条/页</option>
              <option>50 条/页</option>
            </select>
          </div>
        </div>
      </div>

      <!-- Right column (1/3) -->
      <div class="col-right">
        <!-- Abnormal Device Ranking -->
        <div class="panel">
          <div class="panel-header">
            <span class="panel-title">异常设备排行</span>
            <span class="panel-more">更多 ›</span>
          </div>
          <div class="rank-header">
            <span style="width: 36px; text-align: center">排名</span>
            <span style="width: 42%">设备名称(国标ID)</span>
            <span style="width: 25%; text-align: center">异常原因</span>
            <span style="width: 20%; text-align: right">持续时间</span>
          </div>
          <ul class="rank-list">
            <li class="rank-item" v-for="(item, idx) in abnormalDevices" :key="item.id">
              <span class="rank-num" :class="rankClass(idx)">{{ idx + 1 }}</span>
              <div class="rank-info">
                <div class="rank-name">{{ item.name }}</div>
                <div class="rank-id">{{ item.id }}</div>
              </div>
              <span class="rank-reason">
                <span class="reason-tag" :class="item.reasonClass">{{ item.reason }}</span>
              </span>
              <span class="rank-duration">{{ item.duration }}</span>
            </li>
          </ul>
        </div>

        <!-- Alarm Stats -->
        <div class="panel">
          <div class="panel-header">
            <span class="panel-title">告警统计</span>
            <select class="time-select">
              <option>近 24 小时</option>
              <option>近 7 天</option>
            </select>
          </div>
          <div class="alarm-pie-wrap">
            <DonutChart :data="alarmData" total="32" totalLabel="告警总数" />
            <div class="alarm-legend">
              <div class="alarm-legend-item" v-for="item in alarmData" :key="item.name">
                <span class="dot" :style="{ background: item.color }" />
                <span class="name">{{ item.name }}</span>
                <span class="count">{{ item.count }}</span>
                <span class="pct">{{ item.pct }}</span>
              </div>
            </div>
          </div>
        </div>

        <!-- System Uptime -->
        <div class="panel">
          <div class="uptime-head">
            <span class="panel-title">系统运行时长</span>
            <div class="uptime-version">
              版本信息:v2.3.0(2025-05-06)
              <a href="#">检查更新</a>
            </div>
          </div>
          <div class="uptime-clock">
            <span class="num">15</span><span class="unit"> 天 </span>
            <span class="num">6</span><span class="unit"> 小时 </span>
            <span class="num">42</span><span class="unit"> 分钟 </span>
            <span class="num">18</span><span class="unit"> 秒</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import LineChart from "./components/line-chart.vue";
import DonutChart from "./components/donut-chart.vue";

import iconDevices from "@/assets/img/icon-devices.svg";
import iconOnline from "@/assets/img/icon-online.svg";
import iconChannels from "@/assets/img/icon-channels.svg";
import iconPlaying from "@/assets/img/icon-playing.svg";
import iconServers from "@/assets/img/icon-servers.svg";
import iconAlarm from "@/assets/img/icon-alarm.svg";

defineOptions({ name: "Home" });

const statCards = [
  { label: "设备总数", value: "1,248", icon: iconDevices, extra: "较昨日 +3 ↑", extraType: "up" },
  { label: "在线设备", value: "1,032", icon: iconOnline, rateBadge: "在线率 82.69%", extra: "较昨日 +18 ↑", extraType: "up" },
  { label: "通道总数", value: "6,732", icon: iconChannels, extra: "较昨日 +57 ↑", extraType: "up" },
  { label: "正在播放", value: "465", icon: iconPlaying, extra: "较昨日 +23 ↑", extraType: "up" },
  { label: "流媒体节点", value: "6", icon: iconServers, split: { ok: 6, err: 0 } },
  { label: "今日告警", value: "32", icon: iconAlarm, extra: "较昨日 +8 ↑", extraType: "down" }
];

const hours = ["14:00", "16:00", "18:00", "20:00", "22:00", "00:00", "02:00", "04:00", "06:00", "08:00", "10:00", "12:00"];

const sipSeries = [
  { label: "注册成功", color: "#1890ff", data: [680, 720, 580, 420, 280, 180, 120, 150, 380, 850, 1100, 1250] },
  { label: "注册失败", color: "#ff4d4f", data: [35, 42, 28, 18, 12, 8, 5, 6, 22, 48, 65, 72] },
  { label: "未注册", color: "#d9d9d9", data: [120, 135, 150, 180, 200, 210, 215, 205, 160, 95, 60, 45] }
];

const playSeries = [
  { label: "请求总数", color: "#1890ff", data: [450, 520, 380, 220, 120, 65, 40, 55, 280, 580, 780, 920] },
  { label: "成功次数", color: "#52c41a", data: [435, 502, 365, 210, 115, 60, 38, 52, 268, 560, 755, 895] },
  { label: "失败次数", color: "#ff4d4f", data: [15, 18, 15, 10, 5, 5, 2, 3, 12, 20, 25, 25] }
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
  { time: "2026-04-29 10:18:42", level: "信息", levelClass: "info", type: "GB28181呼叫", content: "INVITE 呼叫成功,建立 RTP 推流", source: "枪机-2" },
  { time: "2026-04-29 10:15:30", level: "告警", levelClass: "warning", type: "播放失败", content: "INVITE 超时,设备未响应 200 OK", source: "单元门口机-5" },
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
  { name: "心跳超时", count: 12, pct: "37.50%", color: "#ff4d4f" },
  { name: "Catalog失败", count: 7, pct: "21.88%", color: "#fa8c16" },
  { name: "RTP无流", count: 6, pct: "18.75%", color: "#faad14" },
  { name: "INVITE超时", count: 4, pct: "12.50%", color: "#1890ff" },
  { name: "其他", count: 3, pct: "9.38%", color: "#722ed1" }
];

const rankClass = (idx: number) => {
  if (idx === 0) return "top1";
  if (idx === 1) return "top2";
  if (idx === 2) return "top3";
  return "normal";
};
</script>

<style lang="scss" scoped>
.dashboard {
  --primary: #1890ff;
  --success: #52c41a;
  --warning: #fa8c16;
  --danger: #ff4d4f;
  --text: #333;
  --text-secondary: #666;
  --text-hint: #999;
  --border: #e8e8e8;
  --bg: #f0f2f5;
  --white: #fff;

  padding: 16px;
  background: var(--bg);
  min-height: calc(100vh - 56px);
  color: var(--text);
  font-family: -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif;
}

/* === Stat Cards === */
.stat-row {
  display: grid;
  grid-template-columns: repeat(6, 1fr);
  gap: 12px;
  margin-bottom: 16px;
}
.stat-card {
  background: var(--white);
  border-radius: 8px;
  padding: 16px 20px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.04);
  display: flex;
  align-items: center;
  gap: 20px;

  .stat-card-icon {
    width: 52px;
    height: 52px;
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;

    img {
      width: 48px;
      height: 48px;
      object-fit: contain;
    }
  }
  .stat-card-content {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .stat-card-label {
    font-size: 13px;
    color: var(--text-hint);
  }
  .stat-card-value {
    font-size: 28px;
    font-weight: 700;
    color: var(--text);
    line-height: 1.2;
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: nowrap;
  }
  .rate-badge {
    font-size: 12px;
    font-weight: 400;
    background: #e8f8e8;
    color: #389e0d;
    padding: 3px 10px;
    border-radius: 12px;
    white-space: nowrap;
    line-height: 1.4;
  }
  .stat-card-extra {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: var(--text-hint);

    .up { color: var(--success); }
    .down { color: var(--danger); }
  }
}

/* === Waterfall Layout === */
.waterfall {
  display: flex;
  gap: 12px;
  align-items: flex-start;

  .col-left {
    flex: 2;
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
  }
  .col-right {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
  }
}
.charts-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}

/* === Panel === */
.panel {
  background: var(--white);
  border-radius: 8px;
  padding: 16px 20px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.04);
}
.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}
.panel-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--text);
  display: flex;
  align-items: center;
  gap: 6px;
}
.panel-more {
  font-size: 13px;
  color: var(--primary);
  cursor: pointer;

  &:hover { text-decoration: underline; }
}
.time-select {
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 4px 10px;
  font-size: 12px;
  color: var(--text-secondary);
  background: var(--white);
  cursor: pointer;
  outline: none;
}

/* === Data Table === */
.data-table {
  width: 100%;
  border-collapse: collapse;

  th {
    background: #fafafa;
    padding: 10px 12px;
    text-align: left;
    font-size: 12px;
    color: var(--text-secondary);
    font-weight: 500;
    border-bottom: 1px solid var(--border);
  }
  td {
    padding: 10px 12px;
    font-size: 13px;
    color: var(--text);
    border-bottom: 1px solid #f5f5f5;
  }
  tr:hover td { background: #fafafa; }
}
.status-dot {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;

  &::before {
    content: "";
    width: 6px;
    height: 6px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  &.green::before { background: var(--success); }
  &.red::before { background: var(--danger); }
}
.progress-bar {
  width: 60px;
  height: 6px;
  background: #f0f0f0;
  border-radius: 3px;
  overflow: hidden;
  display: inline-block;
  vertical-align: middle;
  margin-left: 6px;

  .fill {
    height: 100%;
    border-radius: 3px;
    display: block;

    &.blue { background: var(--primary); }
    &.green { background: var(--success); }
  }
}
.tag {
  display: inline-block;
  padding: 1px 8px;
  border-radius: 3px;
  font-size: 12px;

  &.info { background: #e6f7ff; color: var(--primary); }
  &.warning { background: #fff7e6; color: var(--warning); }
  &.error { background: #fff1f0; color: var(--danger); }
  &.success { background: #f6ffed; color: var(--success); }
}

/* === Event filter === */
.event-filter {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 12px;

  select {
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 5px 10px;
    font-size: 13px;
    background: var(--white);
    outline: none;
  }
  .spacer { flex: 1; }
  .refresh-btn {
    width: 28px;
    height: 28px;
    border: 1px solid var(--border);
    border-radius: 4px;
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    background: var(--white);
    color: var(--text-hint);

    &:hover { border-color: var(--primary); color: var(--primary); }
  }
}

.link-btn {
  color: var(--primary);
  font-size: 13px;
  cursor: pointer;
  background: none;
  border: none;
  font-family: inherit;

  &:hover { text-decoration: underline; }
}

/* === Pagination === */
.pagination {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid #f5f5f5;

  .total { font-size: 13px; color: var(--text-hint); margin-right: 12px; }
  .page-btn {
    min-width: 32px;
    height: 32px;
    border: 1px solid var(--border);
    border-radius: 4px;
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    font-size: 13px;
    color: var(--text);
    background: var(--white);

    &:hover { border-color: var(--primary); color: var(--primary); }
    &.active { background: var(--primary); color: var(--white); border-color: var(--primary); }
    &.disabled { color: #d9d9d9; cursor: not-allowed; }
  }
  .dots { font-size: 13px; color: var(--text-hint); padding: 0 4px; }
  .page-size {
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 5px 8px;
    font-size: 13px;
    margin-left: 12px;
    outline: none;
  }
}

/* === Rank list === */
.rank-header {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 0;
  border-bottom: 1px solid #f0f0f0;
  font-size: 12px;
  color: var(--text-hint);
}
.rank-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.rank-item {
  display: flex;
  align-items: center;
  padding: 7px 0;
  border-bottom: 1px solid #f5f5f5;
  gap: 12px;

  &:last-child { border-bottom: none; }

  .rank-num {
    width: 36px;
    height: 22px;
    border-radius: 11px;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 12px;
    font-weight: 600;
    flex-shrink: 0;

    &.top1 { background: #ff4d4f; color: #fff; }
    &.top2 { background: #fa8c16; color: #fff; }
    &.top3 { background: #faad14; color: #fff; }
    &.normal { background: #f0f0f0; color: #999; }
  }
  .rank-info {
    width: 42%;
    min-width: 0;

    .rank-name {
      font-size: 13px;
      color: var(--text);
      font-weight: 500;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .rank-id {
      font-size: 11px;
      color: #bbb;
      margin-top: 1px;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
  }
  .rank-reason {
    width: 25%;
    text-align: center;
    flex-shrink: 0;

    .reason-tag {
      display: inline-block;
      padding: 2px 8px;
      border-radius: 3px;
      font-size: 11px;
      border: 1px solid;

      &.red {
        color: var(--danger);
        border-color: var(--danger);
        background: #fff1f0;
      }
      &.orange {
        color: var(--warning);
        border-color: var(--warning);
        background: #fff7e6;
      }
    }
  }
  .rank-duration {
    font-size: 12px;
    color: var(--text-hint);
    width: 20%;
    text-align: right;
    flex-shrink: 0;
  }
}

/* === Alarm pie === */
.alarm-pie-wrap {
  display: flex;
  align-items: center;
  gap: 20px;
}
.alarm-legend {
  display: flex;
  flex-direction: column;
  gap: 10px;
  flex: 1;

  .alarm-legend-item {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;

    .dot {
      width: 8px;
      height: 8px;
      border-radius: 50%;
      flex-shrink: 0;
    }
    .name { color: var(--text); flex: 1; }
    .count { color: var(--text); font-weight: 600; margin-right: 4px; }
    .pct { color: var(--text-hint); font-size: 12px; }
  }
}

/* === Uptime === */
.uptime-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}
.uptime-version {
  font-size: 13px;
  color: var(--text-hint);

  a {
    color: var(--primary);
    text-decoration: none;
    margin-left: 12px;
  }
}
.uptime-clock {
  text-align: center;
  padding: 4px 0;

  .num {
    font-size: 28px;
    font-weight: 700;
    color: var(--text);
  }
  .unit {
    font-size: 13px;
    color: var(--text-hint);
  }
}
</style>
