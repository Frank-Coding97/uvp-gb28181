package com.uvp.openapi.spring;

import com.uvp.openapi.client.UvpClient;
import com.uvp.openapi.client.UvpClientConfig;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.boot.autoconfigure.AutoConfiguration;
import org.springframework.boot.autoconfigure.condition.ConditionalOnExpression;
import org.springframework.boot.autoconfigure.condition.ConditionalOnMissingBean;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.context.annotation.Bean;

/**
 * Creates a {@link UvpClient} bean from {@code uvp.openapi.*}.
 *
 * <p>The bean is only created when {@code uvp.openapi.base-url} is set, so test profiles and
 * services that do not talk to a platform start without configuration. Once a base URL is present,
 * a missing key or audience fails the startup instead of turning every call into a 401 later.</p>
 */
@AutoConfiguration
@EnableConfigurationProperties(UvpOpenApiProperties.class)
@ConditionalOnProperty(prefix = "uvp.openapi", name = "enabled", havingValue = "true", matchIfMissing = true)
public class UvpOpenApiAutoConfiguration {

    private static final Logger log = LoggerFactory.getLogger(UvpOpenApiAutoConfiguration.class);

    @Bean(destroyMethod = "close")
    @ConditionalOnMissingBean
    // An empty value must not create the bean: a placeholder that resolves to "" is still
    // "present" as far as ConditionalOnProperty is concerned.
    @ConditionalOnExpression("!'${uvp.openapi.base-url:}'.trim().isEmpty()")
    public UvpClient uvpClient(UvpOpenApiProperties properties) {
        UvpClientConfig config = UvpClientConfig.builder()
                .baseUrl(properties.getBaseUrl())
                .accessKey(require(properties.getAccessKey(), "uvp.openapi.access-key"))
                .secretKey(require(properties.getSecretKey(), "uvp.openapi.secret-key"))
                .audience(require(properties.getAudience(), "uvp.openapi.audience"))
                .connectTimeout(properties.getConnectTimeout())
                .readTimeout(properties.getReadTimeout())
                .slowReadTimeout(properties.getSlowReadTimeout())
                .maxRetries(properties.getMaxRetries())
                .build();

        // Deliberately logs neither the secret key nor the full access key.
        log.info("UVP OpenAPI client ready: baseUrl={} accessKey={}... audience={}",
                config.getBaseUrl(), config.getAccessKey().substring(0, 8), config.getAudience());
        return new UvpClient(config);
    }

    private static String require(String value, String name) {
        if (value == null || value.trim().isEmpty()) {
            throw new IllegalStateException(name + " must be set when uvp.openapi.base-url is configured");
        }
        return value;
    }
}
