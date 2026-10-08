import QtQuick 2.5
import QtTest 1.2
import "../../app/ui"

// The sheet a tap region opens is built from text the BYOS server chose (title,
// action labels, confirm questions) and from the gateway's answer. All of it is
// drawn as typed: a Text that parses markup would turn "<b>x</b>" into "x", and an
// <img> in it would make the tablet fetch a URL. A Button parses its own `text` as
// rich text, so a label sits in a child Text instead.
//
// CI runs it (the "QML lint and resources" step). From the repository root:
//   QT_QPA_PLATFORM=offscreen qmltestrunner -input tests/qml
TestCase {
    id: tc
    name: "TapSheet"
    when: windowShown
    visible: true                // a TestCase is hidden by default, and a hidden item takes no clicks
    width: 1404; height: 1872

    readonly property string markup: '<b>x</b> <img src="http://127.0.0.1:9/x.png">'

    TapSheet { id: sheet; anchors.fill: parent }
    SignalSpy { id: requests; target: sheet; signalName: "actionRequested" }

    function tapOf(title) {
        return {x: 1, y: 2, w: 3, h: 4, widget: "stocks.watch", key: "AAPL", title: title, actions: [
            {id: "refresh", label: "Refresh " + title},
            {id: "sell", label: "Sell " + title, confirm: "Sell {title}? {title} is gone."}]}
    }
    // The arguments of the nth actionRequested, as an array.
    function args(n) { return Array.prototype.slice.call(requests.signalArguments[n]) }
    // Layout runs on the next frame; clicks need the buttons where they will be.
    function settle() { waitForRendering(sheet) }
    function init() { sheet.close(); sheet.acting = false; sheet.actingAction = ""; sheet.actingSerial = -1; requests.clear() }

    // Every item with a text format, hidden ones too, at any depth.
    function texts(root, out) {
        out = out || []
        for (var i = 0; i < root.children.length; ++i) {
            var c = root.children[i]
            if (c.textFormat !== undefined) out.push(c)
            texts(c, out)
        }
        return out
    }
    function named(root, name, out) {
        out = out || []
        for (var i = 0; i < root.children.length; ++i) {
            var c = root.children[i]
            if (c.objectName === name) out.push(c)
            named(c, name, out)
        }
        return out
    }
    function textNamed(name) { return tc.named(sheet, name)[0] }
    // The visible Text that reads `what`.
    function labelled(what) {
        return tc.texts(sheet).filter(function(t) { return t.text === what })
    }
    function plainEverywhere(what) {
        var bad = tc.texts(sheet).filter(function(t) { return t.textFormat !== Text.PlainText })
                    .map(function(t) { return "'" + t.text + "' (format " + t.textFormat + ")" })
        compare(bad, [], what + ": text that is not PlainText")
    }
    // The buttons that show a Text reading `what` (a button's label is its child Text).
    function buttonLabelled(what) {
        var found = tc.labelled(what)
        compare(found.length, 1, "the '" + what + "' label")
        return found[0].parent
    }

    function test_every_text_is_plain() {
        var m = tc.markup
        sheet.show(tc.tapOf(m), "page1")
        tc.settle()
        tc.plainEverywhere("actions")
        // the title and both labels carry the markup and are drawn as typed
        compare(tc.textNamed("tapTitle").text, m)
        compare(tc.labelled("Refresh " + m).length, 1)
        compare(tc.labelled("Sell " + m).length, 1)

        // the confirm step: the question and the Yes button
        sheet.choose(sheet.tap.actions[1])
        tc.settle()
        tc.plainEverywhere("confirm")
        compare(tc.textNamed("tapQuestion").text, "Sell " + m + "? " + m + " is gone.")
        compare(tc.labelled("Yes, Sell " + m).length, 1)

        // the answers, good and bad
        sheet.confirming = null
        sheet.acting = true; sheet.actingSerial = sheet.serial
        sheet.result({ok: false, message: m, outcome: "refused", refresh: true})
        tc.plainEverywhere("refusal")
        compare(tc.textNamed("tapMessage").text, m)
        sheet.acting = true; sheet.actingSerial = sheet.serial
        sheet.result({ok: true, message: m, outcome: "done", refresh: true})
        tc.plainEverywhere("success")
        compare(tc.textNamed("tapMessage").text, m)
    }

    function test_a_plain_action_is_sent_with_the_tap_and_its_screen() {
        sheet.show(tc.tapOf("Apple"), "page1")
        tc.settle()
        compare(sheet.visible, true)
        compare(tc.textNamed("tapTitle").text, "Apple")
        compare(tc.named(sheet, "tapAction").length, 2)        // one button per action
        mouseClick(tc.buttonLabelled("Refresh Apple"))
        compare(requests.count, 1)
        compare(tc.args(0), ["stocks.watch", "AAPL", "refresh", "page1", "Apple"])
        verify(sheet.confirming === null)
        compare(sheet.acting, true)
    }

    function test_confirm_substitutes_the_title_and_asks_first() {
        sheet.show(tc.tapOf("A{title}B <i>"), "page1")        // a title that holds the placeholder cannot loop
        tc.settle()
        mouseClick(tc.buttonLabelled("Sell A{title}B <i>"))
        tc.settle()
        compare(requests.count, 0, "nothing is sent before the answer")
        compare(tc.textNamed("tapQuestion").visible, true)
        compare(tc.textNamed("tapQuestion").text, "Sell A{title}B <i>? A{title}B <i> is gone.")
        compare(tc.named(sheet, "tapAction").length, 0, "the actions give way to the question")
        compare(tc.named(sheet, "tapYes")[0].visible, true)

        // Cancel goes back to the actions
        mouseClick(tc.named(sheet, "tapCancel")[0])
        tc.settle()
        verify(sheet.confirming === null)
        compare(tc.named(sheet, "tapAction").length, 2)
        compare(requests.count, 0)

        mouseClick(tc.buttonLabelled("Sell A{title}B <i>"))
        tc.settle()
        mouseClick(tc.named(sheet, "tapYes")[0])
        compare(requests.count, 1)
        compare(tc.args(0), ["stocks.watch", "AAPL", "sell", "page1", "A{title}B <i>"])
    }

    function test_buttons_are_off_while_a_request_runs() {
        sheet.show(tc.tapOf("Apple"), "page1")
        tc.settle()
        mouseClick(tc.buttonLabelled("Refresh Apple"))
        compare(requests.count, 1)
        var buttons = tc.named(sheet, "tapAction")
        compare(buttons.length, 2)
        for (var i = 0; i < buttons.length; ++i) compare(buttons[i].enabled, false, "button " + i)
        compare(tc.labelled("…").length, 1, "the running action shows an ellipsis")
        mouseClick(buttons[1])                                  // nothing happens
        verify(sheet.confirming === null)
        compare(requests.count, 1)

        // the answer ends it
        sheet.result({ok: false, message: "Refused: stale", outcome: "refused", refresh: true})
        compare(sheet.acting, false)
        for (var j = 0; j < buttons.length; ++j) compare(buttons[j].enabled, true, "button " + j + " again")
        compare(tc.textNamed("tapMessage").text, "Refused: stale")
    }

    function test_confirm_is_off_while_a_request_runs() {
        sheet.show(tc.tapOf("Apple"), "page1")
        tc.settle()
        sheet.choose(sheet.tap.actions[1])
        tc.settle()
        var yes = tc.named(sheet, "tapYes")[0]
        compare(yes.enabled, true)
        mouseClick(yes)
        compare(requests.count, 1)
        // Yes is asked again while the request runs (as if choose() had not guarded it): off, and run() refuses too.
        sheet.confirming = sheet.tap.actions[1]
        tc.settle()
        compare(yes.visible, true)
        compare(yes.enabled, false, "a second Yes while one runs")
        mouseClick(yes)
        sheet.run(sheet.confirming)
        compare(requests.count, 1)
    }

    function test_the_answer_is_shown_and_close_ends_the_sheet() {
        sheet.show(tc.tapOf("Apple"), "page1")
        tc.settle()
        mouseClick(tc.buttonLabelled("Refresh Apple"))
        sheet.result({ok: true, message: "Refreshed", outcome: "done", refresh: true})
        compare(tc.textNamed("tapMessage").text, "Refreshed")
        compare(tc.textNamed("tapMessage").visible, true)
        compare(tc.named(sheet, "tapAction").length, 0, "nothing more to run after a success")
        mouseClick(tc.named(sheet, "tapClose")[0])
        compare(sheet.visible, false)
        verify(sheet.tap === null)
        compare(tc.textNamed("tapMessage").text, "")
    }

    function test_a_failed_action_can_be_tried_again() {
        sheet.show(tc.tapOf("Apple"), "page1")
        tc.settle()
        mouseClick(tc.buttonLabelled("Refresh Apple"))
        sheet.result({ok: false, message: "The Mac answered HTTP 500", outcome: "error", refresh: false})
        compare(tc.textNamed("tapMessage").text, "The Mac answered HTTP 500")
        compare(tc.named(sheet, "tapAction").length, 2)
        mouseClick(tc.buttonLabelled("Refresh Apple"))
        compare(requests.count, 2)
        compare(tc.textNamed("tapMessage").text, "", "the old answer goes when another action starts")
    }

    // An answer that arrives after the sheet was closed (and maybe opened for another tap) is not shown.
    function test_a_late_answer_is_not_shown_in_another_opening() {
        sheet.show(tc.tapOf("Apple"), "page1")
        tc.settle()
        mouseClick(tc.buttonLabelled("Refresh Apple"))
        sheet.close()
        compare(sheet.acting, true, "the request is still out")
        sheet.show(tc.tapOf("Pear"), "page2")
        tc.settle()
        compare(tc.named(sheet, "tapAction")[0].enabled, false, "buttons wait for the old answer")
        sheet.result({ok: true, message: "Refreshed Apple", outcome: "done", refresh: true})
        compare(sheet.acting, false)
        compare(tc.textNamed("tapMessage").text, "")
        compare(tc.named(sheet, "tapAction")[0].enabled, true)
    }

    function test_a_new_image_closes_a_sheet_that_is_only_asking() {
        sheet.show(tc.tapOf("Apple"), "page1")
        tc.settle()
        sheet.imageChanged()
        compare(sheet.visible, false)

        // one showing an answer, or waiting for one, stays
        sheet.show(tc.tapOf("Apple"), "page1")
        tc.settle()
        mouseClick(tc.buttonLabelled("Refresh Apple"))
        sheet.imageChanged()
        compare(sheet.visible, true)
        sheet.result({ok: true, message: "Refreshed", outcome: "done", refresh: true})
        sheet.imageChanged()
        compare(sheet.visible, true)
    }

    function test_a_tap_without_actions_shows_only_its_title() {
        sheet.show({widget: "a.b", key: "k", title: "Just a title", actions: []}, "page1")
        tc.settle()
        compare(sheet.visible, true)
        compare(tc.textNamed("tapTitle").text, "Just a title")
        compare(tc.named(sheet, "tapAction").length, 0)
        compare(tc.named(sheet, "tapClose").length, 1)
    }
}
