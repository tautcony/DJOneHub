import AppKit
import SwiftUI

@MainActor
public final class NotifierPanel {
    private static let cardWidth: CGFloat = 286

    private let panel: NSPanel
    private let contentView = NSView(frame: .zero)
    private let surfaceView = RoundedPanelSurfaceView(frame: .zero)
    private var hostingView: NSHostingView<NotifierView>?
    private var autoHideWorkItem: DispatchWorkItem?
    private(set) var currentContent: PanelContent?

    public init() {
        let cardSize = NSSize(width: Self.cardWidth, height: 138)
        panel = NSPanel(
            contentRect: NSRect(origin: .zero, size: cardSize),
            styleMask: [.borderless, .nonactivatingPanel],
            backing: .buffered,
            defer: false
        )
        panel.level = .floating
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = true
        panel.hidesOnDeactivate = false
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .stationary]
        panel.isMovableByWindowBackground = true
        contentView.translatesAutoresizingMaskIntoConstraints = false
        contentView.wantsLayer = true
        contentView.layer?.isOpaque = false
        contentView.layer?.backgroundColor = NSColor.clear.cgColor
        surfaceView.translatesAutoresizingMaskIntoConstraints = false
        contentView.addSubview(surfaceView)
        NSLayoutConstraint.activate([
            surfaceView.leadingAnchor.constraint(equalTo: contentView.leadingAnchor),
            surfaceView.trailingAnchor.constraint(equalTo: contentView.trailingAnchor),
            surfaceView.topAnchor.constraint(equalTo: contentView.topAnchor),
            surfaceView.bottomAnchor.constraint(equalTo: contentView.bottomAnchor),
        ])

        panel.contentView = contentView
    }

    public func show(
        _ content: PanelContent,
        onReject: @escaping () -> Void,
        onOpen: @escaping () -> Void
    ) {
        autoHideWorkItem?.cancel()
        currentContent = content
        let cardSize: NSSize
        switch content {
        case .incoming:
            cardSize = NSSize(width: Self.cardWidth, height: 138)
        case .sms:
            cardSize = NSSize(width: Self.cardWidth, height: 60)
        case .missed, .error:
            cardSize = NSSize(width: Self.cardWidth, height: 76)
        case .idle:
            cardSize = NSSize(width: Self.cardWidth, height: 60)
        }
        hostingView?.removeFromSuperview()
        let hostingView = NSHostingView(
            rootView: NotifierView(content: content, onReject: onReject, onOpen: onOpen)
        )
        hostingView.translatesAutoresizingMaskIntoConstraints = false
        hostingView.wantsLayer = true
        hostingView.layer?.backgroundColor = NSColor.clear.cgColor
        hostingView.layer?.isOpaque = false
        self.hostingView = hostingView
        surfaceView.addSubview(hostingView)
        NSLayoutConstraint.activate([
            hostingView.leadingAnchor.constraint(equalTo: surfaceView.leadingAnchor),
            hostingView.trailingAnchor.constraint(equalTo: surfaceView.trailingAnchor),
            hostingView.topAnchor.constraint(equalTo: surfaceView.topAnchor),
            hostingView.bottomAnchor.constraint(equalTo: surfaceView.bottomAnchor),
            hostingView.widthAnchor.constraint(equalToConstant: cardSize.width),
            hostingView.heightAnchor.constraint(equalToConstant: cardSize.height),
        ])
        position(size: cardSize)
        panel.orderFrontRegardless()
        panel.setFrame(
            NSRect(origin: panel.frame.origin, size: cardSize),
            display: false
        )
        contentView.layoutSubtreeIfNeeded()
        panel.invalidateShadow()

        if !content.isCall {
            let work = DispatchWorkItem { [weak self] in
                self?.hide()
            }
            autoHideWorkItem = work
            DispatchQueue.main.asyncAfter(deadline: .now() + 8, execute: work)
        }
    }

    public func hide() {
        autoHideWorkItem?.cancel()
        autoHideWorkItem = nil
        currentContent = nil
        panel.orderOut(nil)
    }

    var usesWindowServerShadow: Bool {
        panel.hasShadow && surfaceView.layer?.shadowOpacity == 0
    }

    var contentSize: NSSize {
        panel.contentView?.bounds.size ?? .zero
    }

    public func saveSnapshot(to url: URL) throws {
        guard let view = panel.contentView else {
            return
        }
        view.layoutSubtreeIfNeeded()
        guard let bitmap = view.bitmapImageRepForCachingDisplay(in: view.bounds) else {
            return
        }
        view.cacheDisplay(in: view.bounds, to: bitmap)
        guard let data = bitmap.representation(using: .png, properties: [:]) else {
            return
        }
        try data.write(to: url, options: .atomic)
    }

    private func position(size: NSSize) {
        let screen = NSScreen.main ?? NSScreen.screens.first
        guard let visible = screen?.visibleFrame else {
            return
        }
        let origin = NSPoint(
            x: visible.maxX - size.width - 24,
            y: visible.maxY - size.height - 24
        )
        panel.setFrameOrigin(origin)
    }
}
