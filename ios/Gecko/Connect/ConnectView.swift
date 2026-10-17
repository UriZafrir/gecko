import PhotosUI
import SwiftUI

/// Adding and editing machines: a scanned or pasted link is checked with
/// the machine (its /api/info, with the token), named, saved, and opened.
@MainActor
final class ConnectFlow: ObservableObject {
    enum Prompt {
        case typeLink
        case name(GeckoLink)
        case unreachable(GeckoLink, String)
        case message(String)
        case rename(Machine)
        case newLink(Machine)
        case remove(Machine)
    }

    @Published var prompt: Prompt?
    @Published var busy: String?
    /// The text field of the prompt showing.
    @Published var field = ""
    private var suggested = ""
    private let model = AppModel.shared

    /// Shows a prompt once the one closing has gone.
    func next(_ p: Prompt) {
        Task { @MainActor in
            try? await Task.sleep(nanoseconds: 450_000_000)
            self.prompt = p
        }
    }

    func add(text: String) {
        let link: GeckoLink
        do { link = try LinkParser.parse(text) } catch {
            return next(.message((error as? LinkError)?.message ?? "That doesn't look like a link."))
        }
        guard let token = link.token else { return next(.message(Texts.noToken)) }
        busy = "Connecting to \(link.host)…"
        DispatchQueue.global(qos: .userInitiated).async {
            if !link.https && !NetPolicy.cleartextAllowedResolving(link.host) {
                Task { @MainActor in
                    self.busy = nil
                    self.prompt = .message(Texts.cleartext(link.host))
                }
                return
            }
            GeckoInfo.fetch(baseURL: link.baseURL, token: token) { r in
                Task { @MainActor in
                    self.busy = nil
                    switch r {
                    case .refused: self.prompt = .message(Texts.tokenRefused)
                    case .ok(let info): self.named(link, suggested: info.host)
                    case .unreachable(let why): self.prompt = .unreachable(link, why)
                    }
                }
            }
        }
    }

    /// Names a new machine. Scanning a known machine again just updates its token.
    func named(_ link: GeckoLink, suggested: String?) {
        if var known = model.store.find(url: link.baseURL) {
            known.token = link.token ?? known.token
            model.store.save(known)
            model.reload()
            prompt = .message("Updated the token for \(known.name).")
            return
        }
        self.suggested = suggested ?? link.host
        field = self.suggested
        prompt = .name(link)
    }

    func confirmName(_ link: GeckoLink) {
        let name = field.trimmingCharacters(in: .whitespacesAndNewlines)
        let m = Machine(name: name.isEmpty ? suggested : name, url: link.baseURL, token: link.token ?? "")
        model.store.save(m)
        model.reload()
        model.open(m)
    }

    func rename(_ m: Machine) {
        var x = m
        let name = field.trimmingCharacters(in: .whitespacesAndNewlines)
        if !name.isEmpty { x.name = name }
        model.store.save(x)
        model.reload()
    }

    func relink(_ m: Machine) {
        do {
            let l = try LinkParser.parse(field)
            var x = m
            x.url = l.baseURL
            x.token = l.token ?? m.token
            model.store.save(x)
            model.reload()
        } catch {
            next(.message((error as? LinkError)?.message ?? "That doesn't look like a link."))
        }
    }

    func remove(_ m: Machine) {
        model.store.remove(m.id)
        Background.save(statuses: [:], for: m.id)
        model.reload()
    }

    var title: String {
        switch prompt {
        case .typeLink: return "Paste Gecko's link"
        case .name: return "Name this machine"
        case .unreachable(let l, _): return "Couldn't reach \(l.host)"
        case .message: return "Gecko"
        case .rename: return "Rename"
        case .newLink: return "New link"
        case .remove(let m): return "Remove \(m.name)?"
        case nil: return ""
        }
    }

