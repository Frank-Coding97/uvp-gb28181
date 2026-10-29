package com.uvp.demo;

import com.uvp.openapi.client.UvpClient;
import com.uvp.openapi.model.ChannelListResponse;
import com.uvp.openapi.model.DeviceListResponse;
import com.uvp.openapi.model.DeviceRecordQueryRequest;
import com.uvp.openapi.model.EmptyJSONRequest;
import com.uvp.openapi.model.LivePlaybackResponseData;
import com.uvp.openapi.model.OpenAPIDeviceRecordQueryResponse;
import com.uvp.openapi.model.PTZOperationResponse;
import java.time.OffsetDateTime;
import java.util.LinkedHashMap;
import java.util.Map;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

/**
 * The four things integrators ask for first: list what exists, open a live stream, move a camera,
 * and search recordings.
 *
 * <p>Notice what is absent: no URLs, no timestamps, no nonces, no signatures, no idempotency keys.
 * The client bean is the only thing this class knows about.</p>
 */
@RestController
@RequestMapping("/demo")
public class DemoController {

    private final UvpClient uvp;

    public DemoController(UvpClient uvp) {
        this.uvp = uvp;
    }

    @GetMapping("/devices")
    public DeviceListResponse devices(@RequestParam(defaultValue = "1") int page,
                                      @RequestParam(defaultValue = "20") int pageSize,
                                      @RequestParam(required = false) String keyword) {
        return uvp.devices().listDevices(page, pageSize, keyword, null);
    }

    @GetMapping("/devices/{deviceId}/channels")
    public ChannelListResponse channels(@PathVariable String deviceId) {
        return uvp.devices().listChannels(deviceId, 1, 50, null, null);
    }

    @GetMapping("/devices/{deviceId}/status")
    public Object status(@PathVariable String deviceId) {
        return uvp.devices().getDeviceStatus(deviceId);
    }

    /**
     * Starts (or reuses) a live stream and returns the playable URLs.
     *
     * <p>The media itself is not proxied by this SDK. Hand the URL to a player, or to ffmpeg in a
     * server-side pipeline.</p>
     */
    @PostMapping("/devices/{deviceId}/channels/{channelId}/live")
    public Map<String, Object> live(@PathVariable String deviceId, @PathVariable String channelId) {
        LivePlaybackResponseData data = uvp.live().startLivePlayback(deviceId, channelId).getData();
        Map<String, Object> result = new LinkedHashMap<>();
        result.put("streamId", data.getStreamId());
        result.put("status", data.getStatus());
        result.put("reused", data.getReused());
        result.put("urls", data.getUrls());
        return result;
    }

    /** Moves the camera to an existing preset (1-255). */
    @PostMapping("/devices/{deviceId}/channels/{channelId}/ptz/presets/{presetId}/call")
    public PTZOperationResponse callPreset(@PathVariable String deviceId,
                                           @PathVariable String channelId,
                                           @PathVariable String presetId) {
        return uvp.ptz().callPTZPreset(deviceId, channelId, presetId, new EmptyJSONRequest());
    }

    /**
     * Searches device-side recordings for a window.
     *
     * <p>Timestamps are passed as RFC 3339 strings: the platform compares them against what the
     * device reports, so an offset must be explicit.</p>
     */
    @GetMapping("/devices/{deviceId}/channels/{channelId}/records")
    public OpenAPIDeviceRecordQueryResponse records(@PathVariable String deviceId,
                                                    @PathVariable String channelId,
                                                    @RequestParam String startTime,
                                                    @RequestParam String endTime,
                                                    @RequestParam(required = false) String cursor,
                                                    @RequestParam(defaultValue = "50") int pageSize) {
        DeviceRecordQueryRequest body = new DeviceRecordQueryRequest()
                .startTime(OffsetDateTime.parse(startTime))
                .endTime(OffsetDateTime.parse(endTime));
        return uvp.playback().deviceRecordQuery(deviceId, channelId, cursor, pageSize, body);
    }
}
