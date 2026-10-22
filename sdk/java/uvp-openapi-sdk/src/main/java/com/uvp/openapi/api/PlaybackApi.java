// GENERATED FILE - DO NOT EDIT.
// Source: api/openapi-v1.yaml via sdk/java/codegen/generate_apis.py
package com.uvp.openapi.api;

import com.uvp.openapi.internal.ApiInvoker;
import com.uvp.openapi.internal.Call;
import com.uvp.openapi.model.*;

/**
 * Record search, playback sessions and download sessions.
 *
 * <p>Signing, retries and error mapping happen inside {@link ApiInvoker}; no method here takes a
 * signature argument.</p>
 */
public final class PlaybackApi {

    private final ApiInvoker invoker;

    public PlaybackApi(ApiInvoker invoker) {
        this.invoker = invoker;
    }

    /**
     * 读取录像检索参数
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/recordings/options}</p>
     *
     * <p>Required scope: {@code device:record-query:options}</p>
     */
    public OpenAPIDeviceRecordQueryOptionsResponse deviceRecordQueryOptions(String deviceId, String channelId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/recordings/options")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .as(OpenAPIDeviceRecordQueryOptionsResponse.class));
    }

    /**
     * 检索设备录像
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/recordings/search}</p>
     *
     * <p>Required scope: {@code device:record-query}</p>
     */
    public OpenAPIDeviceRecordQueryResponse deviceRecordQuery(String deviceId, String channelId, String cursor, Integer pageSize, DeviceRecordQueryRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/recordings/search")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .query("cursor", cursor)
            .query("pageSize", pageSize)
            .body(body)
            .idempotency(Call.Idempotency.OPTIONAL)
            .slow()
            .as(OpenAPIDeviceRecordQueryResponse.class));
    }

    /** Same as {@link #deviceRecordQuery} with optional arguments left unset. */
    public OpenAPIDeviceRecordQueryResponse deviceRecordQuery(String deviceId, String channelId, DeviceRecordQueryRequest body) {
        return deviceRecordQuery(deviceId, channelId, null, null, body);
    }

    /**
     * 创建设备录像回放
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/playback-sessions}</p>
     *
     * <p>Required scope: {@code device:playback-create}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public OpenAPIDevicePlaybackCreateResponse devicePlaybackCreate(String deviceId, String channelId, DevicePlaybackCreateRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/playback-sessions")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(OpenAPIDevicePlaybackCreateResponse.class));
    }

    /**
     * 读取设备回放会话
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/playback-sessions/{sessionId}}</p>
     *
     * <p>Required scope: {@code device:playback-read}</p>
     */
    public OpenAPIDevicePlaybackReadResponse devicePlaybackRead(String deviceId, String channelId, String sessionId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/playback-sessions/{sessionId}")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .path("sessionId", sessionId)
            .as(OpenAPIDevicePlaybackReadResponse.class));
    }

    /**
     * 控制设备录像回放
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/playback-sessions/{sessionId}/actions}</p>
     *
     * <p>Required scope: {@code device:playback-action}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public OpenAPIDevicePlaybackActionResponse devicePlaybackAction(String deviceId, String channelId, String sessionId, DevicePlaybackActionRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/playback-sessions/{sessionId}/actions")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .path("sessionId", sessionId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(OpenAPIDevicePlaybackActionResponse.class));
    }

    /**
     * 结束设备录像回放
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/playback-sessions/{sessionId}/stop}</p>
     *
     * <p>Required scope: {@code device:playback-stop}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public OpenAPIDevicePlaybackStopResponse devicePlaybackStop(String deviceId, String channelId, String sessionId, EmptyObject body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/playback-sessions/{sessionId}/stop")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .path("sessionId", sessionId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(OpenAPIDevicePlaybackStopResponse.class));
    }

    /**
     * 创建设备录像下载
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/download-sessions}</p>
     *
     * <p>Required scope: {@code device:download-create}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public DeviceDownloadCreate200Response deviceDownloadCreate(String deviceId, String channelId, DeviceDownloadCreateRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/download-sessions")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(DeviceDownloadCreate200Response.class));
    }

    /**
     * 读取设备录像下载
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/download-sessions/{sessionId}}</p>
     *
     * <p>Required scope: {@code device:download-read}</p>
     */
    public Object deviceDownloadRead(String deviceId, String channelId, String sessionId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/download-sessions/{sessionId}")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .path("sessionId", sessionId)
            .as(Object.class));
    }

    /**
     * 取消设备录像下载
     *
     * <p>{@code DELETE /openapi/v1/devices/{deviceId}/channels/{channelId}/download-sessions/{sessionId}}</p>
     *
     * <p>Required scope: {@code device:download-cancel}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public DeviceDownloadCancel200Response deviceDownloadCancel(String deviceId, String channelId, String sessionId) {
        return invoker.execute(Call.delete("/openapi/v1/devices/{deviceId}/channels/{channelId}/download-sessions/{sessionId}")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .path("sessionId", sessionId)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(DeviceDownloadCancel200Response.class));
    }
}
