package com.uvp.openapi.spring;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.uvp.openapi.client.UvpClient;
import java.util.Base64;
import org.junit.jupiter.api.Test;
import org.springframework.boot.autoconfigure.AutoConfigurations;
import org.springframework.boot.test.context.runner.ApplicationContextRunner;

class UvpOpenApiAutoConfigurationTest {

    private static final String SECRET_KEY =
            Base64.getUrlEncoder().withoutPadding().encodeToString(new byte[32]);

    private final ApplicationContextRunner runner = new ApplicationContextRunner()
            .withConfiguration(AutoConfigurations.of(UvpOpenApiAutoConfiguration.class));

    @Test
    void createsNoClientWhenBaseUrlIsAbsent() {
        runner.run(context -> assertThat(context).doesNotHaveBean(UvpClient.class));
    }

    @Test
    void createsNoClientWhenBaseUrlIsBlank() {
        // A placeholder such as ${UVP_BASE_URL:} resolves to "", which still counts as "present"
        // to a plain ConditionalOnProperty; the empty check is what makes this case work.
        runner.withPropertyValues("uvp.openapi.base-url=")
                .run(context -> assertThat(context).doesNotHaveBean(UvpClient.class));
    }

    @Test
    void createsClientWhenFullyConfigured() {
        runner.withPropertyValues(
                        "uvp.openapi.base-url=http://127.0.0.1:8280",
                        "uvp.openapi.access-key=uvp_000102030405060708090a0b0c0d0e0f",
                        "uvp.openapi.secret-key=" + SECRET_KEY,
                        "uvp.openapi.audience=test-audience")
                .run(context -> {
                    assertThat(context).hasSingleBean(UvpClient.class);
                    assertThat(context.getBean(UvpClient.class).config().getAudience())
                            .isEqualTo("test-audience");
                });
    }

    @Test
    void refusesToStartWhenAKeyIsMissing() {
        assertThatThrownBy(() -> runner.withPropertyValues(
                        "uvp.openapi.base-url=http://127.0.0.1:8280",
                        "uvp.openapi.access-key=uvp_000102030405060708090a0b0c0d0e0f",
                        "uvp.openapi.audience=test-audience")
                .run(context -> context.getBean(UvpClient.class)))
                .hasStackTraceContaining("uvp.openapi.secret-key");
    }

    @Test
    void canBeDisabled() {
        runner.withPropertyValues(
                        "uvp.openapi.enabled=false",
                        "uvp.openapi.base-url=http://127.0.0.1:8280",
                        "uvp.openapi.access-key=uvp_000102030405060708090a0b0c0d0e0f",
                        "uvp.openapi.secret-key=" + SECRET_KEY,
                        "uvp.openapi.audience=test-audience")
                .run(context -> assertThat(context).doesNotHaveBean(UvpClient.class));
    }
}
