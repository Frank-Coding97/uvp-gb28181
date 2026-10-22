package com.uvp.openapi.client;

/**
 * Every failure surfaced by the SDK, already classified.
 *
 * <p>Nothing else is thrown to application code for API failures — no {@code ApiException}, no raw
 * {@code IOException}, no {@code RestClientException}. That means a single
 * {@code catch (UvpApiException e)} plus a switch on {@link #getKind()} covers the whole surface.</p>
 */
public class UvpApiException extends RuntimeException {

    private static final long serialVersionUID = 1L;

    private final UvpErrorKind kind;
    private final int httpStatus;
    private final String code;
    private final String requestId;
    private final String rawBody;

    public UvpApiException(UvpErrorKind kind, int httpStatus, String code, String requestId,
                           String message, String rawBody, Throwable cause) {
        super(buildMessage(kind, httpStatus, code, requestId, message), cause);
        this.kind = kind;
        this.httpStatus = httpStatus;
        this.code = code;
        this.requestId = requestId;
        this.rawBody = rawBody;
    }

    private static String buildMessage(UvpErrorKind kind, int httpStatus, String code,
                                       String requestId, String message) {
        StringBuilder sb = new StringBuilder();
        sb.append('[').append(kind).append(']');
        if (httpStatus > 0) {
            sb.append(" HTTP ").append(httpStatus);
        }
        if (code != null && !code.isEmpty()) {
            sb.append(' ').append(code);
        }
        if (message != null && !message.isEmpty()) {
            sb.append(": ").append(message);
        }
        if (requestId != null && !requestId.isEmpty()) {
            sb.append(" (requestId=").append(requestId).append(')');
        }
        return sb.toString();
    }

    public UvpErrorKind getKind() {
        return kind;
    }

    /** HTTP status, or {@code 0} when the request never reached the server. */
    public int getHttpStatus() {
        return httpStatus;
    }

    /** Platform error code when the server returned one, otherwise {@code null}. */
    public String getCode() {
        return code;
    }

    /**
     * Server-side correlation id. Always log this when reporting a problem — support can find the
     * exact request with it, and nothing else.
     */
    public String getRequestId() {
        return requestId;
    }

    /** Response body as received, for troubleshooting. May be {@code null}. */
    public String getRawBody() {
        return rawBody;
    }

    public boolean isAuthProblem() {
        return kind == UvpErrorKind.AUTHENTICATION || kind == UvpErrorKind.AUTHORIZATION;
    }
}
