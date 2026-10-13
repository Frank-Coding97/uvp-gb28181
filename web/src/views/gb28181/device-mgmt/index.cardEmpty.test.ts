import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { mount } from "@vue/test-utils";
import { Empty } from "@arco-design/web-vue";
import { compile, defineComponent } from "vue";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/index.vue"), "utf8");
const cardStart = source.indexOf('class="view-body card-view"');
const cardTemplate = source.slice(cardStart, source.indexOf('class="view-body map-view"', cardStart));

function renderEmpty(assetKind: string, devices: unknown[], channels: unknown[], rowsLoading = false) {
  const markup = cardTemplate.match(/<a-empty\b[^>]*class="card-empty"[^>]*\/>/)?.[0];
  expect(markup, "卡片模式应包含空状态分支").toBeTruthy();
  return mount(
    defineComponent({
      components: { AEmpty: Empty },
      setup: () => ({ assetKind, devices, channels, rowsLoading }),
      render: compile(markup!)
    })
  );
}

describe("channel workspace card empty state", () => {
  it("空数据时显示通道提示与图标", () => {
    const wrapper = renderEmpty("channel", [], []);
    expect(wrapper.text()).toBe("暂无符合条件的通道");
    expect(wrapper.find(".arco-empty-image svg").exists()).toBe(true);
    wrapper.unmount();
  });
  it("有数据或正在加载时不显示空提示", () => {
    const populated = renderEmpty("channel", [], [{ id: 1 }]);
    expect(populated.findComponent(Empty).exists()).toBe(false);
    populated.unmount();
    const loading = renderEmpty("channel", [], [], true);
    expect(loading.findComponent(Empty).exists()).toBe(false);
    loading.unmount();
  });
});
