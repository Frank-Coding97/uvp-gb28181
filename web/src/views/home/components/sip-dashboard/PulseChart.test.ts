import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import PulseChart from "./PulseChart.vue";

describe("SIP pulse chart", () => {
  it("gives the real 60-minute signal pulse enough stable visual height", () => {
    const wrapper = mount(PulseChart, {
      props: {
        samples: [
          { t: 1, msgPerSec: 2, failPct: 0 },
          { t: 2, msgPerSec: 4, failPct: 10 }
        ],
        abnormalWindows: []
      }
    });

    expect(wrapper.find(".pulse__svg").attributes("viewBox")).toBe("0 0 600 90");
  });
});
