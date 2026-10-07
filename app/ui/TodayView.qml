pragma ComponentBehavior: Bound
import QtQuick 2.5
import QtQuick.Controls 2.5

// remarkable-ai: the Today view. It draws whatever sections the backend sends
// (message 109) and knows no source by name, so a new section or action needs
// no change here. Times arrive formatted by the backend in the operator's zone.
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
    property string sheetMessage: ""    // a refused action's answer, in its sheet
    property int sheetSerial: 0         // counts openings of the sheet
    property int actingSerial: -1       // the opening the action in flight came from
    property string actingAction: ""
    // What every section says when none of them has any data (no cache and
    // the Mac unreachable): said once for the view, not in every column.
    readonly property string sharedProblem: view.findSharedProblem()

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
    function openSheet(section, item) {
        view.sheetSection = section
        view.sheetItem = item
        view.sheetMessage = ""
        view.sheetSerial += 1
    }
    function closeSheet() {
        view.sheetSection = null
        view.sheetItem = null
        view.sheetMessage = ""
    }
    function byPlacement(placement) {
        var out = []
        for (var i = 0; i < view.sections.length; ++i)
            if (view.sections[i].placement === placement) out.push(view.sections[i])
        return out
    }
    // A section has data when it fetched, or when it failed but still shows
    // what it cached (and so carries an "as of" time).
    function hasData(s) {
        return s.status === "ok" || !!s.as_of
    }
    function findSharedProblem() {
        var list = view.sections
        if (list.length === 0) return ""
        var text = list[0].error || ""
        for (var i = 0; i < list.length; ++i) {
            var s = list[i]
            if (s.status !== "error" || view.hasData(s) || (s.error || "") !== text) return ""
        }
        return text
    }
    function anyHasData() {
        for (var i = 0; i < view.sections.length; ++i)
            if (view.hasData(view.sections[i])) return true
        return false
    }
    // The plea to wake the Mac is said once, for the whole view. A section
    // without data beside others that have some just says "offline".
    function sectionError(s) {
        var err = s.error || ""
        if (err.indexOf("Can't reach the Mac") === 0 && !view.hasData(s) && view.anyHasData())
            return "offline"
        return err
    }
    function sectionStatus(s) {
        if (s.status === "none") return s.error || "No data yet"
        var parts = []
        if (s.as_of) parts.push("as of " + s.as_of)
        if (s.status === "error" && s.error) parts.push(view.sectionError(s))
        if (s.warning) parts.push(s.warning)
        return parts.join(" · ")
    }
    function itemCount(s) {
        var n = 0
        var groups = s.groups || []
        for (var i = 0; i < groups.length; ++i) n += (groups[i].items || []).length
        return n
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
            anchors.left: parent.left; anchors.leftMargin: 32; anchors.verticalCenter: parent.verticalCenter
            text: "Today"; font.pixelSize: 44; font.bold: true; color: "#111"
        }
    }

    Rectangle {
        id: footer
        anchors.left: parent.left; anchors.right: parent.right; anchors.bottom: parent.bottom
        height: 120
        color: "#f4f2eb"
        Text {
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
            width: parent.width; horizontalAlignment: Text.AlignHCenter; wrapMode: Text.Wrap
            text: "Today isn't set up on this tablet"; font.pixelSize: 36; font.bold: true; color: "#111"
        }
        Text {
            width: parent.width; horizontalAlignment: Text.AlignHCenter; wrapMode: Text.Wrap
            text: view.problem || "Run bin/rm-today-setup on the Mac."; font.pixelSize: 24; color: "#444"
        }
    }

    Text {
        anchors.centerIn: parent
        width: Math.min(parent.width - 96, 1200)
        visible: view.configured && view.sharedProblem !== ""
        horizontalAlignment: Text.AlignHCenter; wrapMode: Text.Wrap
        text: view.sharedProblem; font.pixelSize: 32; color: "#7a1515"
    }

    // Banner sections run across the top.
    Column {
        id: banners
        anchors.left: parent.left; anchors.right: parent.right; anchors.top: header.bottom; anchors.margins: 24
        spacing: 10
        visible: view.configured && view.sharedProblem === ""
        Repeater {
            model: view.byPlacement("banner")
            delegate: Column {
                id: banner
                required property var modelData
                width: banners.width
                spacing: 4
                Text {
                    width: banner.width; elide: Text.ElideRight
                    text: (banner.modelData.title || "") + "   " + view.sectionStatus(banner.modelData)
                    font.pixelSize: 22; color: banner.modelData.status === "error" ? "#7a1515" : "#555"
                }
                Repeater {
                    model: (banner.modelData.groups || []).slice(0, 4)
                    delegate: Text {
                        id: bannerLine
                        required property var modelData
                        width: banner.width
                        wrapMode: Text.Wrap; maximumLineCount: 2; elide: Text.ElideRight
                        font.pixelSize: 24; color: "#111"
                        text: (bannerLine.modelData.title ? bannerLine.modelData.title + ": " : "")
                              + (bannerLine.modelData.items || []).map(function(i) { return i.title || "" }).join(" · ")
                    }
                }
            }
        }
    }

    // Column sections, in the order the backend sends them: two across in
    // landscape, one in portrait.
    Grid {
        id: columnGrid
        anchors.left: parent.left; anchors.right: parent.right
        anchors.top: banners.bottom; anchors.bottom: footer.top; anchors.margins: 24
        columns: view.width > view.height ? 2 : 1
        spacing: 24
        visible: view.configured && view.sharedProblem === ""
        property int rowCount: Math.max(1, Math.ceil(columnRepeater.count / columnGrid.columns))
        Repeater {
            id: columnRepeater
            model: view.byPlacement("column")
            delegate: Rectangle {
                id: column
                required property var modelData
                width: (columnGrid.width - columnGrid.spacing * (columnGrid.columns - 1)) / columnGrid.columns
                height: (columnGrid.height - columnGrid.spacing * (columnGrid.rowCount - 1)) / columnGrid.rowCount
                color: "#ffffff"; border.width: 2; border.color: "#cccccc"; radius: 8
                Column {
                    id: columnHead
                    anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top; anchors.margins: 16
                    spacing: 4
                    Text {
                        width: columnHead.width; elide: Text.ElideRight
                        text: column.modelData.title || ""; font.pixelSize: 32; font.bold: true; color: "#111"
                    }
                    Text {
                        width: columnHead.width; wrapMode: Text.Wrap
                        text: view.sectionStatus(column.modelData); font.pixelSize: 20
                        color: column.modelData.status === "error" ? "#7a1515" : "#555"
                    }
                }
                Flickable {
                    id: columnScroll
                    anchors.left: parent.left; anchors.right: parent.right
                    anchors.top: columnHead.bottom; anchors.bottom: parent.bottom; anchors.margins: 16
                    contentHeight: columnBody.implicitHeight
                    boundsBehavior: Flickable.StopAtBounds
                    clip: true
                    Column {
                        id: columnBody
                        width: columnScroll.width
                        spacing: 8
                        Text {
                            visible: view.hasData(column.modelData) && view.itemCount(column.modelData) === 0
                            text: "Nothing here"; font.pixelSize: 24; color: "#555"
                        }
                        Repeater {
                            // A section with no data yet offers nothing to tap.
                            model: column.modelData.status === "none" ? [] : (column.modelData.groups || [])
                            delegate: Column {
                                id: group
                                required property var modelData
                                width: columnBody.width
                                spacing: 6
                                Text {
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
                                                width: rowText.width; wrapMode: Text.Wrap; maximumLineCount: 2; elide: Text.ElideRight
                                                text: row.modelData.title || ""; font.pixelSize: 26; color: "#111"
                                            }
                                            Text {
                                                width: rowText.width; elide: Text.ElideRight
                                                visible: text !== ""
                                                text: row.modelData.subtitle || ""; font.pixelSize: 20; color: "#555"
                                            }
                                        }
                                        MouseArea {
                                            id: rowArea
                                            anchors.fill: parent
                                            onClicked: view.openSheet(column.modelData, row.modelData)
                                        }
                                    }
                                }
                            }
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
                // Line limits keep the buttons on the screen whatever the text.
                Text { width: sheetBody.width; wrapMode: Text.Wrap; maximumLineCount: 4; elide: Text.ElideRight
                       font.pixelSize: 34; font.bold: true; color: "#111"
                       text: view.sheetItem ? (view.sheetItem.title || "") : "" }
                Text { width: sheetBody.width; wrapMode: Text.Wrap; font.pixelSize: 24; color: "#555"; visible: text !== ""
                       text: view.sheetItem ? (view.sheetItem.subtitle || "") : "" }
                Text { width: sheetBody.width; wrapMode: Text.Wrap; maximumLineCount: 14; elide: Text.ElideRight
                       font.pixelSize: 26; color: "#111"; visible: text !== ""
                       text: view.sheetItem ? (view.sheetItem.detail || "") : "" }
                Text { width: sheetBody.width; wrapMode: Text.Wrap; font.pixelSize: 22; color: "#7a1515"; visible: text !== ""
                       text: view.sheetMessage }
                Row {
                    spacing: 16
                    Repeater {
                        model: view.sheetItem ? (view.sheetItem.actions || []) : []
                        delegate: Button {
                            id: actionButton
                            required property var modelData
                            width: 260; height: 88; font.pixelSize: 26
                            enabled: !view.acting
                            text: view.acting && view.actingSerial === view.sheetSerial
                                  && view.actingAction === (actionButton.modelData.id || "")
                                  ? "…" : (actionButton.modelData.label || "")
                            onClicked: {
                                view.acting = true
                                view.actingAction = actionButton.modelData.id || ""
                                view.actingSerial = view.sheetSerial
                                view.sheetMessage = ""
                                view.actionRequested(view.sheetSection.id, view.sheetSection.rev,
                                                     actionButton.modelData.id || "", view.sheetItem.key || "")
                            }
                        }
                    }
                    Button { text: "Close"; width: 200; height: 88; font.pixelSize: 26; onClicked: view.closeSheet() }
                }
            }
        }
    }
}
