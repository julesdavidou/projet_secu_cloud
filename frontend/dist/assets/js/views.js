import { categories } from "./data.js";
import { icon } from "./icons.js";
import { escapeHTML as e, formatDate, routeURL, selectVideos, clamp, safeMediaURL } from "./utils.js";

const categoryName = id => categories.find(category => category.id === id)?.name || id;
const videoLink = (video, play = false) => routeURL(`${play ? "lecteur" : "video"}/${encodeURIComponent(video.id)}`);

export function renderHeader(profile, route) {
  if (route.name === "lecteur") return `<div class="player-header container"><a href="${routeURL(`video/${encodeURIComponent(route.id)}`)}">${icon("back")}Retour à la fiche</a><a class="player-brand" href="#/accueil">CaribouTV</a></div>`;
  const name = profile?.name || "Mon profil";
  const initials = profile?.name?.split(/\s+/).slice(0, 2).map(part => part[0]).join("") || "";
  const active = name => route.name === name || (name === "catalogue" && route.name === "video");
  return `<div class="header-inner container">
    <a class="brand" href="#/accueil" aria-label="CaribouTV, accueil">Caribou<span>TV</span></a>
    <nav class="site-nav" aria-label="Navigation principale">
      <a href="#/accueil" ${active("accueil") ? 'aria-current="page"' : ""}>Accueil</a>
      <a href="#/catalogue" ${active("catalogue") ? 'aria-current="page"' : ""}>Catalogue</a>
      <a class="desktop-category-link" href="#/categories" ${active("categories") ? 'aria-current="page"' : ""}>Catégories</a>
      <a class="mobile-profile-link" href="#/profil" ${active("profil") ? 'aria-current="page"' : ""}>Profil</a>
    </nav>
    <form class="header-search" data-search role="search" aria-label="Recherche rapide">
      <label class="search-field"><span class="sr-only">Rechercher une vidéo</span><input id="header-query" type="search" name="q" placeholder="Rechercher une vidéo" maxlength="120" autocomplete="off"><button class="search-submit" aria-label="Lancer la recherche">${icon("search")}</button></label>
    </form>
    <a class="header-search-link" href="#/recherche" aria-label="Rechercher une vidéo">${icon("search")}</a>
    <a class="profile-link" href="#/profil" aria-label="Ouvrir le profil de ${e(name)}"><span class="avatar" aria-hidden="true">${initials ? e(initials) : icon("user")}</span><span>${e(name)}</span></a>
  </div>`;
}

export function metadata(video) {
  return `<div class="metadata"><span class="category-label">${e(categoryName(video.category))}</span><span>${e(video.minutes)} min</span><time datetime="${e(video.publishedAt)}">${e(formatDate(video.publishedAt))}</time></div>`;
}

export function renderCard(video, profile, { home = false, progress = false } = {}) {
  const favorite = profile.favorites.includes(video.id);
  const watched = profile.history.find(item => item.videoId === video.id);
  const percent = watched ? clamp(watched.position / watched.duration * 100) : 0;
  const image = safeMediaURL(home ? video.homeImage || video.image : video.image);
  return `<article class="video-card" data-video-id="${e(video.id)}">
    <a class="video-card-link" href="${videoLink(video, progress)}" aria-label="${progress ? "Reprendre" : "Découvrir"} ${e(video.title)}, ${e(video.minutes)} minutes">
      <div class="thumbnail"><img src="${e(image)}" alt="" loading="lazy" width="640" height="360"><span class="card-play"><span>${icon("play")}</span></span><span class="duration-badge">${e(video.minutes)} min</span>${progress && percent ? `<div class="card-progress" aria-hidden="true"><span style="width:${percent}%"></span></div>` : ""}</div>
      <div class="card-body"><h3 class="card-title">${e(video.title)}</h3><div class="card-meta"><span class="category-label">${e(categoryName(video.category))}</span><time datetime="${e(video.publishedAt)}">${e(formatDate(video.publishedAt))}</time></div></div>
    </a>
    <button class="quick-favorite" data-action="favorite" data-id="${e(video.id)}" aria-pressed="${favorite}" aria-label="${favorite ? "Retirer des favoris" : "Ajouter aux favoris"} : ${e(video.title)}" title="${favorite ? "Retirer des favoris" : "Ajouter aux favoris"}">${icon("heart")}</button>
  </article>`;
}

