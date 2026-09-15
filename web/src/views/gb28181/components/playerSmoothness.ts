/**
 * EasyPlayer 原生控制栏「流畅度」徽标。
 *
 * 只消费播放器自己每秒派发的内部统计,不新增任何数据源:
 *   - `stats`       每秒全量统计(含 performance / videoSmooth / fps)
 *   - `videoSmooth` (result, reason) 卡顿结论 + 原因码
 *
 * ⚠️ 关键:**这些事件派发自播放器的内部实例,不是 `new EasyPlayerPro(...)` 返回的
 * 那个顶层对象**。顶层 `_bindEvents()` 只按 `pt` 白名单转发十几个事件
 * (play/pause/error/kBps/timeUpdate…),`stats` / `performance` / `videoSmooth`
 * 都不在其中 —— 直接 `player.on("stats", cb)` 会永远收不到,徽标永远是「检测中」。
 * 内部实例挂在顶层实例的 `.player` 上(`_init(){ this.player = new Core(...) }`),
 * 所以这里统一通过 `resolveCore()` 取:优先 `.player`,回退顶层自身。
 * 另配 1 秒轮询兜底:内部实例若被重建则自动重新订阅,事件连续缺失时直接读
 * `getAllStatsData()`。
 *
 * 判定主据是 `performance` 档位(0-3),它是播放器内部按
 * 「本秒渲染帧数 / FLV 元数据标称帧率」算出来的(标称值取不到时按 25 兜底)。
 * `videoSmooth === false` 是播放器"确认卡顿"的结论,优先级高于档位。
 *
 * 刻意规避了四个会让提示变假的坑:
 *   ① 不用绝对码率判卡顿 —— MSE 预缓冲时接收速率会掉到近 0,画面却是流畅的;
 *   ② 不用单次采样定档 —— `stats.fps` 是瞬时值,抖动很大;
 *   ③ 不自己重算档位 —— 标称帧率只有播放器侧拿得到(GB28181 目录不下发 fps);
 *   ④ 只信 `playing === true` 时的档位 —— 暂停后 `_allStatsData` 里的
 *      `performance` 是上一拍的残留值,照读会一直"谎报流畅";
 *      且 `_allStatsData.fps` 在派发后立刻被清零,只有事件里那一瞬才是真值。
 */
export type SmoothnessState = "pending" | "smooth" | "fair" | "laggy" | "stalled";

export interface SmoothnessSnapshot {
    state: SmoothnessState;
    /** 徽标上的短文案,如"流畅"。 */
    label: string;
    /** 悬浮提示文案,含归因与实测帧率。 */
    detail: string;
    /** 播放器档位 0-3(3 最好),未完成判定时为 null。 */
    performance: number | null;
    /** 播放器卡顿结论,null 表示本拍没有结论。 */
    videoSmooth: boolean | null;
    /** 本秒渲染帧数。 */
    fps: number;
    /** 播放器原因码原文,便于排查。 */
    reason: string;
}

const BADGE_CLASS = "uvp-smooth-badge";
const TEXT_CLASS = "uvp-smooth-text";
const MOUNT_TARGETS = [".easyplayer-controls-right", ".easyplayer-controls-bottom", ".easyplayer-controls"];
const MOUNT_INTERVAL_MS = 60;
const MOUNT_RETRY_LIMIT = 40;
/** 变好需要连续确认的拍数,用来抑制"流畅↔一般"来回闪。 */
const UPGRADE_HOLD_TICKS = 2;
/** 起播宽限拍数,避免刚起播时瞬时 0 帧被报成"严重"。 */
const STARTUP_GRACE_TICKS = 3;
/** 连续多少拍拿不到判定就收起徽标(暂停、回放模式等)。 */
const UNKNOWN_HIDE_TICKS = 12;
/** 容器窄于此宽度时只留状态圆点,给控制栏让出空间。 */
const COMPACT_WIDTH_PX = 420;
/** 兜底轮询间隔(毫秒)。 */
const POLL_INTERVAL_MS = 1000;
/** 事件静默超过此时长才启用轮询兜底(毫秒),避免与每秒事件重复读数。 */
const POLL_FALLBACK_MS = 2500;
/** 同一拍的重复读数去重窗口(毫秒) —— `stats` 事件与兜底轮询可能撞在同一秒。 */
const INGEST_DEDUPE_MS = 600;

