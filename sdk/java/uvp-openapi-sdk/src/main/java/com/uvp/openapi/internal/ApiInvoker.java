package com.uvp.openapi.internal;

import com.google.gson.Gson;
import com.google.gson.GsonBuilder;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import com.uvp.openapi.client.UvpApiException;
import com.uvp.openapi.client.UvpClientConfig;
import com.uvp.openapi.client.UvpErrorKind;
import com.uvp.openapi.client.UvpIdempotency;
import java.io.IOException;
import java.io.InterruptedIOException;
import java.net.ConnectException;
import java.net.SocketTimeoutException;
import java.net.UnknownHostException;
import java.nio.charset.StandardCharsets;
import java.security.SecureRandom;
import java.time.Clock;
import java.time.OffsetDateTime;
import java.time.format.DateTimeFormatter;
import java.util.UUID;
import javax.net.ssl.SSLException;
import okhttp3.HttpUrl;
import okhttp3.MediaType;
import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.RequestBody;
import okhttp3.Response;
import okhttp3.ResponseBody;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/**
 * Sends one prepared request, with signing, retry and error mapping applied.
 *
 * <p>Signed at the byte level: the serialized body is created once and the same bytes are both
 * hashed and transmitted. Re-serializing for the signature would produce mismatched bytes whenever
 * field order or null handling differs, and the platform reports that only as a bare 401.</p>
 */
public final class ApiInvoker {

    private static final Logger log = LoggerFactory.getLogger(ApiInvoker.class);

    /**
     * Exactly {@code application/json}, with no charset parameter.
     *
     * <p>The platform rejects any other value with 400 INVALID_REQUEST: it compares the header to the
     * literal {@code application/json} before verifying the signature, so {@code charset=utf-8} is
     * not a harmless extra. Bytes are already UTF-8 encoded here, so the charset would be redundant
     * anyway.</p>
     */
    private static final MediaType JSON = MediaType.get("application/json");

    private final UvpClientConfig config;
    private final OkHttpClient fastClient;
    private final OkHttpClient slowClient;
    private final Gson gson;
    private final Clock clock;
    private final SecureRandom random = new SecureRandom();
    private final byte[] secretKeyBytes;

    public ApiInvoker(UvpClientConfig config, OkHttpClient fastClient, OkHttpClient slowClient,
                      Gson gson, Clock clock) {
        this.config = config;
        this.fastClient = fastClient;
        this.slowClient = slowClient;
        this.gson = gson;
        this.clock = clock;
        this.secretKeyBytes = HmacSigner.decodeSecretKey(config.getSecretKey());
    }

    /**
     * The Gson instance used for both directions. Shared with the generated models so that a body
     * serialised by application code and one serialised here cannot diverge.
     */
    public static Gson defaultGson() {
        return com.uvp.openapi.UvpJson.getGson();
    }

    public <T> T execute(Call<T> call) {
        int attempt = 0;
        while (true) {
            Request request = buildRequest(call);
            OkHttpClient client = call.slow() ? slowClient : fastClient;
            try {
                Response response = client.newCall(request).execute();
                try {
                    String text = readBody(response);
                    if (response.isSuccessful()) {
                        return parseSuccess(text, call);
                    }
                    UvpApiException failure = parseFailure(response.code(), text);
                    if (canRetry(call, failure, attempt)) {
                        attempt++;
                        backoff(attempt, response);
                        continue;
                    }
                    throw failure;
                } finally {
                    response.close();
                }
            } catch (IOException io) {
                UvpApiException failure = mapIoFailure(call, io);
                if (canRetry(call, failure, attempt)) {
                    attempt++;
                    backoff(attempt, null);
                    continue;
                }
                throw failure;
            }
        }
    }

    private boolean canRetry(Call<?> call, UvpApiException failure, int attempt) {
        // Only reads are replayed automatically. A POST whose outcome is unknown may already have
        // changed device state; the caller must decide, reusing the same Idempotency-Key.
        return call.method() == Call.Method.GET
                && attempt < config.getMaxRetries()
                && failure.getKind().isRetryableForReads();
    }

    private void backoff(int attempt, Response response) {
        long millis = Math.min(2000L, 150L * (1L << (attempt - 1)));
        String retryAfter = response == null ? null : response.header("Retry-After");
        if (retryAfter != null) {
            try {
                millis = Math.max(millis, Long.parseLong(retryAfter.trim()) * 1000L);
            } catch (NumberFormatException ignored) {
                // Header is not always numeric; fall back to our own backoff.
            }
        }
        try {
            Thread.sleep(millis);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new UvpApiException(UvpErrorKind.UNKNOWN, 0, null, null,
                    "interrupted while waiting to retry", null, e);
        }
    }

