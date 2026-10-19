import { icon } from "./icons.js";
import { formatTime, clamp, safeMediaURL } from "./utils.js";

export function mountPlayer(item, { provider, profile, onProgress, notify }) {
  const video = document.getElementById("video-player");
  const shell = document.getElementById("player-shell");
  if (!video || !shell) return () => {};
  const controller = new AbortController();
  const { signal } = controller;
  const get = id => document.getElementById(id);
  const overlay = get("player-toggle");
  const play = get("player-play");
  const mute = get("player-mute");
  const seek = get("player-seek");
  const time = get("player-time");
  const volume = get("player-volume");
  const settings = get("player-settings");
  const settingsToggle = get("player-settings-toggle");
  const errorPanel = get("player-error");
  const loading = get("player-loading");
  const saved = profile.history.find(entry => entry.videoId === item.id);
  let disposed = false;
  let didPlay = false;
  let lastSave = 0;
  let playback;
  let resumePosition = null;
  let firstLoad = true;
  let continueAfterLoad = false;
  let savingErrorReported = false;

  function showError(message) {
    loading.hidden = true;
    errorPanel.hidden = false;
    get("player-error-text").textContent = message || "Vérifiez votre connexion puis réessayez. Votre progression est conservée.";
  }

  function save(force = false) {
    if (!didPlay || !Number.isFinite(video.duration) || video.duration <= 0) return;
    if (!force && performance.now() - lastSave < 2000) return;
    lastSave = performance.now();
    provider.saveProgress(item.id, video.currentTime, video.duration).then(async updated => {
      onProgress(updated || await provider.getProfile());
    }).catch(() => {
      if (!savingErrorReported) notify("Votre progression n’a pas pu être enregistrée. La lecture peut continuer.");
      savingErrorReported = true;
    });
  }

  function update() {
    const duration = Number.isFinite(video.duration) ? video.duration : 0;
    seek.max = String(duration || 100);
    seek.value = String(video.currentTime || 0);
    seek.style.setProperty("--progress", `${duration ? video.currentTime / duration * 100 : 0}%`);
    seek.setAttribute("aria-valuetext", `${formatTime(video.currentTime)} sur ${formatTime(duration)}`);
    time.textContent = `${formatTime(video.currentTime)} / ${formatTime(duration)}`;
    play.innerHTML = icon(video.paused || video.ended ? "play" : "pause");
    play.setAttribute("aria-label", video.paused || video.ended ? "Lire" : "Mettre en pause");
    overlay.classList.toggle("playing", !video.paused && !video.ended);
    overlay.innerHTML = `<span>${icon(video.paused || video.ended ? "play" : "pause")}</span>`;
    overlay.setAttribute("aria-label", video.paused || video.ended ? "Lire la vidéo" : "Mettre la vidéo en pause");
    const muted = video.muted || video.volume === 0;
    mute.innerHTML = icon(muted ? "muted" : "volume");
    mute.setAttribute("aria-label", muted ? "Activer le son" : "Couper le son");
    volume.value = String(video.muted ? 0 : video.volume);
  }

  async function togglePlay() {
    if (!playback || errorPanel.hidden === false) return;
    if (video.paused || video.ended) {
      try { await video.play(); }
      catch (error) { if (error.name !== "AbortError" && !disposed) showError("La lecture n’a pas pu démarrer. Réessayez ou vérifiez votre connexion."); }
    } else video.pause();
  }

  async function load() {
    loading.hidden = false;
    errorPanel.hidden = true;
    try {
      playback = await provider.getPlayback(item.id);
      if (disposed) return;
      const url = safeMediaURL(playback.url);
      if (!url) throw new Error("L’adresse de cette vidéo n’est pas disponible.");
      const sources = [{ label: "Source", url }, ...(playback.sources || []).filter(source => safeMediaURL(source.url))];
      const quality = get("quality-select");
      quality.replaceChildren(...sources.map((source, index) => new Option(source.label, String(index))));
      get("quality-setting").hidden = sources.length <= 1;
      quality.onchange = () => {
        const source = sources[Number(quality.value)];
        if (!source) return;
        resumePosition = video.currentTime;
        continueAfterLoad = !video.paused;
        video.src = source.url;
        get("player-quality").textContent = source.label;
        loading.hidden = false;
        video.load();
      };
      video.querySelectorAll("track").forEach(track => track.remove());
      const subtitles = get("subtitle-select");
      subtitles.replaceChildren(new Option("Désactivés", "off"));
      for (const [index, subtitle] of (playback.subtitles || []).entries()) {
        if (!safeMediaURL(subtitle.url)) continue;
        const track = document.createElement("track");
        track.kind = "captions";
        track.label = subtitle.label || "Français";
        track.srclang = subtitle.language || "fr";
        track.src = subtitle.url;
        video.append(track);
        subtitles.add(new Option(track.label, String(index)));
      }
      get("subtitle-setting").hidden = subtitles.options.length <= 1;
      video.src = url;
      video.load();
    } catch (error) { if (!disposed) showError(error.message); }
  }

  overlay.addEventListener("click", togglePlay, { signal });
  play.addEventListener("click", togglePlay, { signal });
  mute.addEventListener("click", () => { video.muted = !video.muted; }, { signal });
  volume.addEventListener("input", () => { video.volume = Number(volume.value); video.muted = video.volume === 0; }, { signal });
  seek.addEventListener("input", () => { if (Number.isFinite(video.duration)) video.currentTime = clamp(seek.value, 0, video.duration); update(); }, { signal });
  seek.addEventListener("change", () => save(true), { signal });
  video.addEventListener("loadedmetadata", () => {
    if (resumePosition !== null) { video.currentTime = clamp(resumePosition, 0, video.duration); resumePosition = null; }
    else if (firstLoad && saved && saved.position / saved.duration < .95) video.currentTime = clamp(saved.position / saved.duration * video.duration, 0, video.duration);
    firstLoad = false;
    loading.hidden = true;
    errorPanel.hidden = true;
    update();
    if (continueAfterLoad) { continueAfterLoad = false; video.play().catch(() => {}); }
  }, { signal });
  video.addEventListener("play", () => { didPlay = true; update(); save(true); }, { signal });
  video.addEventListener("pause", () => { update(); save(true); }, { signal });
  video.addEventListener("ended", () => { update(); save(true); }, { signal });
  video.addEventListener("timeupdate", () => { update(); save(); }, { signal });
  video.addEventListener("volumechange", update, { signal });
  video.addEventListener("waiting", () => { if (errorPanel.hidden) loading.hidden = false; }, { signal });
  video.addEventListener("playing", () => { loading.hidden = true; }, { signal });
  video.addEventListener("canplay", () => { loading.hidden = true; }, { signal });
  video.addEventListener("error", () => showError(), { signal });
  get("player-retry").addEventListener("click", () => { resumePosition = video.currentTime; load(); }, { signal });
  settingsToggle.addEventListener("click", () => {
    settings.hidden = !settings.hidden;
    settingsToggle.setAttribute("aria-expanded", String(!settings.hidden));
  }, { signal });
  get("playback-rate").addEventListener("change", event => { video.playbackRate = Number(event.target.value); }, { signal });
  get("subtitle-select").addEventListener("change", event => {
    [...video.textTracks].forEach((track, index) => { track.mode = String(index) === event.target.value ? "showing" : "disabled"; });
  }, { signal });
  get("player-fullscreen").addEventListener("click", async () => {
    try {
      if (document.fullscreenElement) await document.exitFullscreen();
      else if (shell.requestFullscreen) await shell.requestFullscreen();
      else if (video.webkitEnterFullscreen) video.webkitEnterFullscreen();
      else notify("Le plein écran n’est pas disponible dans ce navigateur.");
    } catch { notify("Le plein écran n’est pas disponible dans ce navigateur."); }
  }, { signal });
  shell.addEventListener("keydown", event => {
    if (event.key === "Escape") { settings.hidden = true; settingsToggle.setAttribute("aria-expanded", "false"); return; }
    if (event.target.closest("button,input,select")) return;
    if (event.code === "Space" || event.key.toLowerCase() === "k") { event.preventDefault(); togglePlay(); }
    if (["ArrowLeft", "ArrowRight"].includes(event.key) && Number.isFinite(video.duration)) { event.preventDefault(); video.currentTime = clamp(video.currentTime + (event.key === "ArrowRight" ? 5 : -5), 0, video.duration); }
    if (event.key.toLowerCase() === "m") video.muted = !video.muted;
  }, { signal });
  document.addEventListener("click", event => {
    if (!settings.hidden && !settings.contains(event.target) && !settingsToggle.contains(event.target)) { settings.hidden = true; settingsToggle.setAttribute("aria-expanded", "false"); }
  }, { signal });
  window.addEventListener("pagehide", () => save(true), { signal });
  load();
  return () => {
    disposed = true;
    save(true);
    controller.abort();
    video.pause();
    video.removeAttribute("src");
    video.load();
  };
}