export function renderRow(title, videos, profile, link, options = {}) {
  if (!videos.length) return "";
  return `<section class="video-section" aria-label="${e(title)}"><div class="section-heading"><h2>${e(title)}</h2>${link ? `<a class="button-link" href="${link}">Tout voir ${icon("arrow")}</a>` : ""}</div><div class="video-row">${videos.map(video => renderCard(video, profile, options)).join("")}</div></section>`;
}

function categoryTiles(videos, className) {
  return `<div class="${className}">${categories.map(category => {
    const count = videos.filter(video => video.category === category.id).length;
    return `<a class="category-tile tile-${category.color}" href="${routeURL("catalogue", { categorie: category.id })}"><strong>${e(category.name)}</strong><small>${count} vidéo${count > 1 ? "s" : ""}</small></a>`;
  }).join("")}</div>`;
}

export function renderHome({ videos, profile }) {
  const featured = selectVideos(videos)[0];
  if (!featured) return `<div class="page container empty-home"><section class="empty-home-hero" aria-labelledby="empty-home-title"><div class="empty-home-copy"><span class="eyebrow">La VOD du campus</span><h1 id="empty-home-title">Le campus<br>en images.</h1><p>Les moments à partager, les projets à découvrir, les idées qui nous rassemblent.</p><div class="empty-home-status" role="status">${icon("empty")}<div><h2>Le catalogue est encore vide</h2><p>Aucune vidéo n’est disponible pour le moment.</p></div></div><a class="button button-primary" href="#/catalogue">Explorer le catalogue ${icon("arrow")}</a></div><div class="empty-home-art" aria-hidden="true"><span class="art-circle"></span><span class="art-screen">${icon("play")}</span><span class="art-dot"></span></div></section><section class="categories-section" aria-label="Catégories"><div class="section-heading"><h2>Explorer par catégorie</h2><a class="button-link" href="#/categories">Toutes les catégories ${icon("arrow")}</a></div>${categoryTiles(videos, "categories-row")}</section></div>`;

  const recent = selectVideos(videos).slice(0, 4);
  const popular = selectVideos(videos, { sort: "popular" }).slice(0, 4);
  const recommended = selectVideos(videos, { sort: "title" }).slice(0, 4);
  return `<section class="hero" aria-labelledby="featured-title"><img class="hero-image" src="${e(safeMediaURL(featured.heroImage || featured.image))}" alt="" width="1600" height="900" fetchpriority="high"><div class="hero-content container"><span class="badge">À la une</span><h1 id="featured-title">${e(featured.title)}</h1><p class="hero-description">${e(featured.description)}</p>${metadata(featured)}<div class="button-row"><a class="button button-primary" href="${videoLink(featured, true)}">${icon("play")}Regarder</a><a class="button button-secondary" href="${videoLink(featured)}">Voir les détails</a></div></div></section>
    <div class="home-content container">${renderRow("Récemment ajoutées", recent, profile, "#/catalogue", { home: true })}${renderRow("Populaires sur le campus", popular, profile, routeURL("catalogue", { tri: "popular" }), { home: true })}${renderRow("À découvrir", recommended.length ? recommended : videos.slice(0, 4), profile, "#/catalogue", { home: true })}<section class="categories-section" aria-label="Catégories"><div class="section-heading"><h2>Explorer par catégorie</h2><a class="button-link" href="#/categories">Toutes les catégories ${icon("arrow")}</a></div>${categoryTiles(videos, "categories-row")}</section></div>`;
}

