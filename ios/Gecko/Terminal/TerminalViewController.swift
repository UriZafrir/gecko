import UIKit
import WebKit
import SwiftUI

/// One machine's Gecko web UI, fitted to the phone, with the extra keys
/// row under it (right above the keyboard while the keyboard is up).
final class TerminalViewController: UIViewController, WKNavigationDelegate, WKUIDelegate, WKScriptMessageHandlerWithReply {
    let machine: Machine
    var onLeave: () -> Void = {}
    var onTheme: (Bool) -> Void = { _ in }

    private(set) var web: WKWebView!
    let keys = ExtraKeysBar()
    private let topFill = UIView()
    private let bottomFill = UIView()
    private let errorView = ErrorPanel()
    private let origin: String
    /// The next main-frame navigation to the machine is the app's own page.
    private var expectingOwnLoad = false
    private var pageLoaded = false
    /// The connected Gecko's build, from its /api/info ("" when it's too old to say).
    private var daemonBuild: String?
    private var pendingSession: String?
    private var probe: UILabel?
    private var probeTimer: Timer?

    init(machine: Machine, session: String?) {
        self.machine = machine
        self.origin = LinkParser.origin(machine.url) ?? ""
        self.pendingSession = session
        super.init(nibName: nil, bundle: nil)
    }

    required init?(coder: NSCoder) { fatalError("not from a storyboard") }

    deinit {
        probeTimer?.invalidate()
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = UIColor(rgb: 0x19211A)
        topFill.backgroundColor = UIColor(rgb: 0x19211A)
        bottomFill.backgroundColor = keys.colors.bar

        let config = WKWebViewConfiguration()
        config.websiteDataStore = .default()
        config.setURLSchemeHandler(AssetSchemeHandler(), forURLScheme: AssetSchemeHandler.scheme)
        config.allowsInlineMediaPlayback = true
        config.mediaTypesRequiringUserActionForPlayback = []
        config.dataDetectorTypes = []
        let ucc = config.userContentController
        ucc.addUserScript(WKUserScript(source: BridgeScript.shim, injectionTime: .atDocumentStart, forMainFrameOnly: true))
        if AppModel.uiTest {
            ucc.addUserScript(WKUserScript(source: BridgeScript.assetCheck, injectionTime: .atDocumentEnd, forMainFrameOnly: true))
        }
        ucc.addScriptMessageHandler(WeakReplyHandler(self), contentWorld: .page, name: "gecko")

        web = WKWebView(frame: .zero, configuration: config)
        web.navigationDelegate = self
        web.uiDelegate = self
        web.allowsBackForwardNavigationGestures = false
        web.allowsLinkPreview = false
        web.isOpaque = false
        web.backgroundColor = .clear
        web.scrollView.backgroundColor = .clear
        // The page is the size of the view and scrolls its own terminals.
        web.scrollView.isScrollEnabled = false
        web.scrollView.bounces = false
        // Pinch is the page's (the font size): the scroll view's own zoom would take the touches.
        web.scrollView.pinchGestureRecognizer?.isEnabled = false
        web.scrollView.contentInsetAdjustmentBehavior = .never
        if #available(iOS 16.4, *) { web.isInspectable = true }
        web.accessibilityIdentifier = "terminal-web"
        web.hideInputAccessoryBar()

        keys.onKey = { [weak self] in self?.pressKey($0) }
        keys.onHideKeyboard = { [weak self] in self?.hideKeyboard() }
        keys.menuItems = { [weak self] in self?.menuItems() ?? [] }

        errorView.onRetry = { [weak self] in self?.load() }
        errorView.onMachines = { [weak self] in self?.onLeave() }
        errorView.isHidden = true

        for v in [topFill, web!, keys, bottomFill, errorView] as [UIView] {
            v.translatesAutoresizingMaskIntoConstraints = false
            view.addSubview(v)
        }
        let safe = view.safeAreaLayoutGuide
        let kb = view.keyboardLayoutGuide
        NSLayoutConstraint.activate([
            topFill.topAnchor.constraint(equalTo: view.topAnchor),
            topFill.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            topFill.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            topFill.bottomAnchor.constraint(equalTo: safe.topAnchor),
            web.topAnchor.constraint(equalTo: safe.topAnchor),
            web.leadingAnchor.constraint(equalTo: safe.leadingAnchor),
            web.trailingAnchor.constraint(equalTo: safe.trailingAnchor),
            web.bottomAnchor.constraint(equalTo: keys.topAnchor),
            keys.leadingAnchor.constraint(equalTo: safe.leadingAnchor),
            keys.trailingAnchor.constraint(equalTo: safe.trailingAnchor),
            keys.heightAnchor.constraint(greaterThanOrEqualToConstant: 46),
            keys.bottomAnchor.constraint(equalTo: kb.topAnchor),
            bottomFill.topAnchor.constraint(equalTo: keys.bottomAnchor),
            bottomFill.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            bottomFill.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            bottomFill.bottomAnchor.constraint(equalTo: view.bottomAnchor),
            errorView.topAnchor.constraint(equalTo: safe.topAnchor),
            errorView.leadingAnchor.constraint(equalTo: safe.leadingAnchor),
            errorView.trailingAnchor.constraint(equalTo: safe.trailingAnchor),
            errorView.bottomAnchor.constraint(equalTo: keys.topAnchor),
        ])

        let barHeight = keys.heightAnchor.constraint(equalToConstant: 46)
        barHeight.priority = .defaultLow
        barHeight.isActive = true

        if AppModel.uiTest { addProbe() }
        load()
    }

