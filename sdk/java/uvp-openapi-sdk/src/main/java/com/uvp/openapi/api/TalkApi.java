// GENERATED FILE - DO NOT EDIT.
// Source: api/openapi-v1.yaml via sdk/java/codegen/generate_apis.py
package com.uvp.openapi.api;

import com.uvp.openapi.internal.ApiInvoker;
import com.uvp.openapi.internal.Call;
import com.uvp.openapi.model.*;

/**
 * Voice talk and broadcast. SDP is passed through; media stays outside the SDK.
 *
 * <p>Signing, retries and error mapping happen inside {@link ApiInvoker}; no method here takes a
 * signature argument.</p>
 */
public final class TalkApi {

    private final ApiInvoker invoker;

    public TalkApi(ApiInvoker invoker) {
        this.invoker = invoker;
    }

    /**
     * Create device talk session
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/talk-sessions}</p>
     *
     * <p>Required scope: {@code device:talk:create}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public VoiceSessionResponse deviceTalkCreate(String deviceId, String channelId, EmptyObject body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/talk-sessions")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(VoiceSessionResponse.class));
    }

    /**
     * Read device talk session
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/talk-sessions/{sessionId}}</p>
     *
     * <p>Required scope: {@code device:talk:read}</p>
     */
    public VoiceSessionResponse deviceTalkRead(String deviceId, String channelId, String sessionId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/talk-sessions/{sessionId}")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .path("sessionId", sessionId)
            .as(VoiceSessionResponse.class));
    }

    /**
     * End device talk session
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/talk-sessions/{sessionId}/end}</p>
     *
     * <p>Required scope: {@code device:talk:end}</p>
     */
    public VoiceSessionResponse deviceTalkEnd(String deviceId, String channelId, String sessionId, EmptyObject body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/talk-sessions/{sessionId}/end")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .path("sessionId", sessionId)
            .body(body)
            .slow()
            .as(VoiceSessionResponse.class));
    }

    /**
     * Create device broadcast session
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/broadcast-sessions}</p>
     *
     * <p>Required scope: {@code device:broadcast:create}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public VoiceSessionResponse deviceBroadcastCreate(String deviceId, String channelId, EmptyObject body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/broadcast-sessions")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(VoiceSessionResponse.class));
    }

    /**
     * Read device broadcast session
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/broadcast-sessions/{sessionId}}</p>
     *
     * <p>Required scope: {@code device:broadcast:read}</p>
     */
    public VoiceSessionResponse deviceBroadcastRead(String deviceId, String channelId, String sessionId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/broadcast-sessions/{sessionId}")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .path("sessionId", sessionId)
            .as(VoiceSessionResponse.class));
    }

    /**
     * End device broadcast session
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/broadcast-sessions/{sessionId}/end}</p>
     *
     * <p>Required scope: {@code device:broadcast:end}</p>
     */
    public VoiceSessionResponse deviceBroadcastEnd(String deviceId, String channelId, String sessionId, EmptyObject body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/broadcast-sessions/{sessionId}/end")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .path("sessionId", sessionId)
            .body(body)
            .slow()
            .as(VoiceSessionResponse.class));
    }

    /**
     * Submit device talk media offer
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/talk-sessions/{sessionId}/media}</p>
     *
     * <p>Required scope: {@code device:talk:media}</p>
     */
    public VoiceMediaAnswerResponse deviceTalkMedia(String deviceId, String channelId, String sessionId, DeviceTalkMediaRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/talk-sessions/{sessionId}/media")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .path("sessionId", sessionId)
            .body(body)
            .slow()
            .as(VoiceMediaAnswerResponse.class));
    }

    /**
     * Submit device broadcast media offer
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/broadcast-sessions/{sessionId}/media}</p>
     *
     * <p>Required scope: {@code device:broadcast:media}</p>
     */
    public VoiceMediaAnswerResponse deviceBroadcastMedia(String deviceId, String channelId, String sessionId, DeviceTalkMediaRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/broadcast-sessions/{sessionId}/media")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .path("sessionId", sessionId)
            .body(body)
            .slow()
            .as(VoiceMediaAnswerResponse.class));
    }
}
