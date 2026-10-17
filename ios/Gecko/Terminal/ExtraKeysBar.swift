import UIKit

/// The row of keys a phone keyboard lacks, right above the keyboard (and at
/// the bottom of the screen while it is down): Esc, Tab, sticky Ctrl and
/// Alt, arrows that repeat while held, the symbols that are awkward to
/// reach, paste, select and compose. Keys never take focus, so the keyboard
/// stays up. The same keys as the Android app's ExtraKeys.kt.
final class ExtraKeysBar: UIView {
    struct Key {
        let name: String
        let label: String
        var repeats = false
        var spoken: String?
    }

    static let keys: [Key] = [
        Key(name: "esc", label: "esc"),
        Key(name: "tab", label: "tab"),
        Key(name: "ctrl", label: "ctrl"),
        Key(name: "alt", label: "alt"),
        Key(name: "left", label: "←", repeats: true, spoken: "Left"),
        Key(name: "up", label: "↑", repeats: true, spoken: "Up"),
        Key(name: "down", label: "↓", repeats: true, spoken: "Down"),
        Key(name: "right", label: "→", repeats: true, spoken: "Right"),
        Key(name: "pipe", label: "|", spoken: "Pipe"),
        Key(name: "tilde", label: "~", spoken: "Tilde"),
        Key(name: "slash", label: "/", spoken: "Slash"),
        Key(name: "dash", label: "-", spoken: "Dash"),
        Key(name: "ctrl-c", label: "^C", spoken: "Control C"),
        Key(name: "home", label: "home"),
        Key(name: "end", label: "end"),
        Key(name: "pgup", label: "pgup", spoken: "Page up"),
        Key(name: "pgdn", label: "pgdn", spoken: "Page down"),
        Key(name: "paste", label: "paste"),
        Key(name: "select", label: "select"),
        Key(name: "compose", label: "compose"),
    ]

    /// What each key sends when the web UI is too old to have pressKey.
    static let fallback: [String: String] = [
        "esc": "\u{1b}", "tab": "\t", "up": "\u{1b}[A", "down": "\u{1b}[B", "left": "\u{1b}[D", "right": "\u{1b}[C",
        "pipe": "|", "tilde": "~", "slash": "/", "dash": "-", "ctrl-c": "\u{03}",
        "home": "\u{1b}[H", "end": "\u{1b}[F", "pgup": "\u{1b}[5~", "pgdn": "\u{1b}[6~",
    ]

    struct Colors {
        var bar = UIColor(rgb: 0x1E2620)
        var key = UIColor(rgb: 0x2B352D)
        var text = UIColor(rgb: 0xD8E0D2)
        var accent = UIColor(rgb: 0x7CCB3A)
        var onAccent = UIColor(rgb: 0x19211A)
    }

    /// Pressed: the web UI's key name (main.js pressKey).
    var onKey: (String) -> Void = { _ in }
    var onHideKeyboard: () -> Void = {}
    /// The ⋮ menu, built when it opens.
    var menuItems: () -> [UIMenuElement] = { [] }

    private(set) var colors = Colors()
    private var ctrl = false
    private var alt = false
    private var buttons: [String: KeyButton] = [:]
    private let scroll = UIScrollView()
    private let row = UIStackView()
    private let hide = UIButton(type: .system)
    let more = UIButton(type: .system)
    private var repeater: Timer?
    private let haptic = UIImpactFeedbackGenerator(style: .light)

