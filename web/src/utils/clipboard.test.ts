import { afterEach, describe, expect, it, vi } from "vitest";
import { writeTextToClipboard } from "./app";

describe("writeTextToClipboard", () => {
    const originalClipboard = Object.getOwnPropertyDescriptor(navigator, "clipboard");
    const originalExecCommand = Object.getOwnPropertyDescriptor(document, "execCommand");

    afterEach(() => {
        if (originalClipboard) Object.defineProperty(navigator, "clipboard", originalClipboard);
        else Reflect.deleteProperty(navigator, "clipboard");
        if (originalExecCommand) Object.defineProperty(document, "execCommand", originalExecCommand);
        else Reflect.deleteProperty(document, "execCommand");
        document.body.replaceChildren();
        vi.restoreAllMocks();
    });

    it("uses the Clipboard API when available", async () => {
        const writeText = vi.fn().mockResolvedValue(undefined);
        Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });

        await expect(writeTextToClipboard("sip.example.test")).resolves.toBe(true);
        expect(writeText).toHaveBeenCalledWith("sip.example.test");
    });

    it("falls back to textarea copy when the Clipboard API is unavailable", async () => {
        Object.defineProperty(navigator, "clipboard", { configurable: true, value: undefined });
        const execCommand = vi.fn().mockReturnValue(true);
        Object.defineProperty(document, "execCommand", { configurable: true, value: execCommand });

        await expect(writeTextToClipboard("34020000002000000001")).resolves.toBe(true);
        expect(execCommand).toHaveBeenCalledWith("copy");
        expect(document.body.querySelector("textarea")).toBeNull();
    });

    it("falls back to textarea copy when the Clipboard API rejects", async () => {
        const writeText = vi.fn().mockRejectedValue(new DOMException("Not allowed", "NotAllowedError"));
        Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
        const execCommand = vi.fn().mockReturnValue(true);
        Object.defineProperty(document, "execCommand", { configurable: true, value: execCommand });

        await expect(writeTextToClipboard("sip.example.test:5060")).resolves.toBe(true);
        expect(execCommand).toHaveBeenCalledWith("copy");
    });

    it("returns false when both copy paths fail", async () => {
        const writeText = vi.fn().mockRejectedValue(new DOMException("Not allowed", "NotAllowedError"));
        Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
        Object.defineProperty(document, "execCommand", { configurable: true, value: vi.fn().mockReturnValue(false) });

        await expect(writeTextToClipboard("secret")).resolves.toBe(false);
    });
});
