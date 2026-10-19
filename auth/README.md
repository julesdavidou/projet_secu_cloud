# SecuCloud - Service d'authentification

Cette partie du projet fournit le service d'authentification utilisable par
le site web et les autres services. Elle ne contient pas la documentation de
l'interface web : le site est développé et intégré séparément.

## Rôle dans le projet

Le service :

- crée les comptes et vérifie les mots de passe ;
- attribue un `client_id` stable à chaque adresse e-mail ;
- crée et révoque des sessions HTTP ;
- permet aux autres composants de vérifier une session avant d'autoriser une
  opération ;
- fournit le `client_id` à utiliser comme identifiant logique pour MQTT et les
  autres segments du projet.

Le mot de passe n'est jamais transmis aux services métier. Après connexion,
ceux-ci utilisent uniquement la session validée et le `client_id`.

## Organisation

```text
src/secucloud/auth/
  AuthService.java          Règles d'authentification et gestion des sessions
  CredentialStore.java      Stockage local des identifiants et du client_id
  WebApplicationServer.java API HTTP d'authentification
README.md                   Documentation d'intégration
```

Le dossier `bin/` n'est pas versionné. Il contient les fichiers `.class`
générés lors de la compilation.

## Données et sessions

Les comptes locaux sont enregistrés dans :

```text
%USERPROFILE%\.caribou-tv\users.properties
```

Chaque entrée contient l'adresse e-mail, un `client_id`, un sel aléatoire et
un dérivé PBKDF2-HMAC-SHA256 du mot de passe. Le mot de passe en clair n'est
jamais enregistré.

Les sessions sont conservées en mémoire par `AuthService`. Elles sont donc
invalidées au redémarrage du service. Une session est valable 30 minutes et
peut être révoquée avec la déconnexion. Le `client_id`, lui, reste persistant.

Le token de session est opaque : aucun autre composant ne doit essayer de le
décoder ou d'en déduire l'adresse e-mail.

## Contrat d'API pour le site et les services

Les requêtes de connexion utilisent
`application/x-www-form-urlencoded`. Le token de session est transmis dans le
cookie HTTP nommé `session`.

### Connexion

```http
POST /api/auth/login
Content-Type: application/x-www-form-urlencoded

email=client%40example.com&password=********
```

En cas de succès, le serveur renvoie le cookie `session` dans `Set-Cookie` et
la réponse suivante :

```json
{
  "status": "AUTHENTICATED",
  "client_id": "client-..."
}
```

Si l'adresse n'existe pas encore, elle est créée lorsque le mot de passe
contient au moins 8 caractères. Une connexion incorrecte renvoie :

```json
{
  "status": "FAILURE",
  "message": "..."
}
```

### Vérification d'une session

```http
GET /api/auth/session
Cookie: session=<token>
```

Le code HTTP `200` indique que la session est valide :

```json
{
  "status": "AUTHENTICATED",
  "client_id": "client-..."
}
```

Le code HTTP `401` indique que la session est absente, expirée ou inconnue.
Le token et le mot de passe ne sont jamais renvoyés.

Un service métier doit vérifier cette réponse avant d'exécuter une opération
protégée. Il peut ensuite associer l'opération au `client_id` reçu.

### Déconnexion

```http
POST /api/auth/logout
Cookie: session=<token>
```

La session est révoquée côté serveur et le cookie est expiré côté client. La
réponse est `204`.

## Intégration avec le site web

Le site externe doit appeler ces endpoints et conserver le cookie `session`
entre les requêtes. Il ne doit pas stocker le mot de passe ni tenter de lire
un cookie `HttpOnly`.

Le serveur ajoute actuellement les attributs `HttpOnly`, `SameSite=Strict` et
`Path=/` au cookie. Si le site et l'API sont servis depuis des origines
différentes, l'intégration doit être faite via un reverse proxy commun ou une
configuration CORS et cookie adaptée au déploiement. Le serveur de
démonstration écoute uniquement sur `127.0.0.1`.

## Intégration avec les autres services

Un service qui reçoit directement une requête HTTP peut extraire le cookie
`session` et vérifier sa validité avec `AuthService.isSessionValid(...)`, puis
récupérer le `client_id` avec
`AuthService.getSessionClientId(...)`.

Pour un service dans un autre processus, il doit transmettre le cookie à
`/api/auth/session` :

```http
GET http://127.0.0.1:8082/api/auth/session
Cookie: session=<token>
```

L'opération est autorisée uniquement si la réponse est `200`. Le token de
session ne doit pas être envoyé dans MQTT, écrit dans les logs ou exposé à un
service qui n'en a pas besoin. Seul le `client_id` doit être utilisé comme
identifiant logique dans les messages MQTT.

## Compiler et lancer le service

Depuis la racine du projet :

```powershell
javac -d bin src\secucloud\auth\*.java
java "-Dweb.root=$PWD\web" "-Dweb.port=8082" -cp "bin" secucloud.auth.WebApplicationServer
```

Le paramètre `web.root` est conservé pour le serveur HTTP existant, mais
l'interface située dans `web/` n'est pas nécessaire à l'intégration : le site
externe peut utiliser uniquement les endpoints `/api/auth/*`.

Pour un déploiement partagé, utiliser HTTPS, ajouter `Secure` au cookie,
remplacer le stockage local par un stockage utilisateur persistant adapté et
prévoir un stockage de sessions partagé entre les instances du service.
