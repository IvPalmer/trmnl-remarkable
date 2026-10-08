pragma Singleton
import QtQuick 2.5

// Stand-in for the part of the reMarkable system's power manager that
// BatteryNudge.qml uses, so qmllint can resolve `import com.remarkable`. The
// real one is registered by xochitl and exists only on the tablet. The names
// are the ones xochitl's own QML uses (displayState with Normal, LightSleep and
// DeepSleep), plus onActivity.
QtObject {
    enum DisplayState { Normal, LightSleep, DeepSleep }
    property int displayState: BatteryManager.Normal
    function onActivity() {}
}
