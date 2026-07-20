export interface TraceRecord {
    id: string;
    ts: number;
    direction: "inbound" | "outbound";
    method: string;
    statusCode?: number;
    from: string;
    to: string;
    callId: string;
    body?: string;
}

export interface TraceQueryParams {
    startTime?: number;
    endTime?: number;
    deviceId?: string;
    callId?: string;
    direction?: "inbound" | "outbound";
    method?: string;
    limit?: number;
    offset?: number;
}

export interface TraceQueryResponse {
    records: TraceRecord[];
    total: number;
}

/**
 * Mock fetch trace records for development.
 * TODO: replace with real API call in T-4.1
 */
export async function fetchTraceRecords(params: TraceQueryParams): Promise<TraceQueryResponse> {
    await new Promise((resolve) => setTimeout(resolve, 300));

    const mockRecords: TraceRecord[] = Array.from({ length: params.limit ?? 50 }, (_, i) => {
        const ts = Date.now() - i * 5000;
        const isInbound = i % 3 === 0;
        const method = ["REGISTER", "MESSAGE", "INVITE", "BYE", "ACK"][i % 5];
        return {
            id: `trace-${i}`,
            ts,
            direction: isInbound ? "inbound" : "outbound",
            method,
            statusCode: method === "INVITE" || method === "REGISTER" ? 200 : undefined,
            from: isInbound ? "34020000001320000001" : "platform",
            to: isInbound ? "platform" : "34020000001320000001",
            callId: `call-${Math.floor(i / 3)}`,
            body:
                method === "MESSAGE"
                    ? `<?xml version="1.0"?><Notify><CmdType>Keepalive</CmdType><SN>${i}</SN><DeviceID>34020000001320000001</DeviceID><Status>OK</Status></Notify>`
                    : method === "INVITE"
                      ? `v=0\r\ns=Play\r\nm=video 0 RTP/AVP 96\r\n`
                      : undefined
        };
    });

    return {
        records: mockRecords,
        total: 500
    };
}
