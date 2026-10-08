pragma ComponentBehavior: Bound
import QtQuick 2.5

// remarkable-ai: charging mode. While the tablet is plugged in and TRMNL is in
// front, the system's idle timer would still draw the sleep screen after the
// usual delay. This item tells the system, once a minute, that someone is
// there (`nudge`), and has Today refetch every five minutes (`refreshToday`) so
// its "as of" time keeps moving.
//
// It holds only the rules. Everything it needs is handed in, and the one call
// that reaches the system's power manager is not here: the owner connects
// `nudge` to it (BatteryNudge.qml). That keeps the rules testable off the
// tablet, and nothing here is saved anywhere. When the charger goes, the app
// leaves the front, the display sleeps or the app is killed, the nudges just
// stop and the system's own timer runs out from the last one.
Item {
    id: mode

    // The newest charger reading from the backend: whether the charger is
    // online, and when it was read (Unix milliseconds, 0 for none yet).
    property bool chargerOnline: false
    property real chargerReadAt: 0
    // TRMNL is the screen in front (dashboard, Today or one of its panels).
    property bool appActive: false
    // The display is awake, never the sleep screen. A nudge must not wake it.
    property bool displayAwake: false
    // Today is showing, and a fetch of it is running.
    property bool todayShowing: false
    property bool todayRefreshing: false

    property int nudgeInterval: 60 * 1000
    property int todayInterval: 5 * 60 * 1000
    // A reading older than this counts as none, so a backend that died while
    // reporting "online" cannot keep the tablet awake. A reading dated ahead of
    // the clock by as much is distrusted too (the clock was stepped back).
    property int maxReadingAge: 2 * 60 * 1000
    // Replaced by tests.
    property var clock: function() { return Date.now() }

    signal nudge()
    signal refreshToday()

    // The charger is online by a reading that is still fresh.
    function charging() {
        if (!mode.chargerOnline || mode.chargerReadAt <= 0) return false
        return Math.abs(mode.clock() - mode.chargerReadAt) <= mode.maxReadingAge
    }
    // Everything charging mode needs, checked again at each beat, which is where
    // an aging reading is noticed.
    function holding() { return mode.charging() && mode.appActive && mode.displayAwake }

    // The timers run only while the cheap conditions hold, so on battery this
    // item wakes nothing. They re-check everything when they fire.
    readonly property bool armed: mode.chargerOnline && mode.appActive && mode.displayAwake

    Timer {
        interval: mode.nudgeInterval
        repeat: true
        running: mode.armed
        onTriggered: if (mode.holding()) mode.nudge()
    }

    // A fetch still running is left alone: the engine would ignore the request
    // anyway, and a skipped beat is dropped, not queued.
    Timer {
        interval: mode.todayInterval
        repeat: true
        running: mode.armed && mode.todayShowing
        onTriggered: if (mode.holding() && mode.todayShowing && !mode.todayRefreshing) mode.refreshToday()
    }
}