    var message: String {
        switch prompt {
        case .typeLink: return "On the computer, open the palette and pick Open on another device, or run gecko url there."
        case .name(let l): return l.baseURL
        case .unreachable(_, let why):
            return "\(why)\n\nIs the phone on the same network or tailnet, and is Gecko listening on it (gecko daemon --listen 0.0.0.0:7681)? You can save it now and connect later."
        case .message(let m): return m
        case .rename(let m): return m.url
        case .newLink: return "Paste a new link to change the token."
        case .remove: return "Its tabs keep running on the computer. You can add it again by scanning its QR code."
        case nil: return ""
        }
    }
}

/// The machines this phone knows; add one by scanning Gecko's QR code,
/// pasting its link, or picking a picture of the QR code.
struct ConnectView: View {
    @EnvironmentObject var model: AppModel
    @StateObject private var flow = ConnectFlow()
    @State private var scanning = false
    @State private var photo: PhotosPickerItem?
    @State private var copied = false

    var body: some View {
        NavigationStack {
            List {
                if model.machines.isEmpty {
                    Section { empty }
                } else {
                    Section("Machines") {
                        ForEach(model.machines) { m in row(m) }
                    }
                }
            }
            .navigationTitle("Gecko")
            .toolbar {
                ToolbarItem(placement: .navigationBarTrailing) {
                    Menu {
                        Toggle(isOn: $model.notifyInBackground) { Label("Notify in background", systemImage: "bell.badge") }
                        Button { AppBuild.copy(); flash() } label: { Label(AppBuild.id, systemImage: "number") }
                    } label: {
                        Image(systemName: "ellipsis.circle")
                    }
                    .accessibilityLabel("More options")
                    .accessibilityIdentifier("connect.menu")
                }
            }
            .safeAreaInset(edge: .bottom) { actions }
            .overlay {
                if let b = flow.busy {
                    VStack(spacing: 12) {
                        ProgressView()
                        Text(b).font(.callout)
                    }
                    .padding(24)
                    .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 14))
                    .accessibilityIdentifier("connect.busy")
                }
            }
            .overlay(alignment: .top) {
                if copied {
                    Text("Build id copied")
                        .font(.callout)
                        .padding(.horizontal, 14).padding(.vertical, 8)
                        .background(.regularMaterial, in: Capsule())
                        .transition(.opacity)
                }
            }
            .alert(flow.title, isPresented: Binding(get: { flow.prompt != nil }, set: { if !$0 { flow.prompt = nil } })) {
                alertActions
            } message: {
                Text(flow.message)
            }
            .sheet(isPresented: $scanning) {
                QRScannerView(onCode: { code in
                    scanning = false
                    flow.add(text: code)
                }, onCancel: { scanning = false })
                .ignoresSafeArea()
            }
            .onChange(of: photo) { item in pick(item) }
        }
    }

    private var empty: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Connect to a computer").font(.title3.weight(.semibold))
            Text("On the computer running Gecko, open the palette and pick Open on another device. Scan the QR code it shows, or paste its link here.")
            Text("The phone needs to reach that computer: the same Wi-Fi, or Tailscale.")
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 6)
        .accessibilityIdentifier("connect.empty")
    }

    private func row(_ m: Machine) -> some View {
        Button { model.open(m) } label: {
            HStack(spacing: 12) {
                Image(systemName: "desktopcomputer")
                    .font(.title3)
                    .foregroundStyle(Color(red: 0.49, green: 0.8, blue: 0.23))
                VStack(alignment: .leading, spacing: 2) {
                    Text(m.name).font(.headline).foregroundStyle(.primary)
                    Text(m.token.isEmpty ? "\(m.host) · scan its QR code again" : m.url)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            .padding(.vertical, 2)
        }
        .accessibilityIdentifier("machine." + m.name)
        .contextMenu {
            Button { flow.field = m.name; flow.prompt = .rename(m) } label: { Label("Rename", systemImage: "pencil") }
            Button { flow.field = ""; flow.prompt = .newLink(m) } label: { Label("Paste a new link", systemImage: "link") }
            Button(role: .destructive) { flow.prompt = .remove(m) } label: { Label("Remove", systemImage: "trash") }
        }
        .swipeActions {
            Button(role: .destructive) { flow.prompt = .remove(m) } label: { Label("Remove", systemImage: "trash") }
        }
    }

    private var actions: some View {
        VStack(spacing: 10) {
            Button { scanning = true } label: {
                Label("Scan QR code", systemImage: "qrcode.viewfinder").frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
            .tint(Color(red: 0.36, green: 0.62, blue: 0.16))
            .accessibilityIdentifier("connect.scan")
            HStack(spacing: 8) {
                // The system's paste button: no "Allow Paste?" question.
                PasteButton(payloadType: String.self) { strings in
                    let text = strings.first ?? ""
                    Task { @MainActor in flow.add(text: text) }
                }
                .labelStyle(.titleAndIcon)
                .buttonBorderShape(.capsule)
                .accessibilityIdentifier("connect.paste")
                Button { flow.field = ""; flow.prompt = .typeLink } label: {
                    Label("Type", systemImage: "keyboard")
                }
                .buttonStyle(.bordered)
                .buttonBorderShape(.capsule)
                .accessibilityIdentifier("connect.type")
                PhotosPicker(selection: $photo, matching: .images) {
                    Label("Picture", systemImage: "photo")
                }
                .buttonStyle(.bordered)
                .buttonBorderShape(.capsule)
                .accessibilityIdentifier("connect.picture")
            }
            Button { AppBuild.copy(); flash() } label: {
                Text(AppBuild.id).font(.footnote.monospacedDigit()).foregroundStyle(.secondary)
            }
            .accessibilityLabel("Build \(AppBuild.id). Tap to copy.")
            .accessibilityIdentifier("connect.build-id")
        }
        .padding(.horizontal)
        .padding(.top, 10)
        .padding(.bottom, 4)
        .background(.bar)
    }

    @ViewBuilder private var alertActions: some View {
        switch flow.prompt {
        case .typeLink:
            TextField("http://192.168.1.20:7681/?token=…", text: $flow.field)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .keyboardType(.URL)
            Button("Add") { flow.add(text: flow.field) }
            Button("Cancel", role: .cancel) {}
        case .name(let link):
            TextField("Name", text: $flow.field)
                .accessibilityIdentifier("connect.name")
            Button("Add and open") { flow.confirmName(link) }
            Button("Cancel", role: .cancel) {}
        case .unreachable(let link, _):
            Button("Save anyway") { flow.next(.name(link)); flow.field = link.host }
            Button("Cancel", role: .cancel) {}
        case .rename(let m):
            TextField("Name", text: $flow.field)
            Button("Save") { flow.rename(m) }
            Button("Cancel", role: .cancel) {}
        case .newLink(let m):
            TextField("Link", text: $flow.field)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            Button("Save") { flow.relink(m) }
            Button("Cancel", role: .cancel) {}
        case .remove(let m):
            Button("Remove", role: .destructive) { flow.remove(m) }
            Button("Cancel", role: .cancel) {}
        case .message, nil:
            Button("OK", role: .cancel) {}
        }
    }

    private func flash() {
        withAnimation { copied = true }
        Task { @MainActor in
            try? await Task.sleep(nanoseconds: 1_500_000_000)
            withAnimation { copied = false }
        }
    }

    private func pick(_ item: PhotosPickerItem?) {
        guard let item else { return }
        Task { @MainActor in
            let data = try? await item.loadTransferable(type: Data.self)
            photo = nil
            guard let data, let img = UIImage(data: data), let text = QRImage.decode(img) else {
                flow.prompt = .message("There's no QR code in that picture that could be read.")
                return
            }
            flow.add(text: text)
        }
    }
}