const STATE_LABEL: Record<SmoothnessState, string> = {
    pending: "检测中",
    smooth: "流畅",
    fair: "一般",
    laggy: "卡顿",
    stalled: "严重"
};

const STATE_BASE_DETAIL: Record<SmoothnessState, string> = {
    pending: "正在采集播放统计",
    smooth: "帧率与标称值相符",
    fair: "帧率略低于标称值",
    laggy: "帧率明显低于标称值",
    stalled: "本秒几乎无可渲染画面"
};

/** 播放器原因码 -> 中文归因。 */
const REASON_DETAIL: Record<string, string> = {
    vbpsIsZero: "未收到视频数据",
    isDroppingIsTrue: "播放器正在丢帧",
    fpsIsLow: "帧率低于近期均值",
    videoCurrentTimeDiffIsNotNormal: "播放进度异常"
};

const STATE_RANK: Record<Exclude<SmoothnessState, "pending">, number> = {
    stalled: 0,
    laggy: 1,
    fair: 2,
    smooth: 3
};

function resolveState(performance: number | null, videoSmooth: boolean | null, graceTicks: number): SmoothnessState {
    if (performance === null) return "pending";
    if (videoSmooth === false) return "laggy";
    if (performance <= 0) return graceTicks > 0 ? "pending" : "stalled";
    if (performance === 1) return "laggy";
    if (performance === 2) return "fair";
    return "smooth";
}

/**
 * 挂载到 EasyPlayer 容器上,把徽标插进原生控制栏并把统计事件翻译成状态。
 * 生命周期由宿主组件控制:`bind()` 绑定播放器,`dispose()` 解绑并移除徽标。
 */
export class PlayerSmoothness {
    private readonly container: HTMLElement;
    /** 顶层 EasyPlayerPro 实例(宿主传进来的那个)。 */
    private host: any = null;
    /** 内部实例:统计事件的真正来源。 */
    private core: any = null;
    private onChange: ((snapshot: SmoothnessSnapshot) => void) | null = null;
    private badge: HTMLElement | null = null;
    private observer: ResizeObserver | null = null;
    private mountTimer: ReturnType<typeof setTimeout> | null = null;
    private mountRetries = 0;
    private pollTimer: ReturnType<typeof setInterval> | null = null;
    /** 最近一次收到 `stats` 事件的时间,用于判断是否要启用轮询兜底。 */
    private lastStatsAt = 0;
    /** 最近一次消费统计的时间,用于同一拍去重。 */
    private lastIngestAt = 0;

    private state: SmoothnessState = "pending";
    private performance: number | null = null;
    private videoSmooth: boolean | null = null;
    private fps = 0;
    private reason = "";
    private upgradeTicks = 0;
    private graceTicks = STARTUP_GRACE_TICKS;
    private unknownTicks = 0;
    private compact = false;

    constructor(container: HTMLElement) {
        this.container = container;
    }

    /** 绑定播放器实例:订阅统计事件并把徽标挂进原生控制栏。 */
    bind(player: any, onChange?: (snapshot: SmoothnessSnapshot) => void): void {
        this.dispose();
        this.host = player ?? null;
        this.onChange = onChange ?? null;
        this.resetState();
        this.buildBadge();
        this.startMounting();
        this.observeWidth();
        this.attachCore();
        this.startPolling();
    }

    /** 当前状态快照,供父组件读取(卡片展示等)。 */
    snapshot(): SmoothnessSnapshot {
        return {
            state: this.state,
            label: STATE_LABEL[this.state],
            detail: this.buildDetail(),
            performance: this.performance,
            videoSmooth: this.videoSmooth,
            fps: this.fps,
            reason: this.reason
        };
    }

    /** 解绑事件、断掉观察器并移除徽标。 */
    dispose(): void {
        this.detachCore();
        this.stopPolling();
        this.host = null;
        this.onChange = null;
        this.stopMounting();
        this.observer?.disconnect();
        this.observer = null;
        this.badge?.remove();
        this.badge = null;
        this.resetState();
    }

