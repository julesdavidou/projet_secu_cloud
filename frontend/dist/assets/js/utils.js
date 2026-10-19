export function escapeHTML(value = "") {
  return String(value).replace(/[&<>"']/g, char => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[char]);
}

export function normalizeText(value = "") {
  return String(value).normalize("NFD").replace(/[\u0300-\u036f]/g, "").toLocaleLowerCase("fr").replace(/[’']/g, " ").trim();
}

export function selectVideos(videos, { query = "", category = "", short = false, sort = "recent" } = {}) {
  const words = normalizeText(query).split(/\s+/).filter(Boolean);
  const selected = videos.filter(video => {
    const searchable = normalizeText([video.title, video.description, video.campus, ...(video.tags || [])].join(" "));
    return (!category || video.category === category)
      && (!short || video.minutes < 20)
      && words.every(word => searchable.includes(word));
  });
  return selected.sort((a, b) => {
    if (sort === "popular") return b.views - a.views;
    if (sort === "duration") return a.minutes - b.minutes;
    if (sort === "title") return a.title.localeCompare(b.title, "fr");
    return b.publishedAt.localeCompare(a.publishedAt);
  });
}

export function formatDate(value, long = false) {
  return new Intl.DateTimeFormat("fr-FR", { day: "numeric", month: long ? "long" : "short", year: "numeric", timeZone: "UTC" }).format(new Date(`${String(value).slice(0, 10)}T12:00:00Z`));
}

export function formatTime(seconds = 0) {
  const value = Math.max(0, Math.floor(Number(seconds) || 0));
  return `${String(Math.floor(value / 60)).padStart(2, "0")}:${String(value % 60).padStart(2, "0")}`;
}

export function routeURL(path, params = {}) {
  const query = new URLSearchParams(Object.entries(params).filter(([, value]) => value !== "" && value !== false && value != null));
  return `#/${path}${query.size ? `?${query}` : ""}`;
}

export function parseRoute(hash = "") {
  try {
    const [pathname, query = ""] = hash.replace(/^#\/?/, "").split("?");
    const parts = pathname.split("/").filter(Boolean).map(decodeURIComponent);
    return { name: parts[0] || "accueil", id: parts[1] || "", params: new URLSearchParams(query) };
  } catch {
    return { name: "introuvable", id: "", params: new URLSearchParams() };
  }
}

export function safeMediaURL(value) {
  if (typeof value !== "string") return "";
  try {
    const url = new URL(value, globalThis.location?.href || "http://localhost/");
    return ["http:", "https:", "blob:"].includes(url.protocol) ? value : "";
  } catch { return ""; }
}

export function clamp(value, min = 0, max = 100) {
  return Math.min(max, Math.max(min, Number(value) || 0));
}
