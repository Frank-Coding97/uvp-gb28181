package com.uvp.demo;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;

/**
 * Runnable reference project for the UVP OpenAPI Java SDK.
 *
 * <p>Set {@code UVP_BASE_URL}, {@code UVP_ACCESS_KEY}, {@code UVP_SECRET_KEY} and
 * {@code UVP_AUDIENCE}, then start the application and open
 * {@code http://localhost:8088/demo/devices}.</p>
 */
@SpringBootApplication
public class DemoApplication {

    public static void main(String[] args) {
        SpringApplication.run(DemoApplication.class, args);
    }
}
