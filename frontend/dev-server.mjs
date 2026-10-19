// Serveur de développement statique : aucune logique métier ni API backend.
import http from "node:http";
import { createReadStream } from "node:fs";
import { stat } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const staticRoot = fileURLToPath(new URL("./dist/", import.meta.url));
const mimeTypes = { ".html": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".json": "application/json; charset=utf-8", ".svg": "image/svg+xml", ".webp": "image/webp", ".png": "image/png", ".mp4": "video/mp4", ".vtt": "text/vtt; charset=utf-8", ".woff2": "font/woff2", ".txt": "text/plain; charset=utf-8" };

export function createStaticServer(root = staticRoot) {
  return http.createServer(async (request, response) => {
    response.setHeader("X-Content-Type-Options", "nosniff");
    response.setHeader("Referrer-Policy", "strict-origin-when-cross-origin");
    response.setHeader("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' https: http: data:; font-src 'self'; media-src 'self' https: http: blob:; connect-src 'self' https: http:; object-src 'none'; base-uri 'self'; frame-ancestors 'none'");
    if (!["GET", "HEAD"].includes(request.method)) { response.writeHead(405, { Allow: "GET, HEAD" }); response.end(); return; }
    let filename;
    try {
      const pathname = decodeURIComponent(new URL(request.url, "http://localhost").pathname);
      if (pathname.split(/[\\/]/).some(part => part.startsWith("."))) throw new Error("Forbidden path");
      filename = path.resolve(root, `.${pathname === "/" ? "/index.html" : pathname}`);
      if (!filename.startsWith(path.resolve(root) + path.sep)) throw new Error("Forbidden path");
    } catch { response.writeHead(400); response.end("Requête invalide"); return; }
    try {
      const file = await stat(filename);
      if (!file.isFile()) throw new Error("Not a file");
      response.setHeader("Content-Type", mimeTypes[path.extname(filename)] || "application/octet-stream");
      response.setHeader("Cache-Control", "no-cache");
      response.setHeader("Accept-Ranges", "bytes");
      let start = 0, end = file.size - 1;
      if (request.headers.range) {
        const range = /^bytes=(\d*)-(\d*)$/.exec(request.headers.range);
        if (!range || (!range[1] && !range[2])) { response.writeHead(416, { "Content-Range": `bytes */${file.size}` }); response.end(); return; }
        start = range[1] ? Number(range[1]) : Math.max(0, file.size - Number(range[2]));
        end = range[1] && range[2] ? Math.min(Number(range[2]), end) : end;
        if (start > end || start >= file.size) { response.writeHead(416, { "Content-Range": `bytes */${file.size}` }); response.end(); return; }
        response.statusCode = 206;
        response.setHeader("Content-Range", `bytes ${start}-${end}/${file.size}`);
      }
      response.setHeader("Content-Length", end - start + 1);
      if (request.method === "HEAD") response.end();
      else {
        const stream = createReadStream(filename, { start, end });
        stream.on("error", () => response.destroy());
        response.on("close", () => stream.destroy());
        stream.pipe(response);
      }
    } catch { response.writeHead(404); response.end("Fichier introuvable"); }
  });
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const port = Number(process.env.PORT || 5173);
  const server = createStaticServer();
  server.on("error", error => { console.error(error.code === "EADDRINUSE" ? `Le port ${port} est occupé. Choisissez un autre port avec la variable PORT.` : error.message); process.exitCode = 1; });
  server.listen(port, "127.0.0.1", () => console.log(`CaribouTV — Local: http://127.0.0.1:${server.address().port}/`));
}
