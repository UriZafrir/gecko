import Foundation
import WebKit

/// Gecko's web UI as shipped in the app (Generated/web, from web/dist by
/// `make ios-web`, see ios/scripts/bundle-web.mjs), shown for every machine.
///
/// WKWebView can't answer a page's http(s) requests itself (a scheme handler
/// takes only schemes of its own), so the app loads this page with
/// loadHTMLString and the machine's address as its base URL: the page then
/// has the machine's own origin, so its token cookie, /api/ and the /ws
/// WebSocket work exactly as in a browser and the daemon's Origin check is
/// unchanged. The page carries its script, styles and fonts inside it; the
/// theme photos come from the app's gecko-asset: scheme.
enum BundledUI {
    static let directory: URL? = Bundle.main.url(forResource: "web", withExtension: nil, subdirectory: "Generated")

    static let html: String? = directory.flatMap { try? String(contentsOf: $0.appendingPathComponent("index.html"), encoding: .utf8) }

    static var available: Bool { html != nil }
}

/// gecko-asset://ui/<path>: files of the bundled UI (theme photos, icons).
final class AssetSchemeHandler: NSObject, WKURLSchemeHandler {
    static let scheme = "gecko-asset"

    /// The file for a URL, or nil for anything outside the bundled UI.
    static func file(for url: URL?, root: URL? = BundledUI.directory) -> URL? {
        guard let url, url.scheme == scheme, url.host == "ui", let root else { return nil }
        let path = url.path
        guard path.hasPrefix("/"), !path.contains(".."), !path.contains("//"), !path.contains("\\"), path.count > 1 else { return nil }
        let f = root.appendingPathComponent(String(path.dropFirst()))
        guard f.standardizedFileURL.path.hasPrefix(root.standardizedFileURL.path + "/") else { return nil }
        return f
    }

    static func mime(_ name: String) -> String {
        switch (name as NSString).pathExtension.lowercased() {
        case "svg": return "image/svg+xml"
        case "png": return "image/png"
        case "jpg", "jpeg": return "image/jpeg"
        case "webp": return "image/webp"
        case "woff2": return "font/woff2"
        case "css": return "text/css"
        case "js": return "text/javascript"
        default: return "application/octet-stream"
        }
    }

    func webView(_ webView: WKWebView, start task: WKURLSchemeTask) {
        guard let f = Self.file(for: task.request.url), let data = try? Data(contentsOf: f), let url = task.request.url else {
            let r = HTTPURLResponse(url: task.request.url ?? URL(string: "gecko-asset://ui/")!, statusCode: 404, httpVersion: "HTTP/1.1", headerFields: nil)!
            task.didReceive(r)
            task.didFinish()
            return
        }
        let headers = [
            "Content-Type": Self.mime(f.lastPathComponent),
            "Content-Length": String(data.count),
            "Access-Control-Allow-Origin": "*",
            "Cache-Control": "max-age=86400",
            "X-Content-Type-Options": "nosniff",
        ]
        task.didReceive(HTTPURLResponse(url: url, statusCode: 200, httpVersion: "HTTP/1.1", headerFields: headers)!)
        task.didReceive(data)
        task.didFinish()
    }

    func webView(_ webView: WKWebView, stop task: WKURLSchemeTask) {}
}

/// window.GeckoIOS in the page (see web/src/util.js): the same calls as the
/// Android app's window.GeckoAndroid. Each message is checked to come from
/// the machine's own origin, in the main frame.
enum BridgeScript {
    static let shim = """
    (function () {
      if (window.GeckoIOS) return;
      var h = window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.gecko;
      if (!h) return;
      function post(m) { try { return h.postMessage(m); } catch (e) { return Promise.reject(e); } }
      window.GeckoIOS = {
        platform: 'ios',
        // A notification also while the page shows (a tab not on screen): iOS shows it as a banner.
        notifyVisible: true,
        copy: function (t) { post({ op: 'copy', text: String(t) }); return true; },
        paste: function () { return post({ op: 'paste' }); },
        notify: function (title, body, id) { post({ op: 'notify', title: String(title), body: String(body || ''), id: String(id || '') }); },
        enableNotifications: function () { post({ op: 'enableNotifications' }); },
        mods: function (c, a) { post({ op: 'mods', ctrl: !!c, alt: !!a }); },
        shareDevice: function () { return false; },
        bundled: function () { var m = document.querySelector('meta[name="gecko-ui"]'); return !!m && m.content === 'ios-app'; },
        theme: function (json) { post({ op: 'theme', json: String(json) }); },
        share: function (text) { post({ op: 'share', text: String(text) }); }
      };
    })();
    """

