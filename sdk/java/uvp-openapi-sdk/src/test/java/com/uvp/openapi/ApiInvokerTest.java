package com.uvp.openapi;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotEquals;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.uvp.openapi.client.UvpApiException;
import com.uvp.openapi.client.UvpClient;
import com.uvp.openapi.client.UvpClientConfig;
import com.uvp.openapi.client.UvpErrorKind;
import com.uvp.openapi.model.EmptyObject;
import com.uvp.openapi.client.UvpIdempotency;
import com.uvp.openapi.internal.HmacSigner;
import java.util.Collections;
import com.uvp.openapi.model.DeviceListResponse;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.ArrayList;
import java.util.Base64;
import java.util.List;
import okhttp3.mockwebserver.MockResponse;
import okhttp3.mockwebserver.MockWebServer;
import okhttp3.mockwebserver.RecordedRequest;
import okhttp3.mockwebserver.SocketPolicy;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

/**
 * Behaviour tests for the request pipeline: what actually leaves the client.
 *
 * <p>The central test re-derives the signature from the bytes the server received. That is the same
 * check the platform performs, so a green run here means a real request would authenticate — not
 * merely that the code compiled.</p>
 */
class ApiInvokerTest {

    private static final String ACCESS_KEY = "uvp_000102030405060708090a0b0c0d0e0f";
    private static final String SECRET_KEY =
            Base64.getUrlEncoder().withoutPadding().encodeToString(seedBytes());
    private static final String AUDIENCE = "uvp-openapi-test-audience-v1";

    private MockWebServer server;
    private UvpClient client;

    private static byte[] seedBytes() {
        byte[] bytes = new byte[32];
        for (int i = 0; i < bytes.length; i++) {
            bytes[i] = (byte) i;
        }
        return bytes;
    }

    @BeforeEach
    void startServer() throws Exception {
        server = new MockWebServer();
        server.start();
        client = new UvpClient(UvpClientConfig.builder()
                .baseUrl(server.url("/").toString())
                .accessKey(ACCESS_KEY)
                .secretKey(SECRET_KEY)
                .audience(AUDIENCE)
                .readTimeout(Duration.ofMillis(800))
                .slowReadTimeout(Duration.ofMillis(800))
                .maxRetries(2)
                .build());
    }

    @AfterEach
    void stopServer() throws Exception {
        client.close();
        server.shutdown();
    }

    /** Recomputes the signature from what the server received, exactly as the platform does. */
    private boolean signatureValid(RecordedRequest request, String body) {
        String idempotencyKey = request.getHeader("Idempotency-Key");
        String canonical = HmacSigner.canonical(
                request.getMethod(),
                request.getPath().split("\\?")[0],
                request.getRequestUrl().query(),
                request.getHeader("Content-Type") == null ? "" : "application/json",
                body.getBytes(StandardCharsets.UTF_8),
                request.getHeader("X-UVP-Access-Key"),
                request.getHeader("X-UVP-Timestamp"),
                request.getHeader("X-UVP-Nonce"),
                idempotencyKey == null ? "" : idempotencyKey,
                AUDIENCE);
        return request.getHeader("X-UVP-Signature").equals(HmacSigner.sign(SECRET_KEY, canonical));
    }

    @Test
    void getRequestCarriesValidSignatureAndEncodesQuery() throws Exception {
        server.enqueue(new MockResponse().setResponseCode(200)
                .setHeader("Content-Type", "application/json")
                .setBody("{\"code\":\"OK\",\"message\":\"success\",\"requestId\":\"r1\","
                        + "\"data\":{\"items\":[],\"page\":1,\"pageSize\":20,\"total\":0}}"));

        DeviceListResponse response = client.devices().listDevices(1, 20, "摄像头 A", "online");

        RecordedRequest request = server.takeRequest();
        assertEquals("GET", request.getMethod());
        // Built from the encoder rather than a literal so a Chinese keyword cannot be mis-typed.
        assertEquals("/openapi/v1/devices?page=1&pageSize=20&keyword="
                        + HmacSigner.rfc3986Encode("摄像头 A") + "&status=online",
                request.getPath());
        assertEquals("1", request.getHeader("X-UVP-Sign-Version"));
        assertNotNull(request.getHeader("X-UVP-Nonce"));
        assertEquals(32, request.getHeader("X-UVP-Nonce").length());
        assertTrue(signatureValid(request, ""), "signature must verify against the received bytes");
        assertNotNull(response);
        assertEquals("r1", response.getRequestId());
    }

    @Test
    void postBodyIsSignedAndIdempotencyKeySuppliedWhenCallerPinsIt() throws Exception {
        server.enqueue(new MockResponse().setResponseCode(200)
                .setHeader("Content-Type", "application/json")
                .setBody("{\"code\":\"OK\",\"message\":\"success\",\"requestId\":\"r2\",\"data\":{}}"));

        UvpIdempotency.run("pinned-key-123",
                () -> client.control().channelRecordingStart("34020000001320000001", "34020000001320000002", new EmptyObject()));

        RecordedRequest request = server.takeRequest();
        assertEquals("pinned-key-123", request.getHeader("Idempotency-Key"));
        // The platform compares this header to the literal application/json before verifying the
        // signature, so "application/json; charset=utf-8" is rejected with 400. Only the live
        // platform exposed that; a mock accepts anything.
        assertEquals("application/json", request.getHeader("Content-Type"));
        String received = request.getBody().readUtf8();
        assertTrue(signatureValid(request, received),
                "signature must verify against the transmitted body bytes, not a re-serialization");
    }

