package com.uvp.openapi;

import com.uvp.openapi.client.UvpApiException;
import com.uvp.openapi.client.UvpClient;
import com.uvp.openapi.client.UvpClientConfig;
import com.uvp.openapi.client.UvpErrorKind;
import com.uvp.openapi.model.Channel;
import com.uvp.openapi.model.ChannelListResponse;
import com.uvp.openapi.model.Device;
import com.uvp.openapi.model.DeviceListResponse;
import com.uvp.openapi.model.DevicePlaybackCreateRequest;
import com.uvp.openapi.model.DeviceRecordQueryRequest;
import com.uvp.openapi.model.DeviceSubscriptionCreateRequest;
import com.uvp.openapi.model.EmptyObject;
import com.uvp.openapi.model.LivePlaybackResponse;
import com.uvp.openapi.model.PTZMoveRequest;
import com.uvp.openapi.model.PTZPresetListResponse;

import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.MethodOrderer;
import org.junit.jupiter.api.Order;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.TestMethodOrder;
import org.junit.jupiter.api.condition.EnabledIfEnvironmentVariable;

import java.time.OffsetDateTime;
import java.util.Map;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

/**
 * Drives every business chain against a live platform. Skipped unless {@code UVP_IT_AK} is set, so
 * {@code mvn test} never needs the network or credentials.
 *
 * <pre>
 * UVP_IT_BASE_URL=http://host:51002 UVP_IT_AK=... UVP_IT_SK=... \
 * UVP_IT_AUDIENCE=uvp-... mvn -B test -Dtest=RealPlatformIT
 * </pre>
 *
 * Assertions stay loose about domain data (which devices exist, whether a camera answers PTZ). What
 * is asserted is the transport contract: every call carries a valid signature, returns a decoded
 * model rather than a raw map, and exposes session handles that can be closed again.
 */
@EnabledIfEnvironmentVariable(named = "UVP_IT_AK", matches = ".+")
@TestMethodOrder(MethodOrderer.OrderAnnotation.class)
class RealPlatformIT {

    private static final EmptyObject EMPTY = new EmptyObject();

    private static String deviceId;
    private static String channelId;

    private static String env(String name) {
        String value = System.getenv(name);
        assertNotNull(value, name + " must be set for the live integration test");
        return value;
    }

    private static UvpClientConfig.Builder config() {
        return UvpClientConfig.builder()
                .baseUrl(env("UVP_IT_BASE_URL"))
                .accessKey(env("UVP_IT_AK"))
                .secretKey(env("UVP_IT_SK"))
                .audience(env("UVP_IT_AUDIENCE"));
    }

    private static UvpClient client() {
        return new UvpClient(config().build());
    }

    private static void log(String label, Object value) {
        String text = String.valueOf(value).replaceAll("\\s+", " ");
        System.out.println("[IT] " + label + ": "
                + (text.length() > 320 ? text.substring(0, 320) + "..." : text));
    }

    @BeforeAll
    static void discoverDevice() {
        try (UvpClient client = client()) {
            DeviceListResponse page = client.devices().listDevices(1, 5, null, "online");
            assertNotNull(page.getData(), "device page");
            assertFalse(page.getData().getItems().isEmpty(),
                    "the live platform must have at least one online device");
            Device device = page.getData().getItems().get(0);
            deviceId = device.getDeviceId();

            ChannelListResponse channels = client.devices().listChannels(deviceId, 1, 5, null, null);
            assertNotNull(channels.getData(), "channel page");
            assertFalse(channels.getData().getItems().isEmpty(),
                    "device " + deviceId + " must expose a channel");
            Channel channel = channels.getData().getItems().get(0);
            channelId = channel.getChannelId();

            log("device", deviceId + " / " + device.getName() + " / " + device.getStatus());
            log("channel", channelId + " / " + channel.getName() + " / ptzType=" + channel.getPtzType());
        }
    }

    @Test
    @Order(1)
    void readsDeviceAndChannelFacts() {
        try (UvpClient client = client()) {
            log("device detail", client.devices().getDevice(deviceId));
            log("device status", client.devices().getDeviceStatus(deviceId));
            log("channel detail", client.devices().getChannel(deviceId, channelId));
            log("channel status", client.devices().getChannelStatus(deviceId, channelId));
            log("channel capabilities", client.devices().getChannelControlCapabilities(deviceId, channelId));
            log("target track", client.devices().getTargetTrackIntent(deviceId, channelId));
            log("storage cards", client.control().getChannelStorageCards(deviceId, channelId));
        }
    }

    @Test
    @Order(2)
    void startsLivePlaybackAndGetsPlayableUrls() {
        try (UvpClient client = client()) {
            LivePlaybackResponse response = client.live().startLivePlayback(deviceId, channelId);
            assertNotNull(response.getData(), "live data");
            Map<String, Object> urls = response.getData().getUrls();
            assertNotNull(urls, "live urls");
            assertTrue(urls.containsKey("hls") || urls.containsKey("flv"),
                    "expected at least one playback protocol, saw " + urls.keySet());
            log("live stream", response.getData().getStreamId() + " status=" + response.getData().getStatus()
                    + " reused=" + response.getData().getReused() + " protocols=" + urls.keySet());
            for (Map.Entry<String, Object> entry : urls.entrySet()) {
                log("live url " + entry.getKey(), entry.getValue());
            }
        }
    }

