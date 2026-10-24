// GENERATED FILE - DO NOT EDIT.
// Source: api/openapi-v1.yaml via sdk/java/codegen/generate_apis.py
package com.uvp.openapi.api;

import com.uvp.openapi.internal.ApiInvoker;
import com.uvp.openapi.internal.Call;
import com.uvp.openapi.model.*;

/**
 * PTZ direction control, presets, cruise tracks and precise positioning.
 *
 * <p>Signing, retries and error mapping happen inside {@link ApiInvoker}; no method here takes a
 * signature argument.</p>
 */
public final class PtzApi {

    private final ApiInvoker invoker;

    public PtzApi(ApiInvoker invoker) {
        this.invoker = invoker;
    }

    /**
     * List PTZ presets for one owned channel
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets}</p>
     *
     * <p>Required scope: {@code ptz:preset:list}</p>
     */
    public PTZPresetListResponse listPTZPresets(String deviceId, String channelId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .as(PTZPresetListResponse.class));
    }

    /**
     * Save a PTZ preset on one owned channel
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets}</p>
     *
     * <p>Required scope: {@code ptz:preset:save}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public PTZOperationResponse savePTZPreset(String deviceId, String channelId, PTZPresetRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(PTZOperationResponse.class));
    }

    /**
     * Call a PTZ preset on one owned channel
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets/{presetId}/call}</p>
     *
     * <p>Required scope: {@code ptz:preset:call}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public PTZOperationResponse callPTZPreset(String deviceId, String channelId, String presetId, EmptyJSONRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets/{presetId}/call")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .path("presetId", presetId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(PTZOperationResponse.class));
    }

    /**
     * Delete a PTZ preset on one owned channel
     *
     * <p>{@code DELETE /openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets/{presetId}}</p>
     *
     * <p>Required scope: {@code ptz:preset:delete}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public PTZOperationResponse deletePTZPreset(String deviceId, String channelId, String presetId, EmptyJSONRequest body) {
        return invoker.execute(Call.delete("/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets/{presetId}")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .path("presetId", presetId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(PTZOperationResponse.class));
    }

    /**
     * Get one PTZ operation for the same owned channel
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/operations/{operationId}}</p>
     *
     * <p>Required scope: {@code ptz:operation:read}</p>
     */
    public PTZOperationResponse getPTZOperation(String deviceId, String channelId, String operationId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/operations/{operationId}")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .path("operationId", operationId)
            .as(PTZOperationResponse.class));
    }

    /**
     * Read the latest cached precise PTZ status and observation time
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/precise-status}</p>
     *
     * <p>Required scope: {@code channel:precise-status}</p>
     */
    public PreciseStatusSnapshotResponse getPrecisePTZStatus(String deviceId, String channelId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/precise-status")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .as(PreciseStatusSnapshotResponse.class));
    }

    /**
     * Read cached PTZ cruise tracks and freshness
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/cruise/tracks}</p>
     *
     * <p>Required scope: {@code ptz:cruise:read}</p>
     */
    public CruiseTrackSnapshotResponse listPTZCruiseTracks(String deviceId, String channelId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/cruise/tracks")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .as(CruiseTrackSnapshotResponse.class));
    }

    /**
     * Create a PTZ cruise track with an ordered batch of device controls
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/cruise/tracks}</p>
     *
     * <p>Required scope: {@code ptz:cruise:track:create}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public CruiseTrackCreateResponse createPTZCruiseTrack(String deviceId, String channelId, CruiseTrackCreateRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/cruise/tracks")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(CruiseTrackCreateResponse.class));
    }

    /**
     * Read one cached PTZ cruise track and its freshness
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/cruise/tracks/{trackId}}</p>
     *
     * <p>Required scope: {@code ptz:cruise:track:read}</p>
     */
    public CruiseTrackReadResponse getPTZCruiseTrack(String deviceId, String channelId, Integer trackId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/cruise/tracks/{trackId}")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .path("trackId", trackId)
            .as(CruiseTrackReadResponse.class));
    }

    /**
     * Move a PTZ head or lens on one owned channel
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/move}</p>
     *
     * <p>Required scope: {@code ptz:move}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public PTZControlOperationResponse movePTZ(String deviceId, String channelId, PTZMoveRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/move")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(PTZControlOperationResponse.class));
    }

    /**
     * Send precise PTZ coordinates to a supported GB/T 28181-2022 device
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/precise}</p>
     *
     * <p>Required scope: {@code ptz:precise}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public PTZControlOperationResponse controlPrecisePTZ(String deviceId, String channelId, PTZPreciseRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/precise")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(PTZControlOperationResponse.class));
    }

    /**
     * Control supported scan or cruise-point functions on one channel
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/extended}</p>
     *
     * <p>Required scope: {@code ptz:extended}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public PTZControlOperationResponse controlExtendedPTZ(String deviceId, String channelId, PTZExtendedRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/extended")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(PTZControlOperationResponse.class));
    }

    /**
     * Turn a channel wiper on or off
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/wiper}</p>
     *
     * <p>Required scope: {@code ptz:wiper}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public PTZControlOperationResponse controlPTZWiper(String deviceId, String channelId, PTZWiperRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/wiper")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(PTZControlOperationResponse.class));
    }

    /**
     * Read cached home-position state and verification source
     *
     * <p>{@code GET /openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/home-position}</p>
     *
     * <p>Required scope: {@code ptz:home-position:read}</p>
     */
    public HomePositionSnapshotResponse getPTZHomePosition(String deviceId, String channelId) {
        return invoker.execute(Call.get("/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/home-position")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .as(HomePositionSnapshotResponse.class));
    }

    /**
     * Set or disable a channel home position
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/home-position}</p>
     *
     * <p>Required scope: {@code ptz:home-position:set}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public PTZControlOperationResponse setPTZHomePosition(String deviceId, String channelId, PTZHomePositionRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/home-position")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(PTZControlOperationResponse.class));
    }

    /**
     * Start, stop, or delete a cruise path on one owned channel
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/cruise/control}</p>
     *
     * <p>Required scope: {@code ptz:cruise:control}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public PTZControlOperationResponse controlPTZCruise(String deviceId, String channelId, PTZCruiseControlRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/cruise/control")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(PTZControlOperationResponse.class));
    }

    /**
     * Send a target-tracking command to a supporting GB/T 28181-2022 device
     *
     * <p>{@code POST /openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/target-track}</p>
     *
     * <p>Required scope: {@code ptz:target-track}</p>
     * <p>The platform requires an {@code Idempotency-Key} for this operation. One is
     * generated automatically; pin it with {@link com.uvp.openapi.client.UvpIdempotency}
     * when you retry after an unknown outcome.</p>
     */
    public PTZTargetTrackResponse setPTZTargetTrack(String deviceId, String channelId, PTZTargetTrackRequest body) {
        return invoker.execute(Call.post("/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/target-track")
            .path("deviceId", deviceId)
            .path("channelId", channelId)
            .body(body)
            .idempotency(Call.Idempotency.REQUIRED)
            .slow()
            .as(PTZTargetTrackResponse.class));
    }
}
