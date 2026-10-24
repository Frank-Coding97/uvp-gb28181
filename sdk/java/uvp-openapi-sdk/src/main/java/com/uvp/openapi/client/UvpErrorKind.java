package com.uvp.openapi.client;

/**
 * Failure classes returned by the UVP machine-client API.
 *
 * <p>The API deliberately uses a small fixed set of HTTP status codes. Mapping them to an enum
 * once, at the client boundary, keeps business code free of raw status numbers and makes the
 * "should I retry" decision explicit instead of accidental.</p>
 */
public enum UvpErrorKind {

    /** 400 / 405 / 415 / 422 — the request itself is malformed; retrying changes nothing. */
    INVALID_REQUEST(false),

    /** 401 — access key unknown, disabled, signature mismatch, or timestamp outside the window. */
    AUTHENTICATION(false),

    /** 403 — the key is valid but the owner lacks the required scope or the channel is not owned. */
    AUTHORIZATION(false),

    /** 404 — device, channel, session or operation id does not exist (or is not visible). */
    NOT_FOUND(false),

    /** 409 — the device or channel cannot accept the operation in its current state. */
    CONFLICT(false),

    /** 429 — per-key concurrency or rate limit. Safe to retry for idempotent reads. */
    RATE_LIMITED(true),

    /** 5xx — platform side failure. Safe to retry for idempotent reads. */
    SERVER(true),

    /** Connection refused, DNS failure, TLS failure, connection reset. */
    NETWORK(true),

    /** Read or connect timeout. The server may still have processed the request. */
    TIMEOUT(true),

    /** Closed client, interrupted thread, or a response that could not be parsed. */
    UNKNOWN(false);

    private final boolean retryableForReads;

    UvpErrorKind(boolean retryableForReads) {
        this.retryableForReads = retryableForReads;
    }

    /**
     * Whether a <em>read</em> request (GET) may be retried after this failure.
     *
     * <p>Never use this to decide whether to retry a POST. A POST whose result is unknown must not
     * be replayed automatically: the platform may already have started a recording or a PTZ
     * command. Retry it yourself with the same {@code Idempotency-Key}.</p>
     */
    public boolean isRetryableForReads() {
        return retryableForReads;
    }

    /** Maps an HTTP status to its failure class. */
    public static UvpErrorKind fromStatus(int status) {
        switch (status) {
            case 400:
            case 405:
            case 415:
            case 422:
                return INVALID_REQUEST;
            case 401:
                return AUTHENTICATION;
            case 403:
                return AUTHORIZATION;
            case 404:
                return NOT_FOUND;
            case 409:
                return CONFLICT;
            case 429:
                return RATE_LIMITED;
            default:
                return status >= 500 ? SERVER : UNKNOWN;
        }
    }
}
