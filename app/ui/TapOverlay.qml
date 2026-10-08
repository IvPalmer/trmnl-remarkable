pragma ComponentBehavior: Bound
import QtQuick 2.5

// remarkable-ai: invisible tap regions over the dashboard image. A BYOS server
// may send `taps` with an image: rectangles in the pixels of that image, each
// of which opens the tap sheet. This item is sized like the Image it covers
// (the same box, the same fill mode) and maps those pixels onto the part of
// the box the image really paints, so the regions follow the picture whether it
// is stretched, fitted (bands at the sides) or cropped. It holds no state of its
// own beyond what it is given, and it names no widget.
Item {
    id: overlay

    // The tap list of message 102: {x, y, w, h, widget, key, title, actions}.
    property var taps: []
    // Natural size of the image shown, in pixels (the Image's sourceSize).
    property real sourceWidth: 0
    property real sourceHeight: 0
    // The Image's fillMode: Image.Stretch, Image.PreserveAspectFit or
    // Image.PreserveAspectCrop.
    property int fillMode: Image.PreserveAspectFit
    // True when the image is loaded and the taps belong to it.
    property bool imageReady: false
    // The screens drawn over the dashboard. Any of them open hides the regions.
    property bool controlsVisible: false
    property bool settingsVisible: false
    property bool todayVisible: false
    property bool brightnessScheduleVisible: false

    readonly property bool shown: overlay.imageReady && !overlay.controlsVisible && !overlay.settingsVisible
                                  && !overlay.todayVisible && !overlay.brightnessScheduleVisible
                                  && overlay.rects.length > 0
    // One rectangle per tap, in this item's own coordinates (the tap's place
    // in `taps` is `index`). Taps wholly outside the painted image are left out.
    readonly property var rects: overlay.place(overlay.taps, overlay.width, overlay.height,
                                               overlay.sourceWidth, overlay.sourceHeight, overlay.fillMode)

    signal tapped(var tap)

    // Pure geometry: where the image paints inside a box of boxW x boxH and how
    // far a source pixel scales. Stretch fills the box on each axis; Fit and Crop
    // keep the aspect ratio, centred (Crop overflows the box and is clipped by it).
    function painted(boxW, boxH, srcW, srcH, mode) {
        var sx = boxW / srcW
        var sy = boxH / srcH
        if (mode !== Image.Stretch) {
            sx = sy = (mode === Image.PreserveAspectCrop) ? Math.max(sx, sy) : Math.min(sx, sy)
        }
        return {sx: sx, sy: sy, x: (boxW - srcW * sx) / 2, y: (boxH - srcH * sy) / 2}
    }
    function place(list, boxW, boxH, srcW, srcH, mode) {
        var out = []
        if (!(srcW > 0 && srcH > 0 && boxW > 0 && boxH > 0)) return out
        var p = overlay.painted(boxW, boxH, srcW, srcH, mode)
        // What is visible of the image: its painted rectangle, cut to the box.
        var left = Math.max(0, p.x)
        var top = Math.max(0, p.y)
        var right = Math.min(boxW, p.x + srcW * p.sx)
        var bottom = Math.min(boxH, p.y + srcH * p.sy)
        var taps = list || []
        for (var i = 0; i < taps.length; ++i) {
            var t = taps[i]
            var x1 = Math.max(left, p.x + t.x * p.sx)
            var y1 = Math.max(top, p.y + t.y * p.sy)
            var x2 = Math.min(right, p.x + (t.x + t.w) * p.sx)
            var y2 = Math.min(bottom, p.y + (t.y + t.h) * p.sy)
            if (x2 > x1 && y2 > y1) out.push({index: i, x: x1, y: y1, w: x2 - x1, h: y2 - y1})
        }
        return out
    }

    Repeater {
        model: overlay.shown ? overlay.rects : []
        delegate: MouseArea {
            id: region
            required property var modelData
            x: region.modelData.x; y: region.modelData.y
            width: region.modelData.w; height: region.modelData.h
            onClicked: overlay.tapped(overlay.taps[region.modelData.index])
        }
    }
}
