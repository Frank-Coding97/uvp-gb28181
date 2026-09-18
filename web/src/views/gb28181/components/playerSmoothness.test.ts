import { afterEach, describe, expect, it, vi } from "vitest";
import { PlayerSmoothness, type SmoothnessSnapshot } from "./playerSmoothness";

type Listener = (...args: unknown[]) => void;

function createEmitter() {
    const listeners: Record<string, Listener[]> = {};
    const offEvents: string[] = [];
    return {
        listeners,
        offEvents,
        on(event: string, callback: Listener) {
            (listeners[event] ??= []).push(callback);
        },
        off(event: string, callback: Listener) {
            offEvents.push(event);
            listeners[event] = (listeners[event] ?? []).filter((fn) => fn !== callback);
        },
        emit(event: string, ...args: unknown[]) {
            (listeners[event] ?? []).forEach((fn) => fn(...args));
        }
    };
}

type FakeEmitter = ReturnType<typeof createEmitter>;

function createCore(): FakeEmitter & { playing: boolean; getAllStatsData: () => Record<string, unknown> } {
    const core = createEmitter() as FakeEmitter & { playing: boolean; getAllStatsData: () => Record<string, unknown> };
    core.playing = true;
    core.getAllStatsData = () => ({});
    return core;
}

/**
 * 还原真实结构:`new EasyPlayerPro(...)` 返回顶层对象,统计事件由内部实例派发
 * (顶层 `_bindEvents()` 只转发 pt 白名单,`stats` / `videoSmooth` 不在其中)。
 */
function createPlayer() {
    const core = createCore();
    const top = createEmitter() as FakeEmitter & { player: unknown };
    top.player = core;
    return { top, core };
}

function createHost(): HTMLElement {
    const container = document.createElement("div");
    container.className = "easyplayer-container";
    container.innerHTML = [
        '<div class="easyplayer-controls">',
        '<div class="easyplayer-controls-bottom">',
        '<div class="easyplayer-controls-right">',
        '<div class="easyplayer-controls-item easyplayer-speed"></div>',
        "</div>",
        "</div>",
        "</div>"
    ].join("");
    return container;
}

/** 推进到下一拍:真实场景里 `stats` 是每秒一次,这里也按秒推进以避开同拍去重。 */
async function tick(ms = 1000) {
    await vi.advanceTimersByTimeAsync(ms);
}

async function setup() {
    vi.useFakeTimers();
    const container = createHost();
    document.body.append(container);
    const { top, core } = createPlayer();
    const snapshots: SmoothnessSnapshot[] = [];
    const smoothness = new PlayerSmoothness(container);
    smoothness.bind(top, (snapshot) => snapshots.push(snapshot));
    await vi.advanceTimersByTimeAsync(50);
    return { container, top, core, smoothness, snapshots };
}

function badgeOf(container: HTMLElement): HTMLElement {
    const badge = container.querySelector(".uvp-smooth-badge");
    if (!badge) throw new Error("流畅度徽标未挂载");
    return badge as HTMLElement;
}

function labelOf(container: HTMLElement): string {
    return badgeOf(container).querySelector(".uvp-smooth-text")?.textContent ?? "";
}

