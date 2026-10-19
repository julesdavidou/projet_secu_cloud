// Le navigateur appellera l'API web Go du control plane.
// Le mode vide permet de travailler sur le front avant sa disponibilité.
export const config = Object.freeze({
  dataSource: "empty", // "empty" ou "api" ; aucune donnée de démonstration
  apiBaseUrl: "/api",
  requestTimeoutMs: 12000,
  storageKey: "cariboutv.viewer.empty.v1",
});