    // MARK: hardware keyboards

    /// The web view gets the keys of a hardware keyboard (an iPad's) itself,
    /// Cmd and Ctrl shortcuts included. Only what iPad keyboards lack is
    /// added: Cmd+. is Escape there, as in other iPad terminals.
    override var keyCommands: [UIKeyCommand]? {
        let esc = UIKeyCommand(input: ".", modifierFlags: .command, action: #selector(escapeKey))
        esc.discoverabilityTitle = "Escape"
        esc.wantsPriorityOverSystemBehavior = true
        return [esc]
    }

    @objc private func escapeKey() {
        pressKey("esc")
    }

    // MARK: loading

    /// Checks the machine (private network or https, and its token), then shows its Gecko.
    func load() {
        errorView.isHidden = true
        if machine.token.isEmpty { return showError("This machine's token couldn't be read on this phone. Scan its QR code again.") }
        guard let link = try? LinkParser.parse(machine.url) else { return showError("This machine's address can't be read. Add it again.") }
        let m = machine
        DispatchQueue.global(qos: .userInitiated).async {
            // The token must not travel in cleartext outside a private network.
            let ok = link.https || NetPolicy.cleartextAllowedResolving(link.host)
            guard ok else {
                Task { @MainActor [weak self] in self?.showError(Texts.cleartext(link.host)) }
                return
            }
            // The page itself never sends the token in a link, so check it here:
            // a wrong one gets a clear message instead of a page that can't connect.
            GeckoInfo.fetch(baseURL: m.url, token: m.token) { result in
                Task { @MainActor [weak self] in
                    guard let self else { return }
                    switch result {
                    case .refused: self.showError(Texts.tokenRefused)
                    case .unreachable(let why): self.showError("Can't reach \(m.name).\n\n\(why)")
                    case .ok(let info):
                        self.daemonBuild = info.build
                        self.openPage()
                    }
                }
            }
        }
    }

    /// The token goes in as the daemon's own cookie (what its ?token= link
    /// would set), then the app's page loads at the machine's address.
    private func openPage() {
        guard let base = URL(string: machine.url) else { return }
        var props: [HTTPCookiePropertyKey: Any] = [
            .name: "gecko_token",
            .value: machine.token,
            .path: "/",
            .originURL: base,
            .expires: Date(timeIntervalSinceNow: 400 * 24 * 3600),
            .sameSitePolicy: HTTPCookieStringPolicy.sameSiteStrict,
            HTTPCookiePropertyKey("HttpOnly"): "TRUE",
        ]
        if base.scheme == "https" { props[.secure] = "TRUE" }
        guard let cookie = HTTPCookie(properties: props) else { return loadBundled() }
        // The cookie store's answer can fail to come at all (seen on a fresh
        // Simulator: the page stayed blank), so the page loads after a few
        // seconds regardless; the cookie is set by then.
        var loaded = false
        let go = { [weak self] in
            guard !loaded else { return }
            loaded = true
            self?.loadBundled()
        }
        web.configuration.websiteDataStore.httpCookieStore.setCookie(cookie) { go() }
        DispatchQueue.main.asyncAfter(deadline: .now() + 3) { go() }
    }

    private func loadBundled() {
        guard let base = URL(string: machine.url) else { return }
        pageLoaded = false
        guard let html = BundledUI.html else {
            // A build without the bundled UI: the machine's own.
            var c = URLComponents(url: base, resolvingAgainstBaseURL: false)
            c?.queryItems = [URLQueryItem(name: "token", value: machine.token)]
            expectingOwnLoad = true
            if let u = c?.url { web.load(URLRequest(url: u)) }
            return
        }
        expectingOwnLoad = true
        web.loadHTMLString(html, baseURL: base)
    }

    private func showError(_ text: String) {
        errorView.text = text
        errorView.isHidden = false
        view.bringSubviewToFront(errorView)
    }

    // MARK: the page

    private func js(_ s: String) -> String {
        let data = try? JSONSerialization.data(withJSONObject: [s])
        let arr = data.flatMap { String(data: $0, encoding: .utf8) } ?? "[\"\"]"
        return String(arr.dropFirst().dropLast())
    }

    func eval(_ script: String, _ done: ((Any?) -> Void)? = nil) {
        web.evaluateJavaScript(script) { r, _ in done?(r) }
    }

    /// Runs action in the page once cond holds (the UI has loaded and synced), for up to 20 seconds.
    func whenReady(_ cond: String, _ action: String) {
        eval("""
        (function(){var n=0;var t=setInterval(function(){var g=window.gecko;n++;
        if(g&&g.firstSync&&(\(cond))){clearInterval(t);\(action)}else if(n>200)clearInterval(t);},100);})()
        """)
    }

    /// A tab to show (a tapped notification).
    func focus(session id: String) {
        Notifier.cancel(machineId: machine.id, sessionId: id)
        whenReady("g.sessions.has(\(js(id)))", "g.focusSession(\(js(id)))")
    }

    private func pressKey(_ name: String) {
        let fallback = ExtraKeysBar.fallback[name].map(js) ?? "null"
        let n = js(name)
        eval("""
        (function(){var g=window.gecko;if(!g)return;
        if(g.pressKey)g.pressKey(\(n));
        else if(\(n)==='ctrl'||\(n)==='alt'){g.mods[\(n)]=!g.mods[\(n)];g.renderKeybar();}
        else if(\(n)==='paste')g.paste();
        else if(\(n)==='select')g.openTextView();
        else if(\(n)==='compose')g.toggleCompose();
        else if(\(fallback)&&g.activeId())g.sendInput(g.activeId(),\(fallback));})()
        """)
    }

    private func hideKeyboard() {
        eval("document.activeElement&&document.activeElement.blur()")
        view.endEditing(true)
    }

    private func toastInPage(_ text: String) {
        eval("window.gecko&&gecko.toast(\(js(text)))")
    }

    /// The web UI's theme, for the bars around the page and the extra keys.
    private func applyTheme(_ json: String) {
        guard let d = json.data(using: .utf8), let o = (try? JSONSerialization.jsonObject(with: d)) as? [String: Any] else { return }
        let dark = o["dark"] as? Bool ?? true
        let bg = UIColor.css(o["bg"] as? String, UIColor(rgb: 0x19211A))
        let panel = UIColor.css(o["panel"] as? String, bg)
        keys.setColors(.init(bar: panel, key: UIColor.css(o["raise"] as? String, panel), text: UIColor.css(o["text"] as? String, UIColor(rgb: 0xD8E0D2)),
                             accent: UIColor.css(o["accent"] as? String, UIColor(rgb: 0x7CCB3A)), onAccent: UIColor.css(o["on-accent"] as? String, bg)))
        view.backgroundColor = bg
        topFill.backgroundColor = bg
        bottomFill.backgroundColor = panel
        onTheme(dark)
    }

    // MARK: the ⋮ menu

    private func menuItems() -> [UIMenuElement] {
        let model = AppModel.shared
        let main: [UIMenuElement] = [
            UIAction(title: "Search and commands", image: UIImage(systemName: "magnifyingglass")) { [weak self] _ in
                self?.eval("window.gecko&&gecko.openPalette()")
            },
            UIAction(title: "Share screen text", image: UIImage(systemName: "square.and.arrow.up")) { [weak self] _ in self?.shareScreen() },
            UIAction(title: "Keep screen on", image: UIImage(systemName: "sun.max"), state: model.keepScreenOn ? .on : .off) { _ in
                model.keepScreenOn.toggle()
            },
            UIAction(title: "Notify in background", image: UIImage(systemName: "bell.badge"), state: model.notifyInBackground ? .on : .off) { [weak self] _ in
                model.notifyInBackground.toggle()
                self?.toastInPage(model.notifyInBackground ? Texts.backgroundOn : "Gecko notifies you only while it is open.")
            },
            UIAction(title: "Reload", image: UIImage(systemName: "arrow.clockwise")) { [weak self] _ in self?.load() },
            UIAction(title: "Touch diagnostics", image: UIImage(systemName: "hand.point.up.left")) { [weak self] _ in self?.showTouchDiagnostics() },
            UIAction(title: "Machines", image: UIImage(systemName: "desktopcomputer")) { [weak self] _ in self?.onLeave() },
        ]
        let build = UIAction(title: AppBuild.id, subtitle: "Build. Tap to copy.") { _ in AppBuild.copy() }
        return main + [UIMenu(options: .displayInline, children: [build])]
    }

    /// The share sheet with what the tab shows (or its selection).
    private func shareScreen() {
        eval("""
        (function(){var g=window.gecko;var p=g&&g.activePane();if(!p)return '';
        if(p.term.hasSelection())return p.term.getSelection();
        var b=p.term.buffer.active,out=[];for(var i=b.viewportY;i<b.viewportY+p.term.rows;i++){var l=b.getLine(i);out.push(l?l.translateToString(true):'')}
        return out.join('\\n').replace(/\\s+$/,'')})()
        """) { [weak self] r in
            guard let text = r as? String, !text.isEmpty else { return }
            self?.share(text)
        }
    }

    func share(_ text: String) {
        let vc = UIActivityViewController(activityItems: [text], applicationActivities: nil)
        vc.popoverPresentationController?.sourceView = keys.more
        present(vc, animated: true)
    }

    /// This app's build id, what the last gesture on a terminal looked like
    /// (gecko.touchReport), and this device's screen.
    private func showTouchDiagnostics() {
        eval("window.gecko&&gecko.touchReport?gecko.touchReport():'The page has no touch report.'") { [weak self] r in
            guard let self else { return }
            let page = r as? String ?? "The page isn't loaded."
            var there = ""
            if let b = self.daemonBuild {
                if b.isEmpty { there = "\nGecko there: too old to report its build" }
                else if !b.hasPrefix(AppBuild.commit + " ") { there = "\nGecko there: build \(b)" }
            }
            let dev = UIDevice.current
            let text = "App build: \(AppBuild.id)\n\(page)\(there)\nDisplay: up to \(self.view.window?.screen.maximumFramesPerSecond ?? 60) Hz; \(dev.systemName) \(dev.systemVersion) (\(dev.model))"
            let a = UIAlertController(title: "Touch diagnostics", message: text, preferredStyle: .alert)
            a.addAction(UIAlertAction(title: "Copy", style: .default) { _ in UIPasteboard.general.string = text })
            a.addAction(UIAlertAction(title: "OK", style: .cancel))
            self.present(a, animated: true)
        }
    }

    // MARK: WKNavigationDelegate

    func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction, decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        let url = action.request.url
        guard let frame = action.targetFrame else {
            openOutside(url)
            return decisionHandler(.cancel)
        }
        guard frame.isMainFrame else { return decisionHandler(.allow) }
        if url?.scheme == "about" { return decisionHandler(.allow) }
        if let u = url, LinkParser.origin(u.absoluteString) == origin {
            if expectingOwnLoad || !BundledUI.available {
                expectingOwnLoad = false
                return decisionHandler(.allow)
            }
            // A reload (Restart Gecko) or a link to the page: the app's page again.
            decisionHandler(.cancel)
            DispatchQueue.main.async { [weak self] in self?.loadBundled() }
            return
        }
        // Links out of Gecko (in terminal output, docs) open in Safari.
        openOutside(url)
        decisionHandler(.cancel)
    }

