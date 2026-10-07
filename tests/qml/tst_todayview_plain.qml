import QtQuick 2.5
import QtTest 1.2
import "../../app/ui"

// Gateway text is third-party data: the Today view must draw it as typed and
// never parse it as markup (an <img> in it would make the tablet fetch a URL).
// Every data-bound Text must be Text.PlainText, in the grid, in the open sheet
// (Controls buttons included) and in the confirm step; a Text that is not
// would turn "<b>x</b>" into "x" and "&lt;" into "<".
//
// CI runs it (the "QML lint and resources" step). From the repository root:
//   QT_QPA_PLATFORM=offscreen qmltestrunner -input tests/qml
TestCase {
    id: tc
    name: "TodayViewPlainText"
    when: windowShown
    width: 1404; height: 1872

    readonly property string markup: '<b>x</b> <img src="http://127.0.0.1:9/x.png">'

    TodayView { id: view; anchors.fill: parent }

    function screenOf(m) {
        var item = {key: "k1", title: m, subtitle: m, detail: m, tone: "warn", actions: [
            {id: "low", label: m, risk: "low"},
            {id: "med", label: m, risk: "medium", confirm: m}]}
        return {id: "widgets", rev: 1, status: "ok", as_of: m, warning: m,
            screen: {cols: 2, rows: 2,
                banners: [{widget: "w.a", alert: {text: m, tone: "warn"}},
                          {widget: "w.b", problem: true, title: m, note: m}],
                cells: [
                    {widget: "w.a", view: "list", x: 0, y: 0, w: 1, h: 1, title: m, note: m, tone: "warn",
                     groups: [{title: m, items: [item]}]},
                    {widget: "w.b", view: "stat", x: 1, y: 0, w: 1, h: 1, title: m, note: m,
                     stat: {value: m, label: m, delta: m, trend: "up", tone: "good"}},
                    {widget: "w.c", view: "spark", x: 0, y: 1, w: 1, h: 1, title: m,
                     spark: {label: m, points: [0, 1, 0.5], min: m, max: m, last: m, unit: m}}]}}
    }

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

    // The texts that carry the markup, checked to be plain; returns how many.
    function markupTexts(what) {
        var held = tc.texts(view).filter(function(t) { return t.text.indexOf(tc.markup) >= 0 })
        var bad = held.filter(function(t) { return t.textFormat !== Text.PlainText })
                      .map(function(t) { return "'" + t.text + "' (format " + t.textFormat + ")" })
        compare(bad, [], what + ": markup drawn as rich text")
        return held.length
    }

    function init() { view.closeSheet(); view.notice = ""; view.apply({configured: true, sections: []}) }

    function test_grid_sheet_and_confirm() {
        view.apply({configured: true, refreshing: true, sections: [tc.screenOf(tc.markup)]})
        view.notice = tc.markup
        // 2 banners, 3 cell titles + 2 notes, list group/row (2), stat (3), spark (4), status strip, notice
        verify(tc.markupTexts("grid") >= 17, "grid texts holding the markup")

        var item = view.screen.cells[0].groups[0].items[0]
        view.openSheet(item, tc.markup)
        // + widget, title, subtitle, detail and the two action buttons
        verify(tc.markupTexts("sheet") >= 17 + 6, "sheet texts holding the markup")

        view.tap(item.actions[1])    // the medium action asks first
        var held = tc.texts(view).filter(function(t) { return t.text === "Yes, " + tc.markup })
        compare(held.length, 1, "the Yes button")
        // the action buttons give way to the question and its Yes
        verify(tc.markupTexts("confirm") >= 17 + 6, "confirm texts holding the markup")
        compare(held[0].textFormat, Text.PlainText)

        view.sheetMessage = tc.markup
        tc.markupTexts("refusal")
    }

    function test_no_data_and_not_configured() {
        view.apply({configured: false, problem: tc.markup, sections: []})
        verify(tc.markupTexts("not configured") >= 1, "problem text")
        view.apply({configured: true, sections: [{id: "widgets", rev: 1, status: "error", error: tc.markup}]})
        verify(tc.markupTexts("error without data") >= 1, "error text")
    }

    // A medium action without a question asks with its label.
    function test_confirm_falls_back_to_the_label() {
        var s = tc.screenOf("Pay")
        delete s.screen.cells[0].groups[0].items[0].actions[1].confirm
        view.apply({configured: true, sections: [s]})
        var item = view.screen.cells[0].groups[0].items[0]
        view.openSheet(item, "")
        var asked = function() { return tc.texts(view).filter(function(t) { return t.text === "Pay?" }).length }
        compare(asked(), 0)
        view.tap(item.actions[1])
        compare(asked(), 1)
    }
}
