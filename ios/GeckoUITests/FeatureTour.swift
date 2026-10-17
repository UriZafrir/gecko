import UIKit
import XCTest

/// 05-tour: the important features, slowly, with a caption per chapter
/// (written to GECKO_CHAPTERS; the job draws them under the video):
/// pairing several machines, a remote machine's tabs, agents that need you,
/// Gecko driven from outside, themes, and scrolling. testPadTour is the
/// short iPad part. The job (ios/scripts/ci-cues.sh) does the steps that
/// happen on the computer when the test asks for them.
final class FeatureTour: GeckoUITestCase {
    /// How build-box is joined: "ssh" (to this Mac's own sshd) or "bridge"
    /// (gecko bridge run locally, when the runner has no sshd).
    var remote: String { env["GECKO_REMOTE"] ?? "bridge" }

    func testPhoneTour() throws {
        guard !isPad else { throw XCTSkip("the iPhone part") }
        let studio = try link(), buildBox = try link("GECKO_BUILDBOX_LINK")

        // 1. Pairing.
        launch()
        XCTAssertTrue(app.descendants(matching: .any)["connect.empty"].waitForExistence(timeout: 10))
        chapter("1 Pairing. The app starts with no machines: paste the link Gecko shows on the computer")
        beat(3)
        addMachine(studio)
        allowNotificationsIfAsked()
        waitOnline()
        beat(2.5)
        shot("05-01-connected")
        chapter("1 Pairing. On the computer, Open on another device shows that link and its QR code")
        js("gecko.openShare()")
        beat(5)
        shot("05-01-share")
        js(click("#modal [data-no]"))
        beat(1)

        chapter("1 Pairing. A second machine, build-box, from its own link. Switch between them")
        key("menu").tap()
        app.buttons["Machines"].tap()
        XCTAssertTrue(app.buttons["machine.studio-mac"].waitForExistence(timeout: 8))
        beat(1.5)
        addMachine(buildBox)
        var p = waitOnline()
        beat(2.5)
        shot("05-01-build-box")
        XCTAssertTrue((p["sessions"] as? [String] ?? []).contains { $0.hasPrefix("build-box/api/") }, "build-box's own tabs")
        key("menu").tap()
        app.buttons["Machines"].tap()
        XCTAssertTrue(app.buttons["machine.build-box"].waitForExistence(timeout: 8))
        beat(2.5)
        shot("05-01-machines")
        app.buttons["machine.studio-mac"].tap()
        waitOnline()
        beat(2)

        // 2. Another machine inside Gecko: studio-mac reaches build-box over ssh.
        chapter(remote == "ssh"
            ? "2 More machines. studio-mac adds build-box over ssh (gecko host add; here a second Gecko on the same CI Mac)"
            : "2 More machines. studio-mac has build-box as a machine (gecko bridge; a second Gecko on the same CI Mac)")
        openDrawer()
        beat(2)
        cue("add-host")
        p = waitProbe("build-box in studio-mac's machines", timeout: 40) { p in
            (p["hosts"] as? [String] ?? []).contains("build-box:connected")
                && (p["sessions"] as? [String] ?? []).contains { $0.hasPrefix("build-box/api/") }
        }
        beat(3)
        shot("05-02-machines-in-drawer")
        chapter("2 More machines. Its tabs sit next to the local ones: open one and use its shell")
        js(focusTab(host: "build-box", name: "api"))
        p = waitProbe("build-box's api tab", timeout: 10) { ($0["active"] as? String ?? "").hasPrefix("build-box/") }
        beat(1)
        tapTerminal()
        beat(0.8)
        app.typeText("pwd; ls\n")
        waitProbe("the remote shell answered", timeout: 10) { ($0["screen"] as? String ?? "").contains("buildbox/api") }
        key("hide-keyboard").tap()
        beat(3)
        shot("05-02-remote-tab")

        // 3. Agents.
        chapter("3 Agents. Claude Code starts in the build tab. Its chip shows it working")
        js(focusTab(host: "studio-mac", name: "build"))
        waitProbe("the build tab", timeout: 10) { ($0["tabNames"] as? [String] ?? []).count > 0 && self.activeName($0) == "build" }
        tapTerminal()
        beat(0.8)
        app.typeText("claude\n")
        waitProbe("the agent at work", timeout: 15) { ($0["agents"] as? [String] ?? []).contains("working") }
        key("hide-keyboard").tap()
        beat(3)
        shot("05-03-working")
        chapter("3 Agents. On another tab, Gecko tells you when the agent needs you")
        js(focusTab(host: "studio-mac", name: "shell"))
        let banner = springboard.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS[c] 'needs you'")).firstMatch
        let bannerShown = banner.waitForExistence(timeout: 40)
        waitProbe("the agent waiting", timeout: 20) { ($0["agents"] as? [String] ?? []).contains("needs-input") }
        shot("05-03-notification")
        beat(1.5)
        if bannerShown && banner.isHittable {
            chapter("3 Agents. Tap the notification: its tab opens")
            banner.tap()
        } else {
            js(focusTab(host: "studio-mac", name: "build"))
        }
        waitProbe("the agent's tab", timeout: 10) { self.activeName($0) == "build" }
        beat(2)
        chapter("3 Agents. Its chip has the answers it offers. Answer 1, Yes")
        js(click("#dock [data-agent]"))
        waitProbe("the chip open", timeout: 6) { !($0["dockOpen"] as? String ?? "").isEmpty }
        beat(3)
        shot("05-03-answers")
        js(click("#dock .agent.open [data-reply=\"1\"]"))
        waitProbe("the answer reached the agent", timeout: 15) { ($0["screen"] as? String ?? "").contains("Updated parser.go") }
        beat(2.5)
        chapter("3 Agents. Idle again: quick replies send its next prompt, such as Go")
        waitProbe("the agent idle", timeout: 20) { !($0["agents"] as? [String] ?? []).contains("needs-input") }
        js("(function(){if(!gecko.dockOpen){var c=document.querySelector('#dock [data-agent]');if(c)c.click();}})()")
        beat(3)
        shot("05-03-quick-replies")
        js(click("#dock .agent.open [data-prompt=\"go\"]"))
        beat(2.5)
        chapter("3 Agents. The drawer lists every agent on every machine")
        openDrawer()
        beat(4)
        shot("05-03-agents-in-drawer")
        closeDrawer()

        // 4. Being managed.
        chapter("4 Driven from outside. An agent on the computer runs gecko new, send, capture and agents")
        js(focusTab(host: "studio-mac", name: "logs"))
        waitProbe("the logs tab", timeout: 10) { self.activeName($0) == "logs" }
        beat(1.5)
        cue("managed")
        waitProbe("the tab it made", timeout: 30) { ($0["sessions"] as? [String] ?? []).contains { $0.contains("/agent-task/") } }
        waitProbe("its capture", timeout: 30) { ($0["screen"] as? String ?? "").components(separatedBy: "hello from an agent").count > 1 }
        beat(6)
        shot("05-04-commands")
        chapter("4 Driven from outside. The tab it made, with what it typed, live on the phone")
        js(focusTab(host: "studio-mac", name: "agent-task"))
        waitProbe("the agent's tab", timeout: 10) { ($0["screen"] as? String ?? "").contains("hello from an agent") }
        beat(4)
        shot("05-04-agent-task")

        // 5. Themes.
        chapter("5 Themes. The theme picker previews each theme as you go")
        js("gecko.openThemePicker()")
        waitProbe("the theme picker", timeout: 6) { ($0["palette"] as? Bool) == true }
        beat(1.5)
        for (q, shotName) in [("sakura", "05-05-sakura"), ("mission control", "05-05-mission-control"), ("dracula", "05-05-dracula"), ("gruvbox", "05-05-gruvbox")] {
            js(paletteType(q))
            beat(Double(q.count) * 0.12 + 3)
            shot(shotName)
        }
        chapter("5 Themes. And back to Tokyo Night")
        js(paletteType("tokyo night", enter: true))
        waitProbe("Tokyo Night", timeout: 10) { ($0["theme"] as? String) == "tokyo-night" && ($0["palette"] as? Bool) == false }
        beat(3)
        shot("05-05-tokyo-night")

        // 6. Scrolling.
        chapter("6 Scrolling. 5000 lines: drag back, flick to coast")
        js(focusTab(host: "studio-mac", name: "shell"))
        waitProbe("the shell tab", timeout: 10) { self.activeName($0) == "shell" }
        tapTerminal()
        beat(0.8)
        app.typeText("seq 1 5000\n")
        p = waitProbe("seq's output", timeout: 15) { ($0["baseY"] as? Int ?? 0) > 4000 }
        let bottom = p["baseY"] as? Int ?? 0
        key("hide-keyboard").tap()
        beat(1.5)
        drag(from: terminalPoint(0.5, 0.3), to: terminalPoint(0.5, 0.75), hold: 0.05, velocity: .slow)
        beat(1.5)
        for _ in 0..<3 {
            drag(from: terminalPoint(0.5, 0.2), to: terminalPoint(0.5, 0.85), hold: 0.02, velocity: .fast)
            beat(1.6)
        }
        p = probe()
        XCTAssertLessThan(p["viewportY"] as? Int ?? bottom, bottom, "scrolled back")
        shot("05-06-flicked")
        chapter("6 Scrolling. The fast-scroll thumb on the right edge: to the top, and back to the end")
        let edgeX: CGFloat = 1
        let edge = { (y: CGFloat) in self.web.coordinate(withNormalizedOffset: CGVector(dx: edgeX, dy: y)).withOffset(CGVector(dx: -12, dy: 0)) }
        // A flick brings the thumb up (it fades 1.5 s after the scrolling
        // stops), then a finger on its strip drags it.
        drag(from: terminalPoint(0.5, 0.2), to: terminalPoint(0.5, 0.7), hold: 0.02, velocity: .fast)
        drag(from: edge(0.5), to: edge(0.06), hold: 0.1, velocity: .slow)
        if !waitFor(6, { ($0["viewportY"] as? Int ?? bottom) < bottom / 4 }) {
            XCTContext.runActivity(named: "the thumb drag didn't reach the top (the device tour and TerminalDemo check scrolling)") { _ in }
        }
        beat(2.5)
        shot("05-06-thumb-top")
        drag(from: terminalPoint(0.5, 0.7), to: terminalPoint(0.5, 0.2), hold: 0.02, velocity: .fast)
        drag(from: edge(0.1), to: edge(0.8), hold: 0.1, velocity: .slow)
        if !waitFor(8, { ($0["viewportY"] as? Int ?? 0) >= ($0["baseY"] as? Int ?? 1) - 2 }) {
            XCTContext.runActivity(named: "the thumb drag didn't reach the end") { _ in }
        }
        beat(2.5)
        shot("05-06-end")
        chapter("Gecko for iOS")
        beat(2)
    }

