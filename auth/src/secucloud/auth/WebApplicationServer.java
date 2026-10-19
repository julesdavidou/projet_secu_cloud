package secucloud.auth;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.net.URLDecoder;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.HashMap;
import java.util.Map;

/** Serves the web login and the authentication API for local integration. */
public final class WebApplicationServer {
    private final AuthService authService = new AuthService();
    private final Path webRoot;
    private final HttpServer server;

    public WebApplicationServer(Path webRoot, int port) throws IOException {
        this.webRoot = webRoot.toAbsolutePath().normalize();
        server = HttpServer.create(new InetSocketAddress("127.0.0.1", port), 0);
        server.createContext("/api/auth/login", this::login);
        server.createContext("/api/auth/session", this::session);
        server.createContext("/api/auth/logout", this::logout);
        server.createContext("/", this::staticFile);
    }

    public static void main(String[] args) throws IOException {
        Path root = Path.of(System.getProperty("web.root", "web"));
        int port = Integer.parseInt(System.getProperty("web.port", "8080"));
        WebApplicationServer application = new WebApplicationServer(root, port);
        application.server.start();
        System.out.println("Application web disponible sur http://127.0.0.1:" + port);
    }

    private void login(HttpExchange exchange) throws IOException {
        if (!isMethod(exchange, "POST")) {
            respond(exchange, 405, jsonError("Méthode non autorisée."));
            return;
        }
        Map<String, String> form = readForm(exchange);
        try {
            AuthService.LoginResult result = authService.authenticate(
                    form.get("email"), form.get("password"));
            respondResult(exchange, result);
        } catch (IllegalArgumentException exception) {
            respond(exchange, 400, jsonError(exception.getMessage()));
        } catch (IllegalStateException exception) {
            respond(exchange, 503, jsonError(exception.getMessage()));
        }
    }

    private void session(HttpExchange exchange) throws IOException {
        if (!isMethod(exchange, "GET")) {
            respond(exchange, 405, jsonError("Méthode non autorisée."));
            return;
        }
        String token = sessionToken(exchange);
        String clientId = token == null ? null : authService.getSessionClientId(token);
        respond(exchange, clientId == null ? 401 : 200,
            clientId == null ? "{}" : "{\"status\":\"AUTHENTICATED\",\"client_id\":\""
                + escape(clientId) + "\"}");
    }

    private void logout(HttpExchange exchange) throws IOException {
        if (!isMethod(exchange, "POST")) {
            respond(exchange, 405, jsonError("Méthode non autorisée."));
            return;
        }
        String token = sessionToken(exchange);
        if (token != null) {
            authService.revokeSession(token);
        }
        exchange.getResponseHeaders().add("Set-Cookie", expiredCookie());
        respond(exchange, 204, "");
    }

    private void respondResult(HttpExchange exchange, AuthService.LoginResult result) throws IOException {
        switch (result.getStatus()) {
            case AUTHENTICATED -> {
                exchange.getResponseHeaders().add("Set-Cookie", sessionCookie(result.getSessionToken()));
                respond(exchange, 200, "{\"status\":\"AUTHENTICATED\",\"client_id\":\""
                        + escape(result.getClientId()) + "\"}");
            }
            case FAILURE -> respond(exchange, 200, jsonError(result.getMessage()));
        }
    }

    private void staticFile(HttpExchange exchange) throws IOException {
        String requestPath = exchange.getRequestURI().getPath();
        if (requestPath.equals("/")) {
            requestPath = "/index.html";
        }
        if (requestPath.equals("/connected.html")
                && (sessionToken(exchange) == null
                || !authService.isSessionValid(sessionToken(exchange)))) {
            exchange.getResponseHeaders().add("Location", "/");
            respond(exchange, 302, "");
            return;
        }
        Path file = webRoot.resolve(requestPath.substring(1)).normalize();
        if (!file.startsWith(webRoot) || !Files.isRegularFile(file)) {
            respond(exchange, 404, "Not found");
            return;
        }
        byte[] content = Files.readAllBytes(file);
        exchange.getResponseHeaders().set("Content-Type", contentType(file));
        exchange.sendResponseHeaders(200, content.length);
        try (OutputStream output = exchange.getResponseBody()) {
            output.write(content);
        }
    }

    private Map<String, String> readForm(HttpExchange exchange) throws IOException {
        String body = new String(exchange.getRequestBody().readAllBytes(), StandardCharsets.UTF_8);
        Map<String, String> values = new HashMap<>();
        for (String part : body.split("&")) {
            String[] pair = part.split("=", 2);
            if (pair.length == 2) {
                values.put(URLDecoder.decode(pair[0], StandardCharsets.UTF_8),
                        URLDecoder.decode(pair[1], StandardCharsets.UTF_8));
            }
        }
        return values;
    }

    private boolean isMethod(HttpExchange exchange, String method) {
        return method.equals(exchange.getRequestMethod());
    }

    private String sessionToken(HttpExchange exchange) {
        String cookie = exchange.getRequestHeaders().getFirst("Cookie");
        if (cookie == null) {
            return null;
        }
        for (String item : cookie.split(";")) {
            String[] pair = item.trim().split("=", 2);
            if (pair.length == 2 && pair[0].equals("session")) {
                return pair[1];
            }
        }
        return null;
    }

    private void respond(HttpExchange exchange, int status, String body) throws IOException {
        byte[] content = body.getBytes(StandardCharsets.UTF_8);
        exchange.getResponseHeaders().set("Content-Type", "application/json; charset=UTF-8");
        exchange.sendResponseHeaders(status, content.length);
        try (OutputStream output = exchange.getResponseBody()) {
            output.write(content);
        }
    }

    private String sessionCookie(String token) {
        return "session=" + token + "; HttpOnly; SameSite=Strict; Path=/";
    }

    private String expiredCookie() {
        return "session=; Max-Age=0; HttpOnly; SameSite=Strict; Path=/";
    }

    private String jsonError(String message) {
        return "{\"status\":\"FAILURE\",\"message\":\"" + escape(message) + "\"}";
    }

    private String escape(String value) {
        if (value == null) {
            return "";
        }
        StringBuilder escaped = new StringBuilder(value.length());
        for (int index = 0; index < value.length(); index++) {
            char character = value.charAt(index);
            switch (character) {
                case '\\' -> escaped.append("\\\\");
                case '"' -> escaped.append("\\\"");
                case '\b' -> escaped.append("\\b");
                case '\f' -> escaped.append("\\f");
                case '\n' -> escaped.append("\\n");
                case '\r' -> escaped.append("\\r");
                case '\t' -> escaped.append("\\t");
                default -> {
                    if (character < 0x20) {
                        escaped.append(String.format("\\u%04x", (int) character));
                    } else {
                        escaped.append(character);
                    }
                }
            }
        }
        return escaped.toString();
    }

    private String contentType(Path file) {
        String name = file.getFileName().toString();
        if (name.endsWith(".html")) return "text/html; charset=UTF-8";
        if (name.endsWith(".js")) return "text/javascript; charset=UTF-8";
        if (name.endsWith(".css")) return "text/css; charset=UTF-8";
        return "application/octet-stream";
    }
}
