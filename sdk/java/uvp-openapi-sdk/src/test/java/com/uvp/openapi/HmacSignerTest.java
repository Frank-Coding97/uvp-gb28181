package com.uvp.openapi;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import com.uvp.openapi.internal.HmacSigner;
import java.io.File;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Test;

/**
 * Verifies the client signer against the same golden fixture the server tests use.
 *
 * <p>Read from the server tree on purpose rather than copied into test resources: a copy drifts the
 * first time the server changes the canonical format, and a stale fixture would keep reporting
 * green while every real request fails with 401.</p>
 */
class HmacSignerTest {

    private static JsonObject fixture;

    @BeforeAll
    static void loadFixture() throws IOException {
        Path path = locate("server/app/openapi/auth/testdata/signature-v1.json");
        fixture = JsonParser.parseString(new String(Files.readAllBytes(path), StandardCharsets.UTF_8))
                .getAsJsonObject();
    }

    private static Path locate(String relative) {
        Path current = Paths.get(System.getProperty("user.dir")).toAbsolutePath();
        while (current != null) {
            Path candidate = current.resolve(relative);
            if (Files.exists(candidate)) {
                return candidate;
            }
            current = current.getParent();
        }
        throw new IllegalStateException("cannot find " + relative
                + " above " + System.getProperty("user.dir")
                + "; run the tests from inside the repository");
    }

    @Test
    void reproducesEveryServerGoldenSignature() {
        String secretKey = fixture.get("secretKey").getAsString();
        String accessKey = fixture.get("accessKey").getAsString();
        String timestamp = fixture.get("timestamp").getAsString();
        String nonce = fixture.get("nonce").getAsString();
        String audience = fixture.get("audience").getAsString();
        int checked = 0;
        for (com.google.gson.JsonElement element : fixture.getAsJsonArray("vectors")) {
            JsonObject vector = element.getAsJsonObject();
            String name = vector.get("name").getAsString();
            String canonical = HmacSigner.canonical(
                    vector.get("method").getAsString(),
                    vector.get("path").getAsString(),
                    vector.get("rawQuery").getAsString(),
                    vector.get("contentType").getAsString(),
                    vector.get("body").getAsString().getBytes(StandardCharsets.UTF_8),
                    accessKey, timestamp, nonce,
                    vector.has("idempotencyKey") ? vector.get("idempotencyKey").getAsString() : "",
                    audience);

            assertEquals(vector.get("canonical").getAsString(), canonical,
                    "canonical mismatch for " + name);
            assertEquals(vector.get("canonicalBytes").getAsInt(),
                    canonical.getBytes(StandardCharsets.UTF_8).length,
                    "canonical byte length mismatch for " + name);
            assertEquals(vector.get("bodySHA256").getAsString(),
                    HmacSigner.sha256Hex(vector.get("body").getAsString().getBytes(StandardCharsets.UTF_8)),
                    "body hash mismatch for " + name);
            assertEquals(vector.get("signature").getAsString(),
                    HmacSigner.sign(secretKey, canonical),
                    "signature mismatch for " + name);
            checked++;
        }
        assertTrue(checked >= 2, "expected at least two golden vectors, saw " + checked);
    }

    @Test
    void canonicalIsElevenLinesWithNoTrailingNewline() {
        String canonical = HmacSigner.canonical("GET", "/openapi/v1/devices", "", "",
                null, "uvp_000102030405060708090a0b0c0d0e0f", "1790000000",
                "000102030405060708090a0b0c0d0e0f", null, "aud");
        assertEquals(11, canonical.split("\n", -1).length);
        assertFalse(canonical.endsWith("\n"));
        assertTrue(canonical.startsWith("UVP-HMAC-SHA256/1\n"));
        assertTrue(canonical.endsWith("\naud"));
    }

    @Test
    void idempotencyKeyOccupiesTheTenthLine() {
        String without = HmacSigner.canonical("POST", "/p", "", "application/json", new byte[0],
                "uvp_000102030405060708090a0b0c0d0e0f", "1", "000102030405060708090a0b0c0d0e0f",
                null, "aud");
        String with = HmacSigner.canonical("POST", "/p", "", "application/json", new byte[0],
                "uvp_000102030405060708090a0b0c0d0e0f", "1", "000102030405060708090a0b0c0d0e0f",
                "abc123", "aud");
        String[] a = without.split("\n", -1);
        String[] b = with.split("\n", -1);
        assertEquals("", a[9]);
        assertEquals("abc123", b[9]);
        assertEquals(a[10], b[10]);
    }

    @Test
    void queryIsSortedAndRfc3986Encoded() {
        assertEquals("a=1&b=2&c=x%20y", HmacSigner.canonicalQuery("c=x%20y&b=2&a=1"));
        // Keys sort by their encoded form, and uppercase hex is used.
        assertEquals("Camera=%E6%91%84%E5%83%8F%E6%9C%BA%20A",
                HmacSigner.canonicalQuery("Camera=%E6%91%84%E5%83%8F%E6%9C%BA%20A"));
        assertEquals("", HmacSigner.canonicalQuery(""));
    }

    @Test
    void secretKeyMustDecodeToThirtyTwoBytes() {
        assertEquals(32, HmacSigner.decodeSecretKey(fixture.get("secretKey").getAsString()).length);
        assertThrows(IllegalArgumentException.class, () -> HmacSigner.decodeSecretKey("short"));
        assertThrows(IllegalArgumentException.class, () -> HmacSigner.decodeSecretKey(""));
    }

    @Test
    void emptyBodyHashesToTheEmptyDigest() {
        assertEquals("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
                HmacSigner.sha256Hex(new byte[0]));
    }
}