    func webView(_ webView: WKWebView, didCommit navigation: WKNavigation!) {
        expectingOwnLoad = false
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        pageLoaded = true
        webView.hideInputAccessoryBar()
        webView.scrollView.pinchGestureRecognizer?.isEnabled = false
        if let s = pendingSession {
            pendingSession = nil
            focus(session: s)
        }
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        failed(error)
    }

    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
        failed(error)
    }

    private func failed(_ error: Error) {
        let e = error as NSError
        if e.domain == NSURLErrorDomain && e.code == NSURLErrorCancelled { return }
        if e.domain == "WebKitErrorDomain" && e.code == 102 { return } // frame load interrupted: our own reload
        showError("Can't reach \(machine.name).\n\n\(e.localizedDescription)")
    }

    func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
        loadBundled()
    }

    private func openOutside(_ url: URL?) {
        guard let url, let s = url.scheme?.lowercased(), ["http", "https", "mailto"].contains(s) else { return }
        UIApplication.shared.open(url)
    }

    // MARK: WKUIDelegate

    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration, for navigationAction: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        openOutside(navigationAction.request.url)
        return nil
    }

    func webView(_ webView: WKWebView, runJavaScriptAlertPanelWithMessage message: String, initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping () -> Void) {
        let a = UIAlertController(title: nil, message: message, preferredStyle: .alert)
        a.addAction(UIAlertAction(title: "OK", style: .default) { _ in completionHandler() })
        present(a, animated: true)
    }

    func webView(_ webView: WKWebView, runJavaScriptConfirmPanelWithMessage message: String, initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping (Bool) -> Void) {
        let a = UIAlertController(title: nil, message: message, preferredStyle: .alert)
        a.addAction(UIAlertAction(title: "Cancel", style: .cancel) { _ in completionHandler(false) })
        a.addAction(UIAlertAction(title: "OK", style: .default) { _ in completionHandler(true) })
        present(a, animated: true)
    }

    // MARK: the bridge (window.GeckoIOS)

    private func trusted(_ m: WKScriptMessage) -> Bool {
        guard m.frameInfo.isMainFrame else { return false }
        let o = m.frameInfo.securityOrigin
        return LinkParser.origin(scheme: o.protocol, host: o.host, port: o.port) == origin
    }

    func userContentController(_ ucc: WKUserContentController, didReceive message: WKScriptMessage, replyHandler: @escaping (Any?, String?) -> Void) {
        guard trusted(message), let body = message.body as? [String: Any], let op = body["op"] as? String else {
            return replyHandler(nil, "not allowed")
        }
        switch op {
        case "copy":
            UIPasteboard.general.string = body["text"] as? String ?? ""
            replyHandler(true, nil)
        case "paste":
            replyHandler(UIPasteboard.general.string, nil)
        case "notify":
            let id = body["id"] as? String ?? ""
            if !id.isEmpty {
                Notifier.agent(machine: machine, sessionId: id, title: body["title"] as? String ?? "", body: body["body"] as? String ?? "")
            }
            replyHandler(nil, nil)
        case "enableNotifications":
            AppModel.shared.enableNotifications { [weak self] text in self?.toastInPage(text) }
            replyHandler(nil, nil)
        case "mods":
            keys.setMods(ctrl: body["ctrl"] as? Bool ?? false, alt: body["alt"] as? Bool ?? false)
            replyHandler(nil, nil)
        case "theme":
            applyTheme(body["json"] as? String ?? "")
            replyHandler(nil, nil)
        case "share":
            share(body["text"] as? String ?? "")
            replyHandler(nil, nil)
        default:
            replyHandler(nil, "unknown")
        }
    }

    // MARK: UI tests

    /// A label the UI tests read: the page's state as JSON (only with GECKO_UITEST=1).
    /// UI tests: scripts the test leaves in GECKO_UITEST_CMDS (a folder on
    /// the Mac, which the Simulator's apps can read) run in the page, in order.
    private func runTestScripts() {
        guard let dir = ProcessInfo.processInfo.environment["GECKO_UITEST_CMDS"], !dir.isEmpty,
              let names = try? FileManager.default.contentsOfDirectory(atPath: dir) else { return }
        for n in names.filter({ $0.hasSuffix(".js") }).sorted() {
            let path = (dir as NSString).appendingPathComponent(n)
            guard let s = try? String(contentsOfFile: path, encoding: .utf8) else { continue }
            try? FileManager.default.removeItem(atPath: path)
            eval(s)
        }
    }

    private func addProbe() {
        let l = UILabel(frame: CGRect(x: 0, y: 0, width: 2, height: 2))
        l.isAccessibilityElement = true
        l.accessibilityIdentifier = "gecko.probe"
        l.accessibilityLabel = "{}"
        l.textColor = .clear
        view.addSubview(l)
        probe = l
        probeTimer = Timer.scheduledTimer(timeInterval: 0.5, target: self, selector: #selector(updateProbe), userInfo: nil, repeats: true)
    }

    @objc private func updateProbe() {
        runTestScripts()
        eval(BridgeScript.probe) { [weak self] r in
            guard let s = r as? String else { return }
            self?.probe?.accessibilityLabel = s
        }
    }
}

