<template>
  <svg class="architecture" viewBox="-13 0 433 284" role="img" aria-labelledby="login-arch-title login-arch-description">
    <title id="login-arch-title">UVP GB28181：信令控制与媒体传输分离</title>
    <desc id="login-arch-description">
      设备通过双向 SIP 信令连接 UVP，RTP/PS 视频流直接送达 ZLMediaKit；UVP 通过 API 与 Hook 调度流媒体，并读写数据库和 Redis
      或内存缓存。
    </desc>
    <defs>
      <linearGradient id="login-arch-hub-fill" x1="0" y1="0" x2="1" y2="1">
        <stop stop-color="#2267ad" />
        <stop offset="1" stop-color="#153b68" />
      </linearGradient>
      <symbol id="login-arch-camera" viewBox="0 0 24 24">
        <rect x="3" y="5" width="15" height="11" rx="2" />
        <path d="m18 8 4-2v9l-4-2M8 16v4m-4 0h8" />
        <circle cx="10" cy="10.5" r="2" />
      </symbol>
      <symbol id="login-arch-recorder" viewBox="0 0 24 24">
        <rect x="2" y="6" width="20" height="12" rx="3" />
        <path d="M6 10h8m-8 4h5" />
        <circle cx="18" cy="12" r="1" />
      </symbol>
      <symbol id="login-arch-lower-platform" viewBox="0 0 24 24">
        <rect x="5" y="2" width="14" height="8" rx="2" />
        <path d="M12 10v5m-7 0h14M5 15v5m7-5v5m7-5v5" />
      </symbol>
      <symbol id="login-arch-media" viewBox="0 0 24 24">
        <rect x="2" y="4" width="20" height="16" rx="3" />
        <path d="m10 8 6 4-6 4Z" />
      </symbol>
      <symbol id="login-arch-database" viewBox="0 0 24 24">
        <ellipse cx="12" cy="5" rx="8" ry="3" />
        <path d="M4 5v14c0 4 16 4 16 0V5M4 12c0 4 16 4 16 0" />
      </symbol>
      <symbol id="login-arch-cache" viewBox="0 0 24 24"><path d="m12 3 10 5-10 5L2 8Zm-10 9 10 5 10-5M2 16l10 5 10-5" /></symbol>

      <path id="login-arch-in-a" d="M85 85H102Q114 85 114 97V132Q114 144 126 144H146" />
      <path id="login-arch-in-b" d="M85 159H146" />
      <path id="login-arch-in-c" d="M85 233H102Q114 233 114 221V186Q114 174 126 174H146" />
      <path id="login-arch-out-a" d="M258 144H275Q287 144 287 132V97Q287 85 299 85H320" />
      <path id="login-arch-out-b" d="M258 159H320" />
      <path id="login-arch-out-c" d="M258 174H275Q287 174 287 186V221Q287 233 299 233H320" />
      <path id="login-arch-video-a" d="M1 85H-7V42Q-7 34 1 34H351Q363 34 363 46V61" />
      <path id="login-arch-video-b" d="M1 159H-7V42Q-7 34 1 34H351Q363 34 363 46V61" />
      <path id="login-arch-video-c" d="M1 233H-7V42Q-7 34 1 34H351Q363 34 363 46V61" />
    </defs>
    <text class="column-name" x="42" y="12" text-anchor="middle">设备接入</text>
    <text class="column-name" x="202" y="12" text-anchor="middle">统一平台</text>
    <text class="column-name" x="362" y="12" text-anchor="middle">基础服务</text>
    <text class="media-label" x="202" y="27" text-anchor="middle">RTP / PS 视频流 · 直达流媒体</text>
    <g aria-hidden="true">
      <path class="connection media" data-flow="media-direct" d="M1 233H-7V42Q-7 34 1 34H351Q363 34 363 46V61M1 159H-7M1 85H-7" />
      <use href="#login-arch-in-a" class="connection" data-flow="signal" />
      <use href="#login-arch-in-b" class="connection" data-flow="signal" />
      <use href="#login-arch-in-c" class="connection" data-flow="signal" />
      <use href="#login-arch-out-a" class="connection" data-flow="control" />
      <use href="#login-arch-out-b" class="connection data" data-flow="data" />
      <use href="#login-arch-out-c" class="connection data" data-flow="data" />
    </g>
    <g class="node">
      <rect class="node-box" x="1" y="61" width="84" height="48" rx="8" />
      <use href="#login-arch-camera" class="icon" x="12" y="75" width="21" height="21" />
      <text class="node-title" x="39" y="83">摄像机</text>
      <text class="node-subtitle" x="39" y="97">IPC</text>
    </g>
    <g class="node">
      <rect class="node-box" x="1" y="135" width="84" height="48" rx="8" />
      <use href="#login-arch-recorder" class="icon" x="12" y="149" width="21" height="21" />
      <text class="node-title" x="39" y="157">NVR</text>
      <text class="node-subtitle" x="39" y="171">录像设备</text>
    </g>
    <g class="node">
      <rect class="node-box" x="1" y="209" width="84" height="48" rx="8" />
      <use href="#login-arch-lower-platform" class="icon" x="12" y="223" width="21" height="21" />
      <text class="node-title" x="39" y="231">下级平台</text>
      <text class="node-subtitle" x="39" y="245">国标级联</text>
    </g>
    <g class="hub">
      <rect class="hub-box" x="146" y="113" width="112" height="92" rx="12" />
      <image :href="uvpMark" x="184" y="124" width="36" height="36" />
      <text x="202" y="179" text-anchor="middle" fill="#fff" font-size="13" font-weight="600">UVP GB28181</text>
      <text x="202" y="194" text-anchor="middle" fill="#b3d5f7" font-size="8.5">统一视频接入平台</text>
    </g>
    <g class="node">
      <rect class="service-box" x="320" y="61" width="86" height="48" rx="8" />
      <use href="#login-arch-media" class="icon service-icon" x="331" y="75" width="20" height="20" />
      <text class="node-title" x="357" y="83">流媒体</text>
      <text class="node-subtitle" x="357" y="97" font-size="7">ZLMediaKit</text>
    </g>
    <g class="node">
      <rect class="service-box" x="320" y="135" width="86" height="48" rx="8" />
      <use href="#login-arch-database" class="icon service-icon" x="331" y="149" width="20" height="20" />
      <text class="node-title" x="357" y="157">数据库</text>
      <text class="node-subtitle" x="357" y="171">持久存储</text>
    </g>
    <g class="node">
      <rect class="service-box" x="320" y="209" width="86" height="48" rx="8" />
      <use href="#login-arch-cache" class="icon service-icon" x="331" y="223" width="20" height="20" />
      <text class="node-title" x="357" y="231">缓存</text>
      <text class="node-subtitle" x="363" y="245" text-anchor="middle" style="font-size: 7px">Redis / Memory</text>
    </g>
    <text x="112" y="154" text-anchor="middle" fill="#8fc8ff" font-size="7">SIP</text>
    <text x="286" y="122" text-anchor="middle" fill="#8fc8ff" font-size="7">API / Hook</text>
    <g aria-hidden="true">
      <circle class="particle" r="1.8" data-route="in-a" data-direction="forward">
        <animateMotion dur="3.5s" begin="-0.4s" repeatCount="indefinite"><mpath href="#login-arch-in-a" /></animateMotion>
      </circle>
      <circle class="particle" r="1.4" data-route="in-a" data-direction="reverse">
        <animateMotion dur="3.5s" begin="-2.0s" repeatCount="indefinite" keyPoints="1;0" keyTimes="0;1" calcMode="linear">
          <mpath href="#login-arch-in-a" />
        </animateMotion>
      </circle>
      <circle class="particle" r="1.8" data-route="in-b" data-direction="forward">
        <animateMotion dur="3.7s" begin="-1.0s" repeatCount="indefinite"><mpath href="#login-arch-in-b" /></animateMotion>
      </circle>
      <circle class="particle" r="1.4" data-route="in-b" data-direction="reverse">
        <animateMotion dur="3.7s" begin="-2.6s" repeatCount="indefinite" keyPoints="1;0" keyTimes="0;1" calcMode="linear">
          <mpath href="#login-arch-in-b" />
        </animateMotion>
      </circle>
      <circle class="particle" r="1.8" data-route="in-c" data-direction="forward">
        <animateMotion dur="3.9s" begin="-1.6s" repeatCount="indefinite"><mpath href="#login-arch-in-c" /></animateMotion>
      </circle>
      <circle class="particle" r="1.4" data-route="in-c" data-direction="reverse">
        <animateMotion dur="3.9s" begin="-3.2s" repeatCount="indefinite" keyPoints="1;0" keyTimes="0;1" calcMode="linear">
          <mpath href="#login-arch-in-c" />
        </animateMotion>
      </circle>
      <circle class="particle" r="1.8" data-route="out-a" data-direction="forward">
        <animateMotion dur="4.1s" begin="-2.2s" repeatCount="indefinite"><mpath href="#login-arch-out-a" /></animateMotion>
      </circle>
      <circle class="particle" r="1.4" data-route="out-a" data-direction="reverse">
        <animateMotion dur="4.1s" begin="-3.8s" repeatCount="indefinite" keyPoints="1;0" keyTimes="0;1" calcMode="linear">
          <mpath href="#login-arch-out-a" />
        </animateMotion>
      </circle>
      <circle class="particle media" r="2.1" data-route="video-a" data-direction="forward">
        <animateMotion dur="4.2s" begin="0.0s" repeatCount="indefinite"><mpath href="#login-arch-video-a" /></animateMotion>
      </circle>
      <circle class="particle media" r="2.1" data-route="video-b" data-direction="forward">
        <animateMotion dur="4.2s" begin="-1.4s" repeatCount="indefinite"><mpath href="#login-arch-video-b" /></animateMotion>
      </circle>
      <circle class="particle media" r="2.1" data-route="video-c" data-direction="forward">
        <animateMotion dur="4.2s" begin="-2.8s" repeatCount="indefinite"><mpath href="#login-arch-video-c" /></animateMotion>
      </circle>
      <circle class="particle data" r="1.3" data-route="out-b" data-direction="forward">
        <animateMotion dur="6s" begin="-1.0s" repeatCount="indefinite"><mpath href="#login-arch-out-b" /></animateMotion>
      </circle>
      <circle class="particle data" r="1.3" data-route="out-b" data-direction="reverse">
        <animateMotion dur="6s" begin="-4.0s" repeatCount="indefinite" keyPoints="1;0" keyTimes="0;1" calcMode="linear">
          <mpath href="#login-arch-out-b" />
        </animateMotion>
      </circle>
      <circle class="particle data" r="1.3" data-route="out-c" data-direction="forward">
        <animateMotion dur="7s" begin="-2.7s" repeatCount="indefinite"><mpath href="#login-arch-out-c" /></animateMotion>
      </circle>
      <circle class="particle data" r="1.3" data-route="out-c" data-direction="reverse">
        <animateMotion dur="7s" begin="-5.7s" repeatCount="indefinite" keyPoints="1;0" keyTimes="0;1" calcMode="linear">
          <mpath href="#login-arch-out-c" />
        </animateMotion>
      </circle>
    </g>
    <g aria-hidden="true" fill="#92d2ff">
      <circle cx="85" cy="85" r="1.8" />
      <circle cx="85" cy="159" r="1.8" />
      <circle cx="85" cy="233" r="1.8" />
      <circle cx="146" cy="144" r="1.8" />
      <circle cx="146" cy="159" r="1.8" />
      <circle cx="146" cy="174" r="1.8" />
      <circle cx="258" cy="144" r="1.8" />
      <circle cx="258" cy="159" r="1.8" />
      <circle cx="258" cy="174" r="1.8" />
      <circle cx="320" cy="85" r="1.8" />
      <circle cx="320" cy="159" r="1.8" />
      <circle cx="320" cy="233" r="1.8" />
    </g>
    <g aria-hidden="true">
      <circle cx="38" cy="277" r="2" fill="#92d2ff" />
      <text class="legend-label" x="46" y="280">双向信令</text>
      <circle cx="153" cy="277" r="2" fill="#91ecdb" />
      <text class="legend-label" x="161" y="280">媒体直传</text>
      <path d="M266 277h10" class="connection data" />
      <text class="legend-label" x="282" y="280">数据读写</text>
    </g>
  </svg>
