<template>
    <div class="unplayed-cover" aria-label="未播放画面">
        <div class="cover-center">
            <span class="protocol-chip">GB/T 28181-2022</span>
            <strong class="cover-brand">UVP</strong>
        </div>
        <span class="cover-hint">选择设备通道</span>
    </div>
</template>

<style scoped>
.unplayed-cover {
    position: relative;
    display: grid;
    width: 100%;
    min-height: 0;
    flex: 1;
    overflow: hidden;
    color: #FFFFFF;
    background: linear-gradient(135deg, #0B1E3F 0%, #0F2A57 52%, #1A4480 100%);
    place-items: center;
}
.cover-center { display: flex; align-items: center; flex-direction: column; gap: 10px; }
.protocol-chip {
    padding: 3px 10px;
    color: rgb(255 255 255 / 70%);
    background: rgb(255 255 255 / 8%);
    border: 1px solid rgb(255 255 255 / 18%);
    border-radius: 10px;
    font-family: var(--zlm-font-mono);
    font-size: 9px;
    letter-spacing: 1px;
}
.cover-brand {
    color: #FFFFFF;
    font-size: clamp(40px, 5vw, 60px);
    font-weight: 900;
    letter-spacing: 10px;
    line-height: 1;
    text-shadow: 0 0 18px rgb(124 196 255 / 20%);
}
.cover-hint {
    position: absolute;
    bottom: 10px;
    color: rgb(255 255 255 / 55%);
    font-size: 10px;
}
</style>