/// Holds the controller weakly: the content controller keeps its handlers.
final class WeakReplyHandler: NSObject, WKScriptMessageHandlerWithReply {
    weak var target: (NSObject & WKScriptMessageHandlerWithReply)?
    init(_ t: NSObject & WKScriptMessageHandlerWithReply) { target = t }
    func userContentController(_ ucc: WKUserContentController, didReceive message: WKScriptMessage, replyHandler: @escaping (Any?, String?) -> Void) {
        guard let t = target else { return replyHandler(nil, "gone") }
        t.userContentController(ucc, didReceive: message, replyHandler: replyHandler)
    }
}

extension WKWebView {
    /// The bar WebKit puts above the keyboard for a focused field (arrows
    /// and Done) is replaced by the app's own extra keys: its content view
    /// gets a subclass with no input accessory view.
    func hideInputAccessoryBar() {
        guard let content = scrollView.subviews.first(where: { String(describing: type(of: $0)).hasPrefix("WKContent") }) else { return }
        let base: AnyClass = type(of: content)
        let baseName = String(cString: class_getName(base))
        if baseName.hasPrefix("GeckoNoBar_") { return }
        let name = "GeckoNoBar_" + baseName
        var cls: AnyClass? = NSClassFromString(name)
        if cls == nil, let made = objc_allocateClassPair(base, name, 0) {
            let sel = #selector(getter: UIResponder.inputAccessoryView)
            let none: @convention(block) (AnyObject) -> UIView? = { _ in nil }
            if let m = class_getInstanceMethod(UIResponder.self, sel) {
                class_addMethod(made, sel, imp_implementationWithBlock(none), method_getTypeEncoding(m))
            }
            objc_registerClassPair(made)
            cls = made
        }
        if let cls { object_setClass(content, cls) }
    }
}

