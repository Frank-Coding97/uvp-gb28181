package com.uvp.openapi.client;

import java.time.Duration;
import java.util.regex.Pattern;

/**
 * Immutable connection settings for one UVP platform instance.
 *
 * <p>One config = one platform + one access key. If an application talks to several platforms (for
 * example one per district), build one {@link UvpClient} per config instead of mutating a shared
 * one.</p>
 */
public final class UvpClientConfig {

    private static final Pattern ACCESS_KEY = Pattern.compile("^uvp_[0-9a-f]{32}$");

    private final String baseUrl;
    private final String accessKey;
    private final String secretKey;
    private final String audience;
    private final Duration connectTimeout;
    private final Duration readTimeout;
    private final Duration slowReadTimeout;
    private final int maxRetries;
    private final String userAgent;

    private UvpClientConfig(Builder builder) {
        this.baseUrl = normalizeBaseUrl(builder.baseUrl);
        this.accessKey = requireText(builder.accessKey, "accessKey");
        this.secretKey = requireText(builder.secretKey, "secretKey");
        this.audience = requireText(builder.audience, "audience");
        this.connectTimeout = positive(builder.connectTimeout, "connectTimeout");
        this.readTimeout = positive(builder.readTimeout, "readTimeout");
        this.slowReadTimeout = positive(builder.slowReadTimeout, "slowReadTimeout");
        this.maxRetries = builder.maxRetries;
        this.userAgent = builder.userAgent == null ? "uvp-openapi-java/1.0" : builder.userAgent;

        if (!ACCESS_KEY.matcher(accessKey).matches()) {
            throw new IllegalArgumentException(
                    "accessKey must look like uvp_ followed by 32 lowercase hex characters");
        }
        if (maxRetries < 0 || maxRetries > 5) {
            throw new IllegalArgumentException("maxRetries must be between 0 and 5");
        }
        if (slowReadTimeout.compareTo(readTimeout) < 0) {
            throw new IllegalArgumentException("slowReadTimeout must be >= readTimeout");
        }
    }

    private static String normalizeBaseUrl(String value) {
        String url = requireText(value, "baseUrl").trim();
        while (url.endsWith("/")) {
            url = url.substring(0, url.length() - 1);
        }
        if (!url.startsWith("http://") && !url.startsWith("https://")) {
            throw new IllegalArgumentException("baseUrl must start with http:// or https://");
        }
        return url;
    }

    private static String requireText(String value, String name) {
        if (value == null || value.trim().isEmpty()) {
            throw new IllegalArgumentException(name + " is required");
        }
        return value;
    }

    private static Duration positive(Duration value, String name) {
        if (value == null || value.isZero() || value.isNegative()) {
            throw new IllegalArgumentException(name + " must be positive");
        }
        return value;
    }

    public static Builder builder() {
        return new Builder();
    }

    /**
     * Shortcut for the common case: every value is known and no timeout needs tuning.
     *
     * @param audience the deployment-specific audience string, exactly as configured on the platform
     *                 ({@code openapi.audience}); it participates in the signature, so a mismatch
     *                 yields a plain 401 with no hint about the cause
     */
    public static UvpClientConfig of(String baseUrl, String accessKey, String secretKey, String audience) {
        return builder()
                .baseUrl(baseUrl)
                .accessKey(accessKey)
                .secretKey(secretKey)
                .audience(audience)
                .build();
    }

    public String getBaseUrl() {
        return baseUrl;
    }

    public String getAccessKey() {
        return accessKey;
    }

    /**
     * Base64url secret key. Exposed only so the signing layer can reach it; never log it and never
     * return it from an endpoint of your own.
     */
    public String getSecretKey() {
        return secretKey;
    }

    public String getAudience() {
        return audience;
    }

    public Duration getConnectTimeout() {
        return connectTimeout;
    }

    public Duration getReadTimeout() {
        return readTimeout;
    }

    public Duration getSlowReadTimeout() {
        return slowReadTimeout;
    }

    public int getMaxRetries() {
        return maxRetries;
    }

    public String getUserAgent() {
        return userAgent;
    }

    @Override
    public String toString() {
        // Never include the secret key, and never include the full access key.
        return "UvpClientConfig{baseUrl='" + baseUrl + "', accessKey='" + mask(accessKey)
                + "', audience='" + audience + "'}";
    }

    private static String mask(String value) {
        if (value.length() <= 8) {
            return "***";
        }
        return value.substring(0, 8) + "***";
    }

    public static final class Builder {
        private String baseUrl;
        private String accessKey;
        private String secretKey;
        private String audience;
        private Duration connectTimeout = Duration.ofSeconds(3);
        private Duration readTimeout = Duration.ofSeconds(15);
        private Duration slowReadTimeout = Duration.ofSeconds(60);
        private int maxRetries = 2;
        private String userAgent;

        private Builder() {
        }

        public Builder baseUrl(String value) {
            this.baseUrl = value;
            return this;
        }

        public Builder accessKey(String value) {
            this.accessKey = value;
            return this;
        }

        public Builder secretKey(String value) {
            this.secretKey = value;
            return this;
        }

        public Builder audience(String value) {
            this.audience = value;
            return this;
        }

        public Builder connectTimeout(Duration value) {
            this.connectTimeout = value;
            return this;
        }

        public Builder readTimeout(Duration value) {
            this.readTimeout = value;
            return this;
        }

        /**
         * Read timeout for the few operations that wait on the device (live start, playback start,
         * record query, download start, PTZ preset calls). Kept separate so that raising it does not
         * make every ordinary call hang for a minute.
         */
        public Builder slowReadTimeout(Duration value) {
            this.slowReadTimeout = value;
            return this;
        }

        /** Retries for GET only. Default 2. Set to 0 to disable. */
        public Builder maxRetries(int value) {
            this.maxRetries = value;
            return this;
        }

        public Builder userAgent(String value) {
            this.userAgent = value;
            return this;
        }

        public UvpClientConfig build() {
            return new UvpClientConfig(this);
        }
    }
}
