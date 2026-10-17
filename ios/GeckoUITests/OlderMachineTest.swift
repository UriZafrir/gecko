import XCTest

/// A machine running an older Gecko (protocol 6, GECKO_OLD_LINK): the app
/// shows its own UI there, with the phone features, and the tabs work.
final class OlderMachineTest: GeckoUITestCase {
    func testTheAppsOwnUiOnAnOlderGecko() throws {
        launch(open: try link("GECKO_OLD_LINK"), name: "older")
        allowNotificationsIfAsked()
        let p = waitOnline()
        XCTAssertEqual(p["bundled"] as? Bool, true, "served from the app")
        XCTAssertLessThan(p["daemon"] as? Int ?? 99, 7, "the machine speaks an older protocol")
        XCTAssertEqual(p["outdated"] as? Bool, false, "no restart nag")
        terminalPoint().tap()
        app.typeText("echo older-ok-$((6*7))\n")
        waitProbe("the shell there answers", timeout: 20) { ($0["screen"] as? String ?? "").contains("older-ok-42") }
        shot("04-older-gecko")
    }
}
