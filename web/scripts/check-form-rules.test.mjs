import assert from "node:assert/strict";
import test from "node:test";
import { findFormRuleViolations } from "./check-form-rules.mjs";

test("requires allow-clear on text inputs and recognizes bound attributes", () => {
  const template = [
    "<a-input />",
    "<a-textarea allow-clear />",
    '<a-input-password :allow-clear="canClear" />',
    "<a-select />"
  ].join("\n");

  assert.deepEqual(findFormRuleViolations(template), [{ line: 1, tag: "a-input", rule: "allow-clear" }]);
});

test("rejects min and max clamping on a-input-number", () => {
  const template = ['<a-input-number min="1" />', '<a-input-number v-bind:max="limit" />', "<a-input-number />"].join("\n");

  assert.deepEqual(findFormRuleViolations(template), [
    { line: 1, tag: "a-input-number", rule: "min/max" },
    { line: 2, tag: "a-input-number", rule: "min/max" }
  ]);
});
