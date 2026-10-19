import { provider } from "./api.js";
import { icon } from "./icons.js";
import { escapeHTML as e, parseRoute, routeURL } from "./utils.js";
import { renderHeader, renderHome, renderCatalogue, renderCategories, renderDetail, renderProfile, renderPlayerPage, renderNotFound, renderLoading, renderError } from "./views.js";
import { mountPlayer } from "./player.js";

const main = document.getElementById("contenu");
const header = document.getElementById("site-header");
const dialog = document.getElementById("app-dialog");
const toast = document.getElementById("toast");
const state = { status: "loading", videos: [], profile: null, error: "", visibleCount: 12 };
let destroyPlayer = () => {};
let toastTimeout;
let searchTimeout;
let loadGeneration = 0;

function notify(message) {
  clearTimeout(toastTimeout);
  toast.textContent = message;
  toast.classList.add("visible");
  toastTimeout = setTimeout(() => toast.classList.remove("visible"), 3500);
}

function updateParams(changes, { replace = true, focusId } = {}) {
  clearTimeout(searchTimeout);
  const route = parseRoute(location.hash);
  for (const [key, value] of Object.entries(changes)) {
    if (!value) route.params.delete(key);
    else route.params.set(key, String(value));
  }
  state.visibleCount = 12;
  const target = routeURL(route.name, Object.fromEntries(route.params));
  if (replace) { history.replaceState(null, "", target); render({ preserveScroll: true, focusId }); }
  else location.hash = target;
}

function rememberFocus() {
  const element = document.activeElement;
  if (element?.dataset?.action) {
    const action = element.dataset.action;
    const extra = element.dataset.category != null ? `[data-category="${CSS.escape(element.dataset.category)}"]` : "";
    return { selector: `[data-action="${CSS.escape(action)}"]${extra}` };
  }
  if (!element || !element.id || !["INPUT", "SELECT"].includes(element.tagName)) return null;
  return { id: element.id, start: element.selectionStart, end: element.selectionEnd, value: element.value };
}

function render({ preserveScroll = false, focusId } = {}) {
  const focus = preserveScroll ? rememberFocus() : null;
  const scroll = window.scrollY;
  const headerQuery = document.getElementById("header-query")?.value || "";
  destroyPlayer();
  destroyPlayer = () => {};
  const route = parseRoute(location.hash);
  const context = { ...state, route };
  document.body.classList.toggle("is-player", route.name === "lecteur");
  header.innerHTML = renderHeader(state.profile, route);
  if (headerQuery && document.getElementById("header-query")) document.getElementById("header-query").value = headerQuery;
  main.setAttribute("aria-busy", String(state.status === "loading"));
  if (state.status === "loading") main.innerHTML = renderLoading();
  else if (state.status === "error") main.innerHTML = renderError(state.error);
  else {
    const renders = { accueil: renderHome, catalogue: renderCatalogue, recherche: ctx => renderCatalogue(ctx, true), categories: renderCategories, video: renderDetail, profil: renderProfile, lecteur: ctx => renderPlayerPage(ctx) };
    main.innerHTML = (renders[route.name] || (() => renderNotFound()))(context);
    if (route.name === "lecteur") {
      const video = state.videos.find(item => item.id === route.id);
      if (video) destroyPlayer = mountPlayer(video, { provider, profile: state.profile, onProgress: profile => {
        state.profile = profile;
        if (parseRoute(location.hash).name === "profil") render({ preserveScroll: true });
      }, notify });
    }
  }
  const videoTitle = state.videos.find(video => video.id === route.id)?.title;
  const title = { accueil: "Le campus en images", catalogue: "Catalogue", recherche: "Recherche", categories: "Catégories", profil: "Mon profil", video: videoTitle, lecteur: videoTitle }[route.name] || "Page introuvable";
  document.title = `${title} — CaribouTV`;
  const focusTarget = focus?.selector && !focusId ? document.querySelector(focus.selector) : document.getElementById(focusId || focus?.id);
  if (focusTarget) {
    focusTarget.focus({ preventScroll: true });
    if (focus && focus.id === focusTarget.id && typeof focusTarget.setSelectionRange === "function" && focus.start != null) {
      try { focusTarget.setSelectionRange(focus.start, focus.end); } catch { /* Select sans sélection textuelle. */ }
    }
  } else if (!preserveScroll) main.focus({ preventScroll: true });
  window.scrollTo({ top: preserveScroll ? scroll : 0, behavior: "instant" });
}

