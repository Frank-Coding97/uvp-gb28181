// GENERATED FILE - DO NOT EDIT.
// Source: api/openapi-v1.yaml via sdk/java/codegen/generate_apis.py
package com.uvp.openapi.api;

import com.uvp.openapi.internal.ApiInvoker;
import com.uvp.openapi.internal.Call;
import com.uvp.openapi.model.*;

/**
 * Recording control, alarm guard, reboot, storage card format, task polling.
 *
 * <p>Signing, retries and error mapping happen inside {@link ApiInvoker}; no method here takes a
 * signature argument.</p>
 */
public final class ControlApi {

    private final ApiInvoker invoker;

    public ControlApi(ApiInvoker invoker) {
        this.invoker = invoker;
    }

    /**
     * Read cached storage card status for one owned channel
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/storage-cards}</p>
     *
     * <p>Required scope: {@code channel:storage-cards}</p>
     */
    public StorageCardSnapshotResponse getChannelStorageCards(String deviceId, String channelId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/storage-cards")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .as(StorageCardSnapshotResponse.class));
    }

    /**
     * 启动设备录像
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/recording/start}</p>
     *
     * <p>Required scope: {@code device:recording:start}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public OpenAPIChannelRecordingStartResponse channelRecordingStart(String deviceId, String channelId, EmptyObject body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/recording/start")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(OpenAPIChannelRecordingStartResponse.class));
    }

    /**
     * 停止设备录像
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/recording/stop}</p>
     *
     * <p>Required scope: {@code device:recording:stop}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public OpenAPIChannelRecordingStopResponse channelRecordingStop(String deviceId, String channelId, EmptyObject body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/recording/stop")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(OpenAPIChannelRecordingStopResponse.class));
    }

    /**
     * 设备重启
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/maintenance/reboot}</p>
     *
     * <p>Required scope: {@code device:maintenance:reboot}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public OpenAPIDeviceMaintenanceRebootResponse deviceMaintenanceReboot(String deviceId, DeviceMaintenanceRebootRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/maintenance/reboot")
            .path("deviceId", deviceId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(OpenAPIDeviceMaintenanceRebootResponse.class));
    }

    /**
     * 存储卡格式化
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/storage-cards/format}</p>
     *
     * <p>Required scope: {@code channel:storage-card:format}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public OpenAPIChannelStorageCardFormatResponse channelStorageCardFormat(String deviceId, String channelId, ChannelStorageCardFormatRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/storage-cards/format")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(OpenAPIChannelStorageCardFormatResponse.class));
    }

    /**
     * 设备维护操作记录
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/maintenance/operations}</p>
     *
     * <p>Required scope: {@code device:maintenance:operation:list}</p>
     */
    public OpenAPIDeviceMaintenanceOperationListResponse deviceMaintenanceOperationList(String deviceId, Integer page, Integer pageSize) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/maintenance/operations")
            .path("deviceId", deviceId)
            .query("page", page)
            .query("pageSize", pageSize)
            .as(OpenAPIDeviceMaintenanceOperationListResponse.class));
    }

    /** Same as {@link #deviceMaintenanceOperationList} with optional arguments left unset. */
    public OpenAPIDeviceMaintenanceOperationListResponse deviceMaintenanceOperationList(String deviceId) {
        return deviceMaintenanceOperationList(deviceId, null, null);
    }

    /**
     * 维护操作状态
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/maintenance/operations/{operationId}}</p>
     *
     * <p>Required scope: {@code device:maintenance:operation:read}</p>
     */
    public OpenAPIDeviceMaintenanceOperationReadResponse deviceMaintenanceOperationRead(String deviceId, String operationId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/maintenance/operations/{operationId}")
            .path("deviceId", deviceId)
            .path("operationId", operationId)
            .as(OpenAPIDeviceMaintenanceOperationReadResponse.class));
    }
}
