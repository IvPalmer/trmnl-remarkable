import QtQuick 2.5
import QtTest 1.2
import "../../app/ui"

// Alerts come from the gateway and can be many and long. They must not eat
// the grid: each banner is at most two lines, at most three are drawn, and
// the rest are counted in one line ("+N more alerts").
//
// CI runs it (the "QML lint and resources" step). From the repository root:
//   QT_QPA_PLATFORM=offscreen qmltestrunner -input tests/qml
TestCase {
    id: tc
    name: "TodayViewBanners"
    when: windowShown
    visible: true                   // a TestCase is hidden by default; the checks read what shows
    width: 1872; height: 1404       // landscape: the worst case for the grid's height

    TodayView { id: view; anchors.fill: parent }

    // 500 characters of ordinary words, so that it wraps onto several lines.
    function longAlert(i) {
        return ("Alert " + i + " " + new Array(40).join("pay the invoice ")).slice(0, 500)
    }

    function screenWith(count) {
        var banners = []
        for (var i = 0; i < count; ++i)
            banners.push({widget: "w.a" + i, alert: {text: tc.longAlert(i), tone: "warn"}})
        return {id: "widgets", rev: 1, status: "ok", as_of: "08:00",
            screen: {cols: 2, rows: 1, banners: banners,
                cells: [{widget: "w.b", view: "stat", x: 0, y: 0, w: 1, h: 1, title: "Money",
                         stat: {value: "12", label: "open", tone: "good"}}]}}
    }

    // Every Text under root, hidden ones too, at any depth.
    function texts(root, out) {
        out = out || []
        for (var i = 0; i < root.children.length; ++i) {
            var c = root.children[i]
            if (c.textFormat !== undefined) out.push(c)
            texts(c, out)
        }
        return out
    }

    // The grid is the one Flickable that is a direct child of the view.
    function gridOf() {
        var g = view.children.filter(function(c) { return c.boundsBehavior !== undefined })
        compare(g.length, 1, "the grid")
        return g[0]
    }

    function init() { view.closeSheet(); view.apply({configured: true, sections: []}) }

    function test_many_long_alerts_leave_the_grid_its_room() {
        compare(view.landscape, true)
        view.apply({configured: true, sections: [tc.screenWith(12)]})
        waitForRendering(view)      // the Column and the anchors settle when the frame is polished
        var banners = tc.texts(view).filter(function(t) { return t.text.indexOf("Alert ") >= 0 })
        compare(banners.length, 3, "banners drawn")
        for (var i = 0; i < banners.length; ++i) {
            compare(banners[i].lineCount, 2, "banner " + i + " lines")
            verify(banners[i].truncated, "banner " + i + " is elided")
        }
        var more = tc.texts(view).filter(function(t) { return t.text === "+9 more alerts" })
        compare(more.length, 1, "the count of the rest")
        compare(more[0].textFormat, Text.PlainText)
        verify(more[0].visible, "the count shows")
        verify(tc.gridOf().height > view.height / 2, "grid height " + tc.gridOf().height + " of " + view.height)
    }

    function test_alerts_that_fit_show_no_count() {
        view.apply({configured: true, sections: [tc.screenWith(3)]})
        compare(tc.texts(view).filter(function(t) { return t.text.indexOf("Alert ") >= 0 }).length, 3)
        compare(tc.texts(view).filter(function(t) { return t.text.indexOf("more alert") >= 0 && t.visible }).length, 0)
        view.apply({configured: true, sections: [tc.screenWith(4)]})
        compare(tc.texts(view).filter(function(t) { return t.text === "+1 more alert" }).length, 1)
    }
}