    override init(frame: CGRect) {
        super.init(frame: frame)
        accessibilityIdentifier = "extra-keys"
        scroll.showsHorizontalScrollIndicator = false
        scroll.delaysContentTouches = false
        scroll.canCancelContentTouches = true
        scroll.alwaysBounceHorizontal = true
        row.axis = .horizontal
        row.spacing = 5
        row.alignment = .center
        for k in Self.keys { row.addArrangedSubview(button(k)) }

        hide.setImage(UIImage(systemName: "keyboard.chevron.compact.down"), for: .normal)
        hide.accessibilityLabel = "Hide keyboard"
        hide.accessibilityIdentifier = "key.hide-keyboard"
        hide.addAction(UIAction { [weak self] _ in self?.onHideKeyboard() }, for: .touchUpInside)

        more.setImage(UIImage(systemName: "ellipsis.circle"), for: .normal)
        more.accessibilityLabel = "More options"
        more.accessibilityIdentifier = "key.menu"
        more.showsMenuAsPrimaryAction = true
        more.menu = UIMenu(children: [UIDeferredMenuElement.uncached { [weak self] done in done(self?.menuItems() ?? []) }])

        for v in [scroll, row, hide, more] as [UIView] { v.translatesAutoresizingMaskIntoConstraints = false }
        scroll.addSubview(row)
        addSubview(scroll)
        addSubview(hide)
        addSubview(more)
        NSLayoutConstraint.activate([
            scroll.leadingAnchor.constraint(equalTo: leadingAnchor),
            scroll.topAnchor.constraint(equalTo: topAnchor),
            scroll.bottomAnchor.constraint(equalTo: bottomAnchor),
            scroll.trailingAnchor.constraint(equalTo: hide.leadingAnchor, constant: -2),
            row.leadingAnchor.constraint(equalTo: scroll.contentLayoutGuide.leadingAnchor, constant: 6),
            row.trailingAnchor.constraint(equalTo: scroll.contentLayoutGuide.trailingAnchor, constant: -6),
            row.topAnchor.constraint(equalTo: scroll.contentLayoutGuide.topAnchor),
            row.bottomAnchor.constraint(equalTo: scroll.contentLayoutGuide.bottomAnchor),
            row.heightAnchor.constraint(equalTo: scroll.frameLayoutGuide.heightAnchor),
            hide.widthAnchor.constraint(equalToConstant: 40),
            hide.topAnchor.constraint(equalTo: topAnchor),
            hide.bottomAnchor.constraint(equalTo: bottomAnchor),
            hide.trailingAnchor.constraint(equalTo: more.leadingAnchor),
            more.widthAnchor.constraint(equalToConstant: 44),
            more.topAnchor.constraint(equalTo: topAnchor),
            more.bottomAnchor.constraint(equalTo: bottomAnchor),
            more.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -2),
        ])
        paint()
    }

    required init?(coder: NSCoder) { fatalError("not from a storyboard") }

    func setMods(ctrl: Bool, alt: Bool) {
        self.ctrl = ctrl
        self.alt = alt
        paint()
    }

    func setColors(_ c: Colors) {
        colors = c
        paint()
    }

    private func button(_ k: Key) -> KeyButton {
        let b = KeyButton(label: k.label)
        b.accessibilityLabel = k.spoken ?? k.label
        b.accessibilityIdentifier = "key." + k.name
        b.addAction(UIAction { [weak self] _ in self?.down(k) }, for: .touchDown)
        for e: UIControl.Event in [.touchUpInside, .touchUpOutside, .touchCancel, .touchDragExit] {
            b.addAction(UIAction { [weak self] _ in self?.stopRepeat() }, for: e)
        }
        buttons[k.name] = b
        return b
    }

    private func down(_ k: Key) {
        haptic.impactOccurred()
        stopRepeat()
        press(k)
        guard k.repeats else { return }
        // Arrows repeat while held, as a keyboard's keys do.
        repeating = k
        repeater = Timer.scheduledTimer(timeInterval: 0.42, target: self, selector: #selector(startRepeating), userInfo: nil, repeats: false)
    }

    private var repeating: Key?

    @objc private func startRepeating() {
        repeater = Timer.scheduledTimer(timeInterval: 0.06, target: self, selector: #selector(repeatOnce), userInfo: nil, repeats: true)
    }

    @objc private func repeatOnce() {
        if let k = repeating { press(k) }
    }

    private func stopRepeat() {
        repeater?.invalidate()
        repeater = nil
        repeating = nil
    }

    private func press(_ k: Key) {
        // Sticky keys light up at once; the web UI confirms (GeckoIOS.mods).
        if k.name == "ctrl" { ctrl.toggle() }
        if k.name == "alt" { alt.toggle() }
        paint()
        onKey(k.name)
    }

    func isOn(_ name: String) -> Bool { (name == "ctrl" && ctrl) || (name == "alt" && alt) }

    private func paint() {
        backgroundColor = colors.bar
        for (name, b) in buttons {
            let on = isOn(name)
            b.backgroundColor = on ? colors.accent : colors.key
            b.title.textColor = on ? colors.onAccent : colors.text
            b.accessibilityValue = (name == "ctrl" || name == "alt") ? (on ? "on" : "off") : nil
        }
        hide.tintColor = colors.text
        more.tintColor = colors.text
    }
}

/// One key: a label in a rounded box. A control, not a button that could
/// take the keyboard's focus.
final class KeyButton: UIControl {
    let title = UILabel()

    init(label: String) {
        super.init(frame: .zero)
        isAccessibilityElement = true
        accessibilityTraits = .keyboardKey
        layer.cornerRadius = 6
        layer.cornerCurve = .continuous
        title.text = label
        let word = label.count > 1 && (label.first?.isLetter ?? false)
        // Larger text settings make the keys larger too, up to a point.
        let base: UIFont = word ? .systemFont(ofSize: 14, weight: .medium) : .monospacedSystemFont(ofSize: 17, weight: .regular)
        title.font = UIFontMetrics(forTextStyle: .body).scaledFont(for: base, maximumPointSize: word ? 22 : 26)
        title.adjustsFontForContentSizeCategory = true
        title.textAlignment = .center
        title.translatesAutoresizingMaskIntoConstraints = false
        title.isUserInteractionEnabled = false
        addSubview(title)
        translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            title.centerXAnchor.constraint(equalTo: centerXAnchor),
            title.centerYAnchor.constraint(equalTo: centerYAnchor),
            title.leadingAnchor.constraint(greaterThanOrEqualTo: leadingAnchor, constant: 8),
            widthAnchor.constraint(greaterThanOrEqualToConstant: word ? 44 : 36),
            widthAnchor.constraint(greaterThanOrEqualTo: title.widthAnchor, constant: 16),
            heightAnchor.constraint(greaterThanOrEqualToConstant: 36),
            heightAnchor.constraint(greaterThanOrEqualTo: title.heightAnchor, constant: 10),
        ])
        let h = heightAnchor.constraint(equalToConstant: 36)
        h.priority = .defaultLow
        h.isActive = true
    }

    required init?(coder: NSCoder) { fatalError("not from a storyboard") }

    override var isHighlighted: Bool {
        didSet { alpha = isHighlighted ? 0.6 : 1 }
    }
}

extension UIColor {
    convenience init(rgb: UInt32, alpha: CGFloat = 1) {
        self.init(red: CGFloat((rgb >> 16) & 0xff) / 255, green: CGFloat((rgb >> 8) & 0xff) / 255, blue: CGFloat(rgb & 0xff) / 255, alpha: alpha)
    }

    /// A CSS color as the web UI's themes write them: #rgb, #rrggbb or #rrggbbaa.
    static func css(_ s: String?, _ fallback: UIColor) -> UIColor {
        guard var h = s?.trimmingCharacters(in: .whitespaces), h.hasPrefix("#") else { return fallback }
        h.removeFirst()
        if h.count == 3 { h = h.map { "\($0)\($0)" }.joined() }
        guard h.count == 6 || h.count == 8, let v = UInt64(h, radix: 16) else { return fallback }
        if h.count == 6 { return UIColor(rgb: UInt32(v)) }
        return UIColor(rgb: UInt32(v >> 8), alpha: CGFloat(v & 0xff) / 255)
    }
}