/// What shows instead of the page when the machine can't be reached.
final class ErrorPanel: UIView {
    var onRetry: () -> Void = {}
    var onMachines: () -> Void = {}
    private let label = UILabel()

    var text: String {
        get { label.text ?? "" }
        set { label.text = newValue }
    }

    override init(frame: CGRect) {
        super.init(frame: frame)
        backgroundColor = UIColor(rgb: 0x19211A)
        accessibilityIdentifier = "terminal-error"
        label.numberOfLines = 0
        label.textColor = UIColor(rgb: 0xD8E0D2)
        label.font = .preferredFont(forTextStyle: .body)
        label.textAlignment = .center
        label.accessibilityIdentifier = "terminal-error-text"
        var c = UIButton.Configuration.filled()
        c.title = "Try again"
        c.baseBackgroundColor = UIColor(rgb: 0x7CCB3A)
        c.baseForegroundColor = UIColor(rgb: 0x19211A)
        let retry = UIButton(configuration: c, primaryAction: UIAction { [weak self] _ in self?.onRetry() })
        var c2 = UIButton.Configuration.plain()
        c2.title = "Machines"
        c2.baseForegroundColor = UIColor(rgb: 0xD8E0D2)
        let machines = UIButton(configuration: c2, primaryAction: UIAction { [weak self] _ in self?.onMachines() })
        let stack = UIStackView(arrangedSubviews: [label, retry, machines])
        stack.axis = .vertical
        stack.spacing = 16
        stack.alignment = .center
        stack.translatesAutoresizingMaskIntoConstraints = false
        addSubview(stack)
        NSLayoutConstraint.activate([
            stack.centerYAnchor.constraint(equalTo: centerYAnchor),
            stack.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 28),
            stack.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -28),
        ])
    }

    required init?(coder: NSCoder) { fatalError("not from a storyboard") }
}