function filters(route, mode) {
  const selected = route.params.get("categorie") || "";
  const short = route.params.get("courtes") === "1";
  const choices = mode === "search" ? categories.filter(category => ["culture", "evenements"].includes(category.id)) : categories;
  return `<div class="filter-list" role="group" aria-label="Filtrer les vidéos"><button class="pill" data-action="category" data-category="" aria-pressed="${!selected}">Tout</button>${choices.map(category => `<button class="pill" data-action="category" data-category="${category.id}" aria-pressed="${selected === category.id}">${e(category.name === "Projets étudiants" ? "Projets" : category.name)}</button>`).join("")}${mode === "search" ? `<button class="pill" data-action="short" aria-pressed="${short}">Moins de 20 min</button>` : ""}</div>`;
}

function searchField(id, query, live = false, large = false) {
  return `<form data-search role="search" class="${large ? "page-search" : "catalogue-search"}" aria-label="Rechercher dans le catalogue"><label class="search-field ${large ? "large-search" : ""}"><span class="sr-only">${large ? "Votre recherche" : "Rechercher dans le catalogue"}</span><input id="${id}" type="search" name="q" value="${e(query)}" placeholder="Rechercher une vidéo" maxlength="120" autocomplete="off" ${live ? 'data-live-search="true"' : ""}><button class="search-submit" aria-label="Lancer la recherche">${icon("search")}</button>${query ? `<button type="button" class="clear-search" data-action="clear-query" aria-label="Effacer la recherche">${icon("close")}</button>` : ""}</label></form>`;
}

export function renderEmpty({ title = "Aucune vidéo trouvée", message = "Essayez un autre mot-clé ou retirez un filtre.", action = "filters" } = {}) {
  return `<div class="feedback-panel empty-state" role="status"><span class="feedback-icon">${icon("empty")}</span><div><h2>${e(title)}</h2><p>${e(message)}</p></div>${action === "none" ? "" : action === "filters" ? '<button class="button button-secondary button-small" data-action="clear-filters">Effacer les filtres</button>' : '<a class="button button-secondary button-small" href="#/catalogue">Explorer le catalogue</a>'}</div>`;
}

export function renderCatalogue({ videos, profile, route, visibleCount }, search = false) {
  const query = route.params.get("q") || "";
  const category = route.params.get("categorie") || "";
  const filtered = selectVideos(videos, { query, category, short: route.params.get("courtes") === "1", sort: route.params.get("tri") || "recent" });
  const items = filtered.slice(0, visibleCount);
  const summary = query ? `${filtered.length} résultat${filtered.length > 1 ? "s" : ""} pour « ${query} »` : `${filtered.length} vidéo${filtered.length > 1 ? "s" : ""} à découvrir`;
  return `<div class="page container ${search ? "search-page" : "catalogue-page"}">
    ${search ? `<h1>Rechercher sur CaribouTV</h1>${searchField("search-query", query, true, true)}<div class="results-summary"><p role="status">${e(summary)}</p>${filters(route, "search")}</div>` : `<div class="catalogue-intro"><div class="page-intro"><span class="eyebrow">Toute la VOD du campus</span><h1>Explorer le catalogue</h1><p>${videos.length} vidéos à découvrir, créées par la communauté.</p></div>${searchField("catalogue-query", query, true)}</div><div class="filter-toolbar">${filters(route, "catalogue")}<label class="sort-label"><span class="sr-only">Trier les vidéos</span><select id="sort-videos" data-sort><option value="recent" ${!route.params.get("tri") || route.params.get("tri") === "recent" ? "selected" : ""}>Trier : Ajout récent</option><option value="popular" ${route.params.get("tri") === "popular" ? "selected" : ""}>Trier : Popularité</option><option value="duration" ${route.params.get("tri") === "duration" ? "selected" : ""}>Trier : Durée</option><option value="title" ${route.params.get("tri") === "title" ? "selected" : ""}>Trier : A → Z</option></select>${icon("chevron")}</label></div>${query || category ? `<p class="sr-only" role="status">${e(summary)}</p>` : ""}`}
    ${items.length ? `<div class="video-grid" id="video-results">${items.map(video => renderCard(video, profile)).join("")}</div><div class="pagination"><p>${items.length} vidéo${items.length > 1 ? "s" : ""} sur ${filtered.length}</p><div class="pagination-track" aria-hidden="true"><span style="width:${items.length / filtered.length * 100}%"></span></div>${items.length < filtered.length ? '<button class="button button-secondary button-small" data-action="load-more">Charger plus de vidéos</button>' : ""}</div>` : renderEmpty(videos.length ? {} : { title: "Le catalogue est encore vide", message: "Aucune vidéo n’est disponible pour le moment.", action: "none" })}
    ${search && items.length && query ? `<aside class="feedback-panel"><span class="feedback-icon">${icon("search")}</span><div><h2>Vous cherchez autre chose ?</h2><p>Essayez un thème plus large comme « campus », « sport » ou « associations ».</p></div><button class="button button-secondary button-small" data-action="clear-filters">Effacer les filtres</button></aside>` : ""}
  </div>`;
}

