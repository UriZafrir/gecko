import XCTest

/// 01-connect: paste Gecko's link, name the machine, and its Gecko opens.
final class ConnectDemo: GeckoUITestCase {
    func testPasteLinkAndConnect() throws {
        let link = try link()
        launch()
        XCTAssertTrue(app.descendants(matching: .any)["connect.empty"].waitForExistence(timeout: 10))
        let build = app.buttons["connect.build-id"]
        XCTAssertTrue(build.exists)
        XCTAssertFalse(build.label.contains("unknown"), build.label)
        beat(1.5)
        shot("01-machines-empty")

        // The link Gecko's Open on another device shows, copied on the phone.
        UIPasteboard.general.string = link
        let paste = app.buttons["connect.paste"].exists ? app.buttons["connect.paste"] : app.buttons["Paste"]
        XCTAssertTrue(paste.waitForExistence(timeout: 5))
        paste.tap()
        var nameAlert = app.alerts["Name this machine"]
        if !nameAlert.waitForExistence(timeout: 8) {
            // The system paste button didn't take the synthesized tap: type the link instead.
            XCTContext.runActivity(named: "paste button gave nothing; typing the link") { _ in
                if app.alerts.buttons["OK"].exists { app.alerts.buttons["OK"].tap() }
                app.buttons["connect.type"].tap()
                let field = app.alerts.textFields.firstMatch
                XCTAssertTrue(field.waitForExistence(timeout: 5))
                field.typeText(link)
                app.alerts.buttons["Add"].tap()
            }
            nameAlert = app.alerts["Name this machine"]
            XCTAssertTrue(nameAlert.waitForExistence(timeout: 10))
        }
        beat()
        shot("01-name-machine")
        nameAlert.buttons["Add and open"].tap()
        allowNotificationsIfAsked()

        let p = waitOnline()
        XCTAssertEqual(p["bundled"] as? Bool, true, "the app's own web UI")
        XCTAssertEqual(p["ios"] as? Bool, true)
        XCTAssertEqual(p["keybar"] as? String, "none", "the web key bar gives way to the app's")
        XCTAssertTrue(key("esc").exists && key("ctrl").exists && key("menu").exists)
        beat(2)
        shot("01-connected")
        waitProbe("the theme photos over gecko-asset:", timeout: 10) { ($0["asset"] as? String) == "ok" }

        // The build id at the bottom of the menu.
        key("menu").tap()
        let id = app.buttons["menu.build-id"].exists ? app.buttons["menu.build-id"] : app.buttons.containing(NSPredicate(format: "label CONTAINS ' · '")).firstMatch
        XCTAssertTrue(id.waitForExistence(timeout: 5))
        beat(1.5)
        shot("01-menu-build-id")
        // Back to the machine list: it is remembered.
        app.buttons["Machines"].tap()
        let row = app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'machine.'")).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 5))
        beat(1.5)
        shot("01-machine-saved")
        row.tap()
        waitOnline()
        beat(1.5)
    }
}
