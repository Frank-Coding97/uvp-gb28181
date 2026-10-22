package com.uvp.demo;

import com.uvp.openapi.client.UvpApiException;
import java.util.LinkedHashMap;
import java.util.Map;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;

/**
 * Turns the SDK's classified failure into an HTTP response for the caller.
 *
 * <p>Two deliberate choices:</p>
 * <ul>
 *   <li>A platform authentication failure is reported as 502, not 401. The browser caller is not
 *       the one failing to authenticate; our server is. Returning 401 would make the front end
 *       think the user's session expired and log them out.</li>
 *   <li>{@code requestId} is always passed through. It is the only handle platform support can use
 *       to find the exact request, and it costs nothing to forward.</li>
 * </ul>
 */
@RestControllerAdvice
public class UvpExceptionHandler {

    private static final Logger log = LoggerFactory.getLogger(UvpExceptionHandler.class);

    @ExceptionHandler(UvpApiException.class)
    public ResponseEntity<Map<String, Object>> handle(UvpApiException failure) {
        log.warn("platform call failed: {}", failure.toString());

        HttpStatus status = switch (failure.getKind()) {
            case INVALID_REQUEST -> HttpStatus.BAD_REQUEST;
            case AUTHORIZATION -> HttpStatus.FORBIDDEN;
            case NOT_FOUND -> HttpStatus.NOT_FOUND;
            case CONFLICT -> HttpStatus.CONFLICT;
            case RATE_LIMITED -> HttpStatus.TOO_MANY_REQUESTS;
            case AUTHENTICATION, SERVER -> HttpStatus.BAD_GATEWAY;
            case TIMEOUT, NETWORK -> HttpStatus.GATEWAY_TIMEOUT;
            default -> HttpStatus.INTERNAL_SERVER_ERROR;
        };

        Map<String, Object> body = new LinkedHashMap<>();
        body.put("kind", failure.getKind().name());
        body.put("message", failure.getMessage());
        body.put("platformStatus", failure.getHttpStatus());
        body.put("platformCode", failure.getCode());
        body.put("requestId", failure.getRequestId());
        return ResponseEntity.status(status).body(body);
    }
}
