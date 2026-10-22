package com.uvp.openapi;

import com.google.gson.Gson;
import com.google.gson.GsonBuilder;
import com.google.gson.JsonDeserializer;
import com.google.gson.JsonPrimitive;
import com.google.gson.JsonSerializer;
import java.time.LocalDateTime;
import java.time.OffsetDateTime;
import java.time.ZoneOffset;
import java.time.format.DateTimeFormatter;
import java.time.format.DateTimeParseException;

/**
 * The single Gson instance shared by the SDK and the generated models.
 *
 * <p>Kept at the package root because generated model helpers call it directly. Everything that
 * touches wire format must go through this instance, otherwise a caller could serialise a request
 * body with different settings than the ones used for signing.</p>
 */
public final class UvpJson {

    private static final DateTimeFormatter ISO = DateTimeFormatter.ISO_OFFSET_DATE_TIME;

    private static final Gson GSON = new GsonBuilder()
            .registerTypeAdapter(OffsetDateTime.class, (JsonSerializer<OffsetDateTime>)
                    (src, type, ctx) -> src == null ? null : new JsonPrimitive(ISO.format(src)))
            .registerTypeAdapter(OffsetDateTime.class, (JsonDeserializer<OffsetDateTime>)
                    (json, type, ctx) -> parseDateTime(json == null || json.isJsonNull() ? null : json.getAsString()))
            .create();

    private UvpJson() {
    }

    public static Gson getGson() {
        return GSON;
    }

    /**
     * Accepts RFC 3339 as the platform emits it, and falls back to a local timestamp interpreted as
     * UTC. The fallback exists because a timestamp without an offset would otherwise throw deep
     * inside response parsing, which is hard to trace back to its cause.
     */
    static OffsetDateTime parseDateTime(String value) {
        if (value == null || value.isEmpty()) {
            return null;
        }
        try {
            return OffsetDateTime.parse(value, ISO);
        } catch (DateTimeParseException ignored) {
            try {
                return LocalDateTime.parse(value, DateTimeFormatter.ISO_LOCAL_DATE_TIME)
                        .atOffset(ZoneOffset.UTC);
            } catch (DateTimeParseException stillInvalid) {
                throw new IllegalArgumentException("unsupported date-time value: " + value, stillInvalid);
            }
        }
    }
}
