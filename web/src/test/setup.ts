import { config } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach } from "vitest";
import { defineComponent, h, ref } from "vue";

const ArcoInputStub = defineComponent({
  inheritAttrs: false,
  props: { modelValue: { type: [String, Number], default: "" }, disabled: Boolean, placeholder: String },
  emits: ["update:modelValue", "input", "change", "pressEnter"],
  setup(props, { attrs, emit }) {
    return () =>
      h("input", {
        ...attrs,
        modelvalue: props.modelValue,
        value: props.modelValue,
        disabled: props.disabled,
        placeholder: props.placeholder,
        onInput: (event: Event) => {
          const value = (event.target as HTMLInputElement).value;
          emit("update:modelValue", value);
          emit("input", value, event);
        },
        onChange: (event: Event) => emit("change", (event.target as HTMLInputElement).value, event),
        onKeydown: (event: KeyboardEvent) => {
          if (event.key === "Enter") emit("pressEnter", event);
        }
      });
  }
});

const ArcoInputNumberStub = defineComponent({
  inheritAttrs: false,
  props: {
    modelValue: { type: [Number, String], default: undefined },
    min: Number,
    max: Number,
    step: Number,
    disabled: Boolean
  },
  emits: ["update:modelValue", "input", "change"],
  setup(props, { attrs, emit }) {
    return () =>
      h("input", {
        ...attrs,
        modelvalue: props.modelValue,
        type: "number",
        value: props.modelValue ?? "",
        min: props.min,
        max: props.max,
        step: props.step,
        disabled: props.disabled,
        onInput: (event: Event) => {
          const raw = (event.target as HTMLInputElement).value;
          const value = raw === "" ? undefined : Number(raw);
          emit("update:modelValue", value);
          emit("input", value, raw, event);
        },
        onChange: (event: Event) => {
          const raw = (event.target as HTMLInputElement).value;
          const value = raw === "" ? undefined : Number(raw);
          emit("update:modelValue", value);
          emit("change", value, event);
        }
      });
  }
});

const ArcoSelectStub = defineComponent({
  inheritAttrs: false,
  props: { modelValue: { type: [String, Number], default: "" }, disabled: Boolean },
  emits: ["update:modelValue", "change"],
  setup(props, { attrs, emit, slots }) {
    return () =>
      h(
        "select",
        {
          ...attrs,
          modelvalue: props.modelValue,
          "model-value": props.modelValue,
          value: props.modelValue ?? "",
          disabled: props.disabled,
          onChange: (event: Event) => {
            const target = event.target as HTMLSelectElement;
            const option = target.selectedOptions[0] as HTMLOptionElement & { _value?: unknown };
            const value = option?._value ?? target.value;
            emit("update:modelValue", value);
            emit("change", value, event);
          }
        },
        slots.default?.()
      );
  }
});

const ArcoOptionStub = defineComponent({
  props: { value: { type: [String, Number, Boolean, Object], default: undefined }, disabled: Boolean },
  setup(props, { attrs, slots }) {
    return () => h("option", { ...attrs, value: props.value, disabled: props.disabled }, slots.default?.());
  }
});

/**
 * a-range-picker 的 v-model 是 [开始, 结束] 二元组（配 value-format 时是字符串）。
 * 桩必须保持这个形状，否则「二元组 ↔ 两个表单字段」的桥接在单测里测不出来。
 */
const ArcoRangePickerStub = defineComponent({
  inheritAttrs: false,
  props: { modelValue: { type: Array, default: () => [] }, disabled: Boolean },
  emits: ["update:modelValue", "change"],
  setup(props, { attrs, emit }) {
    const setPart = (index: 0 | 1, value: string) => {
      const next = [...((props.modelValue ?? []) as string[])];
      next[index] = value;
      emit("update:modelValue", next);
      emit("change", next);
    };
    const input = (index: 0 | 1, className: string) =>
      h("input", {
        class: className,
        value: (props.modelValue as string[] | undefined)?.[index] ?? "",
        disabled: props.disabled,
        onInput: (event: Event) => setPart(index, (event.target as HTMLInputElement).value)
      });
    return () => h("span", attrs, [input(0, "range-start"), input(1, "range-end")]);
  }
});

const ArcoSliderStub = defineComponent({
  inheritAttrs: false,
  props: { modelValue: { type: Number, default: 0 }, min: Number, max: Number, step: Number, disabled: Boolean },
  emits: ["update:modelValue", "change"],
  setup(props, { attrs, emit }) {
    return () =>
      h("input", {
        ...attrs,
        type: "range",
        value: props.modelValue,
        min: props.min,
        max: props.max,
        step: props.step,
        disabled: props.disabled,
        onInput: (event: Event) => emit("update:modelValue", Number((event.target as HTMLInputElement).value))
      });
  }
});

const ArcoCheckboxStub = defineComponent({
  inheritAttrs: false,
  props: { modelValue: Boolean, disabled: Boolean },
  emits: ["update:modelValue", "change"],
  setup(props, { attrs, emit, slots }) {
    return () =>
      h("label", { ...attrs }, [
        h("input", {
          type: "checkbox",
          checked: props.modelValue,
          disabled: props.disabled,
          onChange: (event: Event) => emit("update:modelValue", (event.target as HTMLInputElement).checked)
        }),
        ...((slots.default?.() ?? []) as ReturnType<typeof h>[])
      ]);
  }
});

config.global.stubs = {
  "a-button": { template: "<button><slot name='icon' /><slot /></button>" },
  "a-input": ArcoInputStub,
  "a-input-password": { template: "<input type='password' />" },
  "a-input-number": ArcoInputNumberStub,
  "a-modal": { template: "<div><slot name='title' /><slot /></div>" },
  "a-select": ArcoSelectStub,
  "a-option": ArcoOptionStub,
  "a-range-picker": ArcoRangePickerStub,
  "a-slider": ArcoSliderStub,
  "a-checkbox": ArcoCheckboxStub,
  "a-tag": { template: "<span><slot /></span>" }
};

/**
 * ⛔ 补齐**构建期自动导入**的 API。
 *
 * `src/store/modules/system.ts` 里写了 `const dict = ref<any>([])`，但那个 `ref` 是
 * unplugin-auto-import（vite 插件）在**构建期**注入的；vitest 不带该插件
 * ⇒ 只要有组件消费字典（`useSystemStore()`），就会抛
 * `ReferenceError: ref is not defined`（栈指向 `system.ts:12`，极易误判成组件的问题）。
 *
 * ⚠️ 在测试文件里 `import { ref } from "vue"` **不管用** —— 模块作用域不会污染全局，
 *    必须挂到 `globalThis` 上（`vi.stubGlobal` 同理，这里一次性挂好）。
 */
Object.assign(globalThis, { ref });

/**
 * 组件消费字典时需要**活跃 pinia**（`system` store）。每个用例给一个新实例。
 *
 * ⛔ 用 `beforeEach` 而不是只往 `config.global.plugins` 里塞：后者是**同一个实例**，
 *    前一个用例写进去的 dict 会漏到下一个用例（典型案例：`useDictOptions` 的字典改名断言）。
 * ⛔ 具体文件里若已显式 `vi.stubGlobal("ref", ref)` / `setActivePinia(...)`（声明"我依赖字典"），
 *    保留即可，与本兜底不冲突。
 */
beforeEach(() => {
  setActivePinia(createPinia());
});