export function renderCategories({ videos }) {
  return `<div class="page container"><div class="page-intro"><span class="eyebrow">À chacun ses découvertes</span><h1>Toutes les catégories</h1><p>Des terrains de sport aux coulisses du campus, suivez vos envies.</p></div>${categoryTiles(videos, "categories-page-grid")}</div>`;
}

export function renderDetail({ videos, profile, route }) {
  const video = videos.find(item => item.id === route.id);
  if (!video) return renderNotFound("Cette vidéo n’est plus disponible.");
  const favorite = profile.favorites.includes(video.id);
  const similar = videos.filter(item => item.id !== video.id).sort((a, b) => Number(b.category === video.category) - Number(a.category === video.category)).slice(0, 4);
  return `<div class="page container"><section class="detail-intro"><a class="detail-poster" href="${videoLink(video, true)}" aria-label="Regarder ${e(video.title)}"><img src="${e(safeMediaURL(video.detailImage || video.image))}" alt="Affiche de ${e(video.title)}" width="800" height="500" fetchpriority="high"><span class="poster-play"><span>${icon("play")}</span></span></a><div class="detail-copy"><span class="badge">${e(categoryName(video.category))}</span><h1>${e(video.title)}</h1>${metadata(video)}<p class="detail-description">${e(video.description)}</p><p class="detail-access">Lecture en ligne</p><div class="button-row"><a class="button button-primary" href="${videoLink(video, true)}">${icon("play")}Regarder</a><button class="button button-secondary" data-action="favorite" data-id="${e(video.id)}" aria-pressed="${favorite}">${icon("heart")}<span>${favorite ? "Dans mes favoris" : "Ajouter aux favoris"}</span></button></div></div></section><div class="credits"><dl><dt>Réalisation</dt><dd>${e(video.creator)}</dd></dl><dl><dt>Avec</dt><dd>${e(video.contributors)}</dd></dl><dl><dt>Campus</dt><dd>${e(video.campus)}</dd></dl><dl><dt>Publication</dt><dd>${e(formatDate(video.publishedAt, true))}</dd></dl></div>${renderRow("Vidéos similaires", similar, profile, routeURL("catalogue", { categorie: video.category }))}</div>`;
}

function renderHistory(history, videos) {
  const items = history.map(entry => ({ ...entry, video: videos.find(video => video.id === entry.videoId) })).filter(entry => entry.video);
  return `<ul class="history-list">${items.map(({ video, position, duration }) => `<li><a class="history-item" href="${videoLink(video, true)}"><span class="history-dot" aria-hidden="true"></span><span><span class="history-title">${e(video.title)}</span><span class="history-meta">${e(formatDate(video.publishedAt))} · ${e(video.minutes)} min</span></span><span class="history-action">${position / duration >= .95 ? "Terminé" : "Reprendre"}</span></a></li>`).join("")}</ul>`;
}

