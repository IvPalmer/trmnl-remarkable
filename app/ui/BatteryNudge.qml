import QtQuick 2.5
import com.remarkable

// remarkable-ai: the only place TRMNL reaches the system's power manager.
// `com.remarkable` is registered by the reMarkable software itself and can be
// imported only inside it, so charging mode (ChargingMode.qml) is given this
// file through a Loader: where the module is missing or different, the Loader
// fails, charging mode does nothing and the rest of the app is untouched.
QtObject {
    // The display is showing the page, not the sleep screen. A nudge while it
    // sleeps would wake it, so charging mode asks first.
    readonly property bool awake: BatteryManager.displayState === BatteryManager.Normal

    // Reports user activity, which restarts the system's idle timer. Nothing is
    // changed or saved: the timer's setting stays as the user left it.
    function nudge() {
        try {
            BatteryManager.onActivity()
        } catch (e) {
            console.warn("TRMNL charging mode: reporting activity failed: " + e)
        }
    }
}
