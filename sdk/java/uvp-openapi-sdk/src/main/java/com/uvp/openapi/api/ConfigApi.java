// GENERATED FILE - DO NOT EDIT.
// Source: api/openapi-v1.yaml via sdk/java/codegen/generate_apis.py
package com.uvp.openapi.api;

import com.uvp.openapi.internal.ApiInvoker;
import com.uvp.openapi.internal.Call;
import com.uvp.openapi.model.*;

/**
 * Device configuration, video parameters and firmware upgrade tasks.
 *
 * <p>Signing, retries and error mapping happen inside {@link ApiInvoker}; no method here takes a
 * signature argument.</p>
 */
public final class ConfigApi {

    private final ApiInvoker invoker;

    public ConfigApi(ApiInvoker invoker) {
        this.invoker = invoker;
    }

    /**
     * Query supported device configuration blocks
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/device-configs/refresh}</p>
     *
     * <p>Required scope: {@code device:config:refresh}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public DeviceConfigOperationResponse deviceConfigRefresh(String deviceId, String channelId, DeviceConfigRefreshRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/device-configs/refresh")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(DeviceConfigOperationResponse.class));
    }

    /**
     * Read the latest stored device configuration snapshot
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/device-configs}</p>
     *
     * <p>Required scope: {@code device:config:read}</p>
     */
    public DeviceConfigSnapshotResponse deviceConfigRead(String deviceId, String channelId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/device-configs")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .as(DeviceConfigSnapshotResponse.class));
    }

    /**
     * Apply validated configuration blocks to a device
     *
     * <p>{@code PUT /openapi/v1/devices/{deviceId}/channels/{channelId}/device-configs}</p>
     *
     * <p>Required scope: {@code device:config:apply}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public DeviceConfigOperationResponse deviceConfigApply(String deviceId, String channelId, DeviceConfigApplyRequest body) {
        return invoker.execute(Call.put("/openapi/v1/devices/{deviceId}/channels/{channelId}/device-configs")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(DeviceConfigOperationResponse.class));
    }

    /**
     * Query GB/T 28181-2022 video stream parameters
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/video-params/refresh}</p>
     *
     * <p>Required scope: {@code channel:video-params:refresh}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public DeviceConfigOperationResponse videoParamsRefresh(String deviceId, String channelId, EmptyObject body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/video-params/refresh")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(DeviceConfigOperationResponse.class));
    }

    /**
     * Read the latest stored GB/T 28181-2022 video parameters
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/video-params}</p>
     *
     * <p>Required scope: {@code channel:video-params:read}</p>
     */
    public VideoParamsSnapshotResponse videoParamsRead(String deviceId, String channelId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/video-params")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .as(VideoParamsSnapshotResponse.class));
    }

    /**
     * Apply GB/T 28181-2022 video stream parameters
     *
     * <p>{@code PUT /openapi/v1/devices/{deviceId}/channels/{channelId}/video-params}</p>
     *
     * <p>Required scope: {@code channel:video-params:apply}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public DeviceConfigOperationResponse videoParamsApply(String deviceId, String channelId, VideoParamsApplyRequest body) {
        return invoker.execute(Call.put("/openapi/v1/devices/{deviceId}/channels/{channelId}/video-params")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(DeviceConfigOperationResponse.class));
    }

    /**
     * Read a configuration operation owned by the authenticated client
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/config-operations/{operationId}}</p>
     *
     * <p>Required scope: {@code device:config:operation:read}</p>
     */
    public DeviceConfigOperationResponse deviceConfigOperationRead(String deviceId, String channelId, String operationId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/config-operations/{operationId}")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .path("operationId", operationId)
            .as(DeviceConfigOperationResponse.class));
    }

    /**
     * 设备固件升级历史
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/firmware-upgrades}</p>
     *
     * <p>Required scope: {@code device:firmware-upgrade:list}</p>
     */
    public OpenAPIFirmwareUpgradeHistoryResponse deviceFirmwareUpgradeList(String deviceId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/firmware-upgrades")
            .path("deviceId", deviceId)
            .as(OpenAPIFirmwareUpgradeHistoryResponse.class));
    }

    /**
     * 设备固件升级
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/firmware-upgrades}</p>
     *
     * <p>Required scope: {@code device:firmware-upgrade:execute}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public OpenAPIDeviceFirmwareUpgradeResponse deviceFirmwareUpgradeCreate(String deviceId, DeviceFirmwareUpgradeCreateRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/firmware-upgrades")
            .path("deviceId", deviceId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(OpenAPIDeviceFirmwareUpgradeResponse.class));
    }

    /**
     * 设备固件升级结果
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/firmware-upgrades/{operationId}}</p>
     *
     * <p>Required scope: {@code device:firmware-upgrade:read}</p>
     */
    public OpenAPIDeviceFirmwareUpgradeResponse deviceFirmwareUpgradeRead(String deviceId, String operationId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/firmware-upgrades/{operationId}")
            .path("deviceId", deviceId)
            .path("operationId", operationId)
            .as(OpenAPIDeviceFirmwareUpgradeResponse.class));
    }
}
