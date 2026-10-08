import QtQuick 2.5
import QtTest 1.2
import "../../app/ui"

// The metrics view: a few labelled rows. The label is at the left, the value at
// the right in bold, the detail small under them; warn and bad rows carry a
// marker and a bold label. Every string is the gateway's, so it is drawn as typed:
// a Text that parses markup would turn "<b>x</b>" into "x".
//
// CI runs it (the "QML lint and resources" step). From the repository root:
//   QT_QPA_PLATFORM=offscreen qmltestrunner -input tests/qml
TestCase {
    id: tc
    name: "TodayViewMetrics"
    when: windowShown
    visible: true                // a TestCase is hidden by default, and so is everything in it
    width: 1404; height: 1872

    readonly property string markup: '<b>x</b> <img src="http://127.0.0.1:9/x.png">'

    TodayView { id: view; anchors.fill: parent }

    function row(label, value, detail, tone) {
        var r = {label: label, value: value, tone: tone || "neutral"}
        if (detail) r.detail = detail
        return r
    }
    function cellOf(rows, extra) {
        var c = {widget: "w.m", view: "metrics", x: 0, y: 0, w: 2, h: 1, title: "Trading", note: "", tone: "neutral"}
        if (rows !== null) c.metrics = {rows: rows}
        for (var k in (extra || {})) c[k] = extra[k]
        return c
    }
    function screenOf(cells) {
        return {id: "widgets", rev: 1, status: "ok", as_of: "08:00",
                screen: {cols: 2, rows: Math.max(1, cells.length), banners: [], cells: cells}}
    }
    function show(cells) {
        view.apply({configured: true, sections: [tc.screenOf(cells)]})
        waitForRendering(view)           // layout runs on the next frame
    }

    function named(root, name, out) {
        out = out || []
        for (var i = 0; i < root.children.length; ++i) {
            var c = root.children[i]
            if (c.objectName === name) out.push(c)
            tc.named(c, name, out)
        }
        return out
    }
    function texts(root, out) {
        out = out || []
        for (var i = 0; i < root.children.length; ++i) {
            var c = root.children[i]
            if (c.textFormat !== undefined) out.push(c)
            tc.texts(c, out)
        }
        return out
    }
    function strings(items) { return items.map(function(t) { return t.text }) }

    function init() { view.closeSheet(); view.apply({configured: true, sections: []}) }

    function test_rows_come_in_order_with_label_value_and_detail() {
        tc.show([tc.cellOf([
            tc.row("Realized P&L", "+$48.20", "31 closed trades", "good"),
            tc.row("Open risk", "3.1%", "", "warn"),
            tc.row("Drawdown", "-8%", "past the limit", "bad"),
            tc.row("Trades today", "4", "", "neutral")])])
        var labels = tc.named(view, "metricLabel")
        var values = tc.named(view, "metricValue")
        var details = tc.named(view, "metricDetail")
        compare(tc.strings(labels), ["Realized P&L", "! Open risk", "!! Drawdown", "Trades today"])
        compare(tc.strings(values), ["+$48.20", "3.1%", "-8%", "4"])
        compare(tc.strings(details), ["31 closed trades", "", "past the limit", ""])
        compare(details.map(function(d) { return d.visible }), [true, false, true, false], "only a detail that exists takes room")

        // top to bottom in the order sent; the label at the left, the value at the right
        var lp = labels.map(function(l) { return l.mapToItem(view, 0, 0) })
        var vp = values.map(function(v) { return v.mapToItem(view, 0, 0) })
        for (var i = 0; i < labels.length; ++i) {
            verify(vp[i].x > lp[i].x, "row " + i + ": value right of its label")
            if (i > 0) verify(lp[i].y > lp[i - 1].y, "row " + i + " below row " + (i - 1))
        }
        // the detail sits under its own label, left-aligned with it
        var dp = details[0].mapToItem(view, 0, 0)
        verify(dp.y > lp[0].y + labels[0].height - 1, "detail below its row")
        compare(dp.x, lp[0].x)
        // a row with a detail is taller than one without (row 0 has one, row 1 has none)
        verify(lp[1].y - lp[0].y > lp[2].y - lp[1].y, "the detail takes room")
    }

    function test_value_is_bold_and_only_warn_and_bad_rows_are_marked() {
        tc.show([tc.cellOf([
            tc.row("a", "1", "", "good"), tc.row("b", "2", "", "neutral"),
            tc.row("c", "3", "", "warn"), tc.row("d", "4", "", "bad")])])
        var labels = tc.named(view, "metricLabel")
        var values = tc.named(view, "metricValue")
        compare(values.map(function(v) { return v.font.bold }), [true, true, true, true])
        compare(labels.map(function(l) { return l.font.bold }), [false, false, true, true])
        compare(tc.strings(labels), ["a", "b", "! c", "!! d"])
        verify(values[0].font.pixelSize > labels[0].font.pixelSize - 1, "the value is at least as large as the label")
    }

    function test_every_text_is_plain() {
        var m = tc.markup
        tc.show([tc.cellOf([tc.row(m, m, m, "warn"), tc.row(m, m, m, "bad"), tc.row(m, m, "", "good")], {title: m, note: m})])
        var held = tc.texts(view).filter(function(t) { return t.text.indexOf(m) >= 0 })
        // title, note, three labels, three values, two details
        verify(held.length >= 10, "texts holding the markup: " + held.length)
        var bad = held.filter(function(t) { return t.textFormat !== Text.PlainText })
                      .map(function(t) { return "'" + t.text + "' (format " + t.textFormat + ")" })
        compare(bad, [], "markup drawn as rich text")
        var rows = tc.named(view, "metricLabel").concat(tc.named(view, "metricValue"), tc.named(view, "metricDetail"))
        compare(rows.length, 9)
        compare(rows.filter(function(t) { return t.textFormat !== Text.PlainText }).length, 0)
    }

    function test_a_cell_without_rows_says_nothing_here() {
        tc.show([tc.cellOf(null)])
        compare(view.empty(view.screen.cells[0]), true)
        compare(tc.named(view, "metricLabel").length, 0)
        verify(tc.texts(view).filter(function(t) { return t.text === "Nothing here" && t.visible }).length === 1)

        tc.show([tc.cellOf([])])
        compare(view.empty(view.screen.cells[0]), true)

        tc.show([tc.cellOf([tc.row("a", "1")])])
        compare(view.empty(view.screen.cells[0]), false)
        verify(tc.texts(view).filter(function(t) { return t.text === "Nothing here" && t.visible }).length === 0)
        // another view's data does not make a metrics cell non-empty
        compare(view.empty({view: "metrics", stat: {value: "1"}}), true)
    }

    // The most rows the gateway sends is eight, each with a detail: in a cell of the least height they scroll
    // (clipped) rather than spill out of the cell.
    function test_eight_rows_scroll_inside_the_cell() {
        var rows = []
        for (var i = 0; i < 8; ++i) rows.push(tc.row("Row " + i, String(i), "detail " + i))
        // Eight cells stacked in portrait leave each only the least row height.
        var cells = [tc.cellOf(rows, {w: 1, h: 1})]
        for (var c = 1; c < 8; ++c) cells.push(tc.cellOf([tc.row("x", "1")], {x: 0, y: c, w: 1, h: 1}))
        tc.show(cells)
        var labels = tc.named(view, "metricLabel").slice(0, 8)
        compare(tc.strings(labels), rows.map(function(r) { return r.label }))
        var scroller = labels[0]
        while (scroller !== null && scroller.boundsBehavior === undefined) scroller = scroller.parent
        verify(scroller !== null, "the rows sit in a Flickable")
        compare(scroller.clip, true)
        verify(scroller.contentHeight > scroller.height, "eight rows with details need more than the cell: " + scroller.contentHeight + " vs " + scroller.height)
    }
}
