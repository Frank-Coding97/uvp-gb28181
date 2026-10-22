// GENERATED FILE - DO NOT EDIT.
// Source: api/openapi-v1.yaml via sdk/java/codegen/generate_apis.py
package com.uvp.openapi.api;

import com.uvp.openapi.internal.ApiInvoker;
import com.uvp.openapi.internal.Call;
import com.uvp.openapi.model.*;

/**
 * Alarms, status events, subscriptions and reported positions.
 *
 * <p>Signing, retries and error mapping happen inside {@link ApiInvoker}; no method here takes a
 * signature argument.</p>
 */
public final class AlarmApi {

    private final ApiInvoker invoker;

    public AlarmApi(ApiInvoker invoker) {
        this.invoker = invoker;
    }

    /**
     * List persisted alarms for one owned device
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/alarms}</p>
     *
     * <p>Required scope: {@code device:alarms}</p>
     */
    public DeviceAlarmPageResponse listDeviceAlarms(String deviceId, Integer page, Integer pageSize) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/alarms")
            .path("deviceId", deviceId)
            .query("page", page)
            .query("pageSize", pageSize)
            .as(DeviceAlarmPageResponse.class));
    }

    /** Same as {@link #listDeviceAlarms} with optional arguments left unset. */
    public DeviceAlarmPageResponse listDeviceAlarms(String deviceId) {
        return listDeviceAlarms(deviceId, null, null);
    }

    /**
     * Continue reading device status events with an opaque cursor
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/status-events}</p>
     *
     * <p>Required scope: {@code device:status-events}</p>
     */
    public DeviceStatusEventPageResponse listDeviceStatusEvents(String deviceId, String cursor, Integer pageSize) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/status-events")
            .path("deviceId", deviceId)
            .query("cursor", cursor)
            .query("pageSize", pageSize)
            .as(DeviceStatusEventPageResponse.class));
    }

    /** Same as {@link #listDeviceStatusEvents} with optional arguments left unset. */
    public DeviceStatusEventPageResponse listDeviceStatusEvents(String deviceId) {
        return listDeviceStatusEvents(deviceId, null, null);
    }

    /**
     * List the client-owned subscriptions of one device
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/subscriptions}</p>
     *
     * <p>Required scope: {@code device:subscription:list}</p>
     */
    public DeviceSubscriptionListResponse deviceSubscriptionList(String deviceId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/subscriptions")
            .path("deviceId", deviceId)
            .as(DeviceSubscriptionListResponse.class));
    }

    /**
     * Create a client-owned subscription to persisted device events
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/subscriptions}</p>
     *
     * <p>Required scope: {@code device:subscription:create}</p>
     */
    public DeviceSubscriptionResponse deviceSubscriptionCreate(String deviceId, DeviceSubscriptionCreateRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/subscriptions")
            .path("deviceId", deviceId)
            .body(body)
            .slow()
            .as(DeviceSubscriptionResponse.class));
    }

    /**
     * Read a subscription owned by the authenticated client
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/subscriptions/{subscriptionId}}</p>
     *
     * <p>Required scope: {@code device:subscription:read}</p>
     */
    public DeviceSubscriptionResponse deviceSubscriptionRead(String deviceId, String subscriptionId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/subscriptions/{subscriptionId}")
            .path("deviceId", deviceId)
            .path("subscriptionId", subscriptionId)
            .as(DeviceSubscriptionResponse.class));
    }

    /**
     * Cancel this client's logical event subscription
     *
     * <p>{@code DELETE /openapi/v1/devices/{deviceId}/subscriptions/{subscriptionId}}</p>
     *
     * <p>Required scope: {@code device:subscription:cancel}</p>
     */
    public DeviceSubscriptionResponse deviceSubscriptionCancel(String deviceId, String subscriptionId) {
        return invoker.execute(Call.delete("/openapi/v1/devices/{deviceId}/subscriptions/{subscriptionId}")
            .path("deviceId", deviceId)
            .path("subscriptionId", subscriptionId)
            .slow()
            .as(DeviceSubscriptionResponse.class));
    }

    /**
     * Renew an active device event subscription
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/subscriptions/{subscriptionId}/renew}</p>
     *
     * <p>Required scope: {@code device:subscription:renew}</p>
     */
    public DeviceSubscriptionResponse deviceSubscriptionRenew(String deviceId, String subscriptionId, EmptyObject body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/subscriptions/{subscriptionId}/renew")
            .path("deviceId", deviceId)
            .path("subscriptionId", subscriptionId)
            .body(body)
            .slow()
            .as(DeviceSubscriptionResponse.class));
    }

    /**
     * Continue reading persisted events with a signed opaque cursor
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/subscriptions/{subscriptionId}/events}</p>
     *
     * <p>Required scope: {@code device:events:read}</p>
     */
    public DeviceEventPageResponse deviceEventsRead(String deviceId, String subscriptionId, String cursor, Integer pageSize) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/subscriptions/{subscriptionId}/events")
            .path("deviceId", deviceId)
            .path("subscriptionId", subscriptionId)
            .query("cursor", cursor)
            .query("pageSize", pageSize)
            .as(DeviceEventPageResponse.class));
    }

    /** Same as {@link #deviceEventsRead} with optional arguments left unset. */
    public DeviceEventPageResponse deviceEventsRead(String deviceId, String subscriptionId) {
        return deviceEventsRead(deviceId, subscriptionId, null, null);
    }

    /**
     * Read the latest platform-collected positions for a device
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/positions}</p>
     *
     * <p>Required scope: {@code device:position:read}</p>
     */
    public DevicePositionPageResponse devicePositionRead(String deviceId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/positions")
            .path("deviceId", deviceId)
            .as(DevicePositionPageResponse.class));
    }

    /**
     * Continue reading persisted mobile position history
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/positions/history}</p>
     *
     * <p>Required scope: {@code device:position:history:read}</p>
     */
    public DeviceEventPageResponse devicePositionHistoryRead(String deviceId, String cursor, Integer pageSize) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/positions/history")
            .path("deviceId", deviceId)
            .query("cursor", cursor)
            .query("pageSize", pageSize)
            .as(DeviceEventPageResponse.class));
    }

    /** Same as {@link #devicePositionHistoryRead} with optional arguments left unset. */
    public DeviceEventPageResponse devicePositionHistoryRead(String deviceId) {
        return devicePositionHistoryRead(deviceId, null, null);
    }

    /**
     * 设备报警布防
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/alarms/guard}</p>
     *
     * <p>Required scope: {@code device:alarm:guard:set}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public OpenAPIChannelAlarmGuardSetResponse channelAlarmGuardSet(String deviceId, String channelId, EmptyObject body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/alarms/guard")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(OpenAPIChannelAlarmGuardSetResponse.class));
    }

    /**
     * 设备报警撤防
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/alarms/guard/reset}</p>
     *
     * <p>Required scope: {@code device:alarm:guard:reset}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public OpenAPIChannelAlarmGuardResetResponse channelAlarmGuardReset(String deviceId, String channelId, EmptyObject body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/alarms/guard/reset")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(OpenAPIChannelAlarmGuardResetResponse.class));
    }

    /**
     * 设备报警复位
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/alarms/reset}</p>
     *
     * <p>Required scope: {@code device:alarm:reset}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public OpenAPIChannelAlarmResetResponse channelAlarmReset(String deviceId, String channelId, ChannelAlarmResetRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/alarms/reset")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(OpenAPIChannelAlarmResetResponse.class));
    }
}
