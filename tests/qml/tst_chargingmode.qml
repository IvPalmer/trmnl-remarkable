import QtQuick 2.5
import QtTest 1.2
import "../../app/ui"

// Charging mode keeps the tablet awake and Today fresh while it is plugged in
// and TRMNL is in front. ChargingMode is only the rules: it is handed what is
// known (the charger reading, whether the app is in front, whether the display
// is awake, whether Today is showing and busy) and emits `nudge` and
// `refreshToday`. TRMNL.qml cannot be loaded here (the AppLoad plugin is a
// stub, and the system's power manager exists only on the tablet), so the one
// call that touches that manager stays in BatteryNudge.qml and the rules are
// tested through their signals, with a clock the test owns.
//
// CI runs it (the "QML lint and resources" step). From the repository root:
//   QT_QPA_PLATFORM=offscreen qmltestrunner -input tests/qml
TestCase {
    id: tc
    name: "ChargingMode"
    when: windowShown
    visible: true
    width: 400; height: 400

    // The test's clock, in Unix milliseconds. The component reads it through
    // its `clock` property, so a reading's age never depends on real time.
    property real fakeNow: 1800000000000

    // Short intervals so the real Timers can be waited for: a "heartbeat" every
    // 20 ms and a Today refetch every 60 ms stand in for 60 s and 5 min.
    readonly property int beat: 20
    readonly property int today: 60

    Component { id: modeComponent; ChargingMode {} }
    Component { id: spyComponent; SignalSpy {} }

    function make(overrides) {
        var props = {
            nudgeInterval: tc.beat, todayInterval: tc.today, maxReadingAge: 120000,
            clock: function() { return tc.fakeNow },
            chargerOnline: true, chargerReadAt: tc.fakeNow - 1000,
            appActive: true, displayAwake: true, todayShowing: false, todayRefreshing: false
        }
        for (var k in overrides) props[k] = overrides[k]
        return createTemporaryObject(modeComponent, tc, props)
    }
    function spy(mode, signalName) {
        return createTemporaryObject(spyComponent, tc, {target: mode, signalName: signalName})
    }
    // Waits a few intervals and reports whether the spy heard anything.
    function stayedQuiet(s, ms) {
        s.clear()
        wait(ms)
        return s.count === 0
    }
    // Resets the clock between tests.
    function init() { tc.fakeNow = 1800000000000 }

    // --- the heartbeat -----------------------------------------------------

    function test_nudges_every_beat_while_charging_in_front_and_awake() {
        var mode = make({})
        var nudges = spy(mode, "nudge")
        tryVerify(function() { return nudges.count >= 3 }, 2000)
        // One per beat, not a burst: 3 beats cannot have produced a dozen.
        var t0 = Date.now(), n0 = nudges.count
        wait(5 * tc.beat)
        var elapsed = Date.now() - t0
        var made = nudges.count - n0
        verify(made >= 1, "no nudge in five beats")
        verify(made <= elapsed / tc.beat + 1, made + " nudges in " + elapsed + " ms")
    }

    function test_no_nudge_when_a_condition_is_missing(data) {
        var mode = make(data.props)
        var nudges = spy(mode, "nudge")
        verify(stayedQuiet(nudges, 6 * tc.beat), data.tag + " still nudged " + nudges.count + " times")
    }
    function test_no_nudge_when_a_condition_is_missing_data() {
        return [
            {tag: "charger offline", props: {chargerOnline: false}},
            {tag: "no reading yet", props: {chargerReadAt: 0}},
            {tag: "stale reading", props: {chargerReadAt: 1800000000000 - 120001}},
            {tag: "TRMNL not in front", props: {appActive: false}},
            {tag: "display asleep", props: {displayAwake: false}},
            {tag: "reading from the future (clock stepped back)", props: {chargerReadAt: 1800000000000 + 120001}}
        ]
    }

    function test_a_reading_exactly_at_the_limit_still_counts() {
        var mode = make({chargerReadAt: tc.fakeNow - 120000})
        var nudges = spy(mode, "nudge")
        tryVerify(function() { return nudges.count >= 1 }, 2000)
    }

    // After any condition fails, the very next beat must not nudge.
    function test_stops_at_once_when_a_condition_fails_data() {
        return [
            {tag: "unplugged", change: function(m) { m.chargerOnline = false }},
            {tag: "app left the front", change: function(m) { m.appActive = false }},
            {tag: "display fell asleep", change: function(m) { m.displayAwake = false }},
            {tag: "reading went stale", change: function(m) { tc.fakeNow += 120001 }}
        ]
    }
    function test_stops_at_once_when_a_condition_fails(data) {
        var mode = make({})
        var nudges = spy(mode, "nudge")
        tryVerify(function() { return nudges.count >= 2 }, 2000)
        data.change(mode)
        verify(stayedQuiet(nudges, 6 * tc.beat), data.tag + ": nudged " + nudges.count + " more times")
    }

    function test_starts_again_when_the_conditions_return() {
        var mode = make({chargerOnline: false})
        var nudges = spy(mode, "nudge")
        verify(stayedQuiet(nudges, 4 * tc.beat))
        mode.chargerOnline = true
        tryVerify(function() { return nudges.count >= 2 }, 2000)
        mode.displayAwake = false
        verify(stayedQuiet(nudges, 4 * tc.beat), "nudged while asleep")
        mode.displayAwake = true
        tryVerify(function() { return nudges.count >= 1 }, 2000)
    }

    // A fresh reading keeps it going: the age is judged against the newest one.
    function test_a_newer_reading_revives_a_stale_one() {
        var mode = make({chargerReadAt: tc.fakeNow - 500000})
        var nudges = spy(mode, "nudge")
        verify(stayedQuiet(nudges, 4 * tc.beat))
        mode.chargerReadAt = tc.fakeNow - 10
        tryVerify(function() { return nudges.count >= 1 }, 2000)
    }

    // --- Today's refetch ---------------------------------------------------

    function test_today_refetches_each_interval_while_charging_and_showing() {
        var mode = make({todayShowing: true})
        var refreshes = spy(mode, "refreshToday")
        tryVerify(function() { return refreshes.count >= 3 }, 3000)
    }

    function test_today_does_not_refetch_while_the_dashboard_is_showing() {
        var mode = make({todayShowing: false})
        var nudges = spy(mode, "nudge")
        var refreshes = spy(mode, "refreshToday")
        tryVerify(function() { return nudges.count >= 1 }, 2000)   // charging mode is on...
        verify(stayedQuiet(refreshes, 5 * tc.today), "refetched Today with the dashboard showing")
    }

    function test_today_does_not_refetch_while_one_is_in_flight() {
        var mode = make({todayShowing: true, todayRefreshing: true})
        var refreshes = spy(mode, "refreshToday")
        verify(stayedQuiet(refreshes, 5 * tc.today), "refetched while a fetch was running")
        // The skipped beats are dropped, not queued: nothing arrives in a burst.
        mode.todayRefreshing = false
        refreshes.clear()
        tryVerify(function() { return refreshes.count >= 1 }, 2000)
        wait(tc.today / 4)
        verify(refreshes.count <= 2, "a queue of skipped refetches was released: " + refreshes.count)
    }

    function test_today_refetch_follows_the_same_gates_as_the_heartbeat_data() {
        return [
            {tag: "charger offline", props: {chargerOnline: false}},
            {tag: "stale reading", props: {chargerReadAt: 1800000000000 - 120001}},
            {tag: "TRMNL not in front", props: {appActive: false}},
            {tag: "display asleep", props: {displayAwake: false}}
        ]
    }
    function test_today_refetch_follows_the_same_gates_as_the_heartbeat(data) {
        var props = data.props
        props.todayShowing = true
        var mode = make(props)
        var refreshes = spy(mode, "refreshToday")
        verify(stayedQuiet(refreshes, 5 * tc.today), data.tag + ": refetched " + refreshes.count + " times")
    }

    function test_today_refetch_stops_when_today_closes_or_the_charger_goes() {
        var mode = make({todayShowing: true})
        var refreshes = spy(mode, "refreshToday")
        tryVerify(function() { return refreshes.count >= 2 }, 3000)
        mode.todayShowing = false
        verify(stayedQuiet(refreshes, 4 * tc.today), "refetched after Today closed")
        mode.todayShowing = true
        tryVerify(function() { return refreshes.count >= 1 }, 3000)
        mode.chargerOnline = false
        verify(stayedQuiet(refreshes, 4 * tc.today), "refetched after unplugging")
    }

    // The real intervals are what the issue asks for.
    function test_default_intervals() {
        var mode = createTemporaryObject(modeComponent, tc, {})
        compare(mode.nudgeInterval, 60000)
        compare(mode.todayInterval, 300000)
        compare(mode.maxReadingAge, 120000)
    }
}
