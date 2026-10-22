package com.uvp.openapi.client;

import com.google.gson.Gson;
import com.uvp.openapi.api.AlarmApi;
import com.uvp.openapi.api.ConfigApi;
import com.uvp.openapi.api.ControlApi;
import com.uvp.openapi.api.DeviceApi;
import com.uvp.openapi.api.LiveApi;
import com.uvp.openapi.api.PlaybackApi;
import com.uvp.openapi.api.PtzApi;
import com.uvp.openapi.api.TalkApi;
import com.uvp.openapi.internal.ApiInvoker;
import java.time.Clock;
import java.util.concurrent.TimeUnit;
import okhttp3.ConnectionPool;
import okhttp3.Dispatcher;
import okhttp3.OkHttpClient;

/**
 * Entry point of the SDK. One instance per platform + access key; create it once and share it.
 *
 * <pre>{@code
 * UvpClient client = new UvpClient(UvpClientConfig.of(
 *         "http://192.168.1.220:8280", accessKey, secretKey, audience));
 *
 * DeviceListResponse devices = client.devices().listDevices(1, 50, null, "online");
 * }</pre>
 *
 * <p>Thread-safe. {@link #close()} releases the connection pool; in Spring, expose it as a bean with
 * {@code destroyMethod = "close"} (the starter does this for you).</p>
 */
public final class UvpClient implements AutoCloseable {

    private final UvpClientConfig config;
    private final OkHttpClient fastClient;
    private final OkHttpClient slowClient;
    private final Dispatcher dispatcher;
    private final ConnectionPool connectionPool;
    private final ApiInvoker invoker;

    private final DeviceApi devices;
    private final LiveApi live;
    private final PlaybackApi playback;
    private final PtzApi ptz;
    private final TalkApi talk;
    private final AlarmApi alarms;
    private final ConfigApi configs;
    private final ControlApi control;

    public UvpClient(UvpClientConfig config) {
        this(config, Clock.systemUTC(), ApiInvoker.defaultGson());
    }

    public UvpClient(UvpClientConfig config, Clock clock) {
        this(config, clock, ApiInvoker.defaultGson());
    }

    public UvpClient(UvpClientConfig config, Clock clock, Gson gson) {
        this.config = config;
        this.dispatcher = new Dispatcher();
        // Enough for a polling loop plus a few interactive calls, without letting one stuck device
        // starve every other request.
        this.dispatcher.setMaxRequests(64);
        this.dispatcher.setMaxRequestsPerHost(32);
        this.connectionPool = new ConnectionPool(20, 5, TimeUnit.MINUTES);

        OkHttpClient.Builder base = new OkHttpClient.Builder()
                .dispatcher(dispatcher)
                .connectionPool(connectionPool)
                .connectTimeout(config.getConnectTimeout().toMillis(), TimeUnit.MILLISECONDS)
                .writeTimeout(config.getReadTimeout().toMillis(), TimeUnit.MILLISECONDS)
                .retryOnConnectionFailure(true);

        this.fastClient = base.build().newBuilder()
                .readTimeout(config.getReadTimeout().toMillis(), TimeUnit.MILLISECONDS)
                .build();
        this.slowClient = base.build().newBuilder()
                .readTimeout(config.getSlowReadTimeout().toMillis(), TimeUnit.MILLISECONDS)
                .build();

        this.invoker = new ApiInvoker(config, fastClient, slowClient, gson, clock);

        this.devices = new DeviceApi(invoker);
        this.live = new LiveApi(invoker);
        this.playback = new PlaybackApi(invoker);
        this.ptz = new PtzApi(invoker);
        this.talk = new TalkApi(invoker);
        this.alarms = new AlarmApi(invoker);
        this.configs = new ConfigApi(invoker);
        this.control = new ControlApi(invoker);
    }

    /** Devices, channels, status and storage cards. */
    public DeviceApi devices() {
        return devices;
    }

    /** Live preview: start or reuse a stream and get playable URLs. */
    public LiveApi live() {
        return live;
    }

    /** Record search, playback sessions and download sessions. */
    public PlaybackApi playback() {
        return playback;
    }

    /** PTZ direction, presets, cruise tracks, precise positioning. */
    public PtzApi ptz() {
        return ptz;
    }

    /** Voice talk and broadcast (SDP pass-through; media plane is not handled by this SDK). */
    public TalkApi talk() {
        return talk;
    }

    /** Alarms, status events, subscriptions and reported positions. */
    public AlarmApi alarms() {
        return alarms;
    }

    /** Device configuration, video parameters and firmware upgrade tasks. */
    public ConfigApi configs() {
        return configs;
    }

    /** Recording start/stop, alarm guard, reboot, storage card format, maintenance task polling. */
    public ControlApi control() {
        return control;
    }

    public UvpClientConfig config() {
        return config;
    }

    @Override
    public void close() {
        dispatcher.cancelAll();
        dispatcher.executorService().shutdown();
        connectionPool.evictAll();
    }
}
