import XCTest

/// Make anchor in the iOS app (docs/anchor.md): the saved machine
/// (studio-mac) lists build-box; the anchor icon is on studio-mac, Make
/// anchor on build-box moves it there with "via build-box", the app
/// reopens on build-box (the choice is kept for that saved machine), and
/// Back to studio-mac switches back.
final class AnchorTest: GeckoUITestCase {
    func testMakeAnchorAndBack() throws {
        let link = try link()
        launch(open: link, name: "studio-mac")
        allowNotificationsIfAsked()
        var p = waitOnline()
        let door = p["host"] as? String ?? ""
        js("localStorage.removeItem('gecko.anchorPath'); gecko.anchorPath.length && location.reload()")
        // build-box is studio-mac's machine through gecko bridge, or (when
        // the runner has sshd) added here by its Gecko link.
        let bb = try link("GECKO_BUILDBOX_LINK")
        let bbJS = String(data: try JSONEncoder().encode(bb), encoding: .utf8)!
        js("gecko.hosts.some((h) => h.name === 'build-box') || gecko.conn.request({ t: 'addhost', kind: 'link', name: 'build-box', secret: \(bbJS) }).then(() => { window.__anchorAdded = 1; })")
        p = waitProbe("build-box among the machines, the anchor marked", timeout: 60) { p in
            (p["hosts"] as? [String])?.contains("build-box:connected") == true && (p["anchorRow"] as? String) == door && (p["proxied"] as? Bool) == false
        }
        // The Simulator opens studio-mac at 127.0.0.1: the anchor is the
        // device itself here, so no "via".
        XCTAssertEqual(p["via"] as? String, "")
        js("gecko.toggleSidebar && document.body.classList.contains('side-hidden') && gecko.toggleSidebar(); document.getElementById('menu-btn') && getComputedStyle(document.getElementById('side')).position === 'fixed' && !document.body.classList.contains('drawer') && document.getElementById('menu-btn').click()")
        beat(1.5)
        shot("anchor-1-studio-mac")
        // The machine's menu (a long-press opens it; here as right-click would), Make anchor, confirm.
        js("document.querySelector('#side .host[data-host=\"build-box\"] .host-main').dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, clientX: 60, clientY: 240 }))")
        beat(1.2)
        shot("anchor-2-menu")
        js("[...document.querySelectorAll('#ctxmenu button')].find((b) => b.textContent.includes('Make anchor')).click()")
        beat(1.2)
        shot("anchor-3-confirm")
        js("document.querySelector('#modal [data-yes]').click()")
        p = waitProbe("build-box is the anchor, through studio-mac", timeout: 60) { p in
            (p["host"] as? String) == "build-box" && (p["proxied"] as? Bool) == true && (p["anchorRow"] as? String) == "build-box"
        }
        XCTAssertEqual(p["door"] as? String, door)
        js("gecko.sessions.size || gecko.newTab({ name: 'on-build-box' })")
        waitProbe("via build-box in the bottom bar") { ($0["via"] as? String) == "via build-box" }
        js("getComputedStyle(document.getElementById('side')).position === 'fixed' && !document.body.classList.contains('drawer') && document.getElementById('menu-btn').click()")
        p = waitProbe("the way back offered") { ($0["prev"] as? [String])?.contains(door) == true || (($0["hosts"] as? [String])?.contains { $0.hasPrefix(door + ":") } == true) }
        beat(1.5)
        shot("anchor-4-build-box")

        // Next launch: the app opens the saved machine, on build-box.
        app.terminate()
        launch(reset: false)
        let row = app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'machine.'")).firstMatch
        if row.waitForExistence(timeout: 5) { row.tap() }
        waitProbe("reopened on build-box", timeout: 60) { ($0["host"] as? String) == "build-box" && ($0["proxied"] as? Bool) == true }
        beat(1.2)
        shot("anchor-5-relaunched")

        // Back to studio-mac.
        js("gecko.makeAnchor(\(String(data: try JSONEncoder().encode(door), encoding: .utf8)!))")
        waitProbe("back on studio-mac", timeout: 60) { ($0["host"] as? String) == door && ($0["proxied"] as? Bool) == false && ($0["anchorRow"] as? String) == door }
        js("getComputedStyle(document.getElementById('side')).position === 'fixed' && !document.body.classList.contains('drawer') && document.getElementById('menu-btn').click()")
        beat(1.5)
        shot("anchor-6-back")
        if env["GECKO_REMOTE"] == "ssh" { js("gecko.conn.request({ t: 'rmhost', name: 'build-box' })") }
    }
}
