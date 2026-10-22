// GENERATED FILE - DO NOT EDIT.
// Source: api/openapi-v1.yaml via sdk/java/codegen/generate_apis.py
package com.uvp.openapi.api;

import com.uvp.openapi.internal.ApiInvoker;
import com.uvp.openapi.internal.Call;
import com.uvp.openapi.model.*;

/**
 * Devices, channels, live status and storage cards.
 *
 * <p>Signing, retries and error mapping happen inside {@link ApiInvoker}; no method here takes a
 * signature argument.</p>
 */
public final class DeviceApi {

    private final ApiInvoker invoker;

    public DeviceApi(ApiInvoker invoker) {
        this.invoker = invoker;
    }

    /**
     * List devices owned by the authenticated machine client
     *
     * <p>{@code GET /openapi/v1/devices}</p>
     *
     * <p>Required scope: {@code device:list}</p>
     */
    public DeviceListResponse listDevices(Integer page, Integer pageSize, String keyword, String status) {
        return invoker.execute(Call.get("/openapi/v1/devices")
            .query("page", page)
            .query("pageSize", pageSize)
            .query("keyword", keyword)
            .query("status", status)
            .as(DeviceListResponse.class));
    }

    /** Same as {@link #listDevices} with optional arguments left unset. */
    public DeviceListResponse listDevices() {
        return listDevices(null, null, null, null);
    }