    /// The iPad part: the sidebar beside the terminal in landscape, the
    /// machines in it, an agent card, and a theme.
    func testPadTour() throws {
        guard isPad else { throw XCTSkip("the iPad part") }
        XCUIDevice.shared.orientation = .landscapeLeft
        launch(open: try link(), name: "studio-mac")
        allowNotificationsIfAsked()
        cue("add-host")
        var p = waitProbe("the page in landscape, with build-box", timeout: 60) { p in
            (p["online"] as? String) == "online" && (p["width"] as? Int ?? 0) > (p["height"] as? Int ?? 0)
                && (p["hosts"] as? [String] ?? []).contains("build-box:connected")
        }
        assertLandscape(p)
        XCTAssertEqual(p["sideFixed"] as? Bool, false, "the sidebar beside the terminal")
        chapter("iPad. The sidebar stays beside the terminal: workspaces, studio-mac and build-box")
        beat(4)
        shot("05-pad-sidebar")
        chapter("iPad. An agent's card in the dock, with its state and answers")
        js(focusTab(host: "studio-mac", name: "build"))
        waitProbe("the build tab", timeout: 10) { self.activeName($0) == "build" }
        tapTerminal()
        beat(0.8)
        app.typeText("claude\n")
        waitProbe("the agent waiting", timeout: 40) { ($0["agents"] as? [String] ?? []).contains("needs-input") }
        if key("hide-keyboard").exists { key("hide-keyboard").tap() }
        beat(3)
        p = probe()
        assertLandscape(p)
        shot("05-pad-agent")
        js(click("#dock .agent.s-needs-input [data-reply=\"1\"]"))
        waitProbe("the answer reached the agent", timeout: 15) { ($0["screen"] as? String ?? "").contains("Updated parser.go") }
        beat(2)
        chapter("iPad. Themes, previewed as you go, ending on Tokyo Night")
        js("gecko.openThemePicker()")
        waitProbe("the theme picker", timeout: 6) { ($0["palette"] as? Bool) == true }
        js(paletteType("neo-tokyo"))
        beat(3.5)
        shot("05-pad-neo-tokyo")
        js(paletteType("tokyo night", enter: true))
        waitProbe("Tokyo Night", timeout: 10) { ($0["theme"] as? String) == "tokyo-night" }
        beat(3)
        p = probe()
        assertLandscape(p)
        shot("05-pad-tokyo-night")
    }