    private resetState(): void {
        this.state = "pending";
        this.performance = null;
        this.videoSmooth = null;
        this.fps = 0;
        this.reason = "";
        this.upgradeTicks = 0;
        this.graceTicks = STARTUP_GRACE_TICKS;
        this.unknownTicks = 0;
        this.lastStatsAt = 0;
        this.lastIngestAt = 0;
    }

    /**
     * 解析统计事件的真正来源。
     *
     * 顶层 EasyPlayerPro 的 `_bindEvents()` 只转发 `pt` 白名单里的事件,
     * `stats` / `videoSmooth` 不在名单内,所以必须拿到内部实例;
     * 但仍保留顶层作为回退,以防后续版本改成顶层直接转发。
     */
    private resolveCore(): any {
        const host = this.host;
        if (!host) return null;
        const candidates = [host.player, host.easyplayer, host._player, host];
        for (const candidate of candidates) {
            if (candidate && typeof candidate.on === "function") return candidate;
        }
        return null;
    }

    /** 订阅内部实例的统计事件;内部实例被重建时会重新订阅。 */
    private attachCore(): void {
        const core = this.resolveCore();
        if (!core || core === this.core) return;
        this.detachCore();
        this.core = core;
        core.on("stats", this.handleStats);
        core.on("videoSmooth", this.handleVideoSmooth);
        // 订阅到即视为刚刚有数据,避免起播瞬间轮询抢跑
        this.lastStatsAt = Date.now();
    }

    private detachCore(): void {
        const core = this.core;
        if (!core) return;
        if (typeof core.off === "function") {
            try {
                core.off("stats", this.handleStats);
                core.off("videoSmooth", this.handleVideoSmooth);
            } catch (e) {
                console.warn("PlayerSmoothness unbind error", e);
            }
        }
        this.core = null;
    }

    /**
     * 兜底轮询:①内部实例被重建时重新订阅;②事件连续缺失时直接读
     * `getAllStatsData()`。1 秒一次,开销可忽略。
     */
    private startPolling(): void {
        this.stopPolling();
        this.pollTimer = setInterval(() => {
            this.attachCore();
            if (Date.now() - this.lastStatsAt < POLL_FALLBACK_MS) return;
            const core = this.core;
            const data = core && typeof core.getAllStatsData === "function" ? core.getAllStatsData() : null;
            if (data) this.ingest(data, false);
        }, POLL_INTERVAL_MS);
    }

    private stopPolling(): void {
        if (this.pollTimer !== null) {
            clearInterval(this.pollTimer);
            this.pollTimer = null;
        }
    }

    private buildBadge(): void {
        const badge = document.createElement("div");
        badge.className = `${BADGE_CLASS} is-pending`;
        const dot = document.createElement("span");
        dot.className = "uvp-smooth-dot";
        const text = document.createElement("span");
        text.className = TEXT_CLASS;
        text.textContent = STATE_LABEL.pending;
        badge.append(dot, text);
        badge.title = this.buildDetail();
        this.badge = badge;
    }

    /**
     * 控制栏 DOM 在播放器构造时创建,但可能因容器尚未挂载而晚到一拍,
     * 因此按固定间隔重试有限次。
     */
    private startMounting(): void {
        this.stopMounting();
        this.mountRetries = 0;
        const tick = () => {
            if (!this.badge) return;
            if (this.tryMount() || this.mountRetries >= MOUNT_RETRY_LIMIT) {
                this.mountTimer = null;
                return;
            }
            this.mountRetries += 1;
            this.mountTimer = setTimeout(tick, MOUNT_INTERVAL_MS);
        };
        this.mountTimer = setTimeout(tick, 0);
    }

    private stopMounting(): void {
        if (this.mountTimer !== null) {
            clearTimeout(this.mountTimer);
            this.mountTimer = null;
        }
    }

    private tryMount(): boolean {
        if (!this.badge) return false;
        if (this.badge.isConnected) return true;
        for (const selector of MOUNT_TARGETS) {
            const host = this.container.querySelector(selector);
            if (host) {
                // 插在右侧区最前面,紧贴原生"速率"读数左侧。
                host.prepend(this.badge);
                this.render();
                return true;
            }
        }
        return false;
    }