describe("PlayerSmoothness 控制栏徽标", () => {
    afterEach(() => {
        vi.useRealTimers();
        document.body.innerHTML = "";
    });

    it("把徽标插在原生速率读数左侧,不新增自定义按钮", async () => {
        const { container, smoothness } = await setup();

        const right = container.querySelector(".easyplayer-controls-right") as HTMLElement;
        expect(right.firstElementChild).toBe(badgeOf(container));
        // 原生"速率"元素仍在,且紧跟徽标之后,顺序为 [徽标][速率]
        expect(right.children[1]?.className).toContain("easyplayer-speed");
        expect(labelOf(container)).toBe("检测中");

        smoothness.dispose();
    });

    it("订阅播放器内部实例的统计事件(顶层不转发 stats/videoSmooth)", async () => {
        const { top, core, smoothness } = await setup();

        // 回归点:曾经订阅顶层实例,导致 stats 永远收不到、徽标一直「检测中」
        expect(core.listeners.stats).toHaveLength(1);
        expect(core.listeners.videoSmooth).toHaveLength(1);
        expect(top.listeners.stats).toBeUndefined();
        expect(top.listeners.videoSmooth).toBeUndefined();

        smoothness.dispose();
    });

    it("按播放器档位给出流畅/一般/卡顿提示", async () => {
        const { container, core, smoothness } = await setup();

        core.emit("stats", { performance: 3, videoSmooth: true, fps: 25 });
        await tick();
        expect(labelOf(container)).toBe("流畅");

        core.emit("stats", { performance: 2, videoSmooth: true, fps: 18 });
        await tick();
        expect(labelOf(container)).toBe("一般");

        // 变差立即生效,无需等待确认
        core.emit("stats", { performance: 1, videoSmooth: true, fps: 9 });
        await tick();
        expect(labelOf(container)).toBe("卡顿");

        smoothness.dispose();
    });

    it("暂停后不吃上一拍的残留档位,回落为检测中", async () => {
        const { container, core, smoothness } = await setup();

        core.emit("stats", { performance: 3, videoSmooth: true, fps: 25 });
        await tick();
        expect(labelOf(container)).toBe("流畅");

        // 暂停后 _allStatsData 里的 performance 仍是上一拍的残留值
        core.playing = false;
        core.emit("stats", { performance: 3, videoSmooth: true, fps: 25 });
        await tick();
        expect(labelOf(container)).toBe("检测中");

        core.playing = true;
        core.emit("stats", { performance: 3, videoSmooth: true, fps: 25 });
        await tick();
        expect(labelOf(container)).toBe("流畅");

        smoothness.dispose();
    });

    it("起播宽限期内不把零帧报成严重,宽限过后才降档", async () => {
        const { container, core, smoothness } = await setup();

        core.emit("stats", { performance: 0, videoSmooth: true, fps: 0 });
        await tick();
        expect(labelOf(container)).toBe("检测中");

        for (let i = 0; i < 3; i += 1) {
            core.emit("stats", { performance: 0, videoSmooth: true, fps: 0 });
            await tick();
        }
        expect(labelOf(container)).toBe("严重");

        smoothness.dispose();
    });

    it("videoSmooth 为 false 时优先判卡顿并带上归因", async () => {
        const { container, core, smoothness } = await setup();

        core.emit("stats", { performance: 3, videoSmooth: true, fps: 25 });
        await tick();
        expect(labelOf(container)).toBe("流畅");

        core.emit("videoSmooth", false, "isDroppingIsTrue");
        core.emit("stats", { performance: 3, videoSmooth: false, fps: 25 });
        await tick();

        expect(labelOf(container)).toBe("卡顿");
        expect(badgeOf(container).title).toContain("播放器正在丢帧");

        smoothness.dispose();
    });

    it("变好需要连续两拍确认,避免提示来回闪", async () => {
        const { container, core, smoothness } = await setup();

        core.emit("stats", { performance: 2, videoSmooth: true, fps: 18 });
        await tick();
        expect(labelOf(container)).toBe("一般");

        core.emit("stats", { performance: 3, videoSmooth: true, fps: 25 });
        await tick();
        expect(labelOf(container)).toBe("一般");

        core.emit("stats", { performance: 3, videoSmooth: true, fps: 25 });
        await tick();
        expect(labelOf(container)).toBe("流畅");

        smoothness.dispose();
    });

    it("内部实例被重建后自动重新订阅", async () => {
        const { container, top, core, smoothness } = await setup();

        core.emit("stats", { performance: 3, videoSmooth: true, fps: 25 });
        await tick();
        expect(labelOf(container)).toBe("流畅");

        // 播放器内部重建 core(切换流等场景),旧实例上的订阅失效
        const nextCore = createCore();
        top.player = nextCore;
        await tick(1200);

        expect(nextCore.listeners.stats).toHaveLength(1);
        nextCore.emit("stats", { performance: 1, videoSmooth: true, fps: 9 });
        await tick();
        expect(labelOf(container)).toBe("卡顿");

        smoothness.dispose();
    });

    it("事件静默时用轮询兜底读档位,且不被清零后的帧数覆盖", async () => {
        const { container, core, smoothness, snapshots } = await setup();

        core.emit("stats", { performance: 3, videoSmooth: true, fps: 25 });
        await tick();
        expect(snapshots.at(-1)?.fps).toBe(25);

        // 事件断供,getAllStatsData() 里的 fps 已被派发逻辑清零
        core.getAllStatsData = () => ({ performance: 2, videoSmooth: true, fps: 0 });
        await tick(4000);

        expect(labelOf(container)).toBe("一般");
        expect(snapshots.at(-1)?.fps).toBe(25);

        smoothness.dispose();
    });

    it("长时间拿不到判定时收起徽标,恢复判定后重新显示", async () => {
        const { container, core, smoothness } = await setup();

        for (let i = 0; i < 12; i += 1) {
            core.emit("stats", { fps: 0 });
            await tick();
        }
        expect(badgeOf(container).classList.contains("is-hidden")).toBe(true);

        core.emit("stats", { performance: 3, videoSmooth: true, fps: 25 });
        await tick();
        expect(badgeOf(container).classList.contains("is-hidden")).toBe(false);
        expect(labelOf(container)).toBe("流畅");

        smoothness.dispose();
    });

    it("把每秒统计回调给宿主,并在释放时移除徽标、解绑事件", async () => {
        const { container, core, smoothness, snapshots } = await setup();

        core.emit("stats", { performance: 2, videoSmooth: true, fps: 17.5 });
        await tick();
        expect(snapshots.at(-1)).toMatchObject({ state: "fair", label: "一般", fps: 17.5 });

        smoothness.dispose();
        expect(container.querySelector(".uvp-smooth-badge")).toBeNull();
        expect(core.offEvents).toEqual(["stats", "videoSmooth"]);
    });
});
