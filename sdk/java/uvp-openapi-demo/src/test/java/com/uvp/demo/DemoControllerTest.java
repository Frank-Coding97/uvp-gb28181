package com.uvp.demo;

import static org.mockito.ArgumentMatchers.any;
import static org.mockito.BDDMockito.given;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import com.uvp.openapi.api.DeviceApi;
import com.uvp.openapi.client.UvpApiException;
import com.uvp.openapi.client.UvpClient;
import com.uvp.openapi.client.UvpErrorKind;
import com.uvp.openapi.model.DeviceListResponse;
import org.junit.jupiter.api.Test;
import org.mockito.Mockito;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.test.web.servlet.MockMvc;

/**
 * Checks that the demo wires the SDK correctly and that platform failures become sensible HTTP
 * responses. Uses MockMvc rather than a real port so the test does not depend on binding one.
 */
@SpringBootTest
@AutoConfigureMockMvc
class DemoControllerTest {

    @Autowired
    private MockMvc mvc;

    @MockBean
    private UvpClient uvp;

    @Test
    void devicesEndpointReturnsPlatformPayload() throws Exception {
        DeviceListResponse response = new DeviceListResponse();
        response.setRequestId("req-1");

        DeviceApi api = Mockito.mock(DeviceApi.class);
        given(api.listDevices(any(), any(), any(), any())).willReturn(response);
        given(uvp.devices()).willReturn(api);

        mvc.perform(get("/demo/devices"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.requestId").value("req-1"));
    }

    @Test
    void platformAuthenticationFailureBecomesBadGatewayNotUnauthorized() throws Exception {
        // A platform-side 401 is our configuration problem, not the browser user's session
        // expiring; returning 401 would make the front end log the user out.
        DeviceApi api = throwingApi(new UvpApiException(UvpErrorKind.AUTHENTICATION, 401,
                "AUTHENTICATION_FAILED", "req-2", "signature mismatch", null, null));
        given(uvp.devices()).willReturn(api);

        mvc.perform(get("/demo/devices"))
                .andExpect(status().isBadGateway())
                .andExpect(jsonPath("$.kind").value("AUTHENTICATION"))
                .andExpect(jsonPath("$.requestId").value("req-2"));
    }

    @Test
    void notFoundIsForwardedAsNotFound() throws Exception {
        DeviceApi api = throwingApi(new UvpApiException(UvpErrorKind.NOT_FOUND, 404,
                "RESOURCE_NOT_FOUND", "req-3", "no such device", null, null));
        given(uvp.devices()).willReturn(api);

        mvc.perform(get("/demo/devices"))
                .andExpect(status().isNotFound())
                .andExpect(jsonPath("$.requestId").value("req-3"));
    }

    private DeviceApi throwingApi(RuntimeException failure) {
        DeviceApi api = Mockito.mock(DeviceApi.class);
        given(api.listDevices(any(), any(), any(), any())).willThrow(failure);
        return api;
    }
}
