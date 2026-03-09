/**
 * FleetQ Bridge install gateway — get.fleetq.net
 *
 * Routes:
 *   GET /bridge          → install.sh (curl | sh)
 *   GET /bridge/windows  → PowerShell install script
 *   GET /               → redirect to docs
 */

const REPO = "escapeboy/fleetq-bridge";
const INSTALL_SCRIPT_URL =
  `https://raw.githubusercontent.com/${REPO}/master/scripts/install.sh`;

export default {
  async fetch(request) {
    const url = new URL(request.url);
    const path = url.pathname.replace(/\/+$/, ""); // strip trailing slash

    // GET /bridge → shell install script
    if (path === "/bridge") {
      const script = await fetch(INSTALL_SCRIPT_URL);
      if (!script.ok) {
        return new Response("Install script temporarily unavailable. Visit https://github.com/" + REPO + "/releases", {
          status: 502,
          headers: { "Content-Type": "text/plain" },
        });
      }
      const body = await script.text();
      return new Response(body, {
        headers: {
          "Content-Type": "text/plain; charset=utf-8",
          "Cache-Control": "public, max-age=300", // 5 min cache
          "X-Content-Type-Options": "nosniff",
        },
      });
    }

    // GET /bridge/windows → PowerShell script
    if (path === "/bridge/windows") {
      const ps1 = await fetch(
        `https://raw.githubusercontent.com/${REPO}/master/scripts/install.ps1`
      );
      if (!ps1.ok) {
        return Response.redirect(
          `https://github.com/${REPO}/releases/latest`,
          302
        );
      }
      const body = await ps1.text();
      return new Response(body, {
        headers: {
          "Content-Type": "text/plain; charset=utf-8",
          "Cache-Control": "public, max-age=300",
        },
      });
    }

    // GET /bridge/version → latest version tag (JSON)
    if (path === "/bridge/version") {
      const resp = await fetch(
        `https://api.github.com/repos/${REPO}/releases/latest`,
        { headers: { "User-Agent": "fleetq-get-worker" } }
      );
      if (!resp.ok) {
        return new Response(JSON.stringify({ error: "unavailable" }), {
          status: 502,
          headers: { "Content-Type": "application/json" },
        });
      }
      const data = await resp.json();
      return new Response(JSON.stringify({ version: data.tag_name }), {
        headers: {
          "Content-Type": "application/json",
          "Cache-Control": "public, max-age=60",
          "Access-Control-Allow-Origin": "*",
        },
      });
    }

    // Everything else → redirect to GitHub releases
    return Response.redirect(
      `https://github.com/${REPO}/releases/latest`,
      302
    );
  },
};