    private observeWidth(): void {
        if (typeof ResizeObserver === "undefined") return;
        this.observer = new ResizeObserver((entries) => {
            const width = entries[0]?.contentRect.width ?? 0;
            const compact = width > 0 && width < COMPACT_WIDTH_PX;
            if (compact !== this.compact) {
                this.compact = compact;
                this.render();
            }
        });
        this.observer.observe(this.container);
    }

    private readonly handleStats = (stats: Record<string, unknown> = {}): void => {
        this.lastStatsAt = Date.now();
        this.ingest(stats, true);
    };

    private readonly handleVideoSmooth = (result: unknown, reason: unknown): void => {
        if (typeof result === "boolean") this.videoSmooth = result;
        this.reason = typeof reason === "string" ? reason : "";
    };

    /**
     * 消费一拍统计。
     *
     * `fromEvent` 为 true 时数据取自 `stats` 事件(帧数尚未清零,是真值);
     * 为 false 时取自兜底轮询的 `getAllStatsData()` —— 那份数据里 `fps`
     * 已被派发逻辑清零,只能沿用上一拍的帧数。
     */
    private ingest(stats: Record<string, unknown>, fromEvent: boolean): void {
        const now = Date.now();
        // `stats` 事件与兜底轮询可能落在同一秒,去重后只推进一次
        if (now - this.lastIngestAt < INGEST_DEDUPE_MS) return;
        this.lastIngestAt = now;

        const playing = this.isPlaying();
        this.performance = playing && typeof stats.performance === "number" ? stats.performance : null;
        if (typeof stats.fps === "number" && (fromEvent || stats.fps > 0)) this.fps = stats.fps;
        if (typeof stats.videoSmooth === "boolean") this.videoSmooth = stats.videoSmooth;

        this.unknownTicks = this.performance === null ? this.unknownTicks + 1 : 0;
        this.graceTicks = Math.max(0, this.graceTicks - 1);

        this.applyState(resolveState(this.performance, this.videoSmooth, this.graceTicks));
        this.render();
        this.onChange?.(this.snapshot());
    }

    /**
     * 播放器是否处于播放态。
     *
     * 暂停/停止后 `_allStatsData` 里的 `performance` / `videoSmooth` 仍留着
     * 上一拍的残留值,只有 `playing` 能区分"本拍刚算出来"和"上次的旧值"。
     */
    private isPlaying(): boolean {
        const core = this.core;
        return !!core && core.playing === true;
    }

    /** 变差立即生效,变好需连续确认;不参与滞回的 pending 直接切换。 */
    private applyState(next: SmoothnessState): void {
        if (next === this.state) {
            this.upgradeTicks = 0;
            return;
        }
        if (this.state === "pending" || next === "pending") {
            this.state = next;
            this.upgradeTicks = 0;
            return;
        }
        if (STATE_RANK[next] < STATE_RANK[this.state as Exclude<SmoothnessState, "pending">]) {
            this.state = next;
            this.upgradeTicks = 0;
            return;
        }
        this.upgradeTicks += 1;
        if (this.upgradeTicks >= UPGRADE_HOLD_TICKS) {
            this.state = next;
            this.upgradeTicks = 0;
        }
    }

    private buildDetail(): string {
        const parts = [STATE_BASE_DETAIL[this.state]];
        const reasonText = this.reason ? REASON_DETAIL[this.reason] ?? this.reason : "";
        if (reasonText) parts.push(reasonText);
        parts.push(`${this.fps.toFixed(1)} fps`);
        if (typeof this.performance === "number") parts.push(`播放器档位 ${this.performance + 1}/4`);
        return `${STATE_LABEL[this.state]} · ${parts.join(" · ")}`;
    }

    private render(): void {
        const badge = this.badge;
        if (!badge) return;
        const hidden = this.unknownTicks >= UNKNOWN_HIDE_TICKS;
        const classes = [BADGE_CLASS, `is-${this.state}`];
        if (this.compact) classes.push("is-compact");
        if (hidden) classes.push("is-hidden");
        badge.className = classes.join(" ");
        badge.title = this.buildDetail();
        const text = badge.querySelector(`.${TEXT_CLASS}`);
        const label = STATE_LABEL[this.state];
        if (text && text.textContent !== label) text.textContent = label;
    }
}