    /// UI tests: whether the page could load a file over gecko-asset:.
    static let assetCheck = """
    (function () {
      var i = new Image();
      i.onload = function () { window.__geckoAsset = 'ok'; };
      i.onerror = function () { window.__geckoAsset = 'error'; };
      i.src = 'gecko-asset://ui/icon.svg';
      // What touches reach the page (the last few events, and the most fingers at once).
      var t = window.__geckoTouch = { max: 0, log: [] };
      ['touchstart', 'touchmove', 'touchend', 'touchcancel', 'gesturestart', 'gestureend'].forEach(function (k) {
        window.addEventListener(k, function (e) {
          var n = e.touches ? e.touches.length : -1;
          if (n > t.max) t.max = n;
          var last = t.log[t.log.length - 1];
          if (last && last.k === k && last.n === n) last.c++; else t.log.push({ k: k, n: n, c: 1 });
          if (t.log.length > 12) t.log.shift();
        }, { capture: true, passive: true });
      });
    })();
    """

    /// UI tests: the page's state as JSON, read into an accessibility label.
    static let probe = """
    (function () {
      var g = window.gecko, r = { ready: !!g };
      if (!g) return JSON.stringify(r);
      try {
        r.online = g.conn.status; r.sync = !!g.firstSync; r.bundled = !!g.bundled; r.daemon = g.daemonVersion || 0;
        r.outdated = !!g.outdated; r.active = g.activeId() || '';
        var tabs = [].slice.call(document.querySelectorAll('#tabs [role=tab]'));
        r.tabs = tabs.length;
        r.tab = tabs.findIndex(function (t) { return t.getAttribute('aria-selected') === 'true'; });
        var p = g.activePane();
        if (p) {
          var b = p.term.buffer.active, out = [];
          r.font = p.term.options.fontSize; r.viewportY = b.viewportY; r.baseY = b.baseY; r.rows = p.term.rows; r.cols = p.term.cols;
          r.cursorX = b.cursorX;
          for (var i = b.viewportY; i < b.viewportY + p.term.rows; i++) { var l = b.getLine(i); out.push(l ? l.translateToString(true) : ''); }
          r.screen = out.join('\\n');
        }
        r.drawer = document.body.classList.contains('drawer');
        r.width = innerWidth; r.height = innerHeight;
        r.sideFixed = getComputedStyle(document.getElementById('side')).position === 'fixed';
        r.ios = document.documentElement.classList.contains('ios');
        r.keybar = getComputedStyle(document.getElementById('keybar')).display;
        r.asset = window.__geckoAsset || '';
        var t = window.__geckoTouch;
        if (t) r.touch = t.max + ' ' + t.log.map(function (x) { return x.k.replace('touch', '') + x.n + 'x' + x.c; }).join(',');
        if (g.touchStats) r.gesture = g.touchStats.kind + ' ' + (g.touchStats.what || '');
        r.agents = g.allAgents().map(function (a) { return a.state.status; });
        r.ctrl = !!g.mods.ctrl; r.palette = !!g.paletteOpen; r.textview = !!g.textViewOpen;
        r.focus = document.activeElement ? document.activeElement.tagName + '.' + document.activeElement.className : '';
      } catch (e) { r.err = String(e); }
      return JSON.stringify(r);
    })()
    """
}
