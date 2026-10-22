package com.uvp.openapi.spring;

import java.time.Duration;
import org.springframework.boot.context.properties.ConfigurationProperties;

/**
 * Connection settings, bound from {@code uvp.openapi.*}.
 *
 * <pre>{@code
 * uvp:
 *   openapi:
 *     base-url: http://192.168.1.220:8280
 *     access-key: ${UVP_ACCESS_KEY:}
 *     secret-key: ${UVP_SECRET_KEY:}
 *     audience: uvp-prod-site-a
 * }</pre>
 */
@ConfigurationProperties(prefix = "uvp.openapi")
public class UvpOpenApiProperties {

    /** Whether to create the client at all. Set to false in tests or in profiles without a platform. */
    private boolean enabled = true;

    /** Platform base URL, without the {@code /openapi/v1} suffix. */
    private String baseUrl;

    /** Access key issued by the platform, shaped {@code uvp_} + 32 hex characters. */
    private String accessKey;

    /** Secret key shown once when the access key was created. Keep it in a secret store. */
    private String secretKey;

    /**
     * Deployment-specific audience string ({@code openapi.audience} on the platform side). It
     * participates in the signature: a wrong value produces a plain 401 with no explanation, so it
     * is worth surfacing in the startup check.
     */
    private String audience;

    private Duration connectTimeout = Duration.ofSeconds(3);

    /** Read timeout for ordinary calls. */
    private Duration readTimeout = Duration.ofSeconds(15);

    /** Read timeout for operations that wait on the device: live start, playback, record query. */
    private Duration slowReadTimeout = Duration.ofSeconds(60);

    /** Retries for GET only. Writes are never retried automatically. */
    private int maxRetries = 2;

    public boolean isEnabled() {
        return enabled;
    }

    public void setEnabled(boolean enabled) {
        this.enabled = enabled;
    }

    public String getBaseUrl() {
        return baseUrl;
    }

    public void setBaseUrl(String baseUrl) {
        this.baseUrl = baseUrl;
    }

    public String getAccessKey() {
        return accessKey;
    }

    public void setAccessKey(String accessKey) {
        this.accessKey = accessKey;
    }

    public String getSecretKey() {
        return secretKey;
    }

    public void setSecretKey(String secretKey) {
        this.secretKey = secretKey;
    }

    public String getAudience() {
        return audience;
    }

    public void setAudience(String audience) {
        this.audience = audience;
    }

    public Duration getConnectTimeout() {
        return connectTimeout;
    }

    public void setConnectTimeout(Duration connectTimeout) {
        this.connectTimeout = connectTimeout;
    }

    public Duration getReadTimeout() {
        return readTimeout;
    }

    public void setReadTimeout(Duration readTimeout) {
        this.readTimeout = readTimeout;
    }

    public Duration getSlowReadTimeout() {
        return slowReadTimeout;
    }

    public void setSlowReadTimeout(Duration slowReadTimeout) {
        this.slowReadTimeout = slowReadTimeout;
    }

    public int getMaxRetries() {
        return maxRetries;
    }

    public void setMaxRetries(int maxRetries) {
        this.maxRetries = maxRetries;
    }
}