    /**
     * Get one device owned by the authenticated machine client
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}}</p>
     *
     * <p>Required scope: {@code device:detail}</p>
     */
    public DeviceResponse getDevice(String deviceId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}")
            .path("deviceId", deviceId)
            .as(DeviceResponse.class));
    }

    /**
     * Get the status of one owned device
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/status}</p>
     *
     * <p>Required scope: {@code device:status}</p>
     */
    public DeviceStatusResponse getDeviceStatus(String deviceId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/status")
            .path("deviceId", deviceId)
            .as(DeviceStatusResponse.class));
    }

    /**
     * List channels of one owned device
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels}</p>
     *
     * <p>Required scope: {@code channel:list}</p>
     */
    public ChannelListResponse listChannels(String deviceId, Integer page, Integer pageSize, String keyword, String status) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels")
            .path("deviceId", deviceId)
            .query("page", page)
            .query("pageSize", pageSize)
            .query("keyword", keyword)
            .query("status", status)
            .as(ChannelListResponse.class));
    }

    /** Same as {@link #listChannels} with optional arguments left unset. */
    public ChannelListResponse listChannels(String deviceId) {
        return listChannels(deviceId, null, null, null, null);
    }

    /**
     * Get one channel of one owned device
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}}</p>
     *
     * <p>Required scope: {@code channel:detail}</p>
     */
    public ChannelResponse getChannel(String deviceId, String channelId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .as(ChannelResponse.class));
    }

    /**
     * Get the status of one owned channel
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/status}</p>
     *
     * <p>Required scope: {@code channel:status}</p>
     */
    public ChannelStatusResponse getChannelStatus(String deviceId, String channelId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/status")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .as(ChannelStatusResponse.class));
    }

    /**
     * Get explicitly reported control capabilities for one owned channel
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/control-capabilities}</p>
     *
     * <p>Required scope: {@code channel:capabilities}</p>
     */
    public ControlCapabilitiesResponse getChannelControlCapabilities(String deviceId, String channelId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/control-capabilities")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .as(ControlCapabilitiesResponse.class));
    }

    /**
     * Read the latest target-track intent and reported capability
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/target-track}</p>
     *
     * <p>Required scope: {@code channel:target-track:read}</p>
     */
    public TargetTrackSnapshotResponse getTargetTrackIntent(String deviceId, String channelId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/target-track")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .as(TargetTrackSnapshotResponse.class));
    }

    /**
     * 手动刷新设备目录
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/catalog/refresh}</p>
     *
     * <p>Required scope: {@code device:catalog:refresh}</p>
     */
    public DeviceCatalogRefreshResponse refreshDeviceCatalog(String deviceId) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/catalog/refresh")
            .path("deviceId", deviceId)
            .slow()
            .as(DeviceCatalogRefreshResponse.class));
    }

    /**
     * 读取目录刷新进度
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/catalog/refresh/{operationId}}</p>
     *
     * <p>Required scope: {@code device:catalog:refresh:read}</p>
     */
    public DeviceCatalogRefreshProgressResponse getDeviceCatalogRefreshProgress(String deviceId, String operationId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/catalog/refresh/{operationId}")
            .path("deviceId", deviceId)
            .path("operationId", operationId)
            .as(DeviceCatalogRefreshProgressResponse.class));
    }

    /**
     * 通道目录挂载位置
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/mounts}</p>
     *
     * <p>Required scope: {@code channel:mounts:read}</p>
     */
    public OpenAPIChannelMountListResponse channelMountList(String deviceId, String channelId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/mounts")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .as(OpenAPIChannelMountListResponse.class));
    }

    /**
     * 通道 24 小时在线时序
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/timeline}</p>
     *
     * <p>Required scope: {@code channel:timeline:read}</p>
     */
    public OpenAPIChannelTimelineResponse channelTimeline(String deviceId, String channelId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/timeline")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .as(OpenAPIChannelTimelineResponse.class));
    }

    /**
     * 设备抓图
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/snapshot-sessions}</p>
     *
     * <p>Required scope: {@code channel:snapshot:create}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public CreateChannelSnapshotSession200Response createChannelSnapshotSession(String deviceId, String channelId, CreateChannelSnapshotSessionRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/snapshot-sessions")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(CreateChannelSnapshotSession200Response.class));
    }

    /**
     * 读取抓图会话
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/snapshot-sessions/{sessionId}}</p>
     *
     * <p>Required scope: {@code channel:snapshot:read}</p>
     */
    public Object getChannelSnapshotSession(String deviceId, String channelId, String sessionId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/snapshot-sessions/{sessionId}")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .path("sessionId", sessionId)
            .as(Object.class));
    }

    /**
     * 读取抓图图像内容
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/snapshot-sessions/{sessionId}/images/{fileName}/content}</p>
     *
     * <p>Required scope: {@code channel:snapshot:image:read}</p>
     */
    public GetChannelSnapshotImageContent200Response getChannelSnapshotImageContent(String deviceId, String channelId, String sessionId, String fileName) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/snapshot-sessions/{sessionId}/images/{fileName}/content")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .path("sessionId", sessionId)
            .path("fileName", fileName)
            .as(GetChannelSnapshotImageContent200Response.class));
    }

    /**
     * 通道抓拍图列表
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/snapshot-library}</p>
     *
     * <p>Required scope: {@code channel:snapshot:library:read}</p>
     */
    public ListChannelSnapshotLibrary200Response listChannelSnapshotLibrary(String deviceId, String channelId, Integer page, Integer pageSize, String from, String to, String source) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/snapshot-library")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .query("page", page)
            .query("pageSize", pageSize)
            .query("from", from)
            .query("to", to)
            .query("source", source)
            .as(ListChannelSnapshotLibrary200Response.class));
    }

    /** Same as {@link #listChannelSnapshotLibrary} with optional arguments left unset. */
    public ListChannelSnapshotLibrary200Response listChannelSnapshotLibrary(String deviceId, String channelId) {
        return listChannelSnapshotLibrary(deviceId, channelId, null, null, null, null, null);
    }

    /**
     * 批量删除抓拍图
     *
     * <p>{@code DELETE /openapi/v1/devices/{deviceId}/channels/{channelId}/snapshot-library}</p>
     *
     * <p>Required scope: {@code channel:snapshot:library:delete}</p>
     */
    public DeleteChannelSnapshotLibrary200Response deleteChannelSnapshotLibrary(String deviceId, String channelId, DeleteChannelSnapshotLibraryRequest body) {
        return invoker.execute(Call.delete("/openapi/v1/devices/{deviceId}/channels/{channelId}/snapshot-library")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.OPTIONAL)
            .slow()
            .as(DeleteChannelSnapshotLibrary200Response.class));
    }

    /**
     * 读取抓拍图内容
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/snapshot-library/{snapshotId}/content}</p>
     *
     * <p>Required scope: {@code channel:snapshot:library:image:read}</p>
     */
    public Object getChannelSnapshotLibraryImageContent(String deviceId, String channelId, String snapshotId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/snapshot-library/{snapshotId}/content")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .path("snapshotId", snapshotId)
            .as(Object.class));
    }
}
