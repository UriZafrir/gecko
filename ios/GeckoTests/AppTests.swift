import CoreImage
import CoreImage.CIFilterBuiltins
import UIKit
import XCTest
@testable import Gecko

/// The app's own pieces that need iOS: the bundled web UI, QR pictures,
/// the Keychain.
final class AppTests: XCTestCase {
    func testBundledUiIsInTheApp() throws {
        let html = try XCTUnwrap(BundledUI.html, "Generated/web/index.html is missing: run make ios-web")
        XCTAssertTrue(html.contains(#"<meta name="gecko-ui" content="ios-app">"#))
        XCTAssertTrue(html.contains("user-scalable=no"))
        // Everything the page needs to start is inside it.
        XCTAssertFalse(html.contains(#"src="/main.js""#))
        XCTAssertFalse(html.contains(#"href="/main.css""#))
        XCTAssertTrue(html.contains("data:font/woff2;base64,"))
        XCTAssertNotNil(AssetSchemeHandler.file(for: URL(string: "gecko-asset://ui/themes/fuji.jpg")).flatMap { try? Data(contentsOf: $0) })
    }

    func testAssetPathsStayInside() {
        let root = URL(fileURLWithPath: "/app/Generated/web")
        XCTAssertEqual(AssetSchemeHandler.file(for: URL(string: "gecko-asset://ui/icon.svg"), root: root)?.path, "/app/Generated/web/icon.svg")
        for bad in ["gecko-asset://ui/../secret", "gecko-asset://ui/a/../../b", "gecko-asset://other/icon.svg", "gecko-asset://ui/", "http://ui/icon.svg", "gecko-asset://ui//etc/passwd"] {
            XCTAssertNil(AssetSchemeHandler.file(for: URL(string: bad), root: root), bad)
        }
    }

    func testBuildIdIsSet() {
        XCTAssertNotEqual(AppBuild.id, "unknown")
        XCTAssertFalse(AppBuild.commit.isEmpty)
    }

    func testQrCodeInAPicture() throws {
        let link = "http://192.168.1.20:7681/?token=4dd99c1f25e82e4c078bc059dcab8d7f262727a88ea7ae6e"
        let f = CIFilter.qrCodeGenerator()
        f.message = Data(link.utf8)
        f.correctionLevel = "M"
        let code = try XCTUnwrap(f.outputImage).transformed(by: CGAffineTransform(scaleX: 8, y: 8))
        // On a white page, like a screenshot.
        let page = code.composited(over: CIImage(color: .white).cropped(to: code.extent.insetBy(dx: -80, dy: -80)))
        let cg = try XCTUnwrap(CIContext().createCGImage(page, from: page.extent))
        XCTAssertEqual(QRImage.decode(UIImage(cgImage: cg)), link)
        XCTAssertNil(QRImage.decode(UIImage(cgImage: try XCTUnwrap(CIContext().createCGImage(CIImage(color: .white).cropped(to: CGRect(x: 0, y: 0, width: 200, height: 200)), from: CGRect(x: 0, y: 0, width: 200, height: 200))))))
    }

    func testKeychainRoundTrip() {
        let k = Keychain()
        let id = "test-" + UUID().uuidString
        XCTAssertTrue(k.setToken("first-token", for: id), "Keychain status \(k.lastStatus)")
        XCTAssertEqual(k.token(for: id), "first-token")
        XCTAssertTrue(k.setToken("second-token", for: id))
        XCTAssertEqual(k.token(for: id), "second-token")
        k.removeToken(for: id)
        XCTAssertNil(k.token(for: id))
    }

    func testThemeColors() {
        var r: CGFloat = 0, g: CGFloat = 0, b: CGFloat = 0, a: CGFloat = 0
        UIColor.css("#7ccb3a", .black).getRed(&r, green: &g, blue: &b, alpha: &a)
        XCTAssertEqual([r, g, b, a].map { Int(($0 * 255).rounded()) }, [124, 203, 58, 255])
        UIColor.css("#11223380", .black).getRed(&r, green: &g, blue: &b, alpha: &a)
        XCTAssertEqual([r, g, b, a].map { Int(($0 * 255).rounded()) }, [17, 34, 51, 128])
        UIColor.css("rgb(1,2,3)", .white).getRed(&r, green: &g, blue: &b, alpha: &a)
        XCTAssertEqual([r, g, b], [1, 1, 1])
    }

    func testInfoFromAnOlderGecko() {
        // Protocol 6 Gecko: /api/info has no build.
        XCTAssertEqual(GeckoInfo.parse(["host": "old", "urls": []]).build, "")
        XCTAssertEqual(GeckoInfo.parse(["host": "old"]).host, "old")
        XCTAssertTrue(GeckoInfo.parse(["commit": "abc1234", "built": "2026-10-16T23:15:00Z", "version": "v0.2.0"]).build.hasSuffix("(v0.2.0)"))
    }
}
