import java.io.IOException;
import java.math.BigDecimal;
import java.nio.ByteBuffer;
import java.nio.CharBuffer;
import java.nio.charset.CharacterCodingException;
import java.nio.charset.Charset;
import java.nio.charset.CharsetEncoder;
import java.nio.charset.CharsetDecoder;
import java.nio.charset.CodingErrorAction;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Base64;
import java.util.Collections;
import java.util.Comparator;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;

/**
 * A standard-library-only Java 8 reference signer for UVP HMAC v1.
 *
 * It intentionally does not call the server auth package. Run
 * {@code java OpenAPISignExample.java --self-test} from either the repository
 * root or its server directory to consume the public golden fixture.
 * Keep SK only in the server-side secret store. If a GET result is unknown,
 * retry with a new timestamp, nonce, and signature; do not automatically
 * retry a POST whose result is unknown. Never put AK/SK or a signature into a
 * media URL or browser storage.
 */
public final class OpenAPISignExample {
    private static final int MAX_BODY_BYTES = 64 * 1024;
    private static final int MAX_QUERY_BYTES = 8 * 1024;
    private static final int MAX_JSON_DEPTH = 64;
    private static final Charset UTF8 = StandardCharsets.UTF_8;

    private OpenAPISignExample() {
    }

    public static void main(String[] args) throws Exception {
        if (args.length == 1 && "--self-test".equals(args[0])) {
            selfTest();
            System.out.println("OpenAPI signing self-test passed: 2 golden vectors");
            return;
        }
        System.err.println("usage: java OpenAPISignExample.java --self-test");
        System.exit(2);
    }

    /** The request fields covered by the v1 signature. The body is raw bytes. */
    public static final class Request {
        private final String method;
        private final String path;
        private final String rawQuery;
        private final String contentType;
        private final byte[] body;
        private final String accessKey;
        private final String timestamp;
        private final String nonce;
        private final String audience;

        public Request(String method, String path, String rawQuery, String contentType,
                byte[] body, String accessKey, String timestamp, String nonce, String audience) {
            this.method = method;
            this.path = path;
            this.rawQuery = rawQuery;
            this.contentType = contentType;
            this.body = body == null ? null : Arrays.copyOf(body, body.length);
            this.accessKey = accessKey;
            this.timestamp = timestamp;
            this.nonce = nonce;
            this.audience = audience;
        }

    }

    private static final class HeaderValues {
        private String[] signVersion = new String[0];
        private String[] accessKey = new String[0];
        private String[] timestamp = new String[0];
        private String[] nonce = new String[0];
        private String[] signature = new String[0];
        private String[] contentType = new String[0];
        private String[] contentEncoding = new String[0];
        private String[] methodOverride = new String[0];
    }

    private static String canonical(Request input) {
        require(input != null && input.body != null);
        String method = normalizeMethod(input.method);
        require(validAccessKey(input.accessKey));
        require(validTimestamp(input.timestamp));
        require(validNonce(input.nonce));
        require(validLineField(input.audience));
        require(validPath(input.path));
        require(input.body.length <= MAX_BODY_BYTES);
        String query = canonicalQuery(input.rawQuery);
        String contentType = input.contentType;
        if ("GET".equals(method)) {
            require(contentType.isEmpty() && input.body.length == 0);
        } else if ("POST".equals(method)) {
            require("application/json".equals(contentType));
        } else {
            require(contentType.isEmpty() || "application/json".equals(contentType));
        }
        if ("application/json".equals(contentType) && input.body.length > 0) {
            parseJson(input.body);
        }
        String bodyHash = hex(sha256(input.body));
        return joinLines("UVP-HMAC-SHA256/1", input.accessKey, input.timestamp, input.nonce,
                method, input.path, query, contentType, bodyHash, input.audience);
    }