    @Test
    void requiredIdempotencyKeyIsGeneratedWhenCallerDoesNotPinOne() throws Exception {
        server.enqueue(new MockResponse().setResponseCode(200)
                .setHeader("Content-Type", "application/json")
                .setBody("{\"code\":\"OK\",\"message\":\"success\",\"requestId\":\"r3\",\"data\":{}}"));
        server.enqueue(new MockResponse().setResponseCode(200)
                .setHeader("Content-Type", "application/json")
                .setBody("{\"code\":\"OK\",\"message\":\"success\",\"requestId\":\"r4\",\"data\":{}}"));

        client.control().channelRecordingStart("d", "c", new EmptyObject());
        client.control().channelRecordingStart("d", "c", new EmptyObject());

        String first = server.takeRequest().getHeader("Idempotency-Key");
        String second = server.takeRequest().getHeader("Idempotency-Key");
        assertNotNull(first);
        assertNotNull(second);
        assertNotEquals(first, second, "each logical write must get its own key");
    }

    @Test
    void getIsRetriedWithFreshNonceAfterServerError() throws Exception {
        server.enqueue(new MockResponse().setResponseCode(503).setBody("{\"code\":\"SERVICE_UNAVAILABLE\","
                + "\"message\":\"busy\",\"requestId\":\"r5\"}"));
        server.enqueue(new MockResponse().setResponseCode(200)
                .setHeader("Content-Type", "application/json")
                .setBody("{\"code\":\"OK\",\"message\":\"success\",\"requestId\":\"r6\","
                        + "\"data\":{\"items\":[],\"page\":1,\"pageSize\":20,\"total\":0}}"));

        DeviceListResponse response = client.devices().listDevices(1, 20, null, null);

        RecordedRequest first = server.takeRequest();
        RecordedRequest second = server.takeRequest();
        assertNotEquals(first.getHeader("X-UVP-Nonce"), second.getHeader("X-UVP-Nonce"),
                "a retry must be re-signed with a new nonce, otherwise the platform sees a replay");
        assertTrue(signatureValid(second, ""));
        assertEquals("r6", response.getRequestId());
    }

    @Test
    void postIsNeverRetriedAutomatically() throws Exception {
        server.enqueue(new MockResponse().setResponseCode(503).setBody("{\"code\":\"SERVICE_UNAVAILABLE\","
                + "\"message\":\"busy\",\"requestId\":\"r7\"}"));

        UvpApiException failure = assertThrows(UvpApiException.class,
                () -> client.control().channelRecordingStart("d", "c", new EmptyObject()));

        assertEquals(UvpErrorKind.SERVER, failure.getKind());
        assertEquals(503, failure.getHttpStatus());
        assertEquals("r7", failure.getRequestId());
        assertEquals(1, server.getRequestCount(),
                "an unknown write outcome must not be replayed without the caller's consent");
    }

    @Test
    void errorStatusesMapToDistinctKinds() throws Exception {
        // Retries disabled: this test is about the mapping, and a retried 429 would need extra
        // responses queued and would obscure which status produced which kind.
        UvpClient noRetry = new UvpClient(UvpClientConfig.builder()
                .baseUrl(server.url("/").toString())
                .accessKey(ACCESS_KEY)
                .secretKey(SECRET_KEY)
                .audience(AUDIENCE)
                .readTimeout(Duration.ofMillis(800))
                .slowReadTimeout(Duration.ofMillis(800))
                .maxRetries(0)
                .build());
        try {
            errorStatusesWith(noRetry);
        } finally {
            noRetry.close();
        }
    }

    private void errorStatusesWith(UvpClient noRetry) {
        List<MockResponse> responses = new ArrayList<MockResponse>();
        int[] statuses = {401, 403, 404, 409, 429};
        UvpErrorKind[] expected = {UvpErrorKind.AUTHENTICATION, UvpErrorKind.AUTHORIZATION,
                UvpErrorKind.NOT_FOUND, UvpErrorKind.CONFLICT, UvpErrorKind.RATE_LIMITED};
        for (int status : statuses) {
            responses.add(new MockResponse().setResponseCode(status)
                    .setBody("{\"code\":\"X\",\"message\":\"m" + status + "\",\"requestId\":\"rid" + status + "\"}"));
        }
        for (MockResponse response : responses) {
            server.enqueue(response);
        }

        for (int i = 0; i < statuses.length; i++) {
            UvpApiException failure = assertThrows(UvpApiException.class,
                    () -> noRetry.devices().getDevice("34020000001320000001"));
            assertEquals(expected[i], failure.getKind(), "status " + statuses[i]);
        }
        assertEquals(statuses.length, server.getRequestCount());
    }

    @Test
    void timeoutOnWriteCarriesIdempotencyGuidance() throws Exception {
        server.enqueue(new MockResponse().setSocketPolicy(SocketPolicy.NO_RESPONSE));

        UvpApiException failure = assertThrows(UvpApiException.class,
                () -> client.control().channelRecordingStart("d", "c", new EmptyObject()));

        assertEquals(UvpErrorKind.TIMEOUT, failure.getKind());
        assertTrue(failure.getMessage().contains("Idempotency-Key"),
                "the message must tell the caller how to retry safely: " + failure.getMessage());
        assertEquals(1, server.getRequestCount());
    }

    @Test
    void transportFailureIsReportedAsNetworkWithoutRetryingWrites() throws Exception {
        server.shutdown();

        UvpApiException failure = assertThrows(UvpApiException.class,
                () -> client.control().channelRecordingStart("d", "c", new EmptyObject()));

        assertEquals(UvpErrorKind.NETWORK, failure.getKind());
        assertEquals(0, failure.getHttpStatus());
    }
}