/// The terminal screen in SwiftUI.
struct TerminalScreen: UIViewControllerRepresentable {
    let machine: Machine
    @EnvironmentObject var model: AppModel

    func makeUIViewController(context: Context) -> TerminalViewController {
        let vc = TerminalViewController(machine: machine, session: model.pendingSession)
        model.pendingSession = nil
        vc.onLeave = { [weak model] in model?.leave() }
        vc.onTheme = { [weak model] dark in
            if model?.terminalDark != dark { model?.terminalDark = dark }
        }
        model.terminal = vc
        return vc
    }

    func updateUIViewController(_ vc: TerminalViewController, context: Context) {}
}

/// Messages shared by the connect screen and the terminal.
enum Texts {
    static func cleartext(_ host: String) -> String {
        "\(host) isn't on a private network, so Gecko's token would cross the internet unencrypted. Use https there (for example tailscale serve 7681), or the machine's LAN or Tailscale address."
    }
    static let tokenRefused = "Gecko refused the token. It changes when Gecko is set up again: scan the QR code again."
    static let noToken = "That link has no access token in it. Use the link or QR code from Open on another device on the computer itself, or run gecko url there."
    static let backgroundOn = "Gecko keeps watching your agents for a short while after you leave the app, and checks now and then while iOS allows it."
}
