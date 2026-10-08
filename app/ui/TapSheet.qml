pragma ComponentBehavior: Bound
import QtQuick 2.5
import QtQuick.Controls 2.5

// remarkable-ai: the sheet a tap region opens. It shows the tap's title and one
// button per action. An action that comes with a `confirm` question asks it first
// ({title} is replaced by the tap's title), then Yes or Cancel. A running action
// disables every button until the answer (message 111) arrives, which is shown in
// the sheet. Everything here is third-party text, so every Text is PlainText and a
// button's label is a child Text (a Button parses its own `text` as rich text).
// Inside the displayed area (not a Popup), so it rotates with the screen.
Item {
    id: sheet

    property var tap: null              // the tap region that opened the sheet
    property string screen: ""          // the tap_screen it came with
    property var confirming: null       // an action waiting for its Yes
    property bool acting: false         // a request is in flight
    property string message: ""         // the answer, once there is one
    property bool answerOk: false
    property bool answered: false
    property int serial: 0              // counts openings
    property int actingSerial: -1       // the opening the request in flight came from
    property string actingAction: ""

    readonly property bool open: sheet.tap !== null
    readonly property string title: sheet.tap ? (sheet.tap.title || "") : ""
    // After a success there is nothing more to run from this opening.
    readonly property var offered: sheet.tap && !(sheet.answered && sheet.answerOk) ? (sheet.tap.actions || []) : []

    // widget, key, action, screen, title: the fields of message 20.
    signal actionRequested(string widget, string key, string action, string screen, string title)

    function show(tap, screen) {
        sheet.tap = tap
        sheet.screen = screen || ""
        sheet.confirming = null
        sheet.message = ""
        sheet.answered = false
        sheet.answerOk = false
        sheet.serial += 1
    }
    function close() {
        sheet.tap = null
        sheet.confirming = null
        sheet.message = ""
        sheet.answered = false
        sheet.answerOk = false
        sheet.screen = ""
    }
    // The question, with {title} replaced (split/join: the title is data, never a pattern).
    function question(action) {
        var q = action.confirm || ""
        return q.split("{title}").join(sheet.title)
    }
    function choose(action) {
        if (sheet.acting) return
        sheet.message = ""
        sheet.answered = false
        if (action.confirm) sheet.confirming = action
        else sheet.run(action)
    }
    function run(action) {
        if (sheet.acting || !sheet.tap) return
        sheet.confirming = null
        sheet.acting = true
        sheet.actingAction = action.id || ""
        sheet.actingSerial = sheet.serial
        sheet.actionRequested(sheet.tap.widget || "", sheet.tap.key || "", action.id || "", sheet.screen, sheet.title)
    }
    // Message 111. The answer is shown only in the opening the request came from.
    function result(data) {
        var mine = sheet.tap !== null && sheet.actingSerial === sheet.serial
        sheet.acting = false
        sheet.actingAction = ""
        sheet.actingSerial = -1
        if (!mine) return
        sheet.message = data.message || (data.ok ? "Done" : "Failed")
        sheet.answerOk = !!data.ok
        sheet.answered = true
    }
    // The image under the sheet was replaced. A sheet still asking about the old
    // one goes; one showing an answer stays until it is closed.
    function imageChanged() {
        if (!sheet.acting && !sheet.answered) sheet.close()
    }

    // A button whose label is a child Text. A Button parses its own `text` as rich
    // text (an <img> in it crashes Qt), so `text` stays empty and a PlainText draws
    // the caption. Off while a request runs, except Close.
    component SheetButton: Button {
        id: button
        property string caption: ""
        height: 88; font.pixelSize: 26
        enabled: !sheet.acting
        Text {
            anchors.fill: parent; anchors.margins: 12
            textFormat: Text.PlainText; elide: Text.ElideRight; font: button.font
            horizontalAlignment: Text.AlignHCenter; verticalAlignment: Text.AlignVCenter
            color: button.enabled ? "#111" : "#777"
            text: button.caption
        }
    }

    visible: sheet.open

    MouseArea { anchors.fill: parent }   // taps behind the sheet go nowhere

    Rectangle {
        anchors.centerIn: parent
        width: Math.min(parent.width - 96, 1100)
        height: Math.min(parent.height - 96, body.implicitHeight + 64)
        color: "#ffffff"; border.width: 3; radius: 12
        Column {
            id: body
            anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top; anchors.margins: 32
            spacing: 18
            // Line limits keep the buttons on the screen whatever the text.
            Text {
                objectName: "tapTitle"
                textFormat: Text.PlainText; width: body.width; wrapMode: Text.Wrap; maximumLineCount: 4; elide: Text.ElideRight
                font.pixelSize: 34; font.bold: true; color: "#111"
                text: sheet.title
            }
            // A question, in the server's words.
            Text {
                objectName: "tapQuestion"
                textFormat: Text.PlainText; width: body.width; wrapMode: Text.Wrap; maximumLineCount: 4; elide: Text.ElideRight
                font.pixelSize: 28; font.bold: true; color: "#111"
                visible: sheet.confirming !== null
                text: sheet.confirming ? sheet.question(sheet.confirming) : ""
            }
            Text {
                objectName: "tapMessage"
                textFormat: Text.PlainText; width: body.width; wrapMode: Text.Wrap; maximumLineCount: 4; elide: Text.ElideRight
                font.pixelSize: 26; font.bold: sheet.answered && !sheet.answerOk
                color: sheet.answered && !sheet.answerOk ? "#7a1515" : "#111"
                visible: text !== ""
                text: sheet.message
            }
            Flow {
                width: body.width
                spacing: 16
                Repeater {
                    model: sheet.confirming !== null ? [] : sheet.offered
                    delegate: SheetButton {
                        id: actionButton
                        required property var modelData
                        objectName: "tapAction"
                        width: 300
                        caption: sheet.acting && sheet.actingSerial === sheet.serial
                                 && sheet.actingAction === (actionButton.modelData.id || "")
                                 ? "…" : (actionButton.modelData.label || "")
                        onClicked: sheet.choose(actionButton.modelData)
                    }
                }
                SheetButton {
                    objectName: "tapYes"
                    visible: sheet.confirming !== null
                    width: 340
                    caption: sheet.confirming ? "Yes, " + (sheet.confirming.label || "") : ""
                    onClicked: sheet.run(sheet.confirming)
                }
                SheetButton {
                    objectName: "tapCancel"
                    visible: sheet.confirming !== null
                    width: 200
                    caption: "Cancel"
                    onClicked: sheet.confirming = null
                }
                SheetButton {
                    objectName: "tapClose"
                    width: 200
                    enabled: true
                    caption: "Close"
                    onClicked: sheet.close()
                }
            }
        }
    }
}
