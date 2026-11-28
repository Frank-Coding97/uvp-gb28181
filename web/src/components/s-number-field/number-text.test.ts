import { describe, expect, it } from "vitest";
import { parseNumberText, validateNumberText } from "./number-text";

const options = { required: true, integer: true, min: 1, max: 65535 };

describe("s-number-field number-text", () => {
  it("parses clean integer text and rejects partial junk", () => {
    expect(parseNumberText("5060", true)).toBe(5060);
    expect(parseNumberText(" 5060 ", true)).toBe(5060);
    expect(parseNumberText("", true)).toBeNull();
    expect(parseNumberText("50a", true)).toBeNull();
    expect(parseNumberText("5.5", true)).toBeNull();
    expect(parseNumberText("abc", true)).toBeNull();
  });

  it("supports decimal mode when integer is false", () => {
    expect(parseNumberText("1.5", false)).toBe(1.5);
    expect(parseNumberText("1.5", true)).toBeNull();
  });

  it("reports required error only for empty text", () => {
    expect(validateNumberText("", options)).toBe("该项为必填项");
    expect(validateNumberText("   ", options)).toBe("该项为必填项");
    expect(validateNumberText("", { ...options, required: false })).toBe("");
  });

  it("never clamps silently: out-of-range input reports instead of being fixed", () => {
    expect(validateNumberText("70000", options)).toBe("范围为 1 ~ 65535");
    expect(validateNumberText("0", options)).toBe("范围为 1 ~ 65535");
    expect(validateNumberText("65535", options)).toBe("");
  });

  it("reports non-integer input instead of truncating", () => {
    expect(validateNumberText("50.6", options)).toBe("请输入整数");
    expect(validateNumberText("abc", options)).toBe("请输入整数");
  });
});