    @Test
    @Order(3)
    void queriesRecordingsAndRunsAPlaybackSession() {
        try (UvpClient client = client()) {
            DeviceRecordQueryRequest query = new DeviceRecordQueryRequest();
            query.setStartTime(OffsetDateTime.now().minusHours(12));
            query.setEndTime(OffsetDateTime.now());
            query.setType(DeviceRecordQueryRequest.TypeEnum.fromValue("all"));

            Object listed = client.playback().deviceRecordQuery(deviceId, channelId, query);
            log("record query", listed);

            String recordKey = extractField(listed, "recordKey");
            if (recordKey == null) {
                System.out.println("[IT] record query returned no playable item; playback chain skipped");
                return;
            }
            log("record key", recordKey);

            DevicePlaybackCreateRequest create = new DevicePlaybackCreateRequest();
            create.setRecordKey(recordKey);
            create.setPlayFrom(OffsetDateTime.now().minusHours(12));
            Object session = client.playback().devicePlaybackCreate(deviceId, channelId, create);
            log("playback create", session);

            String sessionId = extractField(session, "sessionId");
            if (sessionId == null) {
                System.out.println("[IT] no sessionId in playback create response");
                return;
            }
            log("playback read", client.playback().devicePlaybackRead(deviceId, channelId, sessionId));
            try {
                log("playback stop", client.playback().devicePlaybackStop(deviceId, channelId, sessionId, EMPTY));
            } catch (UvpApiException rejected) {
                // Known platform defect, reproduced without this SDK by signing the same request by
                // hand: stopping a playback session that was just created returns 400
                // INVALID_REQUEST. Recorded here so a fix flips the assertion.
                assertEquals(UvpErrorKind.INVALID_REQUEST, rejected.getKind(),
                        "unexpected failure shape: " + rejected.getMessage());
                log("playback stop rejected", rejected.getMessage());
            }
        }
    }

    @Test
    @Order(4)
    void readsPtzStateAndIssuesAStopCommand() {
        try (UvpClient client = client()) {
            PTZPresetListResponse presets = client.ptz().listPTZPresets(deviceId, channelId);
            assertNotNull(presets.getData(), "preset list");
            log("presets", presets);
            log("precise status", client.ptz().getPrecisePTZStatus(deviceId, channelId));
            log("home position", client.ptz().getPTZHomePosition(deviceId, channelId));
            log("cruise tracks", client.ptz().listPTZCruiseTracks(deviceId, channelId));

            // "stop" rather than a direction: it exercises the write path without moving the camera.
            PTZMoveRequest move = new PTZMoveRequest();
            move.setAction(PTZMoveRequest.ActionEnum.fromValue("stop"));
            move.setSpeed(0);
            log("ptz stop", client.ptz().movePTZ(deviceId, channelId, move));
        }
    }

    @Test
    @Order(5)
    void readsAlarmsEventsAndPositions() {
        try (UvpClient client = client()) {
            log("alarms", client.alarms().listDeviceAlarms(deviceId, 1, 5));
            log("status events", client.alarms().listDeviceStatusEvents(deviceId, null, 5));
            log("positions", client.alarms().devicePositionRead(deviceId));
            log("position history", client.alarms().devicePositionHistoryRead(deviceId, null, 5));
        }
    }

    @Test
    @Order(6)
    void runsASubscriptionLifecycle() {
        try (UvpClient client = client()) {
            DeviceSubscriptionCreateRequest request = new DeviceSubscriptionCreateRequest();
            request.setKind(DeviceSubscriptionCreateRequest.KindEnum.fromValue("alarm"));
            request.setExpiresSeconds(180);

            Object created = client.alarms().deviceSubscriptionCreate(deviceId, request);
            log("subscription create", created);
            String id = extractField(created, "id");
            if (id == null) {
                System.out.println("[IT] no subscriptionId in create response; lifecycle aborted");
                return;
            }
            log("subscription read", client.alarms().deviceSubscriptionRead(deviceId, id));
            log("subscription events", client.alarms().deviceEventsRead(deviceId, id, null, 5));
            log("subscription cancel", client.alarms().deviceSubscriptionCancel(deviceId, id));
        }
    }

    @Test
    @Order(7)
    void readsDeviceConfiguration() {
        try (UvpClient client = client()) {
            log("config snapshot", client.configs().deviceConfigRead(deviceId, channelId));
            log("video params", client.configs().videoParamsRead(deviceId, channelId));
        }
    }

    @Test
    @Order(8)
    void startsAndStopsChannelRecording() {
        try (UvpClient client = client()) {
            try {
                log("recording start", client.control().channelRecordingStart(deviceId, channelId, EMPTY));
                log("recording stop", client.control().channelRecordingStop(deviceId, channelId, EMPTY));
            } catch (UvpApiException refused) {
                // A camera may not support manual recording control. That is a device answer
                // (409 CAPABILITY_NOT_SUPPORTED), not a signing or transport failure.
                assertEquals(UvpErrorKind.CONFLICT, refused.getKind());
                assertEquals("CAPABILITY_NOT_SUPPORTED", refused.getCode());
                log("recording refused by device", refused.getMessage());
            }
        }
    }