    /** Signs one request with the decoded 32-byte base64url-no-padding SK. */
    public static String sign(Request input, String secretKey) {
        byte[] key = decodeSecretKey(secretKey);
        byte[] canonical = utf8(canonical(input));
        return hex(hmacSha256(key, canonical));
    }

    private static void validateHeaders(String method, HeaderValues values) {
        String normalizedMethod = normalizeMethod(method);
        require(values != null);
        String signVersion = oneHeader(values.signVersion);
        String accessKey = oneHeader(values.accessKey);
        String timestamp = oneHeader(values.timestamp);
        String nonce = oneHeader(values.nonce);
        String signature = oneHeader(values.signature);
        require("1".equals(signVersion) && validAccessKey(accessKey));
        require(validTimestamp(timestamp) && validNonce(nonce) && lowerHex(signature, 32));
        require(values.contentEncoding.length == 0 && values.methodOverride.length == 0);
        if ("GET".equals(normalizedMethod)) {
            require(values.contentType.length == 0);
        } else {
            require(values.contentType.length == 1 && "application/json".equals(values.contentType[0]));
        }
    }

    private static String oneHeader(String[] values) {
        require(values != null && values.length == 1);
        String value = values[0];
        require(value != null && !value.isEmpty() && value.trim().equals(value) && value.indexOf(',') < 0);
        return value;
    }

    private static byte[] decodeSecretKey(String secretKey) {
        require(secretKey != null && secretKey.matches("[A-Za-z0-9_-]{43}"));
        byte[] key;
        try {
            key = Base64.getUrlDecoder().decode(secretKey);
        } catch (IllegalArgumentException ex) {
            throw invalid();
        }
        require(key.length == 32 && Base64.getUrlEncoder().withoutPadding().encodeToString(key).equals(secretKey));
        return key;
    }

    private static String normalizeMethod(String value) {
        require(value != null && !value.isEmpty());
        for (int i = 0; i < value.length(); i++) {
            char c = value.charAt(i);
            require(c <= 0x7f && c > 0x20 && c != 0x7f);
        }
        return value.toUpperCase(Locale.ROOT);
    }

    private static boolean validAccessKey(String value) {
        return value != null && value.length() == 36 && value.startsWith("uvp_")
                && lowerHex(value.substring(4), 16);
    }

    private static boolean validTimestamp(String value) {
        if (value == null || value.isEmpty() || (value.length() > 1 && value.charAt(0) == '0')) {
            return false;
        }
        for (int i = 0; i < value.length(); i++) {
            if (value.charAt(i) < '0' || value.charAt(i) > '9') {
                return false;
            }
        }
        try {
            Long.parseLong(value);
            return true;
        } catch (NumberFormatException ex) {
            return false;
        }
    }

    private static boolean validNonce(String value) {
        return lowerHex(value, 16);
    }

    private static boolean validLineField(String value) {
        if (value == null || value.isEmpty()) {
            return false;
        }
        try {
            utf8(value);
        } catch (IllegalArgumentException ex) {
            return false;
        }
        for (int i = 0; i < value.length(); i++) {
            char c = value.charAt(i);
            if (c == '\r' || c == '\n' || c == 0 || c == 0x7f) {
                return false;
            }
        }
        return true;
    }

