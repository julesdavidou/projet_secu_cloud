import { config } from "./config.js";
import { emptyProfile } from "./data.js";

const clone = value => structuredClone(value);

export class APIError extends Error {
  constructor(message, status = 0) { super(message); this.name = "APIError"; this.status = status; }
}

export function createAPIClient({ baseUrl = "/api", timeoutMs = 12000, fetchImpl = globalThis.fetch } = {}) {
  return async function request(path, { method = "GET", body } = {}) {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), timeoutMs);
    try {
      const response = await fetchImpl(`${baseUrl.replace(/\/$/, "")}${path}`, {
        method, signal: controller.signal, credentials: "include",
        headers: { Accept: "application/json", ...(body !== undefined ? { "Content-Type": "application/json" } : {}) },
        ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
      });
      if (!response.ok) {
        const messages = { 401: "Votre session a expiré. Reconnectez-vous pour continuer.", 403: "Vous n’avez pas accès à ce contenu.", 404: "Ce contenu n’est plus disponible." };
        throw new APIError(messages[response.status] || "Le service est momentanément indisponible. Réessayez dans un instant.", response.status);
      }
      if (response.status === 204) return null;
      return await response.json();
    } catch (error) {
      if (error instanceof APIError) throw error;
      if (controller.signal.aborted) throw new APIError("Le serveur met trop de temps à répondre. Vérifiez votre connexion puis réessayez.");
      throw new APIError("Impossible de joindre CaribouTV. Vérifiez votre connexion puis réessayez.");
    } finally { clearTimeout(timeout); }
  };
}

export function createLocalProvider({ storage, storageKey = config.storageKey, videos = [], initialProfile = emptyProfile, playback = null } = {}) {
  const validIds = new Set(videos.map(video => video.id));
  let currentProfile;
  let storageAvailable = Boolean(storage);
  try {
    const raw = storage?.getItem(storageKey);
    const saved = raw ? JSON.parse(raw) : null;
    if (saved && typeof saved === "object" && !Array.isArray(saved)) {
      currentProfile = {
        ...clone(initialProfile),
        name: typeof saved.name === "string" && saved.name.trim() ? saved.name.slice(0, 60) : initialProfile.name,
        course: typeof saved.course === "string" ? saved.course.slice(0, 90) : initialProfile.course,
        promotion: typeof saved.promotion === "string" ? saved.promotion.slice(0, 4) : initialProfile.promotion,
        favorites: Array.isArray(saved.favorites) ? [...new Set(saved.favorites.filter(id => validIds.has(id)))] : clone(initialProfile.favorites),
        history: Array.isArray(saved.history) ? saved.history.filter(item => item && validIds.has(item.videoId) && Number.isFinite(item.position) && Number.isFinite(item.duration) && item.position >= 0 && item.duration > 0).slice(0, 100) : clone(initialProfile.history),
      };
    }
  } catch { /* Une donnée locale corrompue ne doit pas bloquer le site. */ }
  currentProfile ||= clone(initialProfile);

  function persist() {
    try { storage?.setItem(storageKey, JSON.stringify(currentProfile)); }
    catch { storageAvailable = false; }
    return clone(currentProfile);
  }

  return {
    mode: "empty",
    get storageAvailable() { return storageAvailable; },
    async listVideos() { return clone(videos); },
    async getProfile() { return clone(currentProfile); },
    async updateProfile(values) {
      const name = String(values.name || "").trim();
      if (!name || name.length > 60) throw new APIError("Indiquez un nom de 1 à 60 caractères.");
      if (!/^20\d{2}$/.test(String(values.promotion))) throw new APIError("Indiquez une année de promotion valide.");
      currentProfile = { ...currentProfile, name, course: String(values.course || "").trim().slice(0, 90), promotion: String(values.promotion) };
      return persist();
    },
    async setFavorite(id, favorite) {
      if (!validIds.has(id)) throw new APIError("Vidéo introuvable.", 404);
      currentProfile.favorites = currentProfile.favorites.filter(item => item !== id);
      if (favorite) currentProfile.favorites.push(id);
      return persist();
    },
    async saveProgress(id, position, duration) {
      if (!validIds.has(id) || !Number.isFinite(duration) || duration <= 0) return clone(currentProfile);
      currentProfile.history = currentProfile.history.filter(item => item.videoId !== id);
      currentProfile.history.unshift({ videoId: id, position: Math.max(0, Math.min(duration, Number(position) || 0)), duration, updatedAt: new Date().toISOString() });
      currentProfile.history = currentProfile.history.slice(0, 100);
      return persist();
    },
    async clearHistory() { currentProfile.history = []; return persist(); },
    async getPlayback(id) {
      if (!validIds.has(id)) throw new APIError("Vidéo introuvable.", 404);
      if (!playback) throw new APIError("Aucune source de lecture disponible.", 404);
      return clone(playback);
    },
  };
}

export function createRemoteProvider(options) {
  const request = createAPIClient(options);
  const videoPath = id => `/videos/${encodeURIComponent(id)}`;
  return {
    mode: "api", storageAvailable: true,
    async listVideos() { const result = await request("/videos"); if (!Array.isArray(result.items)) throw new APIError("Le catalogue reçu est invalide."); return result.items; },
    getProfile: () => request("/me"),
    updateProfile: body => request("/me", { method: "PATCH", body }),
    setFavorite: (id, active) => request(`/me/favorites/${encodeURIComponent(id)}`, { method: active ? "PUT" : "DELETE" }),
    saveProgress: (id, position, duration) => request(`/me/progress/${encodeURIComponent(id)}`, { method: "PUT", body: { position, duration } }),
    clearHistory: () => request("/me/history", { method: "DELETE" }),
    getPlayback: id => request(`${videoPath(id)}/playback`),
  };
}

let localStorage;
try { localStorage = globalThis.localStorage; } catch { /* Navigation privée restrictive. */ }
export const provider = config.dataSource === "api"
  ? createRemoteProvider({ baseUrl: config.apiBaseUrl, timeoutMs: config.requestTimeoutMs })
  : createLocalProvider({ storage: localStorage });