async function loadData() {
  const generation = ++loadGeneration;
  state.status = "loading";
  render();
  try {
    const [videos, profile] = await Promise.all([provider.listVideos(), provider.getProfile()]);
    if (generation !== loadGeneration) return;
    if (!Array.isArray(videos) || !profile || !Array.isArray(profile.favorites) || !Array.isArray(profile.history)) throw new Error("Les données reçues ne peuvent pas être affichées.");
    state.videos = videos;
    state.profile = profile;
    state.status = "ready";
  } catch (error) {
    if (generation !== loadGeneration) return;
    state.status = "error";
    state.error = error.message;
  }
  render();
  if (state.status === "ready" && !provider.storageAvailable) notify("Vos changements seront conservés pour cette session uniquement.");
}

function syncFavoriteButtons(id, favorite) {
  for (const button of document.querySelectorAll('[data-action="favorite"]')) {
    if (button.dataset.id !== id) continue;
    button.setAttribute("aria-pressed", String(favorite));
    if (button.classList.contains("quick-favorite")) {
      const title = state.videos.find(video => video.id === id)?.title || "cette vidéo";
      button.setAttribute("aria-label", `${favorite ? "Retirer des favoris" : "Ajouter aux favoris"} : ${title}`);
      button.title = favorite ? "Retirer des favoris" : "Ajouter aux favoris";
    } else {
      const label = button.querySelector("span");
      if (label) label.textContent = favorite ? "Dans mes favoris" : "Ajouter aux favoris";
    }
  }
}

function openProfileDialog() {
  const profile = state.profile;
  dialog.innerHTML = `<div class="dialog-heading"><h2>Modifier mon profil</h2><button class="dialog-close" data-action="close-dialog" aria-label="Fermer">${icon("close")}</button></div><p class="dialog-description">Faites comme chez vous sur CaribouTV.</p><form id="profile-form"><label class="form-field">Nom affiché<input name="name" value="${e(profile.name)}" autocomplete="nickname" required maxlength="60"></label><label class="form-field">Formation<input name="course" value="${e(profile.course)}" maxlength="90" autocomplete="off"></label><label class="form-field">Promotion<input name="promotion" value="${e(profile.promotion)}" inputmode="numeric" pattern="20[0-9]{2}" maxlength="4" required aria-describedby="promotion-hint"><span class="sr-only" id="promotion-hint">Année de promotion sur quatre chiffres, par exemple 2027</span></label><p class="form-error" id="profile-form-error" role="alert" hidden></p><div class="dialog-actions"><button type="button" class="button button-secondary" data-action="close-dialog">Annuler</button><button class="button button-primary" type="submit">Enregistrer</button></div></form>`;
  dialog.showModal();
}

function confirmClearHistory() {
  dialog.innerHTML = `<div class="dialog-heading"><h2>Effacer l’historique ?</h2><button class="dialog-close" data-action="close-dialog" aria-label="Fermer">${icon("close")}</button></div><p class="dialog-description">Les vidéos regardées et leur progression seront retirées de votre historique. Vos favoris seront conservés.</p><div class="dialog-actions"><button class="button button-secondary" data-action="close-dialog">Annuler</button><button class="button button-primary" data-action="confirm-clear-history">Effacer l’historique</button></div>`;
  dialog.showModal();
}

