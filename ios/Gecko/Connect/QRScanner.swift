import AVFoundation
import CoreImage
import SwiftUI
import UIKit

/// Reads a QR code in a picture (a screenshot of Gecko's Open on another device).
enum QRImage {
    static func decode(_ image: UIImage) -> String? {
        guard let ci = image.ciImage ?? image.cgImage.map({ CIImage(cgImage: $0) }) else { return nil }
        return decode(ci)
    }

    static func decode(_ image: CIImage) -> String? {
        let detector = CIDetector(ofType: CIDetectorTypeQRCode, context: nil, options: [CIDetectorAccuracy: CIDetectorAccuracyHigh])
        let found = detector?.features(in: image).compactMap { ($0 as? CIQRCodeFeature)?.messageString } ?? []
        // A Gecko link first, if the picture has several codes.
        return found.first { $0.contains("token=") } ?? found.first
    }
}

/// The camera, looking for a QR code. Calls back once with what it read.
struct QRScannerView: UIViewControllerRepresentable {
    var onCode: (String) -> Void
    var onCancel: () -> Void

    func makeUIViewController(context: Context) -> ScannerController {
        let c = ScannerController()
        c.onCode = onCode
        c.onCancel = onCancel
        return c
    }

    func updateUIViewController(_ c: ScannerController, context: Context) {}
}

final class ScannerController: UIViewController, AVCaptureMetadataOutputObjectsDelegate {
    var onCode: (String) -> Void = { _ in }
    var onCancel: () -> Void = {}
    private let session = AVCaptureSession()
    private var preview: AVCaptureVideoPreviewLayer?
    private var done = false
    private let note = UILabel()

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .black
        note.textColor = .white
        note.numberOfLines = 0
        note.textAlignment = .center
        note.font = .preferredFont(forTextStyle: .body)
        note.text = "Point at the QR code in Gecko's Open on another device"
        note.translatesAutoresizingMaskIntoConstraints = false
        var c = UIButton.Configuration.filled()
        c.title = "Cancel"
        let cancel = UIButton(configuration: c, primaryAction: UIAction { [weak self] _ in self?.onCancel() })
        cancel.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(note)
        view.addSubview(cancel)
        NSLayoutConstraint.activate([
            note.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 24),
            note.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -24),
            note.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 24),
            cancel.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            cancel.bottomAnchor.constraint(equalTo: view.safeAreaLayoutGuide.bottomAnchor, constant: -24),
        ])
        switch AVCaptureDevice.authorizationStatus(for: .video) {
        case .authorized: setUp()
        case .notDetermined:
            AVCaptureDevice.requestAccess(for: .video) { ok in
                Task { @MainActor [weak self] in
                    if ok { self?.setUp() } else { self?.say("Gecko may not use the camera. Allow it in Settings, Gecko, or paste the link instead.") }
                }
            }
        default:
            say("Gecko may not use the camera. Allow it in Settings, Gecko, or paste the link instead.")
        }
    }

    private func say(_ text: String) {
        note.text = text
    }

    private func setUp() {
        guard let cam = AVCaptureDevice.default(for: .video), let input = try? AVCaptureDeviceInput(device: cam), session.canAddInput(input) else {
            return say("There's no camera here. Paste the link instead, or scan a picture of the QR code.")
        }
        session.addInput(input)
        let out = AVCaptureMetadataOutput()
        guard session.canAddOutput(out) else { return say("The camera can't read QR codes here.") }
        session.addOutput(out)
        out.setMetadataObjectsDelegate(self, queue: .main)
        out.metadataObjectTypes = [.qr]
        let p = AVCaptureVideoPreviewLayer(session: session)
        p.videoGravity = .resizeAspectFill
        p.frame = view.bounds
        view.layer.insertSublayer(p, at: 0)
        preview = p
        let s = session
        DispatchQueue.global(qos: .userInitiated).async { s.startRunning() }
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        preview?.frame = view.bounds
    }

    override func viewWillDisappear(_ animated: Bool) {
        super.viewWillDisappear(animated)
        let s = session
        DispatchQueue.global(qos: .userInitiated).async { if s.isRunning { s.stopRunning() } }
    }

    nonisolated func metadataOutput(_ output: AVCaptureMetadataOutput, didOutput objects: [AVMetadataObject], from connection: AVCaptureConnection) {
        guard let code = objects.compactMap({ ($0 as? AVMetadataMachineReadableCodeObject)?.stringValue }).first else { return }
        Task { @MainActor [weak self] in self?.found(code) }
    }

    private func found(_ code: String) {
        guard !done else { return }
        done = true
        UINotificationFeedbackGenerator().notificationOccurred(.success)
        onCode(code)
    }
}
