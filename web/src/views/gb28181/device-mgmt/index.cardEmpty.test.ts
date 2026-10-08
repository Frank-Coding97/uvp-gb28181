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

describe("device management card empty state", () => {
  it.each([
    ["device", [], [{ id: 1 }], "暂无符合条件的设备"],
    ["channel", [{ id: 1 }], [], "暂无符合条件的通道"]
  ])("%s 空数据时显示提示与图标，忽略另一种资产的数据", (kind, devices, channels, description) => {
    const wrapper = renderEmpty(kind, devices, channels);
    expect(wrapper.text()).toBe(description);
    expect(wrapper.find(".arco-empty-image svg").exists()).toBe(true);
    wrapper.unmount();
  });

  it.each(["device", "channel"])("%s 有数据或正在加载时不显示空提示", kind => {
    const populated = renderEmpty(kind, [{ id: 1 }], [{ id: 1 }]);
    expect(populated.findComponent(Empty).exists()).toBe(false);
    populated.unmount();
    const loading = renderEmpty(kind, [], [], true);
    expect(loading.findComponent(Empty).exists()).toBe(false);
    loading.unmount();
  });
});
