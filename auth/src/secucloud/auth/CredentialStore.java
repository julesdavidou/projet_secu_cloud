package secucloud.auth;

import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardOpenOption;
import java.security.GeneralSecurityException;
import java.security.MessageDigest;
import java.security.SecureRandom;
import java.security.spec.KeySpec;
import java.util.Base64;
import java.util.Properties;
import java.util.UUID;
import javax.crypto.SecretKeyFactory;
import javax.crypto.spec.PBEKeySpec;

/** Stores password verifiers locally, never plaintext passwords. */
public final class CredentialStore {
    private static final int ITERATIONS = 120_000;
    private static final int KEY_LENGTH = 256;
    private final Path file = Path.of(System.getProperty("user.home"), ".caribou-tv", "users.properties");
    private final Path legacyFile = Path.of(
            System.getProperty("user.home"), ".tp-crypto-avancee", "users.properties");

    public boolean hasUser(String username) {
        return load().containsKey(username);
    }

    public String createUser(String username, String password) {
        Properties users = load();
        byte[] salt = new byte[16];
        new SecureRandom().nextBytes(salt);
        String clientId = newClientId();
        users.setProperty(username, clientId + ":" + encode(salt) + ":"
                + encode(hash(password.toCharArray(), salt)));
        save(users);
        return clientId;
    }

    public String getClientId(String username) {
        Properties users = load();
        String stored = users.getProperty(username);
        if (stored == null) {
            return null;
        }
        String[] parts = stored.split(":", 4);
        if (parts.length == 3 && isClientId(parts[0])) {
            return parts[0];
        }
        if (parts.length < 2) {
            throw new IllegalStateException("Identifiants enregistrés invalides.");
        }
        String clientId = newClientId();
        users.setProperty(username, clientId + ":" + parts[0] + ":" + parts[1]);
        save(users);
        return clientId;
    }

    public boolean verifyPassword(String username, String password) {
        String stored = load().getProperty(username);
        if (stored == null) {
            return false;
        }
        String[] parts = stored.split(":", 4);
        int saltIndex = parts.length == 3 && isClientId(parts[0]) ? 1 : 0;
        int hashIndex = saltIndex + 1;
        if (parts.length <= hashIndex) {
            return false;
        }
        byte[] salt = Base64.getDecoder().decode(parts[saltIndex]);
        byte[] expected = Base64.getDecoder().decode(parts[hashIndex]);
        return MessageDigest.isEqual(expected, hash(password.toCharArray(), salt));
    }

    private String newClientId() {
        return "client-" + UUID.randomUUID();
    }

    private boolean isClientId(String value) {
        return value.startsWith("client-") && value.length() == 43;
    }

    private byte[] hash(char[] password, byte[] salt) {
        try {
            KeySpec spec = new PBEKeySpec(password, salt, ITERATIONS, KEY_LENGTH);
            return SecretKeyFactory.getInstance("PBKDF2WithHmacSHA256").generateSecret(spec).getEncoded();
        } catch (GeneralSecurityException exception) {
            throw new IllegalStateException("Impossible de sécuriser le mot de passe.", exception);
        }
    }

    private Properties load() {
        Properties users = new Properties();
        if (!Files.exists(file) && Files.exists(legacyFile)) {
            try (InputStream input = Files.newInputStream(legacyFile)) {
                users.load(input);
                save(users);
                return users;
            } catch (IOException exception) {
                throw new IllegalStateException("Impossible de migrer les identifiants Caribou TV.", exception);
            }
        }
        if (!Files.exists(file)) {
            return users;
        }
        try (InputStream input = Files.newInputStream(file)) {
            users.load(input);
            return users;
        } catch (IOException exception) {
            throw new IllegalStateException("Impossible de lire les identifiants.", exception);
        }
    }

    private void save(Properties users) {
        try {
            Files.createDirectories(file.getParent());
            try (OutputStream output = Files.newOutputStream(
                    file, StandardOpenOption.CREATE, StandardOpenOption.TRUNCATE_EXISTING)) {
                users.store(output, "Caribou TV credentials");
            }
        } catch (IOException exception) {
            throw new IllegalStateException("Impossible d'enregistrer le mot de passe.", exception);
        }
    }

    private String encode(byte[] value) {
        return Base64.getEncoder().encodeToString(value);
    }
}
