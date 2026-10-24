// GENERATED FILE - DO NOT EDIT.
// Source: api/openapi-v1.yaml via sdk/java/codegen/generate_apis.py
package com.uvp.openapi.api;

import com.uvp.openapi.internal.ApiInvoker;
import com.uvp.openapi.internal.Call;
import com.uvp.openapi.model.*;

/**
 * Live preview. Returns playable URLs; the media plane is not handled by this SDK.
 *
 * <p>Signing, retries and error mapping happen inside {@link ApiInvoker}; no method here takes a
 * signature argument.</p>
 */
public final class LiveApi {

    private final ApiInvoker invoker;

    public LiveApi(ApiInvoker invoker) {
        this.invoker = invoker;
    }

    /**
     * Start or reuse live playback for one owned channel
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/live}</p>
     *
     * <p>Required scope: {@code play:live}</p>
     */
    public LivePlaybackResponse startLivePlayback(String deviceId, String channelId) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/live")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .slow()
            .as(LivePlaybackResponse.class));
    }

    /**
     * Stop the live stream this client opened for one channel
     *
     * <p>{@code DELETE /openapi/v1/devices/{deviceId}/channels/{channelId}/live}</p>
     *
     * <p>Required scope: {@code play:live:stop}</p>
     */
    public LiveStopPlaybackResponse stopLivePlayback(String deviceId, String channelId) {
        return invoker.execute(Call.delete("/openapi/v1/devices/{deviceId}/channels/{channelId}/live")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .slow()
            .as(LiveStopPlaybackResponse.class));
    }
}
