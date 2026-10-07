pragma ComponentBehavior: Bound
import QtQuick 2.5
import QtQuick.Controls 2.5

// remarkable-ai: the Today view. It draws the screen the backend sends
// (message 109): alert banners across the top, then a grid of widget cells,
// two columns in landscape and one, in reading order, in portrait. It names
// no widget or app; times arrive formatted by the backend.
Rectangle {
    id: view
    color: "#ffffff"

    property bool configured: true
    property string problem: ""
    property bool refreshing: false
    property var sections: []
    property bool acting: false
    property string notice: ""          // the last action's answer, in the footer
    property var sheetSection: null
    property var sheetItem: null
    property string sheetWidget: ""     // the title of the widget the item is in
    property var confirming: null       // a medium action waiting for its Yes
    property string sheetMessage: ""    // a refused action's answer, in its sheet
    property int sheetSerial: 0         // counts openings of the sheet
    property int actingSerial: -1       // the opening the action in flight came from
    property string actingAction: ""

    readonly property var section: view.sections.length > 0 ? view.sections[0] : null
    readonly property var screen: view.section && view.section.screen ? view.section.screen : null
    readonly property bool landscape: view.width > view.height
    readonly property int gap: 24
    readonly property int minRow: 420   // a grid row's least height; more rows scroll
    readonly property int maxBanners: 3 // alerts drawn; the rest are only counted

    signal refreshRequested()
    signal actionRequested(string section, real rev, string action, string key)
    signal closeRequested()

    function apply(data) {
        view.configured = data.configured !== false
        view.problem = data.problem || ""
        view.refreshing = !!data.refreshing
        view.sections = data.sections || []
    }
    // Message 110. The answer belongs to the sheet on screen only if that
    // sheet is the one the action was started from.
    function actionResult(data) {
        var fromThisSheet = view.sheetItem !== null && view.actingSerial === view.sheetSerial
        view.acting = false
        view.actingAction = ""
        view.actingSerial = -1
        if (fromThisSheet && !data.ok) {
            view.sheetMessage = data.message || ""   // the item stays
            return
        }
        if (fromThisSheet) view.closeSheet()
        view.notice = data.message || ""
        noticeTimer.restart()
    }
    function openSheet(item, widgetTitle) {
        view.sheetSection = view.section
        view.sheetItem = item
        view.sheetWidget = widgetTitle
        view.confirming = null
        view.sheetMessage = ""
        view.sheetSerial += 1
    }
    function closeSheet() {
        view.sheetSection = null
        view.sheetItem = null
        view.sheetWidget = ""
        view.confirming = null
        view.sheetMessage = ""
    }
    // Only low and medium actions are ever offered here, whatever arrives.
    function offered(item) {
        return ((item && item.actions) || []).filter(function(a) { return a.risk === "low" || a.risk === "medium" })
    }
    // A medium action asks first, in the catalog's words.
    function tap(action) {
        view.sheetMessage = ""
        if (action.risk === "medium") view.confirming = action
        else view.run(action)
    }
    function run(action) {
        view.confirming = null
        view.acting = true
        view.actingAction = action.id || ""
        view.actingSerial = view.sheetSerial
        view.actionRequested(view.sheetSection.id, view.sheetSection.rev, action.id || "", view.sheetItem.key || "")
    }
    // The screen has data when it fetched, or failed but still shows its cache.
    function hasData(s) { return !!s && (s.status === "ok" || !!s.as_of) }
    function statusLine(s) {
        if (!s) return ""
        var parts = []
        if (s.as_of) parts.push("as of " + s.as_of)
        if (s.status === "error" && s.error) parts.push(s.error)
        if (s.warning) parts.push(s.warning)
        return parts.join(" · ")
    }
    // Tone on e-ink: a marker, and bold for warn and bad.
    function marker(tone) {
        return tone === "good" ? "✓ " : tone === "warn" ? "! " : tone === "bad" ? "!! " : ""
    }
    function heavy(tone) { return tone === "warn" || tone === "bad" }
    function itemCount(groups) {
        var n = 0
        var list = groups || []
        for (var i = 0; i < list.length; ++i) n += (list[i].items || []).length
        return n
    }
    // A cell with nothing to draw for its view says so.
    function empty(c) {
        if (c.problem) return false
        if (c.view === "stat") return !c.stat
        if (c.view === "list") return view.itemCount(c.groups) === 0
        if (c.view === "spark") return !c.spark
        return true
    }
    function nothingPlaced() {
        return !!view.screen && (view.screen.cells || []).length === 0 && (view.screen.banners || []).length === 0
    }
    // Where each cell goes: its grid place in landscape; stacked in the order
    // sent (reading order) in portrait. A row is at least minRow high.
    function place(width, height) {
        var out = []
        var cells = view.screen ? (view.screen.cells || []) : []
        if (cells.length === 0 || width <= 0) return out
        var cols = view.landscape ? Math.max(1, view.screen.cols) : 1
        var colW = (width - view.gap * (cols - 1)) / cols
        var rows = 0
        if (view.landscape) rows = Math.max(1, view.screen.rows)
        else for (var i = 0; i < cells.length; ++i) rows += cells[i].h
        var row = Math.max(view.minRow, (height - view.gap * (rows - 1)) / rows)
        var y = 0
        for (var j = 0; j < cells.length; ++j) {
            var c = cells[j]
            var h = c.h * row + (c.h - 1) * view.gap
            if (view.landscape) {
                out.push({cell: c, x: c.x * (colW + view.gap), y: c.y * (row + view.gap),
                          w: c.w * colW + (c.w - 1) * view.gap, h: h})
            } else {
                out.push({cell: c, x: 0, y: y, w: width, h: h})
                y += h + view.gap
            }
        }
        return out
    }
    function bottomOf(placed) {
        var b = 0
        for (var i = 0; i < placed.length; ++i) b = Math.max(b, placed[i].y + placed[i].h)
        return b
    }

    // Leaving the view closes any sheet, so it never reopens on stale data.
    onVisibleChanged: {
        view.closeSheet()
        view.notice = ""
    }

    // First, so a tap on empty space stops here and never reaches the Menu
    // or exit corners underneath the view.
    MouseArea { anchors.fill: parent }

    Timer { id: noticeTimer; interval: 5000; onTriggered: view.notice = "" }

    Rectangle {
        id: header
        anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top
        height: 96
        color: "#f4f2eb"
        Text {
            textFormat: Text.PlainText
            anchors.left: parent.left; anchors.leftMargin: 32; anchors.verticalCenter: parent.verticalCenter
            text: "Today"; font.pixelSize: 44; font.bold: true; color: "#111"
        }
    }

    // The screen's own status: when it was fetched, and "offline" (the Mac
    // unreachable) or another error beside the cached screen.
    Text {
        id: statusStrip
        textFormat: Text.PlainText
        anchors.left: parent.left; anchors.right: parent.right; anchors.top: header.bottom
        anchors.leftMargin: 32; anchors.rightMargin: 32; anchors.topMargin: 12
        visible: view.configured && view.hasData(view.section) && text !== ""
        height: text !== "" ? contentHeight : 0
        elide: Text.ElideRight
        font.pixelSize: 24
        font.bold: !!view.section && view.section.status === "error"
        color: view.section && view.section.status === "error" ? "#7a1515" : "#555"
        text: view.statusLine(view.section)
    }

    Rectangle {
        id: footer
        anchors.left: parent.left; anchors.right: parent.right; anchors.bottom: parent.bottom
        height: 120
        color: "#f4f2eb"
        Text {
            textFormat: Text.PlainText
            anchors.left: parent.left; anchors.leftMargin: 32
            anchors.right: footerButtons.left; anchors.rightMargin: 24
            anchors.verticalCenter: parent.verticalCenter
            elide: Text.ElideRight
            font.pixelSize: 24; color: "#333"
            text: [view.refreshing ? "Refreshing…" : "", view.notice]
                  .filter(function(t) { return t !== "" }).join(" · ")
        }
        Row {
            id: footerButtons
            anchors.right: parent.right; anchors.rightMargin: 24; anchors.verticalCenter: parent.verticalCenter
            spacing: 16
            Button {
                text: "Refresh"; width: 180; height: 88; font.pixelSize: 24
                enabled: view.configured && !view.refreshing
                onClicked: view.refreshRequested()
            }
            Button { text: "Dashboard"; width: 200; height: 88; font.pixelSize: 24; onClicked: view.closeRequested() }
        }
    }

    Column {
        anchors.centerIn: parent
        width: Math.min(parent.width - 96, 1200)
        visible: !view.configured
        spacing: 20
        Text {
            textFormat: Text.PlainText
            width: parent.width; horizontalAlignment: Text.AlignHCenter; wrapMode: Text.Wrap
            text: "Today isn't set up on this tablet"; font.pixelSize: 36; font.bold: true; color: "#111"
        }
        Text {
            textFormat: Text.PlainText
            width: parent.width; horizontalAlignment: Text.AlignHCenter; wrapMode: Text.Wrap
            text: view.problem || "Run bin/rm-today-setup on the Mac."; font.pixelSize: 24; color: "#444"
        }
    }

    // No data at all: the error (the plea to wake the Mac, Tailscale, the
    // grant) or "No data yet", once, in the middle.
    Text {
        textFormat: Text.PlainText
        anchors.centerIn: parent
        width: Math.min(parent.width - 96, 1200)
        visible: view.configured && !!view.section && !view.hasData(view.section)
        horizontalAlignment: Text.AlignHCenter; wrapMode: Text.Wrap
        maximumLineCount: 4; elide: Text.ElideRight
        font.pixelSize: 32
        color: view.section && view.section.status === "error" ? "#7a1515" : "#555"
        text: view.section ? (view.section.error || "No data yet") : ""
    }

    Text {
        textFormat: Text.PlainText
        anchors.centerIn: parent
        visible: view.configured && view.hasData(view.section) && view.nothingPlaced()
        text: "Nothing is placed on this screen yet"; font.pixelSize: 28; color: "#555"
    }

    // Alerts, across the top. One with nothing to say never arrives. At most
    // maxBanners are drawn, two lines each, so the grid keeps its room; a line
    // counts the others.
    Column {
        id: banners
        readonly property int total: view.screen ? (view.screen.banners || []).length : 0
        anchors.left: parent.left; anchors.right: parent.right; anchors.top: statusStrip.bottom; anchors.margins: 24
        spacing: 10
        visible: view.configured && view.hasData(view.section)
        Repeater {
            model: view.screen ? (view.screen.banners || []).slice(0, view.maxBanners) : []
            delegate: Rectangle {
                id: banner
                required property var modelData
                readonly property string tone: banner.modelData.alert ? banner.modelData.alert.tone : (banner.modelData.tone || "neutral")
                width: banners.width
                height: bannerText.implicitHeight + 28
                radius: 8; color: "#f4f2eb"
                border.width: view.heavy(banner.tone) || banner.modelData.problem ? 4 : 2
                border.color: "#333333"
                Text {
                    id: bannerText
                    textFormat: Text.PlainText
                    anchors.left: parent.left; anchors.right: parent.right
                    anchors.verticalCenter: parent.verticalCenter; anchors.margins: 16
                    wrapMode: Text.Wrap; maximumLineCount: 2; elide: Text.ElideRight
                    font.pixelSize: 26; font.bold: view.heavy(banner.tone)
                    color: banner.modelData.problem ? "#7a1515" : "#111"
                    text: banner.modelData.problem
                          ? (banner.modelData.title || "") + ": " + (banner.modelData.note || "")
                          : view.marker(banner.tone) + (banner.modelData.alert ? (banner.modelData.alert.text || "") : "")
                            + (banner.modelData.note ? "   (" + banner.modelData.note + ")" : "")
                }
            }
        }
        Text {
            textFormat: Text.PlainText
            width: banners.width; elide: Text.ElideRight
            visible: banners.total > view.maxBanners
            font.pixelSize: 24; color: "#555"
            text: "+" + (banners.total - view.maxBanners) + (banners.total - view.maxBanners === 1 ? " more alert" : " more alerts")
        }
    }

    Flickable {
        id: grid
        anchors.left: parent.left; anchors.right: parent.right
        anchors.top: banners.bottom; anchors.bottom: footer.top; anchors.margins: 24
        anchors.topMargin: banners.height > 0 ? 24 : 0
        visible: view.configured && view.hasData(view.section)
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        readonly property var placed: view.place(grid.width, grid.height)
        contentWidth: grid.width
        contentHeight: view.bottomOf(grid.placed)
        Repeater {
            model: grid.placed
            delegate: Rectangle {
                id: cellFrame
                required property var modelData
                readonly property var cell: cellFrame.modelData.cell
                x: cellFrame.modelData.x; y: cellFrame.modelData.y
                width: cellFrame.modelData.w; height: cellFrame.modelData.h
                color: "#ffffff"; radius: 8
                border.width: view.heavy(cellFrame.cell.tone) ? 4 : 2; border.color: "#666666"

                Column {
                    id: cellHead
                    anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top; anchors.margins: 16
                    spacing: 2
                    Text {
                        textFormat: Text.PlainText
                        width: cellHead.width; elide: Text.ElideRight
                        text: view.marker(cellFrame.cell.tone) + (cellFrame.cell.title || "")
                        font.pixelSize: 30; font.bold: true; color: "#111"
                    }
                    Text {
                        textFormat: Text.PlainText
                        width: cellHead.width; visible: text !== ""
                        wrapMode: Text.Wrap; maximumLineCount: 2; elide: Text.ElideRight
                        text: cellFrame.cell.note || ""
                        font.pixelSize: 20; font.bold: !!cellFrame.cell.problem
                        color: cellFrame.cell.problem ? "#7a1515" : "#333"
                    }
                }

                Item {
                    id: cellBody
                    anchors.left: parent.left; anchors.right: parent.right
                    anchors.top: cellHead.bottom; anchors.bottom: parent.bottom; anchors.margins: 16
                    clip: true
                    visible: !cellFrame.cell.problem

                    Text {
                        textFormat: Text.PlainText
                        visible: view.empty(cellFrame.cell)
                        text: "Nothing here"; font.pixelSize: 24; color: "#555"
                    }

                    // stat: a large number, ▲ or ▼ from its delta.
                    Column {
                        id: statBody
                        visible: cellFrame.cell.view === "stat" && !!cellFrame.cell.stat
                        width: cellBody.width
                        spacing: 6
                        Text {
                            textFormat: Text.PlainText
                            width: statBody.width; elide: Text.ElideRight
                            font.pixelSize: 72; font.bold: true; color: "#111"
                            text: !cellFrame.cell.stat ? "" : (cellFrame.cell.stat.value || "")
                                  + (cellFrame.cell.stat.trend === "up" ? " ▲" : cellFrame.cell.stat.trend === "down" ? " ▼" : "")
                        }
                        Text {
                            textFormat: Text.PlainText
                            width: statBody.width; elide: Text.ElideRight
                            font.pixelSize: 26; color: "#111"
                            font.bold: !!cellFrame.cell.stat && view.heavy(cellFrame.cell.stat.tone)
                            text: !cellFrame.cell.stat ? "" : view.marker(cellFrame.cell.stat.tone) + (cellFrame.cell.stat.label || "")
                        }
                        Text {
                            textFormat: Text.PlainText
                            width: statBody.width; elide: Text.ElideRight; visible: text !== ""
                            font.pixelSize: 22; color: "#333"
                            text: cellFrame.cell.stat ? (cellFrame.cell.stat.delta || "") : ""
                        }
                    }

                    // list: today's rows, each opening the sheet.
                    Flickable {
                        id: listScroll
                        anchors.fill: parent
                        visible: cellFrame.cell.view === "list" && !view.empty(cellFrame.cell)
                        contentHeight: listBody.implicitHeight
                        boundsBehavior: Flickable.StopAtBounds
                        clip: true
                        Column {
                            id: listBody
                            width: listScroll.width
                            spacing: 8
                            Repeater {
                                model: cellFrame.cell.view === "list" ? (cellFrame.cell.groups || []) : []
                                delegate: Column {
                                    id: group
                                    required property var modelData
                                    width: listBody.width
                                    spacing: 6
                                    Text {
                                        textFormat: Text.PlainText
                                        width: group.width; elide: Text.ElideRight
                                        visible: text !== ""; text: group.modelData.title || ""
                                        font.pixelSize: 24; font.bold: true; color: "#333"
                                    }
                                    Repeater {
                                        model: group.modelData.items || []
                                        delegate: Rectangle {
                                            id: row
                                            required property var modelData
                                            width: group.width
                                            height: Math.max(88, rowText.implicitHeight + 24)
                                            radius: 6
                                            color: rowArea.pressed ? "#e5e2da" : "#f4f2eb"
                                            Column {
                                                id: rowText
                                                anchors.left: parent.left; anchors.right: parent.right
                                                anchors.verticalCenter: parent.verticalCenter; anchors.margins: 14
                                                Text {
                                                    textFormat: Text.PlainText
                                                    width: rowText.width; wrapMode: Text.Wrap; maximumLineCount: 2; elide: Text.ElideRight
                                                    text: view.marker(row.modelData.tone) + (row.modelData.title || "")
                                                    font.pixelSize: 26; font.bold: view.heavy(row.modelData.tone); color: "#111"
                                                }
                                                Text {
                                                    textFormat: Text.PlainText
                                                    width: rowText.width; elide: Text.ElideRight; visible: text !== ""
                                                    text: row.modelData.subtitle || ""; font.pixelSize: 20; color: "#555"
                                                }
                                            }
                                            MouseArea {
                                                id: rowArea
                                                anchors.fill: parent
                                                onClicked: view.openSheet(row.modelData, cellFrame.cell.title || "")
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }

                    // spark: a drawn polyline, points already scaled to 0..1.
                    Column {
                        id: sparkBody
                        anchors.fill: parent
                        visible: cellFrame.cell.view === "spark" && !!cellFrame.cell.spark
                        spacing: 6
                        Text {
                            textFormat: Text.PlainText
                            width: sparkBody.width; elide: Text.ElideRight
                            font.pixelSize: 24; color: "#111"
                            text: !cellFrame.cell.spark ? "" : (cellFrame.cell.spark.label || "") + "  " + (cellFrame.cell.spark.last || "")
                                  + (cellFrame.cell.spark.unit ? " " + cellFrame.cell.spark.unit : "")
                        }
                        Canvas {
                            id: sparkLine
                            width: sparkBody.width
                            height: Math.max(40, sparkBody.height - 80)
                            property var points: cellFrame.cell.spark ? (cellFrame.cell.spark.points || []) : []
                            onPointsChanged: sparkLine.requestPaint()
                            onWidthChanged: sparkLine.requestPaint()
                            onHeightChanged: sparkLine.requestPaint()
                            onPaint: {
                                var ctx = sparkLine.getContext("2d")
                                ctx.clearRect(0, 0, sparkLine.width, sparkLine.height)
                                var pts = sparkLine.points || []
                                if (pts.length < 2) return
                                ctx.strokeStyle = "#111111"; ctx.lineWidth = 4; ctx.beginPath()
                                for (var i = 0; i < pts.length; ++i) {
                                    var x = 4 + i * (sparkLine.width - 8) / (pts.length - 1)
                                    var y = sparkLine.height - 4 - pts[i] * (sparkLine.height - 8)
                                    if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y)
                                }
                                ctx.stroke()
                            }
                        }
                        Text {
                            textFormat: Text.PlainText
                            width: sparkBody.width; elide: Text.ElideRight
                            font.pixelSize: 20; color: "#555"
                            text: cellFrame.cell.spark ? "min " + cellFrame.cell.spark.min + " · max " + cellFrame.cell.spark.max : ""
                        }
                    }
                }
            }
        }
    }

    // One item, its full text, and its actions. Inside the view (not a Popup)
    // so it rotates with the screen in landscape.
    Item {
        anchors.fill: parent
        visible: view.sheetItem !== null
        z: 10
        MouseArea { anchors.fill: parent }   // taps behind the sheet go nowhere
        Rectangle {
            anchors.centerIn: parent
            width: Math.min(parent.width - 96, 1300)
            height: Math.min(parent.height - 96, sheetBody.implicitHeight + 64)
            color: "#ffffff"; border.width: 3; radius: 12
            Column {
                id: sheetBody
                anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top; anchors.margins: 32
                spacing: 18
                Text { textFormat: Text.PlainText; width: sheetBody.width; elide: Text.ElideRight; font.pixelSize: 22; color: "#555"; visible: text !== ""
                       text: view.sheetWidget }
                // Line limits keep the buttons on the screen whatever the text.
                Text { textFormat: Text.PlainText; width: sheetBody.width; wrapMode: Text.Wrap; maximumLineCount: 4; elide: Text.ElideRight
                       font.pixelSize: 34; font.bold: true; color: "#111"
                       text: view.sheetItem ? (view.sheetItem.title || "") : "" }
                Text { textFormat: Text.PlainText; width: sheetBody.width; wrapMode: Text.Wrap; maximumLineCount: 3; elide: Text.ElideRight
                       font.pixelSize: 24; color: "#555"; visible: text !== ""
                       text: view.sheetItem ? (view.sheetItem.subtitle || "") : "" }
                Text { textFormat: Text.PlainText; width: sheetBody.width; wrapMode: Text.Wrap; maximumLineCount: 14; elide: Text.ElideRight
                       font.pixelSize: 26; color: "#111"; visible: text !== ""
                       text: view.sheetItem ? (view.sheetItem.detail || "") : "" }
                Text { textFormat: Text.PlainText; width: sheetBody.width; wrapMode: Text.Wrap; maximumLineCount: 3; elide: Text.ElideRight
                       font.pixelSize: 22; color: "#7a1515"; visible: text !== ""
                       text: view.sheetMessage }
                // A medium action's question, in the catalog's words.
                Text { textFormat: Text.PlainText; width: sheetBody.width; wrapMode: Text.Wrap; maximumLineCount: 4; elide: Text.ElideRight
                       font.pixelSize: 28; font.bold: true; color: "#111"
                       visible: view.confirming !== null
                       text: view.confirming ? (view.confirming.confirm || (view.confirming.label || "") + "?") : "" }
                Flow {
                    width: sheetBody.width
                    spacing: 16
                    Repeater {
                        model: view.confirming !== null ? [] : view.offered(view.sheetItem)
                        delegate: Button {
                            id: actionButton
                            required property var modelData
                            width: 260; height: 88; font.pixelSize: 26
                            enabled: !view.acting
                            // The label is gateway text. A Button parses its own text as rich text
                            // (an <img> in it crashes Qt), so a plain Text draws it and `text` stays empty.
                            Text {
                                anchors.fill: parent; anchors.margins: 12
                                textFormat: Text.PlainText; elide: Text.ElideRight; font: actionButton.font
                                horizontalAlignment: Text.AlignHCenter; verticalAlignment: Text.AlignVCenter
                                color: actionButton.enabled ? "#111" : "#777"
                                text: view.acting && view.actingSerial === view.sheetSerial
                                      && view.actingAction === (actionButton.modelData.id || "")
                                      ? "…" : (actionButton.modelData.label || "")
                            }
                            onClicked: view.tap(actionButton.modelData)
                        }
                    }
                    Button {
                        id: yesButton
                        visible: view.confirming !== null
                        width: 340; height: 88; font.pixelSize: 26
                        enabled: !view.acting
                        Text {   // the label is gateway text, as above
                            anchors.fill: parent; anchors.margins: 12
                            textFormat: Text.PlainText; elide: Text.ElideRight; font: yesButton.font
                            horizontalAlignment: Text.AlignHCenter; verticalAlignment: Text.AlignVCenter
                            color: yesButton.enabled ? "#111" : "#777"
                            text: view.confirming ? "Yes, " + (view.confirming.label || "") : ""
                        }
                        onClicked: view.run(view.confirming)
                    }
                    Button {
                        visible: view.confirming !== null
                        text: "Cancel"; width: 200; height: 88; font.pixelSize: 26
                        onClicked: view.confirming = null
                    }
                    Button { text: "Close"; width: 200; height: 88; font.pixelSize: 26; onClicked: view.closeSheet() }
                }
            }
        }
    }
}
