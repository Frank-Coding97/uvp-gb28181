import { config } from "@vue/test-utils";
import { defineComponent, h } from "vue";

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
  "a-slider": ArcoSliderStub,
  "a-checkbox": ArcoCheckboxStub,
  "a-tag": { template: "<span><slot /></span>" }
};
