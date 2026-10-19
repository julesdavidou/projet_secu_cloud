import test from "node:test";
import assert from "node:assert/strict";
import { createAPIClient, createLocalProvider, APIError } from "../dist/assets/js/api.js";
import { selectVideos, parseRoute, safeMediaURL } from "../dist/assets/js/utils.js";
import { emptyProfile } from "../dist/assets/js/data.js";
// Données synthétiques exclusivement utilisées par les tests, jamais importées par dist/.
const demoVideos = [
 {id:"ingenierie-climat",title:"Ingénierie & climat",category:"conferences",minutes:30,tags:["climat"],publishedAt:"2026-01-01",views:3},
 {id:"gala-des-arts-2026",title:"Gala",category:"culture",minutes:15,tags:["gala"],publishedAt:"2026-01-02",views:2},
 {id:"projet-mars-rover",title:"Rover",category:"projets",minutes:12,tags:["robotique"],publishedAt:"2026-01-03",views:1},
];

function memoryStorage() {
  const values = new Map();
  return { getItem: key => values.get(key) || null, setItem: (key, value) => values.set(key, value) };
}

test("une recherche accentuée et multi-mots se combine aux filtres sans modifier le catalogue", () => {
  const original = demoVideos.map(video => video.id);
  const selected = selectVideos(demoVideos, { query: "INGENIERIE climat", category: "conferences" });
  assert.ok(selected.some(video => video.id === "ingenierie-climat"));
  assert.ok(selected.every(video => video.category === "conferences"));
  assert.equal(selectVideos(demoVideos, { query: "basket", category: "culture" }).length, 0);
  assert.ok(selectVideos(demoVideos, { query: "gala", short: true }).every(video => video.minutes < 20));
  assert.deepEqual(demoVideos.map(video => video.id), original);
});

test("favoris, profil et progression survivent au rechargement ; effacer l’historique préserve les favoris", async () => {
  const storage = memoryStorage();
  const first = createLocalProvider({ videos: demoVideos, storage });
  await first.setFavorite("projet-mars-rover", true);
  await first.saveProgress("gala-des-arts-2026", 12, 52);
  await first.updateProfile({ name: "Alex", course: "Informatique", promotion: "2028" });
  const second = createLocalProvider({ videos: demoVideos, storage });
  const profile = await second.getProfile();
  assert.equal(profile.name, "Alex");
  assert.ok(profile.favorites.includes("projet-mars-rover"));
  assert.deepEqual(profile.history.find(item => item.videoId === "gala-des-arts-2026").position, 12);
  const after = await second.clearHistory();
  assert.equal(after.history.length, 0);
  assert.deepEqual(after.favorites, profile.favorites);
});

test("un stockage local invalide ou interdit ne casse pas les parcours", async () => {
  const corrupt = createLocalProvider({ videos: demoVideos, storage: { getItem: () => "{broken", setItem() {} } });
  assert.equal((await corrupt.getProfile()).name, "");
  const unavailable = createLocalProvider({ videos: demoVideos, storage: { getItem() { throw Error("denied"); }, setItem() { throw Error("quota"); } } });
  const profile = await unavailable.setFavorite("projet-mars-rover", true);
  assert.ok(profile.favorites.includes("projet-mars-rover"));
  assert.equal(unavailable.storageAvailable, false);
});

test("l’adaptateur HTTP transmet les données à l’API avec la session et gère les réponses sans corps", async () => {
  const calls = [];
  const request = createAPIClient({ baseUrl: "/api/", fetchImpl: async (...args) => { calls.push(args); return new Response(null, { status: 204 }); } });
  assert.equal(await request("/me/favorites/gala", { method: "PUT", body: { active: true } }), null);
  assert.equal(calls[0][0], "/api/me/favorites/gala");
  assert.equal(calls[0][1].credentials, "include");
  assert.equal(calls[0][1].method, "PUT");
  assert.deepEqual(JSON.parse(calls[0][1].body), { active: true });
});

test("un refus d’accès conserve son statut et ne devient pas une fausse réponse de démonstration", async () => {
  const request = createAPIClient({ fetchImpl: async () => new Response("unauthorized", { status: 401 }) });
  await assert.rejects(request("/me"), error => error instanceof APIError && error.status === 401 && error.message.includes("session"));
});

test("une requête bloquée est interrompue et présente une erreur exploitable", async () => {
  const request = createAPIClient({ timeoutMs: 10, fetchImpl: (_, { signal }) => new Promise((resolve, reject) => signal.addEventListener("abort", () => reject(new Error("aborted")))) });
  await assert.rejects(request("/videos"), error => error instanceof APIError && error.message.includes("temps"));
});

test("les fragments invalides et les adresses de média exécutables sont rejetés", () => {
  assert.equal(parseRoute("#/video/%E0%A4%A").name, "introuvable");
  assert.equal(safeMediaURL("javascript:alert(1)"), "");
  assert.equal(safeMediaURL("data:text/html,hello"), "");
  assert.equal(safeMediaURL("/videos/demo.mp4"), "/videos/demo.mp4");
});

test("la version livrée reste vide, même en présence d’anciennes données fictives", async () => {
 const storage = memoryStorage();
 storage.setItem("cariboutv.viewer.v1", JSON.stringify({name:"Ancienne démo",favorites:["gala-des-arts-2026"],history:[]}));
 const provider = createLocalProvider({storage});
 assert.deepEqual(await provider.listVideos(), []);
 assert.deepEqual(await provider.getProfile(), emptyProfile);
 await assert.rejects(provider.getPlayback("gala-des-arts-2026"), error => error.status === 404);
});
