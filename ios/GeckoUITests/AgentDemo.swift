import XCTest

/// 03-agent: an agent (a stand-in for Claude Code that sets its window
/// title and asks its question) needs you while Gecko is in the background:
/// a notification with its answers; answering it from there.
final class AgentDemo: GeckoUITestCase {
    func testAgentNeedsYou() throws {
        launch(open: try link(), name: "studio-mac")
        allowNotificationsIfAsked()
        waitOnline()
        beat()
        tapTerminal()
        key("ctrl-c").tap() // a clean line, whatever an earlier test left on it
        beat(0.5)
        app.typeText("claude\n")
        waitProbe("the agent at work", timeout: 15) { ($0["agents"] as? [String] ?? []).contains("working") }
        key("hide-keyboard").tap()
        beat(2)
        shot("03-agent-working")

        // Leave the app: Gecko keeps watching for the time iOS gives it.
        XCUIDevice.shared.press(.home)
        let banner = springboard.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS[c] 'needs you'")).firstMatch
        XCTAssertTrue(banner.waitForExistence(timeout: 40), "a notification that claude needs you")
        beat(1)
        shot("03-notification")

        // Its answers, without opening the app.
        banner.press(forDuration: 1.2)
        let yes = springboard.buttons["Yes"]
        if yes.waitForExistence(timeout: 5) {
            beat(1.5)
            shot("03-notification-answers")
            yes.tap()
            beat(2)
            app.activate()
            waitOnline()
            waitProbe("the answer reached the agent", timeout: 20) { ($0["screen"] as? String ?? "").contains("Updated parser.go") }
        } else {
            // No expanded view: open the app from the notification and answer there.
            XCTContext.runActivity(named: "no answer buttons; opening the tab") { _ in
                if banner.exists { banner.tap() } else { app.activate() }
                waitOnline()
                waitProbe("the agent waiting", timeout: 10) { ($0["agents"] as? [String] ?? []).contains("needs-input") }
                tapTerminal()
                app.typeText("1")
                waitProbe("the answer reached the agent", timeout: 20) { ($0["screen"] as? String ?? "").contains("Updated parser.go") }
            }
        }
        beat(2)
        shot("03-answered")
    }
}
