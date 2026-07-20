import { describe, it, expect, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createRouter, createMemoryHistory } from "vue-router";
import { defineComponent, h } from "vue";
import { useTraceFilters } from "./useTraceFilters";

const Probe = defineComponent({
    setup(_, { expose }) {
        const api = useTraceFilters();
        expose(api);
        return () => h("div", "probe");
    }
});

function makeRouter(initial: string) {
    const router = createRouter({
        history: createMemoryHistory(),
        routes: [
            { path: "/gb28181/sip-traces", component: Probe },
            { path: "/", component: { render: () => h("div") } }
        ]
    });
    router.push(initial);
    return router;
}

async function mountProbe(initial: string) {
    const router = makeRouter(initial);
    await router.isReady();
    const wrapper = mount(Probe, { global: { plugins: [router] } });
    await flushPromises();
    return { wrapper, router };
}

describe("useTraceFilters", () => {
    beforeEach(() => {
        // ensure fresh dayjs baseline; parseQuery uses now() when no from/to
    });

    it("initializes with defaults when no query", async () => {
        const { wrapper } = await mountProbe("/gb28181/sip-traces");
        const api = (wrapper.vm as any).$.exposed;
        expect(api.state.view).toBe("text");
        expect(api.state.deviceId).toBe("");
        expect(api.state.range[0]).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/);
        expect(api.state.range[1]).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/);
    });

    it("initializes from query params", async () => {
        const { wrapper } = await mountProbe("/gb28181/sip-traces?view=session&deviceId=D1");
        const api = (wrapper.vm as any).$.exposed;
        expect(api.state.view).toBe("session");
        expect(api.state.deviceId).toBe("D1");
    });

    it("mutating state pushes into router.query", async () => {
        const { wrapper, router } = await mountProbe("/gb28181/sip-traces");
        const api = (wrapper.vm as any).$.exposed;
        api.state.deviceId = "D9";
        await flushPromises();
        expect(router.currentRoute.value.query.deviceId).toBe("D9");
    });

    it("setView updates state and URL", async () => {
        const { wrapper, router } = await mountProbe("/gb28181/sip-traces");
        const api = (wrapper.vm as any).$.exposed;
        api.setView("matrix");
        await flushPromises();
        expect(api.state.view).toBe("matrix");
        expect(router.currentRoute.value.query.view).toBe("matrix");
    });

    it("reset clears free-text filters but keeps view/range", async () => {
        const { wrapper } = await mountProbe("/gb28181/sip-traces?view=session&deviceId=D1&method=INVITE&callId=abc");
        const api = (wrapper.vm as any).$.exposed;
        expect(api.state.deviceId).toBe("D1");
        api.reset();
        await flushPromises();
        expect(api.state.deviceId).toBe("");
        expect(api.state.method).toBe("");
        expect(api.state.callId).toBe("");
        expect(api.state.view).toBe("session");
    });
});