    private static boolean lowerHex(String value, int byteCount) {
        if (value == null || value.length() != byteCount * 2) {
            return false;
        }
        for (int i = 0; i < value.length(); i++) {
            char c = value.charAt(i);
            if (!((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'))) {
                return false;
            }
        }
        return true;
    }

    private static boolean validPath(String path) {
        if (path == null || path.isEmpty() || path.charAt(0) != '/') {
            return false;
        }
        for (int i = 0; i < path.length(); i++) {
            char c = path.charAt(i);
            if (c > 0x7f || c <= 0x20 || c == 0x7f || c == '%' || c == '\\') {
                return false;
            }
            if (i > 0 && c == '/' && path.charAt(i - 1) == '/') {
                return false;
            }
        }
        if (path.length() > 1 && path.endsWith("/")) {
            return false;
        }
        String[] segments = path.split("/", -1);
        for (String segment : segments) {
            if (".".equals(segment) || "..".equals(segment)) {
                return false;
            }
        }
        return true;
    }

    private static String canonicalQuery(String raw) {
        require(raw != null);
        require(utf8(raw).length <= MAX_QUERY_BYTES);
        if (raw.isEmpty()) {
            return "";
        }
        String[] parts = raw.split("&", -1);
        List<QueryPair> pairs = new ArrayList<QueryPair>(parts.length);
        Set<String> seen = new HashSet<String>();
        for (String part : parts) {
            require(!part.isEmpty() && part.indexOf(';') < 0);
            int separator = part.indexOf('=');
            require(separator >= 0);
            String rawKey = part.substring(0, separator);
            String rawValue = part.substring(separator + 1);
            require(rawKey.indexOf('+') < 0 && rawValue.indexOf('+') < 0);
            String key = encodeRfc3986(decodeQueryComponent(rawKey));
            String value = encodeRfc3986(decodeQueryComponent(rawValue));
            require(!key.isEmpty() && seen.add(key));
            pairs.add(new QueryPair(key, value));
        }
        Collections.sort(pairs, new Comparator<QueryPair>() {
            @Override
            public int compare(QueryPair left, QueryPair right) {
                int key = left.key.compareTo(right.key);
                return key != 0 ? key : left.value.compareTo(right.value);
            }
        });
        StringBuilder result = new StringBuilder();
        for (int i = 0; i < pairs.size(); i++) {
            if (i > 0) {
                result.append('&');
            }
            result.append(pairs.get(i).key).append('=').append(pairs.get(i).value);
        }
        return result.toString();
    }

    private static final class QueryPair {
        private final String key;
        private final String value;

        private QueryPair(String key, String value) {
            this.key = key;
            this.value = value;
        }
    }

    private static String decodeQueryComponent(String raw) {
        byte[] source = utf8(raw);
        byte[] decoded = new byte[source.length];
        int size = 0;
        for (int i = 0; i < source.length; i++) {
            if (source[i] != '%') {
                decoded[size++] = source[i];
                continue;
            }
            require(i + 2 < source.length);
            int high = fromHex(source[i + 1]);
            int low = fromHex(source[i + 2]);
            require(high >= 0 && low >= 0);
            decoded[size++] = (byte) ((high << 4) | low);
            i += 2;
        }
        return strictDecode(Arrays.copyOf(decoded, size));
    }

    private static int fromHex(byte value) {
        return fromHexAscii((char) (value & 0xff));
    }

    private static int fromHexAscii(char value) {
        if (value >= '0' && value <= '9') {
            return value - '0';
        }
        if (value >= 'a' && value <= 'f') {
            return value - 'a' + 10;
        }
        if (value >= 'A' && value <= 'F') {
            return value - 'A' + 10;
        }
        return -1;
    }

    private static boolean asciiDigit(char value) {
        return value >= '0' && value <= '9';
    }

    private static String encodeRfc3986(String value) {
        byte[] bytes = utf8(value);
        final char[] hex = "0123456789ABCDEF".toCharArray();
        StringBuilder result = new StringBuilder(bytes.length);
        for (byte valueByte : bytes) {
            int c = valueByte & 0xff;
            if ((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
                    || c == '-' || c == '.' || c == '_' || c == '~') {
                result.append((char) c);
            } else {
                result.append('%').append(hex[c >>> 4]).append(hex[c & 0x0f]);
            }
        }
        return result.toString();
    }

    private static String joinLines(String... values) {
        StringBuilder result = new StringBuilder();
        for (int i = 0; i < values.length; i++) {
            if (i > 0) {
                result.append('\n');
            }
            result.append(values[i]);
        }
        return result.toString();
    }

    private static byte[] utf8(String value) {
        require(value != null);
        try {
            CharsetEncoder encoder = UTF8.newEncoder().onMalformedInput(CodingErrorAction.REPORT)
                    .onUnmappableCharacter(CodingErrorAction.REPORT);
            ByteBuffer bytes = encoder.encode(CharBuffer.wrap(value));
            byte[] result = new byte[bytes.remaining()];
            bytes.get(result);
            return result;
        } catch (CharacterCodingException ex) {
            throw invalid();
        }
    }

    private static String strictDecode(byte[] bytes) {
        CharsetDecoder decoder = UTF8.newDecoder().onMalformedInput(CodingErrorAction.REPORT)
                .onUnmappableCharacter(CodingErrorAction.REPORT);
        try {
            return decoder.decode(ByteBuffer.wrap(bytes)).toString();
        } catch (CharacterCodingException ex) {
            throw invalid();
        }
    }

    private static byte[] sha256(byte[] bytes) {
        try {
            return MessageDigest.getInstance("SHA-256").digest(bytes);
        } catch (NoSuchAlgorithmException ex) {
            throw new IllegalStateException(ex);
        }
    }

    private static byte[] hmacSha256(byte[] key, byte[] value) {
        try {
            Mac mac = Mac.getInstance("HmacSHA256");
            mac.init(new SecretKeySpec(key, "HmacSHA256"));
            return mac.doFinal(value);
        } catch (Exception ex) {
            throw new IllegalStateException(ex);
        }
    }

    private static String hex(byte[] bytes) {
        final char[] digits = "0123456789abcdef".toCharArray();
        char[] result = new char[bytes.length * 2];
        for (int i = 0; i < bytes.length; i++) {
            int value = bytes[i] & 0xff;
            result[i * 2] = digits[value >>> 4];
            result[i * 2 + 1] = digits[value & 0x0f];
        }
        return new String(result);
    }

    private static void parseJson(byte[] body) {
        new JsonParser(strictDecode(body)).parseDocument();
    }

    /*
     * This is not a general JSON library. It is a small, strict, bounded
     * validator for the published flat request body and the local fixture.
     * The depth limit is intentional: callers must not treat this example as
     * proof that arbitrary deep JSON is supported by the service.
     */
    private static final class JsonParser {
        private final String text;
        private int position;

        private JsonParser(String text) {
            this.text = text;
        }

        private Object parseDocument() {
            Object value = parseValue(0);
            skipWhitespace();
            require(position == text.length());
            return value;
        }

        private Object parseValue(int depth) {
            require(depth <= MAX_JSON_DEPTH);
            skipWhitespace();
            require(position < text.length());
            char current = text.charAt(position);
            if (current == '"') {
                return parseString();
            }
            if (current == '{') {
                return parseObject(depth);
            }
            if (current == '[') {
                return parseArray(depth);
            }
            if (text.startsWith("true", position)) {
                position += 4;
                return Boolean.TRUE;
            }
            if (text.startsWith("false", position)) {
                position += 5;
                return Boolean.FALSE;
            }
            if (text.startsWith("null", position)) {
                position += 4;
                return null;
            }
            return parseNumber();
        }

        private Map<String, Object> parseObject(int depth) {
            require(text.charAt(position++) == '{');
            Map<String, Object> result = new LinkedHashMap<String, Object>();
            skipWhitespace();
            if (consume('}')) {
                return result;
            }
            while (true) {
                skipWhitespace();
                require(position < text.length() && text.charAt(position) == '"');
                String key = parseString();
                require(!result.containsKey(key));
                skipWhitespace();
                require(consume(':'));
                result.put(key, parseValue(depth + 1));
                skipWhitespace();
                if (consume('}')) {
                    return result;
                }
                require(consume(','));
            }
        }

        private List<Object> parseArray(int depth) {
            require(text.charAt(position++) == '[');
            List<Object> result = new ArrayList<Object>();
            skipWhitespace();
            if (consume(']')) {
                return result;
            }
            while (true) {
                result.add(parseValue(depth + 1));
                skipWhitespace();
                if (consume(']')) {
                    return result;
                }
                require(consume(','));
            }
        }

        private String parseString() {
            require(consume('"'));
            StringBuilder result = new StringBuilder();
            while (position < text.length()) {
                char current = text.charAt(position++);
                if (current == '"') {
                    return result.toString();
                }
                require(current >= 0x20);
                if (current != '\\') {
                    result.append(current);
                    continue;
                }
                require(position < text.length());
                char escaped = text.charAt(position++);
                switch (escaped) {
                    case '"':
                    case '\\':
                    case '/':
                        result.append(escaped);
                        break;
                    case 'b':
                        result.append('\b');
                        break;
                    case 'f':
                        result.append('\f');
                        break;
                    case 'n':
                        result.append('\n');
                        break;
                    case 'r':
                        result.append('\r');
                        break;
                    case 't':
                        result.append('\t');
                        break;
                    case 'u':
                        char escapedCode = parseUnicodeEscape();
                        if (Character.isHighSurrogate(escapedCode)) {
                            require(position + 1 < text.length() && text.charAt(position) == '\\'
                                    && text.charAt(position + 1) == 'u');
                            position += 2;
                            char lowCode = parseUnicodeEscape();
                            require(Character.isLowSurrogate(lowCode));
                            result.append(escapedCode).append(lowCode);
                        } else {
                            require(!Character.isLowSurrogate(escapedCode));
                            result.append(escapedCode);
                        }
                        break;
                    default:
                        throw invalid();
                }
            }
            throw invalid();
        }

        private char parseUnicodeEscape() {
            require(position + 4 <= text.length());
            int value = 0;
            for (int i = 0; i < 4; i++) {
                int digit = fromHexAscii(text.charAt(position++));
                require(digit >= 0);
                value = (value << 4) | digit;
            }
            return (char) value;
        }

        private BigDecimal parseNumber() {
            int start = position;
            if (consume('-')) {
                require(position < text.length());
            }
            if (consume('0')) {
                require(position >= text.length() || !asciiDigit(text.charAt(position)));
            } else {
                require(position < text.length() && text.charAt(position) >= '1' && text.charAt(position) <= '9');
                while (position < text.length() && asciiDigit(text.charAt(position))) {
                    position++;
                }
            }
            if (consume('.')) {
                require(position < text.length() && asciiDigit(text.charAt(position)));
                while (position < text.length() && asciiDigit(text.charAt(position))) {
                    position++;
                }
            }
            if (position < text.length() && (text.charAt(position) == 'e' || text.charAt(position) == 'E')) {
                position++;
                if (position < text.length() && (text.charAt(position) == '+' || text.charAt(position) == '-')) {
                    position++;
                }
                require(position < text.length() && asciiDigit(text.charAt(position)));
                while (position < text.length() && asciiDigit(text.charAt(position))) {
                    position++;
                }
            }
            return new BigDecimal(text.substring(start, position));
        }

        private boolean consume(char expected) {
            if (position < text.length() && text.charAt(position) == expected) {
                position++;
                return true;
            }
            return false;
        }

        private void skipWhitespace() {
            while (position < text.length()) {
                char current = text.charAt(position);
                if (current != ' ' && current != '\t' && current != '\r' && current != '\n') {
                    return;
                }
                position++;
            }
        }
    }

    private static Map<String, Object> readFixture() throws IOException {
        byte[] data = Files.readAllBytes(findFixture());
        Object root = new JsonParser(strictDecode(data)).parseDocument();
        require(root instanceof Map);
        @SuppressWarnings("unchecked")
        Map<String, Object> fixture = (Map<String, Object>) root;
        return fixture;
    }

    private static Path findFixture() throws IOException {
        String override = System.getenv("UVP_OPENAPI_SIGNATURE_FIXTURE");
        LinkedHashSet<Path> candidates = new LinkedHashSet<Path>();
        if (override != null && !override.isEmpty()) {
            candidates.add(Paths.get(override));
        }
        Path current = Paths.get(System.getProperty("user.dir")).toAbsolutePath().normalize();
        for (Path base = current; base != null; base = base.getParent()) {
            candidates.add(base.resolve("server/app/openapi/auth/testdata/signature-v1.json"));
            candidates.add(base.resolve("app/openapi/auth/testdata/signature-v1.json"));
        }
        for (Path candidate : candidates) {
            if (Files.isRegularFile(candidate)) {
                return candidate;
            }
        }
        throw new IOException("signature-v1.json not found; set UVP_OPENAPI_SIGNATURE_FIXTURE");
    }

    private static void selfTest() throws Exception {
        Map<String, Object> fixture = readFixture();
        String accessKey = stringField(fixture, "accessKey");
        String secretKey = stringField(fixture, "secretKey");
        String decodedKeyHex = stringField(fixture, "decodedKeyHex");
        String timestamp = stringField(fixture, "timestamp");
        String nonce = stringField(fixture, "nonce");
        String audience = stringField(fixture, "audience");
        byte[] decodedKey = decodeSecretKey(secretKey);
        require(decodedKeyHex.equals(hex(decodedKey)));
        Object vectorsValue = fixture.get("vectors");
        require(vectorsValue instanceof List && ((List<?>) vectorsValue).size() == 2);
        @SuppressWarnings("unchecked")
        List<Object> vectors = (List<Object>) vectorsValue;
        for (Object value : vectors) {
            require(value instanceof Map);
            @SuppressWarnings("unchecked")
            Map<String, Object> vector = (Map<String, Object>) value;
            Request request = request(accessKey, timestamp, nonce, audience, vector);
            String expectedCanonical = stringField(vector, "canonical");
            String gotCanonical = canonical(request);
            require(expectedCanonical.equals(gotCanonical));
            require(Integer.valueOf(stringOrNumber(vector, "canonicalBytes")).intValue() == utf8(gotCanonical).length);
            require(stringField(vector, "bodySHA256").equals(hex(sha256(request.body))));
            require(stringField(vector, "signature").equals(hex(hmacSha256(decodedKey, utf8(expectedCanonical)))));
            require(stringField(vector, "signature").equals(sign(request, secretKey)));
        }

        @SuppressWarnings("unchecked")
        Map<String, Object> first = (Map<String, Object>) vectors.get(0);
        Request base = request(accessKey, timestamp, nonce, audience, first);
        for (String badQuery : new String[] {"a+b=1", "=value", "missing-equals", "a=1&a=2", "a=%ZZ", "a=%FF"}) {
            final Request bad = new Request(base.method, base.path, badQuery, base.contentType, base.body,
                    base.accessKey, base.timestamp, base.nonce, base.audience);
            expectFailure(new Action() {
                @Override
                public void run() {
                    sign(bad, secretKey);
                }
            });
        }
        for (String badPath : new String[] {"/openapi/%76%31/devices", "/openapi/v1//devices",
                "/openapi/v1/devices/", "/openapi/v1\\devices", "/openapi/v1/../devices"}) {
            final Request bad = new Request(base.method, badPath, base.rawQuery, base.contentType, base.body,
                    base.accessKey, base.timestamp, base.nonce, base.audience);
            expectFailure(new Action() {
                @Override
                public void run() {
                    sign(bad, secretKey);
                }
            });
        }
        for (final String badSecret : new String[] {"", secretKey + "=", secretKey.replace('A', '+'), "not-base64url", "AA"}) {
            expectFailure(new Action() {
                @Override
                public void run() {
                    sign(base, badSecret);
                }
            });
        }

        Request post = request(accessKey, timestamp, nonce, audience, mapAt(vectors, 1));
        final Request duplicateBody = new Request(post.method, post.path, post.rawQuery, post.contentType,
                utf8("{\"protocol\":\"https-flv\",\"protocol\":\"https-flv\"}"),
                post.accessKey, post.timestamp, post.nonce, post.audience);
        expectFailure(new Action() {
            @Override
            public void run() {
                sign(duplicateBody, secretKey);
            }
        });
        final Request loneSurrogate = new Request(post.method, post.path, post.rawQuery, post.contentType,
                utf8("{\"protocol\":\"" + "\\u" + "D800\"}"),
                post.accessKey, post.timestamp, post.nonce, post.audience);
        expectFailure(new Action() {
            @Override
            public void run() {
                sign(loneSurrogate, secretKey);
            }
        });
        final Request nonAsciiUnicodeEscape = new Request(post.method, post.path, post.rawQuery, post.contentType,
                utf8("{\"protocol\":\"" + "\\u" + "İ234\"}"),
                post.accessKey, post.timestamp, post.nonce, post.audience);
        expectFailure(new Action() {
            @Override
            public void run() {
                sign(nonAsciiUnicodeEscape, secretKey);
            }
        });
        for (final String nonAsciiNumber : new String[] {"١", "１"}) {
            final Request nonAsciiNumberBody = new Request(post.method, post.path, post.rawQuery, post.contentType,
                    utf8("{\"number\":" + nonAsciiNumber + "}"),
                    post.accessKey, post.timestamp, post.nonce, post.audience);
            expectFailure(new Action() {
                @Override
                public void run() {
                    sign(nonAsciiNumberBody, secretKey);
                }
            });
        }
        final Request invalidUtf8 = new Request(post.method, post.path, post.rawQuery, post.contentType,
                new byte[] {'{', '"', 'p', 'r', 'o', 't', 'o', 'c', 'o', 'l', '"', ':', '"', (byte) 0xff, '"', '}'},
                post.accessKey, post.timestamp, post.nonce, post.audience);
        expectFailure(new Action() {
            @Override
            public void run() {
                sign(invalidUtf8, secretKey);
            }
        });
        StringBuilder deeplyNestedBody = new StringBuilder();
        for (int i = 0; i < 16384; i++) {
            deeplyNestedBody.append('[');
        }
        deeplyNestedBody.append('0');
        for (int i = 0; i < 16384; i++) {
            deeplyNestedBody.append(']');
        }
        final Request deeplyNested = new Request(post.method, post.path, post.rawQuery, post.contentType,
                utf8(deeplyNestedBody.toString()), post.accessKey, post.timestamp, post.nonce, post.audience);
        require(deeplyNested.body.length <= MAX_BODY_BYTES);
        expectFailure(new Action() {
            @Override
            public void run() {
                sign(deeplyNested, secretKey);
            }
        });

        Request lowerMethod = new Request("get", base.path, base.rawQuery, base.contentType, base.body,
                base.accessKey, base.timestamp, base.nonce, base.audience);
        require(canonical(lowerMethod).equals(canonical(base)));
        Request reordered = new Request(base.method, base.path,
                "pageSize=20&page=1&keyword=%E6%91%84%E5%83%8F%E6%9C%BA%20A", base.contentType, base.body,
                base.accessKey, base.timestamp, base.nonce, base.audience);
        require(canonical(reordered).equals(canonical(base)));
        Request emptyValue = new Request(base.method, base.path, "empty=&a=1", base.contentType, base.body,
                base.accessKey, base.timestamp, base.nonce, base.audience);
        require(canonical(emptyValue).endsWith("\na=1&empty=\n\n" + stringField(first, "bodySHA256") + "\n" + audience));
        HeaderValues valid = new HeaderValues();
        valid.signVersion = new String[] {"1"};
        valid.accessKey = new String[] {accessKey};
        valid.timestamp = new String[] {timestamp};
        valid.nonce = new String[] {nonce};
        valid.signature = new String[] {stringField(first, "signature")};
        validateHeaders("GET", valid);
        HeaderValues duplicateHeader = copyHeaders(valid);
        duplicateHeader.accessKey = new String[] {accessKey, accessKey};
        expectHeaderFailure(duplicateHeader, "GET");
        HeaderValues combinedHeader = copyHeaders(valid);
        combinedHeader.signature = new String[] {stringField(first, "signature") + ",other"};
        expectHeaderFailure(combinedHeader, "GET");
        HeaderValues uppercaseSignature = copyHeaders(valid);
        uppercaseSignature.signature = new String[] {stringField(first, "signature").toUpperCase(Locale.ROOT)};
        expectHeaderFailure(uppercaseSignature, "GET");
        HeaderValues contentEncoding = copyHeaders(valid);
        contentEncoding.contentEncoding = new String[] {"gzip"};
        expectHeaderFailure(contentEncoding, "GET");

        HeaderValues validPost = copyHeaders(valid);
        validPost.signature = new String[] {stringField(mapAt(vectors, 1), "signature")};
        validPost.contentType = new String[] {"application/json"};
        validateHeaders("POST", validPost);
        HeaderValues duplicateContentType = copyHeaders(validPost);
        duplicateContentType.contentType = new String[] {"application/json", "application/json"};
        expectHeaderFailure(duplicateContentType, "POST");

        String[] bodies = {"{\"protocol\":\"https-flv\"}", "{ \"protocol\": \"https-flv\" }",
                "{\"protocol\":\"https-flv\",\"scope\":\"live\"}"};
        String previous = null;
        for (String body : bodies) {
            Request changed = new Request(post.method, post.path, post.rawQuery, post.contentType,
                    utf8(body), post.accessKey, post.timestamp, post.nonce, post.audience);
            String current = sign(changed, secretKey);
            require(previous == null || !previous.equals(current));
            previous = current;
        }
    }

    private static HeaderValues copyHeaders(HeaderValues source) {
        HeaderValues result = new HeaderValues();
        result.signVersion = copy(source.signVersion);
        result.accessKey = copy(source.accessKey);
        result.timestamp = copy(source.timestamp);
        result.nonce = copy(source.nonce);
        result.signature = copy(source.signature);
        result.contentType = copy(source.contentType);
        result.contentEncoding = copy(source.contentEncoding);
        result.methodOverride = copy(source.methodOverride);
        return result;
    }

    private static String[] copy(String[] values) {
        return values == null ? null : Arrays.copyOf(values, values.length);
    }

    private static Request request(String accessKey, String timestamp, String nonce, String audience,
            Map<String, Object> vector) {
        return new Request(stringField(vector, "method"), stringField(vector, "path"), stringField(vector, "rawQuery"),
                stringField(vector, "contentType"), utf8(stringField(vector, "body")), accessKey, timestamp, nonce, audience);
    }

    @SuppressWarnings("unchecked")
    private static Map<String, Object> mapAt(List<Object> values, int index) {
        require(index >= 0 && index < values.size() && values.get(index) instanceof Map);
        return (Map<String, Object>) values.get(index);
    }

    private static String stringField(Map<String, Object> object, String key) {
        Object value = object.get(key);
        require(value instanceof String);
        return (String) value;
    }

    private static String stringOrNumber(Map<String, Object> object, String key) {
        Object value = object.get(key);
        require(value instanceof String || value instanceof BigDecimal);
        return value.toString();
    }

    private interface Action {
        void run();
    }

    private static void expectFailure(Action action) {
        try {
            action.run();
        } catch (IllegalArgumentException expected) {
            return;
        }
        throw new AssertionError("invalid input was accepted");
    }

    private static void expectHeaderFailure(final HeaderValues values, final String method) {
        expectFailure(new Action() {
            @Override
            public void run() {
                validateHeaders(method, values);
            }
        });
    }

    private static void require(boolean condition) {
        if (!condition) {
            throw invalid();
        }
    }

    private static IllegalArgumentException invalid() {
        return new IllegalArgumentException("invalid OpenAPI signing input");
    }
}
