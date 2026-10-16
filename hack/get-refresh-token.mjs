#!/usr/bin/env node
// One-time helper: prints the SPOTIFY_REFRESH_TOKEN k8s-fm uses to manage your playlists.
//
// 1. Create an app at https://developer.spotify.com/dashboard
//    and add http://127.0.0.1:8888/callback as a Redirect URI.
// 2. Run: SPOTIFY_ID=xxx SPOTIFY_SECRET=yyy node hack/get-refresh-token.mjs
// 3. Open the printed URL, approve, and copy the refresh token it prints.

import http from "node:http";
import crypto from "node:crypto";

const clientId = process.env.SPOTIFY_ID;
const clientSecret = process.env.SPOTIFY_SECRET;
if (!clientId || !clientSecret) {
  console.error("Set SPOTIFY_ID and SPOTIFY_SECRET env vars first.");
  process.exit(1);
}

const redirectUri = "http://127.0.0.1:8888/callback";
const state = crypto.randomBytes(8).toString("hex");

const authUrl = new URL("https://accounts.spotify.com/authorize");
authUrl.search = new URLSearchParams({
  response_type: "code",
  client_id: clientId,
  scope: "playlist-read-private playlist-modify-public playlist-modify-private",
  redirect_uri: redirectUri,
  state,
}).toString();

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, redirectUri);
  if (url.pathname !== "/callback") {
    res.writeHead(404).end();
    return;
  }
  const code = url.searchParams.get("code");
  if (!code || url.searchParams.get("state") !== state) {
    res.writeHead(400).end("Missing code or state mismatch — try again.");
    return;
  }

  const tokenRes = await fetch("https://accounts.spotify.com/api/token", {
    method: "POST",
    headers: {
      Authorization: `Basic ${Buffer.from(`${clientId}:${clientSecret}`).toString("base64")}`,
      "Content-Type": "application/x-www-form-urlencoded",
    },
    body: new URLSearchParams({
      grant_type: "authorization_code",
      code,
      redirect_uri: redirectUri,
    }),
  });
  const json = await tokenRes.json();

  if (!json.refresh_token) {
    res.writeHead(500).end("Token exchange failed — check the terminal.");
    console.error("Token exchange failed:", json);
    process.exit(1);
  }

  res.end("Done! Your refresh token is in the terminal. You can close this tab.");
  console.log("\nSPOTIFY_REFRESH_TOKEN:\n");
  console.log(json.refresh_token);
  server.close();
});

server.listen(8888, "127.0.0.1", () => {
  console.log("Open this URL in your browser and approve access:\n");
  console.log(authUrl.toString());
});