    // MARK: steps

    /// Pastes a link on the machine list and opens the machine under the name Gecko gives.
    private func addMachine(_ link: String) {
        UIPasteboard.general.string = link
        let paste = app.buttons["connect.paste"].exists ? app.buttons["connect.paste"] : app.buttons["Paste"]
        XCTAssertTrue(paste.waitForExistence(timeout: 5))
        paste.tap()
        var name = app.alerts["Name this machine"]
        if !name.waitForExistence(timeout: 8) {
            if app.alerts.buttons["OK"].exists { app.alerts.buttons["OK"].tap() }
            app.buttons["connect.type"].tap()
            let field = app.alerts.textFields.firstMatch
            XCTAssertTrue(field.waitForExistence(timeout: 5))
            field.typeText(link)
            app.alerts.buttons["Add"].tap()
            name = app.alerts["Name this machine"]
            XCTAssertTrue(name.waitForExistence(timeout: 10))
        }
        beat(2)
        name.buttons["Add and open"].tap()
    }

    private func openDrawer() {
        drag(from: web.coordinate(withNormalizedOffset: CGVector(dx: 0, dy: 0.5)).withOffset(CGVector(dx: 2, dy: 0)), to: terminalPoint(0.8, 0.5), hold: 0.02)
        if !waitFor(4, { ($0["drawer"] as? Bool) == true }) {
            // The swipe is the device tour's to check; the tour just needs the drawer.
            XCTContext.runActivity(named: "the swipe didn't open the drawer; opening it from the page") { _ in }
            js("gecko.toggleSidebar()")
        }
        waitProbe("the drawer", timeout: 5) { ($0["drawer"] as? Bool) == true }
    }

