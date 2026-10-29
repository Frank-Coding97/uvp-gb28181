import type { InjectionKey } from "vue";

/** Shared host state for secondary playback-console surfaces. */
export const PLAY_CONSOLE_CONTEXT: InjectionKey<Record<string, unknown>> = Symbol("play-console-context");
