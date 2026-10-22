package com.uvp.openapi.internal;

import java.io.UnsupportedEncodingException;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.ArrayList;
import java.util.Base64;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;

/**
 * UVP HMAC v1 signing, mirroring the server-side {@code auth.CanonicalString}.
 *
 * <p>This class exists in exactly one place on purpose. A second implementation (for example one
 * that re-serialises a request body before hashing) silently produces signatures that never
 * validate, and the only symptom is a bare 401. Keep it single-sourced.</p>
 */
public final class HmacSigner {

    public static final String SIGN_VERSION = "1";

    private static final char[] HEX = "0123456789abcdef".toCharArray();
    private static final char[] HEX_UPPER = "0123456789ABCDEF".toCharArray();
    private static final String HMAC_ALGORITHM = "HmacSHA256";

    private HmacSigner() {
    }

    /**
     * Builds the eleven-line canonical string. There is no trailing newline.
     *
     * <p>Line order: version, access key, timestamp, nonce, method, path, canonical query,
     * content type, body SHA-256, idempotency key, audience.</p>
     *
     * @param body request body exactly as it will be sent; {@code null} is treated as empty
     */
    public static String canonical(String method, String path, String rawQuery, String contentType,
                                   byte[] body, String accessKey, String timestamp, String nonce,
                                   String idempotencyKey, String audience) {
        String canonicalQuery = canonicalQuery(rawQuery);
        StringBuilder sb = new StringBuilder(256);
        sb.append("UVP-HMAC-SHA256/1").append('\n');
        sb.append(accessKey).append('\n');
        sb.append(timestamp).append('\n');
        sb.append(nonce).append('\n');
        sb.append(method).append('\n');
        sb.append(path).append('\n');
        sb.append(canonicalQuery).append('\n');
        sb.append(contentType == null ? "" : contentType).append('\n');
        sb.append(sha256Hex(body == null ? new byte[0] : body)).append('\n');
        sb.append(idempotencyKey == null ? "" : idempotencyKey).append('\n');
        sb.append(audience);
        return sb.toString();
    }

    /** Signs a canonical string; returns lowercase hex. */
    public static String sign(String secretKeyBase64Url, String canonical) {
        return sign(decodeSecretKey(secretKeyBase64Url), canonical);
    }

    public static String sign(byte[] secretKey, String canonical) {
        try {
            Mac mac = Mac.getInstance(HMAC_ALGORITHM);
            mac.init(new SecretKeySpec(secretKey, HMAC_ALGORITHM));
            return toHex(mac.doFinal(canonical.getBytes(StandardCharsets.UTF_8)));
        } catch (NoSuchAlgorithmException | java.security.InvalidKeyException e) {
            throw new IllegalStateException("HmacSHA256 is unavailable in this JVM", e);
        }
    }

    /**
     * Decodes the base64url secret key issued by the platform into the 32 raw bytes used as the
     * HMAC key. Padding is optional.
     */
    public static byte[] decodeSecretKey(String secretKeyBase64Url) {
        if (secretKeyBase64Url == null || secretKeyBase64Url.isEmpty()) {
            throw new IllegalArgumentException("secretKey is empty");
        }
        String normalized = secretKeyBase64Url.trim();
        int remainder = normalized.length() % 4;
        if (remainder == 2) {
            normalized = normalized + "==";
        } else if (remainder == 3) {
            normalized = normalized + "=";
        } else if (remainder == 1) {
            throw new IllegalArgumentException("secretKey is not valid base64url");
        }
        byte[] decoded;
        try {
            decoded = Base64.getUrlDecoder().decode(normalized);
        } catch (IllegalArgumentException e) {
            throw new IllegalArgumentException("secretKey is not valid base64url", e);
        }
        if (decoded.length != 32) {
            throw new IllegalArgumentException(
                    "secretKey must decode to 32 bytes, got " + decoded.length);
        }
        return decoded;
    }

    /**
     * Canonicalises a raw query string the same way the server does: decode, re-encode with
     * RFC 3986, sort by key then value, join with {@code &}.
     */
    public static String canonicalQuery(String rawQuery) {
        if (rawQuery == null || rawQuery.isEmpty()) {
            return "";
        }
        List<String[]> pairs = new ArrayList<String[]>();
        for (String part : rawQuery.split("&", -1)) {
            if (part.isEmpty()) {
                continue;
            }
            int separator = part.indexOf('=');
            if (separator < 0) {
                continue;
            }
            String key = decodeComponent(part.substring(0, separator));
            String value = decodeComponent(part.substring(separator + 1));
            pairs.add(new String[]{rfc3986Encode(key), rfc3986Encode(value)});
        }
        Collections.sort(pairs, (a, b) -> {
            int byKey = a[0].compareTo(b[0]);
            return byKey != 0 ? byKey : a[1].compareTo(b[1]);
        });
        StringBuilder sb = new StringBuilder();
        for (String[] pair : pairs) {
            if (sb.length() > 0) {
                sb.append('&');
            }
            sb.append(pair[0]).append('=').append(pair[1]);
        }
        return sb.toString();
    }

    /** Builds the raw query string from ordered parameters, skipping {@code null} values. */
    public static String buildRawQuery(Map<String, ?> query) {
        if (query == null || query.isEmpty()) {
            return "";
        }
        StringBuilder sb = new StringBuilder();
        for (Map.Entry<String, ?> entry : query.entrySet()) {
            Object value = entry.getValue();
            if (value == null) {
                continue;
            }
            if (sb.length() > 0) {
                sb.append('&');
            }
            sb.append(rfc3986Encode(entry.getKey())).append('=').append(rfc3986Encode(String.valueOf(value)));
        }
        return sb.toString();
    }

    /** RFC 3986 percent-encoding: only unreserved characters survive. Space becomes {@code %20}. */
    public static String rfc3986Encode(String value) {
        if (value == null) {
            return "";
        }
        byte[] bytes = value.getBytes(StandardCharsets.UTF_8);
        StringBuilder sb = new StringBuilder(bytes.length * 3);
        for (byte b : bytes) {
            int c = b & 0xff;
            if ((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
                    || c == '-' || c == '.' || c == '_' || c == '~') {
                sb.append((char) c);
            } else {
                sb.append('%').append(HEX_UPPER[c >> 4]).append(HEX_UPPER[c & 0x0f]);
            }
        }
        return sb.toString();
    }

    public static String sha256Hex(byte[] body) {
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            return toHex(digest.digest(body));
        } catch (NoSuchAlgorithmException e) {
            throw new IllegalStateException("SHA-256 is unavailable in this JVM", e);
        }
    }

    private static String decodeComponent(String raw) {
        // '+' is rejected by the server, but a caller using URLEncoder may still produce it.
        // Treat it as a literal plus so the failure is a clear 400 rather than a silent mismatch.
        try {
            return java.net.URLDecoder.decode(raw.replace("+", "%2B"), "UTF-8");
        } catch (UnsupportedEncodingException e) {
            return raw;
        }
    }

    private static String toHex(byte[] bytes) {
        char[] out = new char[bytes.length * 2];
        for (int i = 0; i < bytes.length; i++) {
            int v = bytes[i] & 0xff;
            out[i * 2] = HEX[v >>> 4];
            out[i * 2 + 1] = HEX[v & 0x0f];
        }
        return new String(out);
    }
}