document.addEventListener("click", async event => {
  if (event.target.closest(".skip-link")) {
    event.preventDefault();
    main.focus();
    main.scrollIntoView({ block: "start" });
    return;
  }
  const button = event.target.closest("[data-action]");
  if (!button || button.disabled) return;
  const action = button.dataset.action;
  const route = parseRoute(location.hash);
  try {
    if (action === "favorite") {
      const favorite = !state.profile.favorites.includes(button.dataset.id);
      button.disabled = true;
      const profile = await provider.setFavorite(button.dataset.id, favorite);
      state.profile = profile || await provider.getProfile();
      syncFavoriteButtons(button.dataset.id, favorite);
      if (route.name === "profil") render({ preserveScroll: true });
      notify(favorite ? "Vidéo ajoutée à vos favoris." : "Vidéo retirée de vos favoris.");
    }
    if (action === "category") updateParams({ categorie: button.dataset.category });
    if (action === "short") updateParams({ courtes: route.params.get("courtes") === "1" ? "" : "1" });
    if (action === "clear-query") updateParams({ q: "" }, { focusId: route.name === "recherche" ? "search-query" : "catalogue-query" });
    if (action === "clear-filters") updateParams({ q: "", categorie: "", courtes: "", tri: "" }, { focusId: route.name === "recherche" ? "search-query" : "catalogue-query" });
    if (action === "load-more") {
      state.visibleCount += 12;
      const count = state.visibleCount;
      render({ preserveScroll: true });
      const cards = main.querySelectorAll(".video-grid .video-card-link");
      cards[Math.max(0, count - 12)]?.focus({ preventScroll: true });
      notify("Les vidéos suivantes sont affichées.");
    }
    if (action === "edit-profile") openProfileDialog();
    if (action === "clear-history") confirmClearHistory();
    if (action === "close-dialog") dialog.close();
    if (action === "confirm-clear-history") {
      button.disabled = true;
      state.profile = await provider.clearHistory() || await provider.getProfile();
      dialog.close();
      render({ preserveScroll: true });
      notify("Votre historique a été effacé. Vos favoris sont conservés.");
    }
    if (action === "retry") loadData();
  } catch (error) { notify(error.message || "L’action n’a pas pu être enregistrée. Réessayez."); }
  finally { button.disabled = false; }
});

document.addEventListener("submit", async event => {
  const form = event.target;
  if (form.matches("[data-search]")) {
    event.preventDefault();
    clearTimeout(searchTimeout);
    const query = String(new FormData(form).get("q") || "").trim().slice(0, 120);
    const input = form.querySelector("[data-live-search]");
    if (input) updateParams({ q: query }, { focusId: input.id });
    else location.hash = routeURL("recherche", { q: query });
  }
  if (form.id === "profile-form") {
    event.preventDefault();
    const button = form.querySelector('[type="submit"]');
    const error = document.getElementById("profile-form-error");
    button.disabled = true;
    error.hidden = true;
    try {
      state.profile = await provider.updateProfile(Object.fromEntries(new FormData(form))) || await provider.getProfile();
      dialog.close();
      render({ preserveScroll: true });
      notify("Votre profil a été mis à jour.");
    } catch (problem) { error.textContent = problem.message; error.hidden = false; }
    finally { button.disabled = false; }
  }
});

document.addEventListener("input", event => {
  if (!event.target.matches("[data-live-search]")) return;
  const input = event.target;
  clearTimeout(searchTimeout);
  searchTimeout = setTimeout(() => updateParams({ q: input.value.slice(0, 120) }, { focusId: input.id }), 180);
});
document.addEventListener("change", event => { if (event.target.matches("[data-sort]")) updateParams({ tri: event.target.value }, { focusId: event.target.id }); });
dialog.addEventListener("click", event => { if (event.target === dialog) { const rect = dialog.getBoundingClientRect(); if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) dialog.close(); } });
window.addEventListener("hashchange", () => { clearTimeout(searchTimeout); state.visibleCount = 12; if (dialog.open) dialog.close(); render(); });
loadData();