export function renderProfile({ videos, profile, route }) {
  const favorites = videos.filter(video => profile.favorites.includes(video.id));
  const history = profile.history.filter(item => videos.some(video => video.id === item.videoId));
  const continuing = history.filter(item => item.position > 0 && item.position / item.duration < .95).map(item => videos.find(video => video.id === item.videoId));
  const view = route.params.get("vue");
  if (view) {
    const title = { favoris: "Mes favoris", historique: "Mon historique", continuer: "Continuer à regarder" }[view] || "Mes vidéos";
    const selection = view === "favoris" ? favorites : continuing;
    return `<div class="page container"><a class="profile-back" href="#/profil">${icon("back")}Retour à mon profil</a><div class="page-intro"><h1>${title}</h1></div>${view === "historique" && history.length ? renderHistory(history, videos) : selection.length ? `<div class="video-grid">${selection.map(video => renderCard(video, profile, { progress: view === "continuer" })).join("")}</div>` : renderEmpty({ title: "Votre prochaine découverte vous attend", message: "Explorez le catalogue pour retrouver vos vidéos ici.", action: "catalogue" })}</div>`;
  }
  const minutes = Math.round(history.reduce((total, entry) => total + Math.min(entry.position, entry.duration), 0) / 60);
  const watchedTime = minutes >= 60 ? `${Math.floor(minutes / 60)} h ${String(minutes % 60).padStart(2, "0")}` : `${minutes} min`;
  const initials = profile.name.split(/\s+/).slice(0, 2).map(part => part[0]).join("");
  return `<div class="page container"><section class="profile-banner" aria-label="Mon profil"><span class="avatar profile-avatar" aria-hidden="true">${initials ? e(initials) : icon("user")}</span><div class="profile-identity"><span class="eyebrow">Mon espace</span><h1>${e(profile.name || "Mon profil")}</h1>${profile.course || profile.promotion ? `<p>${[profile.course, profile.promotion ? `Promotion ${profile.promotion}` : ""].filter(Boolean).map(e).join(" · ")}</p>` : ""}</div><div class="profile-stats"><div><strong>${history.length}</strong><small>vidéos vues</small></div><div><strong>${favorites.length}</strong><small>favoris</small></div><div><strong>${watchedTime}</strong><small>de lecture</small></div></div><button class="button button-secondary button-small" data-action="edit-profile">Modifier le profil</button></section>
    ${continuing.length ? renderRow("Continuer à regarder", continuing.slice(0, 4), profile, routeURL("profil", { vue: "continuer" }), { progress: true }) : `<section class="video-section"><div class="section-heading"><h2>Continuer à regarder</h2></div>${renderEmpty({ title: "Et si vous lanciez une vidéo ?", message: "Votre progression sera conservée pour reprendre plus tard.", action: "catalogue" })}</section>`}
    ${favorites.length ? renderRow("Mes favoris", favorites.slice(0, 4), profile, routeURL("profil", { vue: "favoris" })) : `<section class="video-section"><div class="section-heading"><h2>Mes favoris</h2></div>${renderEmpty({ title: "Gardez vos découvertes à portée de main", message: "Touchez le cœur d’une vidéo pour l’ajouter à vos favoris.", action: "catalogue" })}</section>`}
    <section class="video-section" aria-label="Historique"><div class="section-heading"><h2>Historique</h2>${history.length ? '<button class="button-link" data-action="clear-history">Effacer l’historique '+icon("arrow")+'</button>' : ""}</div>${history.length ? renderHistory(history, videos) : '<div class="feedback-panel"><p>Votre historique est vide. Vos prochaines lectures apparaîtront ici.</p></div>'}</section></div>`;
}