    private Request buildRequest(Call<?> call) {
        String path = expandPath(call);
        String rawQuery = HmacSigner.buildRawQuery(call.query());
        byte[] bodyBytes = call.body() == null ? null
                : gson.toJson(call.body()).getBytes(StandardCharsets.UTF_8);
        String contentType = bodyBytes == null ? "" : "application/json";
        String timestamp = String.valueOf(clock.millis() / 1000L);
        String nonce = newNonce();
        String idempotencyKey = resolveIdempotencyKey(call);
        String canonical = HmacSigner.canonical(
                call.method().name(), path, rawQuery, contentType, bodyBytes,
                config.getAccessKey(), timestamp, nonce, idempotencyKey, config.getAudience());
        String signature = HmacSigner.sign(secretKeyBytes, canonical);

        HttpUrl.Builder url = HttpUrl.get(config.getBaseUrl() + path).newBuilder();
        if (!rawQuery.isEmpty()) {
            url.encodedQuery(rawQuery);
        }

        Request.Builder builder = new Request.Builder()
                .url(url.build())
                .header("X-UVP-Sign-Version", HmacSigner.SIGN_VERSION)
                .header("X-UVP-Access-Key", config.getAccessKey())
                .header("X-UVP-Timestamp", timestamp)
                .header("X-UVP-Nonce", nonce)
                .header("X-UVP-Signature", signature)
                .header("User-Agent", config.getUserAgent())
                .header("Accept", "application/json");

        if (idempotencyKey != null) {
            builder.header("Idempotency-Key", idempotencyKey);
        }
        if (bodyBytes != null) {
            builder.method(call.method().name(), RequestBody.create(bodyBytes, JSON));
        } else {
            // POST without a body must still declare an empty body, otherwise okhttp sends
            // Content-Length: 0 with a null body and some proxies reject the request.
            builder.method(call.method().name(),
                    call.method() == Call.Method.GET ? null : RequestBody.create(new byte[0], null));
        }
        if (log.isDebugEnabled()) {
            log.debug("UVP {} {} nonce={} idem={}", call.method(), path, nonce, idempotencyKey);
        }
        return builder.build();
    }

    private String expandPath(Call<?> call) {
        String path = call.pathTemplate();
        for (java.util.Map.Entry<String, String> entry : call.pathVars().entrySet()) {
            path = path.replace("{" + entry.getKey() + "}", HmacSigner.rfc3986Encode(entry.getValue()));
        }
        return path;
    }

    private String resolveIdempotencyKey(Call<?> call) {
        if (call.idempotency() == Call.Idempotency.NONE) {
            // An explicit key on a read-only operation is harmless but pointless; drop it so that
            // identical GET requests keep identical signatures.
            return call.method() == Call.Method.GET ? null : call.idempotencyKey();
        }
        if (call.idempotencyKey() != null) {
            return call.idempotencyKey();
        }
        String ambient = UvpIdempotency.current();
        if (ambient != null) {
            return ambient;
        }
        return UUID.randomUUID().toString().replace("-", "");
    }

    private String newNonce() {
        byte[] bytes = new byte[16];
        random.nextBytes(bytes);
        StringBuilder sb = new StringBuilder(32);
        for (byte b : bytes) {
            sb.append(Character.forDigit((b >> 4) & 0x0f, 16));
            sb.append(Character.forDigit(b & 0x0f, 16));
        }
        return sb.toString();
    }

    private String readBody(Response response) throws IOException {
        ResponseBody body = response.body();
        return body == null ? "" : body.string();
    }

    private <T> T parseSuccess(String text, Call<T> call) {
        if (call.responseType() == Void.class) {
            return null;
        }
        try {
            return gson.fromJson(text, call.responseType());
        } catch (RuntimeException e) {
            throw new UvpApiException(UvpErrorKind.UNKNOWN, 200, null, null,
                    "response could not be parsed as " + call.responseType().getSimpleName()
                            + ": " + e.getMessage(), text, e);
        }
    }

    private UvpApiException parseFailure(int status, String text) {
        String code = null;
        String message = null;
        String requestId = null;
        try {
            JsonElement parsed = JsonParser.parseString(text);
            if (parsed.isJsonObject()) {
                JsonObject obj = parsed.getAsJsonObject();
                code = stringOrNull(obj, "code");
                message = stringOrNull(obj, "message");
                requestId = stringOrNull(obj, "requestId");
            }
        } catch (RuntimeException ignored) {
            // Non-JSON error page (proxy, gateway). Keep the raw body for diagnosis.
        }
        if (message == null || message.isEmpty()) {
            message = text == null || text.isEmpty() ? "empty response body" : truncate(text, 200);
        }
        return new UvpApiException(UvpErrorKind.fromStatus(status), status, code, requestId, message, text, null);
    }

    private UvpApiException mapIoFailure(Call<?> call, IOException io) {
        UvpErrorKind kind;
        String hint = "";
        if (io instanceof SocketTimeoutException) {
            kind = UvpErrorKind.TIMEOUT;
            if (call.method() == Call.Method.GET) {
                hint = " (read timeout; safe to retry)";
            } else {
                hint = " (the platform may still be processing this write; retry with the same "
                        + "Idempotency-Key instead of a new one)";
            }
        } else if (io instanceof SSLException) {
            kind = UvpErrorKind.NETWORK;
            hint = " (TLS failed. The bundled platform uses a self-signed certificate whose SAN "
                    + "usually does not cover the server IP, so Java rejects it as"
                    + " 'PKIX path building failed'. Use http:// on a trusted network, install a "
                    + "certificate with the IP in its SAN, or add the platform certificate to the "
                    + "JVM truststore.)";
        } else if (io instanceof UnknownHostException) {
            kind = UvpErrorKind.NETWORK;
            hint = " (host cannot be resolved; check baseUrl and DNS)";
        } else if (io instanceof ConnectException) {
            kind = UvpErrorKind.NETWORK;
            hint = " (connection refused; check that the platform is running and the port is open)";
        } else if (io instanceof InterruptedIOException) {
            kind = UvpErrorKind.UNKNOWN;
            hint = " (request was cancelled)";
        } else {
            kind = UvpErrorKind.NETWORK;
        }
        return new UvpApiException(kind, 0, null, null,
                call.method() + " " + call.pathTemplate() + " failed: " + io.getMessage() + hint, null, io);
    }

    private static String stringOrNull(JsonObject obj, String name) {
        JsonElement element = obj.get(name);
        return element == null || element.isJsonNull() ? null : element.getAsString();
    }

    private static String truncate(String value, int max) {
        return value.length() <= max ? value : value.substring(0, max) + "...";
    }
}
