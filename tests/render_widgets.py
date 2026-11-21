#!/usr/bin/env python3
"""Render actual tablet QML with synthetic data; no backend or device connection.

Requires PySide6 6.8 and system Qt graphical libraries (including libEGL).
python tests/render_widgets.py --output /path/to/stills [--source-root CHECKOUT]
Source-root lets the same fixtures capture a baseline and a changed checkout.
"""
import argparse
import copy
import json
import os
from pathlib import Path

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")
os.environ.setdefault("QT_QUICK_BACKEND", "software")

from PySide6.QtCore import QObject, Property, QUrl, Signal, Slot
from PySide6.QtGui import QColor, QFont, QGuiApplication, QImage, QPainter, QPen
from PySide6.QtQml import qmlRegisterType
from PySide6.QtQuick import QQuickView
from PySide6.QtTest import QTest


class AppLoad(QObject):
    """Only the transport is stubbed; production QML paints the screenshots."""
    messageReceived = Signal(int, str)
    applicationID = Property(str, lambda self: "trmnl.remarkable", lambda self, value: None)

    @Slot(int, str)
    def sendMessage(self, kind, contents):
        pass

    @Slot()
    def terminate(self):
        pass


def cell(kind, x, y, **data):
    return dict(widget="demo." + kind, view=kind, title=kind.title(), x=x, y=y,
                w=1, h=1, tone="neutral", note="", problem=False, **data)


def fixture():
    # The message-109 model used by the existing QML tests/AppLoad harness.
    cells = [
        cell("stat", 0, 0, stat=dict(value="123,456,789.00", label="Available balance",
                                    delta="+1,234 today", trend="up")),
        cell("list", 1, 0, groups=[dict(title="Today", items=[
            dict(key=str(i), title="Review synthetic task " + str(i), subtitle="Due today",
                 detail="Synthetic detail", actions=[]) for i in range(8)])]),
        cell("metrics", 0, 1, metrics=dict(rows=[
            dict(label="Metric " + str(i), value=str(100 + i), detail="Synthetic detail",
                 tone="warn" if i == 1 else "neutral") for i in range(8)])),
        cell("spark", 1, 1, spark=dict(label="Activity", points=[.2, .5, .3, .8, .1, .9],
                                      min="10", max="90", last="90", unit="units")),
    ]
    for i in range(4, 12):
        cells.append(cell("stat", i % 2, i // 2,
                          stat=dict(value="42", label="Synthetic count", delta="")))
    cells[0]["note"] = "as of 09:00 · offline"
    return dict(configured=True, sections=[dict(id="widgets", rev=1, status="ok", as_of="09:00",
        screen=dict(cols=2, rows=6, cells=cells, banners=[dict(title="Alert",
            alert=dict(text="Synthetic alert: review today's summary", tone="warn"))]))])