export function renderPlayerPage({ videos, profile, route }) {
  const video = videos.find(item => item.id === route.id);
  if (!video) return renderNotFound("Cette vidéo n’est plus disponible.");
  const next = videos.filter(item => item.id !== video.id).sort((a, b) => Number(b.category === video.category) - Number(a.category === video.category)).slice(0, 4);
  return `<div class="player-stage"><div class="container"><div class="player-shell" id="player-shell" role="region" aria-label="Lecteur vidéo : ${e(video.title)}"><video class="player-video" id="video-player" preload="metadata" playsinline poster="${e(safeMediaURL(video.playerImage || video.image))}" aria-label="${e(video.title)}" tabindex="0"></video><button class="player-toggle" id="player-toggle" aria-label="Lire la vidéo"><span>${icon("play")}</span></button><div class="player-controls"><label class="sr-only" for="player-seek">Position de lecture</label><input class="player-seek" id="player-seek" type="range" min="0" max="100" value="0" step="0.1" aria-valuetext="00:00"><div class="controls-row"><button class="control-button" id="player-play" aria-label="Lire">${icon("play")}</button><button class="control-button" id="player-mute" aria-label="Couper le son">${icon("volume")}</button><label class="sr-only" for="player-volume">Volume</label><input id="player-volume" class="volume-slider" type="range" min="0" max="1" step="0.05" value="1"><span class="player-time" id="player-time">00:00 / 00:00</span><span class="player-controls-spacer"></span><span class="quality-tag" id="player-quality">Source</span><button class="control-button" id="player-settings-toggle" aria-label="Paramètres de lecture" aria-expanded="false" aria-controls="player-settings">${icon("settings")}</button><button class="control-button" id="player-fullscreen" aria-label="Plein écran">${icon("fullscreen")}</button></div></div><div class="player-settings" id="player-settings" hidden><h3>Paramètres de lecture</h3><label>Vitesse<select id="playback-rate"><option value="0.75">0,75×</option><option value="1" selected>Normale</option><option value="1.25">1,25×</option><option value="1.5">1,5×</option><option value="2">2×</option></select></label><label id="quality-setting" hidden>Qualité<select id="quality-select"></select></label><label id="subtitle-setting" hidden>Sous-titres<select id="subtitle-select"><option value="off">Désactivés</option></select></label><p>Raccourcis : espace pour lire, flèches pour avancer ou reculer, M pour le son.</p></div><div class="player-status" id="player-loading" role="status"><span class="spinner"></span>Chargement de la vidéo…</div><div class="player-error" id="player-error" role="alert" hidden><div>${icon("warning")}<h2>Impossible de charger la vidéo</h2><p id="player-error-text">Vérifiez votre connexion puis réessayez. Votre progression est conservée.</p><button class="button button-primary" id="player-retry">${icon("retry")}Réessayer</button></div></div></div></div></div><div class="player-description container"><h1>${e(video.title)}</h1>${metadata(video)}<p>${e(video.description)}</p>${renderRow("À regarder ensuite", next, profile, routeURL("catalogue", { categorie: video.category }))}</div>`;
}

export function renderNotFound(message = "Cette page n’existe pas ou a changé d’adresse.") {
  return `<div class="page container"><div class="page-intro"><span class="eyebrow">Une petite pause dans votre parcours</span><h1>Ce contenu s’est égaré</h1></div>${renderEmpty({ title: "Retrouvons votre prochaine vidéo", message, action: "catalogue" })}</div>`;
}

export function renderLoading() {
  return `<div class="page container" aria-busy="true"><h1 class="sr-only">Chargement du catalogue</h1><div class="skeleton skeleton-heading" aria-hidden="true"></div><div class="video-grid" aria-hidden="true">${Array.from({ length: 8 }, () => '<div class="skeleton-card"><div class="skeleton"></div><div class="skeleton skeleton-line"></div><div class="skeleton skeleton-line short"></div></div>').join("")}</div><p class="loading-status" role="status"><span class="spinner"></span>Chargement des vidéos…</p></div>`;
}

export function renderError(message) {
  return `<div class="page container"><h1>Vos vidéos arrivent…</h1><div class="feedback-panel error" role="alert"><span class="feedback-icon">${icon("warning")}</span><div><h2>Impossible de charger le catalogue</h2><p>${e(message)}</p></div><button class="button button-primary" data-action="retry">${icon("retry")}Réessayer</button></div></div>`;
}