</template>

<script setup lang="ts">
import uvpMark from "@/assets/logo/uvp-mark.png";
</script>

<style scoped>
.architecture {
  position: relative;
  display: block;
  flex: none;
  width: 100%;
  overflow: visible;
}

.column-name {
  font-size: 9px;
  fill: #94b7db;
}

.node-box {
  fill: #122e51;
  stroke: #6ab8ff66;
  stroke-width: 1;
}

.service-box {
  fill: #163858;
  stroke: #6fddcc66;
  stroke-width: 1;
}

.hub-box {
  fill: url("#login-arch-hub-fill");
  stroke: #82c9ff;
  stroke-width: 1.2;
}

.node-title {
  font-size: 10px;
  fill: #e5f2ff;
}

.node-subtitle {
  font-size: 8px;
  fill: #a6c4e1;
}

.icon {
  fill: none;
  stroke: #9cceff;
  stroke-width: 1.6;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.service-icon {
  stroke: #8ae4d6;
}

.connection {
  fill: none;
  stroke: #5d9dd365;
  stroke-width: 1.3;
}

.connection.media {
  opacity: 0.75;
  stroke: #6fe1cf;
  stroke-width: 1.5;
}

.connection.data {
  opacity: 0.65;
  stroke: #7895b2;
  stroke-width: 1;
  stroke-dasharray: 3 4;
}

.particle {
  filter: drop-shadow(0 0 3px #78c5ff);
  fill: #e6f6ff;
}

.particle.media {
  filter: drop-shadow(0 0 3px #70e4cf);
  fill: #c1fff3;
}

.particle.data {
  opacity: 0.5;
  filter: none;
  fill: #a9bfd4;
}

.media-label {
  font-size: 8px;
  fill: #91ecdb;
}

.legend-label {
  font-size: 8px;
  fill: #9bb9d5;
}

@media (prefers-reduced-motion: reduce) {
  .particle {
    display: none;
  }
}
</style>
