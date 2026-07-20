import type { InjectionKey, Ref } from "vue";

export interface SvgHoverCoord {
    x: number;
    y: number;
}

export const SVG_CANVAS_HOVER_KEY: InjectionKey<Ref<SvgHoverCoord | null>> = Symbol("SvgCanvasHover");
