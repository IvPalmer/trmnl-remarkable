import QtQuick 2.5
import QtTest 1.2
import "../../app/ui"

// The tap regions a BYOS server sends are pixels of its image. TapOverlay maps
// them onto the part of the screen the image is painted on: stretched into the
// 16 px margin in landscape, fitted (centred, with bands) in portrait, or
// cropped. TRMNL.qml cannot be loaded here (the AppLoad plugin is a stub), so the
// geometry lives in TapOverlay and TRMNL.qml gives it the Image's own box and
// fill mode.
//
// CI runs it (the "QML lint and resources" step). From the repository root:
//   QT_QPA_PLATFORM=offscreen qmltestrunner -input tests/qml
TestCase {
    id: tc
    name: "TapOverlay"
    when: windowShown
    visible: true                // a TestCase is hidden by default, and a hidden item takes no clicks
    width: 1404; height: 1872

    // Landscape, as the dashboard is laid out: the Image fills the display area
    // minus a 16 px margin and stretches into it.
    // The groups below overlap on the window; a test shows the one it clicks.
    Item {
        id: landscapeArea
        visible: false
        width: 1872; height: 1404
        Item { id: landscapeBox; anchors.fill: parent; anchors.margins: 16 }
        TapOverlay {
            id: landscape
            anchors.fill: landscapeBox
            fillMode: Image.Stretch
            imageReady: true
        }
    }

    // Portrait: the Image fills the display area and fits.
    TapOverlay {
        id: portrait
        visible: false
        width: 1404; height: 1872
        fillMode: Image.PreserveAspectFit
        imageReady: true
    }

    SignalSpy { id: tappedSpy; target: landscape; signalName: "tapped" }

    function regions(overlay) {
        return overlay.children.filter(function(c) { return c.pressed !== undefined })
    }
    function near(actual, expected, what) {
        verify(Math.abs(actual - expected) < 1e-6, what + ": " + actual + " is not " + expected)
    }
    function expectRect(r, x, y, w, h, what) {
        near(r.x, x, what + " x"); near(r.y, y, what + " y")
        near(r.w, w, what + " w"); near(r.h, h, what + " h")
    }
    function tap(x, y, w, h, widget) {
        return {x: x, y: y, w: w, h: h, widget: widget || "a.b", key: "k", title: "T", actions: []}
    }

    function init() {
        landscape.taps = []; landscape.sourceWidth = 0; landscape.sourceHeight = 0
        landscape.controlsVisible = landscape.settingsVisible = landscape.todayVisible = landscape.brightnessScheduleVisible = false
        landscape.imageReady = true
        portrait.taps = []; portrait.sourceWidth = 0; portrait.sourceHeight = 0
        portrait.imageReady = true
        tappedSpy.clear()
        landscapeArea.visible = turned.visible = page.visible = false
    }

    // 920 x 686 pixels into the 1840 x 1372 box: exactly 2 on each axis.
    function test_landscape_stretches_into_the_margin() {
        landscape.sourceWidth = 920; landscape.sourceHeight = 686
        landscape.taps = [tc.tap(100, 50, 200, 80)]
        compare(landscape.width, 1840); compare(landscape.height, 1372)
        var r = landscape.rects
        compare(r.length, 1)
        compare(r[0].index, 0)
        tc.expectRect(r[0], 200, 100, 400, 160, "region")
        // On the display area it sits 16 px further in on each axis.
        var onArea = landscape.mapToItem(landscapeArea, r[0].x, r[0].y)
        compare(onArea.x, 216); compare(onArea.y, 116)
        // The whole image is exactly the box.
        landscape.taps = [tc.tap(0, 0, 920, 686)]
        tc.expectRect(landscape.rects[0], 0, 0, 1840, 1372, "whole image")
    }

    // A TRMNL-sized image (800 x 480): each axis scales on its own.
    function test_landscape_stretches_each_axis_on_its_own() {
        landscape.sourceWidth = 800; landscape.sourceHeight = 480
        landscape.taps = [tc.tap(400, 240, 400, 240)]    // the lower-right quarter
        tc.expectRect(landscape.rects[0], 920, 686, 920, 686, "quarter")
    }

    // 702 x 468 pixels in a 1404 x 1872 box: scale 2, painted 1404 x 936, 468 px bands above and below.
    function test_portrait_fit_offsets() {
        portrait.sourceWidth = 702; portrait.sourceHeight = 468
        portrait.taps = [tc.tap(10, 20, 30, 40)]
        var p = portrait.painted(1404, 1872, 702, 468, Image.PreserveAspectFit)
        compare(p.sx, 2); compare(p.sy, 2); compare(p.x, 0); compare(p.y, 468)
        tc.expectRect(portrait.rects[0], 20, 508, 60, 80, "region")
        // A tall image fits by its height instead: 936 x 1872 at scale 1, 234 px bands left and right.
        portrait.sourceWidth = 936; portrait.sourceHeight = 1872
        portrait.taps = [tc.tap(100, 100, 50, 50)]
        tc.expectRect(portrait.rects[0], 334, 100, 50, 50, "tall image")
    }

    // A region cannot reach into a band or past the picture: it is cut to the painted image.
    function test_regions_are_cut_to_the_painted_image() {
        portrait.sourceWidth = 702; portrait.sourceHeight = 468
        portrait.taps = [
            tc.tap(700, 460, 10, 20),     // runs 8 px past the right and bottom edges of the image
            tc.tap(900, 0, 10, 10),       // wholly right of the image
            tc.tap(0, 0, 702, 468)        // the whole image
        ]
        var r = portrait.rects
        compare(r.length, 2)
        compare(r[0].index, 0)
        tc.expectRect(r[0], 1400, 1388, 4, 16, "edge region")      // 1400..1404 by 1388..1404
        compare(r[1].index, 2)           // the middle tap is gone, the others keep their place in `taps`
        tc.expectRect(r[1], 0, 468, 1404, 936, "whole image")
    }

    // Crop: 500 x 250 into 1000 x 1000 is scale 4, painted 2000 x 1000 with 500 px cut off each side.
    function test_crop_cuts_to_the_box() {
        var p = portrait.painted(1000, 1000, 500, 250, Image.PreserveAspectCrop)
        compare(p.sx, 4); compare(p.sy, 4); compare(p.x, -500); compare(p.y, 0)
        var r = portrait.place([tc.tap(100, 0, 50, 50), tc.tap(0, 0, 100, 10), tc.tap(125, 0, 250, 250)],
                               1000, 1000, 500, 250, Image.PreserveAspectCrop)
        compare(r.length, 2)
        tc.expectRect(r[0], 0, 0, 100, 200, "left edge region")    // starts 100 px off the box
        compare(r[1].index, 2)
        tc.expectRect(r[1], 0, 0, 1000, 1000, "right of the cut")  // x 0..1000 of the box
    }

    function test_nothing_to_map_without_a_size_or_taps() {
        landscape.taps = [tc.tap(0, 0, 10, 10)]
        compare(landscape.rects.length, 0)        // the image has no size yet
        landscape.sourceWidth = 920; landscape.sourceHeight = 686
        compare(landscape.rects.length, 1)
        landscape.taps = []
        compare(landscape.rects.length, 0)
        landscape.taps = null
        compare(landscape.rects.length, 0)
    }

    function test_regions_follow_the_taps() {
        landscape.sourceWidth = 920; landscape.sourceHeight = 686
        landscape.taps = [tc.tap(0, 0, 10, 10), tc.tap(20, 20, 10, 10)]
        compare(tc.regions(landscape).length, 2)
        landscape.taps = [tc.tap(0, 0, 10, 10)]
        compare(tc.regions(landscape).length, 1)
        landscape.taps = []
        compare(tc.regions(landscape).length, 0)
    }

    // Controls, settings, Today and the brightness schedule each take the regions off.
    function test_hidden_while_a_panel_is_open() {
        landscape.sourceWidth = 920; landscape.sourceHeight = 686
        landscapeArea.visible = true
        landscape.taps = [tc.tap(0, 0, 100, 100)]
        compare(landscape.shown, true)
        compare(tc.regions(landscape).length, 1)
        mouseClick(landscape, 50, 50)
        compare(tappedSpy.count, 1, "a click reaches the region when nothing is open")
        var flags = ["controlsVisible", "settingsVisible", "todayVisible", "brightnessScheduleVisible"]
        for (var i = 0; i < flags.length; ++i) {
            landscape[flags[i]] = true
            compare(landscape.shown, false, flags[i])
            compare(tc.regions(landscape).length, 0, flags[i])
            tappedSpy.clear()
            mouseClick(landscape, 50, 50)
            compare(tappedSpy.count, 0, flags[i] + ": a click got through")
            landscape[flags[i]] = false
            compare(landscape.shown, true, flags[i] + " closed")
            compare(tc.regions(landscape).length, 1)
            mouseClick(landscape, 50, 50)
            compare(tappedSpy.count, 1, flags[i] + " closed: the region takes clicks again")
        }
        landscape.imageReady = false      // the image is still loading
        compare(landscape.shown, false)
        compare(tc.regions(landscape).length, 0)
    }

    function test_a_click_reports_its_tap() {
        landscape.sourceWidth = 920; landscape.sourceHeight = 686
        landscapeArea.visible = true
        landscape.taps = [tc.tap(0, 0, 100, 100, "a.one"), tc.tap(300, 300, 100, 100, "a.two")]
        mouseClick(landscape, 1, 1)                   // inside the first (0..200)
        compare(tappedSpy.count, 1)
        compare(tappedSpy.signalArguments[0][0].widget, "a.one")
        mouseClick(landscape, 700, 700)               // inside the second (600..800)
        compare(tappedSpy.count, 2)
        compare(tappedSpy.signalArguments[1][0].widget, "a.two")
        mouseClick(landscape, 400, 400)               // between them
        compare(tappedSpy.count, 2)
    }

    // The display area turns 90 degrees in landscape; the regions turn with it.
    Item {
        id: turned
        visible: false
        width: 1872; height: 1404
        x: -234; y: 234                              // the centre of the window, as anchors.centerIn gives
        rotation: 90
        TapOverlay {
            id: turnedOverlay
            anchors.fill: parent
            fillMode: Image.Stretch
            imageReady: true
            sourceWidth: 1872; sourceHeight: 1404
            taps: [{x: 100, y: 200, w: 100, h: 100, widget: "a.turned", key: "k", title: "T", actions: []}]
        }
    }
    SignalSpy { id: turnedSpy; target: turnedOverlay; signalName: "tapped" }

    function test_regions_turn_with_the_display_area() {
        turned.visible = true
        var p = turnedOverlay.mapToItem(tc, 150, 250)      // the middle of the region, on the window
        verify(p.x > 0 && p.x < tc.width && p.y > 0 && p.y < tc.height, "inside the window at " + p.x + "," + p.y)
        mouseClick(turnedOverlay, 150, 250)
        compare(turnedSpy.count, 1)
        compare(turnedSpy.signalArguments[0][0].widget, "a.turned")
    }

    // TRMNL.qml puts the overlay inside the display area and the Menu (top-right,
    // 112 px) and exit (top-left, 120 px) corners after it in the page. The same
    // structure here: a region over the whole image must not take a click on a corner.
    Item {
        id: page
        visible: false
        width: 1404; height: 1872
        Item {
            id: area
            anchors.fill: parent
            TapOverlay {
                id: covering
                anchors.fill: parent
                fillMode: Image.PreserveAspectFit
                imageReady: true
                sourceWidth: 1404; sourceHeight: 1872
                taps: [{x: 0, y: 0, w: 1404, h: 1872, widget: "a.all", key: "k", title: "T", actions: []}]
            }
        }
        MouseArea { id: menuCorner; anchors.right: parent.right; anchors.top: parent.top; width: 112; height: 112
                    onClicked: tc.corner = "menu" }
        MouseArea { id: exitCorner; anchors.left: parent.left; anchors.top: parent.top; width: 120; height: 120
                    onClicked: tc.corner = "exit" }
    }
    property string corner: ""
    SignalSpy { id: coveringSpy; target: covering; signalName: "tapped" }

    function test_corners_stay_above_the_regions() {
        page.visible = true
        tc.corner = ""
        mouseClick(page, 1404 - 50, 50)
        compare(tc.corner, "menu")
        mouseClick(page, 60, 60)
        compare(tc.corner, "exit")
        compare(coveringSpy.count, 0)
        mouseClick(page, 700, 900)
        compare(coveringSpy.count, 1)                  // elsewhere the region still takes the click
    }
}
