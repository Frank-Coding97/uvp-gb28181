package com.uvp.openapi.internal;

import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * One API request, described declaratively. Built by the generated {@code *Api} classes and executed
 * by {@link ApiInvoker}.
 *
 * <p>Application code never constructs this directly; it is an implementation detail of the
 * generated methods.</p>
 */
public final class Call<T> {

    public enum Method {
        GET, POST, PUT, DELETE
    }

    /** How the platform treats the {@code Idempotency-Key} header for this operation. */
    public enum Idempotency {
        /** Header ignored. */
        NONE,
        /** Header accepted when present. */
        OPTIONAL,
        /** Header required by the platform; the SDK always sends one. */
        REQUIRED
    }

    private final Method method;
    private final String pathTemplate;
    private final Map<String, String> pathVars;
    private final Map<String, Object> query;
    private final Object body;
    private final Idempotency idempotency;
    private final String idempotencyKey;
    private final boolean slow;
    private final Class<T> responseType;

    private Call(Builder builder, Class<T> responseType) {
        this.method = builder.method;
        this.pathTemplate = builder.pathTemplate;
        this.pathVars = Collections.unmodifiableMap(new LinkedHashMap<String, String>(builder.pathVars));
        this.query = Collections.unmodifiableMap(new LinkedHashMap<String, Object>(builder.query));
        this.body = builder.body;
        this.idempotency = builder.idempotency;
        this.idempotencyKey = builder.idempotencyKey;
        this.slow = builder.slow;
        this.responseType = responseType;
    }

    public Method method() {
        return method;
    }

    public String pathTemplate() {
        return pathTemplate;
    }

    public Map<String, String> pathVars() {
        return pathVars;
    }

    public Map<String, Object> query() {
        return query;
    }

    public Object body() {
        return body;
    }

    public Idempotency idempotency() {
        return idempotency;
    }

    /** Explicit caller-supplied key, or {@code null} to let the SDK mint one. */
    public String idempotencyKey() {
        return idempotencyKey;
    }

    public boolean slow() {
        return slow;
    }

    public Class<T> responseType() {
        return responseType;
    }

    public static Builder get(String pathTemplate) {
        return new Builder(Method.GET, pathTemplate);
    }

    public static Builder post(String pathTemplate) {
        return new Builder(Method.POST, pathTemplate);
    }

    public static Builder put(String pathTemplate) {
        return new Builder(Method.PUT, pathTemplate);
    }

    public static Builder delete(String pathTemplate) {
        return new Builder(Method.DELETE, pathTemplate);
    }

    public static final class Builder {
        private final Method method;
        private final String pathTemplate;
        private final Map<String, String> pathVars = new LinkedHashMap<String, String>();
        private final Map<String, Object> query = new LinkedHashMap<String, Object>();
        private Object body;
        private Idempotency idempotency = Idempotency.NONE;
        private String idempotencyKey;
        private boolean slow;

        private Builder(Method method, String pathTemplate) {
            this.method = method;
            this.pathTemplate = pathTemplate;
        }

        public Builder path(String name, Object value) {
            pathVars.put(name, value == null ? "" : String.valueOf(value));
            return this;
        }

        public Builder query(String name, Object value) {
            query.put(name, value);
            return this;
        }

        public Builder body(Object value) {
            this.body = value;
            return this;
        }

        public Builder idempotency(Idempotency mode) {
            this.idempotency = mode;
            return this;
        }

        /**
         * Pins the idempotency key. Required when the caller retries a write after an unknown
         * outcome (timeout, connection reset): replaying with the same key lets the platform return
         * the original result instead of performing the action twice.
         */
        public Builder idempotencyKey(String value) {
            this.idempotencyKey = value;
            return this;
        }

        /** Marks the call as waiting on the device; uses the longer read timeout. */
        public Builder slow() {
            this.slow = true;
            return this;
        }

        public <T> Call<T> as(Class<T> responseType) {
            return new Call<T>(this, responseType);
        }
    }
}
