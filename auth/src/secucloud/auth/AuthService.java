package secucloud.auth;

import java.security.SecureRandom;
import java.time.Duration;
import java.time.Instant;
import java.util.Base64;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

/** Framework-independent password authentication service. */
public final class AuthService {
    private static final Duration SESSION_LIFETIME = Duration.ofMinutes(30);
    private final CredentialStore credentialStore;
    private final Map<String, Session> sessions = new ConcurrentHashMap<>();
    private final SecureRandom random = new SecureRandom();

    public AuthService() {
        this(new CredentialStore());
    }

    public AuthService(CredentialStore credentialStore) {
        this.credentialStore = credentialStore;
    }

    public LoginResult authenticate(String email, String password) {
        String normalizedEmail = normalizeEmail(email);
        if (password == null || password.isEmpty()) {
            return LoginResult.failure("Le mot de passe est obligatoire.");
        }
        if (!credentialStore.hasUser(normalizedEmail)) {
            if (password.length() < 8) {
                return LoginResult.failure("Le mot de passe doit contenir au moins 8 caractères.");
            }
            String clientId = credentialStore.createUser(normalizedEmail, password);
            return LoginResult.authenticated(createSession(normalizedEmail, clientId), clientId);
        }
        if (!credentialStore.verifyPassword(normalizedEmail, password)) {
            return LoginResult.failure("Identifiant ou mot de passe incorrect.");
        }
        String clientId = credentialStore.getClientId(normalizedEmail);
        return LoginResult.authenticated(createSession(normalizedEmail, clientId), clientId);
    }

    public boolean isSessionValid(String sessionToken) {
        return getSession(sessionToken) != null;
    }

    public String getSessionClientId(String sessionToken) {
        Session session = getSession(sessionToken);
        return session == null ? null : session.clientId;
    }

    public void revokeSession(String sessionToken) {
        sessions.remove(sessionToken);
    }

    private Session getSession(String sessionToken) {
        Session session = sessions.get(sessionToken);
        if (session == null || Instant.now().isAfter(session.expiresAt)) {
            sessions.remove(sessionToken);
            return null;
        }
        return session;
    }

    private String createSession(String email, String clientId) {
        String token = Base64.getUrlEncoder().withoutPadding().encodeToString(randomBytes(32));
        sessions.put(token, new Session(email, clientId, Instant.now().plus(SESSION_LIFETIME)));
        return token;
    }

    private byte[] randomBytes(int length) {
        byte[] bytes = new byte[length];
        random.nextBytes(bytes);
        return bytes;
    }

    private String normalizeEmail(String email) {
        if (email == null) {
            throw new IllegalArgumentException("L'identifiant doit être une adresse e-mail valide.");
        }
        String normalized = email.trim().toLowerCase(java.util.Locale.ROOT);
        if (!normalized.matches("^[^\\s@]+@[^\\s@]+\\.[^\\s@]+$")) {
            throw new IllegalArgumentException("L'identifiant doit être une adresse e-mail valide.");
        }
        return normalized;
    }

    private record Session(String email, String clientId, Instant expiresAt) {
    }

    public static final class LoginResult {
        public enum Status { AUTHENTICATED, FAILURE }

        private final Status status;
        private final String message;
        private final String sessionToken;
        private final String clientId;

        private LoginResult(Status status, String message, String sessionToken, String clientId) {
            this.status = status;
            this.message = message;
            this.sessionToken = sessionToken;
            this.clientId = clientId;
        }

        public static LoginResult authenticated(String sessionToken, String clientId) {
            return new LoginResult(Status.AUTHENTICATED, null, sessionToken, clientId);
        }

        public static LoginResult failure(String message) {
            return new LoginResult(Status.FAILURE, message, null, null);
        }

        public Status getStatus() {
            return status;
        }

        public String getMessage() {
            return message;
        }

        public String getSessionToken() {
            return sessionToken;
        }

        public String getClientId() {
            return clientId;
        }
    }
}
