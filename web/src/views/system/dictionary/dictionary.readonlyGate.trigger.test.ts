import { Popconfirm, Link } from "@arco-design/web-vue";
import { mount } from "@vue/test-utils";
import { afterEach, describe, expect, it } from "vitest";
import { defineComponent, h, nextTick } from "vue";

/**
 * 真渲染用例（不是桩测）：验证「置灰的删除链接」与「确认气泡」的关系。
 *
 * ⛔ 为什么必须真跑一遍：`a-popconfirm` **自己不声明 `disabled`**，它靠 Vue 的 attrs 透传
 *    落到根节点 `<Trigger disabled>` 上（Trigger 声明了该 prop）。这类"按我们以为的 prop
 *    建模"的假设，桩测一定测不出来（本仓踩过同类坑）。这里用真 Arco 断言两件事：
 *      ① disabled 时点触发器**不弹**（否则会出现"弹了也没用"的气泡）；
 *      ② 非 disabled 时会弹（证明判据本身有效，不是恒不弹导致假绿）。
 */
const Harness = defineComponent({
  props: { disabled: { type: Boolean, default: false } },
  setup(props) {
    return () =>
      h(
        Popconfirm,
        { disabled: props.disabled, content: "确定删除该字典吗?" },
        {
          default: () =>
            h(Link, { class: "uvp-table-action uvp-table-action--delete", disabled: props.disabled }, { default: () => "删除" })
        }
      );
  }
});

async function clickTrigger(disabled: boolean) {
  const wrapper = mount(Harness, { props: { disabled } });
  await wrapper.find(".arco-link").trigger("click");
  await nextTick();
  await new Promise(resolve => setTimeout(resolve, 0));
  const popped = document.body.querySelector(".arco-popconfirm") !== null;
  wrapper.unmount();
  return popped;
}

describe("只读链接 + 确认气泡", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("disabled：点触发器不弹确认气泡", async () => {
    expect(await clickTrigger(true)).toBe(false);
  });

  it("非 disabled：点触发器会弹（反证判据不是恒 false 的假绿）", async () => {
    expect(await clickTrigger(false)).toBe(true);
  });
});
