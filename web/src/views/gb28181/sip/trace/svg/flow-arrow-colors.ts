export const FLOW_ARROW_COLORS = {
    request: "#6b7280",
    success: "#059669",
    redirect: "#d97706",
    failure: "#d14343"
} as const;

export type FlowArrowDirection = "request" | "response";

export function resolveArrowColor(direction: FlowArrowDirection, statusCode?: number): string {
    if (direction === "request" || statusCode === undefined || statusCode === null) {
        return FLOW_ARROW_COLORS.request;
    }
    if (statusCode >= 200 && statusCode < 300) return FLOW_ARROW_COLORS.success;
    if (statusCode >= 300 && statusCode < 400) return FLOW_ARROW_COLORS.redirect;
    if (statusCode >= 400 && statusCode < 700) return FLOW_ARROW_COLORS.failure;
    return FLOW_ARROW_COLORS.request;
}