def walk(item):
    yield item
    for child in item.childItems():
        yield from walk(child)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--source-root", type=Path, default=Path(__file__).resolve().parents[1])
    args = parser.parse_args()
    dest, root = args.output.resolve(), args.source_root.resolve()
    dest.mkdir(parents=True, exist_ok=True)
    app = QGuiApplication([])
    qmlRegisterType(AppLoad, "net.asivery.AppLoad", 1, 0, "AppLoad")
    captures = []

    def save(view, name):
        QTest.qWait(350)
        image = view.grabWindow()
        if image.isNull() or not image.save(str(dest / (name + ".png"))):
            raise RuntimeError("Screenshot failed: " + name)
        captures.append(dict(name=name, width=image.width(), height=image.height()))

    wrapper = dest / "TodayHarness.qml"
    wrapper.write_text('import QtQuick\nimport "' + (root / "app/ui").as_posix() + '"\n'
        'TodayView {\n'
        ' property string stateJson: "{}"\n'
        ' onStateJsonChanged: apply(JSON.parse(stateJson))\n'
        ' property string itemJson: ""\n'
        ' onItemJsonChanged: { if (itemJson) openSheet(JSON.parse(itemJson), "Synthetic tasks"); else closeSheet() }\n'
        '}\n')

    for source, mode in [(wrapper, "today"), (root / "app/ui/TRMNL.qml", "dashboard")]:
        view = QQuickView()
        view.engine().addImportPath(str(root / "tests/qml-stubs"))
        view.setResizeMode(QQuickView.SizeRootObjectToView)
        view.setSource(QUrl.fromLocalFile(str(source)))
        if view.status() == QQuickView.Error:
            raise RuntimeError(str(view.errors()))
        item = view.rootObject()
        for width, height, orientation in [(2160, 1620, "landscape"), (1620, 2160, "portrait")]:
            view.resize(width, height)
            view.show()
            prefix = mode + "-" + orientation
            if mode == "today":
                data = fixture()
                item.setProperty("stateJson", json.dumps(data))
                save(view, prefix + "-compact")
                geometry = []
                for child in walk(item):
                    text = child.property("text")
                    if text and child.isVisible():
                        pos = child.mapToItem(item, 0, 0)
                        geometry.append(dict(text=str(text), x=pos.x(), y=pos.y(),
                                             width=child.width(), height=child.height()))
                (dest / (prefix + "-geometry.json")).write_text(json.dumps(geometry, indent=2))
                # Scroll each inner list/metrics body, then the grid, to prove access to the end.
                for child in list(walk(item)):
                    content_height = child.property("contentHeight")
                    if content_height and content_height > child.height() and child.isVisible():
                        child.setProperty("contentY", content_height - child.height())
                save(view, prefix + "-scrolled")
                item.setProperty("stateJson", json.dumps(dict(configured=True, refreshing=True,
                    sections=[dict(id="widgets", rev=2, status="error", as_of="09:00", error="offline",
                                   screen=data["sections"][0]["screen"])])))
                save(view, prefix + "-cache-refresh")
                item.setProperty("itemJson", json.dumps(dict(key="demo:1", title="Synthetic task " * 12,
                    subtitle="Synthetic subtitle " * 12, detail="Representative detail " * 100,
                    actions=[dict(id="done", label="Done", risk="low"),
                             dict(id="archive", label="Archive", risk="medium", confirm="Archive this task?")])))
                save(view, prefix + "-sheet")
                item.setProperty("itemJson", "")
                for name, state in [
                    ("empty", dict(configured=True, sections=[dict(id="widgets", status="ok", as_of="09:00",
                        screen=dict(cols=2, rows=1, cells=[cell("metrics", 0, 0, metrics=dict(rows=[])),
                            dict(cell("list", 1, 0), problem=True, note="unavailable: synthetic source")], banners=[]))])),
                    ("offline", dict(configured=True, sections=[dict(id="widgets", status="error", error="Can't reach the Mac. Today needs the Mac awake.")])),
                    ("unconfigured", dict(configured=False)),
                ]:
                    item.setProperty("stateJson", json.dumps(state))
                    save(view, prefix + "-" + name)
            else:
                image = QImage(800, 480, QImage.Format_RGB32)
                image.fill(QColor("white"))
                painter = QPainter(image)
                painter.setPen(QPen(QColor("black"), 4))
                painter.drawRect(2, 2, 795, 475)
                painter.drawEllipse(300, 140, 200, 200)
                painter.setFont(QFont("sans", 24))
                painter.drawText(30, 70, "Synthetic dashboard 800 × 480")
                painter.end()
                image_path = dest / "source.png"
                image.save(str(image_path))
                item.setProperty("dashboardSource", QUrl.fromLocalFile(str(image_path)).toString())
                item.setProperty("dashboardAvailable", True)
                for rotation in ["auto", "landscape"]:
                    for fit in ["fit", "fill", "stretch"]:
                        config = item.property("appConfig").toVariant()
                        config.update(orientation=rotation, fit_mode=fit)
                        item.setProperty("appConfig", config)
                        save(view, prefix + "-" + rotation + "-" + fit)
            view.hide()
        view.close()
    (dest / "index.json").write_text(json.dumps(captures, indent=2))
    print("Captured", len(captures), "actual QML renders at Paper Pro dimensions")


if __name__ == "__main__":
    main()
