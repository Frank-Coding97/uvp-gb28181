package com.uvp.openapi.client;

import java.util.UUID;
import java.util.function.Supplier;

/**
 * Carries an idempotency key across one logical unit of work.
 *
 * <p>A write can fail after the platform already acted on it (read timeout, connection reset). The
 * fix is to replay the <em>same</em> key: the platform then returns the original result instead of
 * stopping the recording twice. Wrapping the work in {@link #call} keeps that key attached even
 * when the retry happens several frames up the stack.</p>
 *
 * <pre>{@code
 * UvpIdempotency.call(UvpIdempotency.newKey(), () ->
 *     recordingApi.start(deviceId, channelId));
 * }</pre>
 *
 * <p>The key is bound to the current thread, which matches how Spring transactions and MVC requests
 * are scoped. Do not cross threads with it; mint a fresh key per unit of work instead.</p>
 */
public final class UvpIdempotency {

    private static final ThreadLocal<String> CURRENT = new ThreadLocal<String>();

    private UvpIdempotency() {
    }

    /** Current key on this thread, or {@code null}. */
    public static String current() {
        return CURRENT.get();
    }

    /** A fresh 32-character key. */
    public static String newKey() {
        return UUID.randomUUID().toString().replace("-", "");
    }

    /**
     * Runs {@code action} with {@code key} attached to every write issued on this thread.
     * If a key was already set it is restored afterwards, so nesting is safe.
     */
    public static <T> T call(String key, Supplier<T> action) {
        String previous = CURRENT.get();
        CURRENT.set(key);
        try {
            return action.get();
        } finally {
            if (previous == null) {
                CURRENT.remove();
            } else {
                CURRENT.set(previous);
            }
        }
    }

    public static void run(String key, Runnable action) {
        call(key, () -> {
            action.run();
            return null;
        });
    }
}
