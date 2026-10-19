# CaribouTV — Front spectateur

Interface responsive en HTML, CSS et JavaScript natifs, réalisée à partir des exports SVG de la maquette fournie. Aucun backend métier ni framework n’est inclus.

## Démarrer

Avec Node.js 20 ou plus récent, depuis ce dossier :

```sh
npm run dev
```

Ouvrir **http://127.0.0.1:5173/**. Aucune dépendance à installer. `Ctrl+C` arrête le serveur statique de développement. Ne pas ouvrir directement `index.html` avec `file://` : les modules JavaScript nécessitent HTTP.

Sous PowerShell, si le port est occupé : `$env:PORT = '5174'; npm run dev`. Si la politique PowerShell bloque `npm.ps1`, utiliser `npm.cmd run dev`.

Le site déployable est dans `dist/`. Ces fichiers peuvent être servis par un hébergeur statique ou par le futur serveur web Go. `dev-server.mjs` sert seulement ces fichiers : il ne gère ni comptes, ni catalogue, ni stockage métier.

## Version livrée : catalogue vide

La configuration par défaut est `dataSource: "empty"`. Le front démarre avec **zéro vidéo, zéro favori et un historique vide**, sans identité étudiante inventée. Aucune vidéo ni vignette de démonstration n’est livrée dans `dist/`.

Ce mode permet d’afficher les pages avant la disponibilité de l’API web. **Il ne lit pas encore le stockage réel**. Lors du branchement à l’API, un stockage sans vidéo doit produire `GET /api/videos → { "items": [] }` ; le même état vide sera affiché. Une erreur réseau affiche une erreur avec possibilité de réessayer, et ne devient jamais un catalogue fictif ou silencieusement vide.

Les catégories reprennent les rubriques de la maquette ; leurs compteurs sont calculés à partir des données. Les anciennes données de l’aperçu de démonstration ne sont pas réutilisées. Les modifications volontaires du profil en mode vide sont conservées uniquement dans ce navigateur, sous la clé `cariboutv.viewer.empty.v1` ; cela ne constitue pas un compte connecté.

Les données synthétiques des tests sont définies uniquement dans `tests/` et ne sont jamais importées par le site. Les médias et jeux de données utilisés pendant la conception ne font pas partie de cette livraison.

## Pages et interactions

- `#/accueil` : accueil sans vignette et catégories ; mise en avant et rangées de vidéos lorsque l’API fournit du contenu.
- `#/catalogue` : recherche, filtres par catégorie, tri et pagination.
- `#/recherche?q=texte` : recherche insensible à la casse et aux accents ; filtres combinables.
- `#/categories` : toutes les catégories et leurs effectifs réels.
- `#/video/:id` : fiche, métadonnées, favoris et vidéos similaires. Sans contenu correspondant, page indisponible.
- `#/lecteur/:id` : lecteur HTML avec lecture/pause, volume, position, vitesse, plein écran et reprise ; nécessite une vidéo fournie par l’API.
- `#/profil` : favoris, historique, reprise et formulaire de profil ; aucune activité préremplie.

Le routage par fragment permet les liens directs et les boutons précédent/suivant sans règle de réécriture du serveur. L’interface couvre mobile, tablette et desktop. Les débordements présents dans certains exports mobiles sont corrigés. La navigation au clavier, les libellés accessibles, les états de chargement et d’erreur et la réduction des animations sont pris en compte.

L’authentification, l’inscription, l’administration et le dépôt de vidéos ne sont pas implémentés. Cette V1 couvre le parcours spectateur de la maquette.

## Brancher le backend Go

Dans `dist/assets/js/config.js`, passer `dataSource` à `"api"` et adapter `apiBaseUrl` si nécessaire. L’adaptateur `dist/assets/js/api.js` centralise les appels `fetch`, la session et les erreurs.

Les routes suivantes constituent un **contrat proposé pour le futur backend web**. Elles ne sont pas ajoutées au Go existant :

| Méthode | Route | Réponse attendue |
| --- | --- | --- |
| GET | `/api/videos` | `{ "items": [] }` ou une liste de vidéos |
| GET | `/api/me` | `Profile` |
| PATCH | `/api/me` | `Profile` mis à jour |
| PUT / DELETE | `/api/me/favorites/:id` | `Profile` ou `204` |
| PUT | `/api/me/progress/:id` | `Profile` ou `204` ; corps `{ "position": 12, "duration": 52 }` en secondes |
| DELETE | `/api/me/history` | `Profile` ou `204` |
| GET | `/api/videos/:id/playback` | `{ "url": "/media/video.mp4", "sources": [], "subtitles": [] }` |

Une mutation avec réponse `204` déclenche un rechargement du profil. Un objet vidéo contient : `id`, `title`, `category`, `minutes`, `publishedAt` (date ISO), `views`, `description`, `tags`, `image`, `campus`, `creator` et `contributors`. Les variantes `homeImage`, `heroImage`, `detailImage` et `playerImage` sont facultatives et utilisent `image` à défaut.

Les catégories autorisées par l’interface sont : `vie-etudiante`, `evenements`, `sport`, `culture`, `projets`, `conferences`, `campus` et `associations`.

Un profil sans activité a cette forme :

```json
{
  "name": "",
  "course": "",
  "promotion": "",
  "favorites": [],
  "history": []
}
```

L’API remplit l’identité réelle. Chaque favori est un identifiant vidéo ; une entrée d’historique contient `videoId`, `position`, `duration` et `updatedAt`.

Les variantes de qualité (`sources: [{ "label": "720p", "url": "..." }]`) et les sous-titres (`subtitles: [{ "language": "fr", "label": "Français", "url": "...vtt" }]`) n’apparaissent que lorsqu’ils sont fournis. Le navigateur doit pouvoir lire le format vidéo, par exemple MP4 ou WebM. Un futur lecteur HLS/DASH adaptatif nécessitera une intégration dédiée.

Le navigateur appelle **l’API web du control plane Go**, qui contrôle les autorisations et fournit les URL de lecture. Il ne contacte pas MQTT directement. Les API internes de l’Edge Agent ne remplacent pas le catalogue et les sessions du site.

`credentials: "include"` prépare une session en cookie ; le backend devra gérer l’authentification et la protection des mutations. Préférer un même domaine pour le front et `/api`. En cas de domaines distincts, configurer CORS et les cookies côté Go.

## Vérifier

```sh
npm test
```

Les tests couvrent l’absence de données fictives dans le mode livré, la recherche combinée aux filtres, la persistance locale, le contrat HTTP, les refus d’accès, les délais et le serveur statique. Les tests d’intégration avec le stockage et les comptes réels attendront l’API Go.

Voir [ASSETS.md](ASSETS.md) pour la provenance des ressources.