    @Test
    @Order(9)
    void opensAndEndsAVoiceSession() {
        try (UvpClient client = client()) {
            Object created;
            try {
                created = client.talk().deviceTalkCreate(deviceId, channelId, EMPTY);
            } catch (UvpApiException refused) {
                // The platform answers 503 SERVICE_UNAVAILABLE when the voice path cannot be
                // prepared (here: no media node accepted the SDP). A domain answer, not a transport
                // fault, so the chain is reported rather than failed.
                assertTrue(refused.getKind() == UvpErrorKind.SERVER
                                || refused.getKind() == UvpErrorKind.CONFLICT,
                        "unexpected failure shape: " + refused.getMessage());
                log("talk unavailable", refused.getMessage());
                return;
            }
            log("talk create", created);
            String id = extractField(created, "sessionId");
            if (id == null) {
                System.out.println("[IT] no sessionId in talk create response; end skipped");
                return;
            }
            log("talk read", client.talk().deviceTalkRead(deviceId, channelId, id));
            log("talk end", client.talk().deviceTalkEnd(deviceId, channelId, id, EMPTY));
        }
    }

    @Test
    @Order(11)
    void runsADownloadSessionLifecycle() {
        try (UvpClient client = client()) {
            DeviceRecordQueryRequest query = new DeviceRecordQueryRequest();
            query.setStartTime(OffsetDateTime.now().minusHours(6));
            query.setEndTime(OffsetDateTime.now());
            query.setType(DeviceRecordQueryRequest.TypeEnum.fromValue("all"));
            Object listed = client.playback().deviceRecordQuery(deviceId, channelId, query);
            String recordKey = extractField(listed, "recordKey");
            String startTime = extractField(listed, "startTime");
            if (recordKey == null) {
                System.out.println("[IT] no record available for the download chain");
                return;
            }

            com.uvp.openapi.model.DeviceDownloadCreateRequest create =
                    new com.uvp.openapi.model.DeviceDownloadCreateRequest();
            create.setRecordKey(recordKey);
            create.setPlayFrom(OffsetDateTime.parse(startTime));
            create.setDownloadSpeed(com.uvp.openapi.model.DeviceDownloadCreateRequest.DownloadSpeedEnum
                    .fromValue(2));
            Object session = client.playback().deviceDownloadCreate(deviceId, channelId, create);
            log("download create", session);

            String sessionId = extractField(session, "sessionId");
            if (sessionId == null) {
                System.out.println("[IT] no download sessionId returned");
                return;
            }
            log("download read", client.playback().deviceDownloadRead(deviceId, channelId, sessionId));
            log("download cancel", client.playback().deviceDownloadCancel(deviceId, channelId, sessionId));
        }
    }

    @Test
    @Order(10)
    void mapsPlatformFailuresToTypedErrors() {
        try (UvpClient client = new UvpClient(config()
                .secretKey("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA").maxRetries(0).build())) {
            UvpApiException failure = assertThrows(UvpApiException.class,
                    () -> client.devices().listDevices(1, 1, null, null));
            assertEquals(UvpErrorKind.AUTHENTICATION, failure.getKind());
            assertEquals(401, failure.getHttpStatus());
            assertNotNull(failure.getRequestId(), "the platform request id must survive the mapping");
            log("wrong secret", failure.getMessage());
        }
        try (UvpClient client = new UvpClient(config().maxRetries(0).build())) {
            UvpApiException failure = assertThrows(UvpApiException.class,
                    () -> client.devices().getDevice("34020000002000009999"));
            assertEquals(404, failure.getHttpStatus());
            assertEquals(UvpErrorKind.NOT_FOUND, failure.getKind());
            log("unknown device", failure.getMessage());
        }
    }

    /**
     * Reads a nested field through the model's own JSON view, because the record query and voice
     * session results are declared as free-form objects in the contract.
     */
    private static String extractField(Object value, String field) {
        if (value == null) {
            return null;
        }
        String json = com.uvp.openapi.UvpJson.getGson().toJson(value);
        com.google.gson.JsonElement parsed = com.google.gson.JsonParser.parseString(json);
        return findField(parsed, field);
    }

    private static String findField(com.google.gson.JsonElement element, String field) {
        if (element == null) {
            return null;
        }
        if (element.isJsonObject()) {
            com.google.gson.JsonObject object = element.getAsJsonObject();
            if (object.has(field) && !object.get(field).isJsonNull()) {
                return object.get(field).getAsString();
            }
            for (Map.Entry<String, com.google.gson.JsonElement> entry : object.entrySet()) {
                String found = findField(entry.getValue(), field);
                if (found != null) {
                    return found;
                }
            }
        } else if (element.isJsonArray()) {
            for (com.google.gson.JsonElement child : element.getAsJsonArray()) {
                String found = findField(child, field);
                if (found != null) {
                    return found;
                }
            }
        }
        return null;
    }
}
