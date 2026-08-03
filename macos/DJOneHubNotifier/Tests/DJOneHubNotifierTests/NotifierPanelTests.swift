import AppKit
import XCTest
@testable import DJOneHubNotifier

final class NotifierPanelTests: XCTestCase {
    @MainActor
    func testSnapshotClipsCardContentToRoundedWindowShape() throws {
        let panel = NotifierPanel()
        panel.show(
            .incoming(number: "10086", startedAt: Date(), state: "incoming", rejecting: false),
            onReject: {},
            onOpen: {}
        )
        defer { panel.hide() }
        XCTAssertEqual(panel.contentSize.width, 286)
        XCTAssertEqual(panel.contentSize.height, 138)

        let url = FileManager.default.temporaryDirectory
            .appendingPathComponent("djonehub-notifier-panel-test.png")
        try panel.saveSnapshot(to: url)
        defer { try? FileManager.default.removeItem(at: url) }

        let image = try XCTUnwrap(NSImage(contentsOf: url))
        let bitmap = try XCTUnwrap(NSBitmapImageRep(data: image.tiffRepresentation!))

        XCTAssertEqual(panel.contentSize.width, 286)
        XCTAssertEqual(panel.contentSize.height, 138)
        XCTAssertGreaterThan(bitmap.pixelsWide, 0)
        XCTAssertGreaterThan(bitmap.pixelsHigh, 0)
        assertTransparentCorners(bitmap)
        XCTAssertTrue(panel.usesWindowServerShadow)

        let centerColor = try XCTUnwrap(
            bitmap.colorAt(x: bitmap.pixelsWide / 2, y: bitmap.pixelsHigh / 2)
        )
        let center = try XCTUnwrap(centerColor.usingColorSpace(.deviceRGB))
        XCTAssertGreaterThan(center.alphaComponent, 0.5)
    }

    @MainActor
    func testGPSPanelUsesSharedRoundedSurfaceAndWindowShadow() {
        let panel = GPSMapPanel()
        XCTAssertTrue(panel.usesSharedSurfaceStyle)
    }

    @MainActor
    func testPanelKeepsConfiguredSizeForEveryContentShape() {
        let panel = NotifierPanel()
        defer { panel.hide() }
        let cases: [(PanelContent, CGFloat)] = [
            (.incoming(number: "1", startedAt: Date(), state: "incoming", rejecting: false), 138),
            (.sms(sender: "1", preview: "短", code: nil), 60),
            (.error(message: "短"), 76),
        ]

        for (content, height) in cases {
            panel.show(content, onReject: {}, onOpen: {})
            XCTAssertEqual(panel.contentSize.width, 286)
            XCTAssertEqual(panel.contentSize.height, height)
        }
    }

    @MainActor
    func testCustomCallPanelUpdatesAndHidesAcrossCallLifecycle() throws {
        let delegate = UIAppDelegate()
        delegate.applyConfig(#"{"notification_preferences":{"incoming_call":"custom","missed_call":"custom","sms":"custom","device_offline":"custom"}}"#)
        let startedAt = "2026-08-03T12:00:00Z"

        delegate.handleEvent(try bridgeEvent(
            id: 1,
            type: BridgeEventType.callIncoming,
            data: #"{"id":"call-1","direction":"incoming","state":"incoming","number":"10086","started_at":"\#(startedAt)","missed":false}"#
        ))
        guard case let .incoming(number, _, state, rejecting) = delegate.presentedPanelContent else {
            return XCTFail("incoming event did not show the custom call panel")
        }
        XCTAssertEqual(number, "10086")
        XCTAssertEqual(state, "incoming")
        XCTAssertFalse(rejecting)

        delegate.handleEvent(try bridgeEvent(
            id: 2,
            type: BridgeEventType.callUpdated,
            data: #"{"id":"call-1","direction":"incoming","state":"active","number":"10010","started_at":"\#(startedAt)","missed":false}"#
        ))
        guard case let .incoming(updatedNumber, _, updatedState, _) = delegate.presentedPanelContent else {
            return XCTFail("updated event removed the custom call panel")
        }
        XCTAssertEqual(updatedNumber, "10010")
        XCTAssertEqual(updatedState, "active")

        delegate.handleEvent(try bridgeEvent(
            id: 3,
            type: BridgeEventType.callEnded,
            data: #"{"id":"call-1","direction":"incoming","state":"active","number":"10010","started_at":"\#(startedAt)","ended_at":"2026-08-03T12:01:00Z","missed":false}"#
        ))
        XCTAssertNil(delegate.presentedPanelContent)
    }

    private func bridgeEvent(id: UInt64, type: String, data: String) throws -> BridgeEvent {
        let json = #"{"id":\#(id),"type":"\#(type)","version":1,"occurred_at":"2026-08-03T12:00:00Z","data":\#(data)}"#
        return try XCTUnwrap(BridgeEvent.parse(json))
    }

    private func assertTransparentCorners(
        _ bitmap: NSBitmapImageRep,
        file: StaticString = #filePath,
        line: UInt = #line
    ) {
        let corners = [
            (0, 0),
            (bitmap.pixelsWide - 1, 0),
            (0, bitmap.pixelsHigh - 1),
            (bitmap.pixelsWide - 1, bitmap.pixelsHigh - 1),
        ]
        let maximumAlpha = corners.map { alpha(bitmap, x: $0.0, y: $0.1) }.max() ?? 1
        XCTAssertLessThan(maximumAlpha, 0.02, file: file, line: line)
    }

    private func alpha(_ bitmap: NSBitmapImageRep, x: Int, y: Int) -> CGFloat {
        bitmap.colorAt(x: x, y: y)?
            .usingColorSpace(.deviceRGB)?
            .alphaComponent ?? 1
    }
}
