// Local acceptance-test transport. Run the API on 8081, with its public URL
// unchanged on 8080. Headers and bodies are forwarded, never logged or saved.
import https from "node:https";
import { readFileSync } from "node:fs";
import { createInterface } from "node:readline";

const cert = readFileSync(new URL("../.cache/tls/cert.pem", import.meta.url));
const key = readFileSync(new URL("../.cache/tls/key.pem", import.meta.url));
let mode = "open";
const nativeSockets = new Set();
const server = https.createServer({ cert, key }, (request, response) => {
  const header = request.headers.authorization;
  const native =
    typeof header === "string" &&
    header.startsWith("Bearer ") &&
    !header.startsWith("Bearer d.");
  if (native && !nativeSockets.has(request.socket)) {
    nativeSockets.add(request.socket);
    request.socket.once("close", () => nativeSockets.delete(request.socket));
  }
  if (native && mode === "offline") {
    response.destroy();
    return;
  }
  const upstream = https.request(
    {
      hostname: "localhost",
      port: 8081,
      path: request.url,
      method: request.method,
      headers: request.headers,
      ca: cert,
    },
    (reply) => {
      if (
        native &&
        mode === "lose-next-write" &&
        request.method === "POST" &&
        request.url.startsWith("/contexts/") &&
        reply.statusCode === 200
      ) {
        mode = "offline";
        reply.resume();
        for (const socket of nativeSockets) socket.destroy();
        console.log(
          "Accepted native Context response dropped; native transport is now offline.",
        );
        return;
      }
      response.writeHead(reply.statusCode, reply.headers);
      reply.pipe(response);
    },
  );
  upstream.on("error", () => response.destroy());
  request.pipe(upstream);
});
server.listen(8080, "127.0.0.1", () =>
  console.log("Local sync acceptance proxy ready on 8080; API upstream 8081."),
);
createInterface({ input: process.stdin }).on("line", (line) => {
  const value = line.trim();
  if (value === "open" || value === "offline" || value === "lose-next-write") {
    mode = value;
    if (mode === "offline")
      for (const socket of nativeSockets) socket.destroy();
    console.log(`Native transport mode: ${mode}`);
  } else if (value === "stop") {
    server.closeAllConnections();
    server.close();
  }
});