    private func closeDrawer() {
        drag(from: terminalPoint(0.7, 0.5), to: terminalPoint(0.05, 0.5), hold: 0.02)
        if !waitFor(4, { ($0["drawer"] as? Bool) == false }) {
            XCTContext.runActivity(named: "the swipe didn't close the drawer; closing it from the page") { _ in }
            js("gecko.closeDrawer()")
        }
        waitProbe("the drawer closed", timeout: 5) { ($0["drawer"] as? Bool) == false }
    }

    /// The active tab's name, from the probe's sessions (host/name/id).
    private func activeName(_ p: [String: Any]) -> String {
        let id = p["active"] as? String ?? ""
        guard !id.isEmpty else { return "" }
        for s in p["sessions"] as? [String] ?? [] where s.hasSuffix("/" + id) {
            let parts = s.split(separator: "/", omittingEmptySubsequences: false)
            if parts.count >= 2 { return String(parts[1]) }
        }
        return ""
    }

    // MARK: scripts for the page

    private func quoted(_ s: String) -> String {
        "'" + s.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "'", with: "\\'") + "'"
    }

    private func click(_ selector: String) -> String {
        "(function(){var e=document.querySelector(\(quoted(selector)));if(e)e.click();})()"
    }

    private func focusTab(host: String, name: String) -> String {
        // The newest of that name (the job opens them again for the tour).
        "(function(){var s=Array.from(gecko.sessions.values()).filter(function(x){return x.host===\(quoted(host))&&x.name===\(quoted(name));}).sort(function(a,b){return (b.created||0)-(a.created||0);})[0];if(s)gecko.focusSession(s.id);})()"
    }

    /// Types into the open palette one letter at a time (so the video shows
    /// it), then Enter if asked. The keyboard goes down first, so the
    /// previewed theme shows.
    private func paletteType(_ text: String, enter: Bool = false) -> String {
        """
        (function(){var i=document.querySelector('#palette input');if(!i)return;i.blur();var t=\(quoted(text)),n=0;i.value='';i.dispatchEvent(new Event('input'));
        var f=function(){if(n<t.length){i.value+=t[n++];i.dispatchEvent(new Event('input'));setTimeout(f,110);}
        else if(\(enter)){setTimeout(function(){i.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',bubbles:true}));},600);}};f();})()
        """
    }
}
