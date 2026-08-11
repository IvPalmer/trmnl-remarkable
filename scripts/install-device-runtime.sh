#!/bin/sh
set -eu

STAGE=/tmp/trmnl-install
PROJECT=/home/root/trmnl-remarkable
STATE=/home/root/.local/share/trmnl-remarkable
created_xovi=0
installed_appload=0
completed=0
rollback_dir="$STAGE/rollback"
project_backup=""
project_created=0

rollback() {
    [ "$completed" -eq 1 ] && return 0
    if [ "$installed_appload" -eq 1 ]; then
        for item in appload.so qtfb-shim.so qtfb-shim-32bit.so; do
            case "$item" in
                appload.so) destination=/home/root/xovi/extensions.d/appload.so ;;
                *) destination="/home/root/shims/$item" ;;
            esac
            if [ -f "$rollback_dir/$item" ]; then
                cp "$rollback_dir/$item" "$destination"
            else
                rm -f -- "$destination"
            fi
        done
    fi
    if [ "$created_xovi" -eq 1 ]; then
        rm -rf -- /home/root/xovi
    fi
    if [ "$project_created" -eq 1 ]; then
        rm -rf -- "$PROJECT"
        [ -z "$project_backup" ] || mv "$project_backup" "$PROJECT"
    fi
    echo "Installation failed; newly installed runtime files were rolled back." >&2
}
trap rollback EXIT HUP INT TERM

expect_hash() {
    expected="$1"; file="$2"
    actual=$(sha256sum "$file" | awk '{print $1}')
    [ "$actual" = "$expected" ] || { echo "Checksum mismatch: $file" >&2; exit 30; }
}

model=$(tr -d '\000' </proc/device-tree/model 2>/dev/null || true)
machine=$(cat /sys/devices/soc0/machine 2>/dev/null || true)
arch=$(uname -m)
# Match install.sh and the installer's own probe: IMG_VERSION may or may not be
# quoted, and an unquoted value must not read as an unsupported firmware.
os_version=$(sed -n 's/^IMG_VERSION="\{0,1\}\([^" ]*\)"\{0,1\}$/\1/p' /etc/os-release | head -n1)
identity=$machine
[ -n "$identity" ] || identity=$model
case "$identity" in
    "reMarkable Ferrari"|*"Paper Pro"*) device=rmpp ;;
    "reMarkable 2.0") device=rm2 ;;
    "reMarkable 1.0"|"reMarkable Prototype 1") device=rm1 ;;
    *) echo "Not a supported reMarkable: $identity" >&2; exit 31 ;;
esac
# The Paper Pro is 64-bit; the reMarkable 1 and 2 are 32-bit ARM. Each has its
# own XOVI, AppLoad and TRMNL build, selected here.
case "$arch" in
    aarch64) payload=aarch64 ;;
    armv7l|armv6l) payload=arm32 ;;
    *) echo "Unsupported architecture: $arch" >&2; exit 32 ;;
esac
if [ "$device" = rmpp ]; then expected_payload=aarch64; else expected_payload=arm32; fi
[ "$payload" = "$expected_payload" ] || { echo "Architecture $arch does not match the detected device ($identity)" >&2; exit 32; }
# The reMarkable 1 no longer receives releases, so its supported window reaches
# back to the last line it shipped instead of ending with the current one.
case "$device" in
    rm1) case "$os_version" in 3.2[0-7].*) ;; *) echo "AppLoad 0.5.3 is incompatible with OS $os_version on the reMarkable 1 (requires >=3.20,<3.28)" >&2; exit 33;; esac ;;
    *) case "$os_version" in 3.26.*|3.27.*) ;; *) echo "AppLoad 0.5.3 is incompatible with OS $os_version (requires >=3.26,<3.28)" >&2; exit 33;; esac ;;
esac
[ "$(systemctl is-active xochitl)" = active ] || { echo "Xochitl is not active" >&2; exit 34; }

if [ "$payload" = aarch64 ]; then
    xovi_archive_hash=32d64d1262ddc984e3235c7d0340a398fe6d5b3efa6a979865f5977b32630d27
    xovi_so_hash=d4df820c25c634c511de11067279d8310fa4f656dc52bd4540db6beac4ffd446
    rebuilder_hash=6726f561557406f36347e43fc2b44a88deef4fb273d2ece88f48f427dad8800f
    appload_hash=31214cbbe64c8bfe7d99096f077c3009dba8a42ef1a733801aa0ec59c134e7cc
    shim_hash=6df704049aa057ff6374eaaa03a4f4a4d683b7c1ce772920d1a124be74d782c4
    shim32_hash=aa4fb1e6f2edf5ef0137360cac77713a24ab508800301f81c19c579fee3f5031
else
    xovi_archive_hash=9aa00537ad41e9be0c3151992bfc25106465318cf5bb4c41cf59b3ddd4866377
    xovi_so_hash=2878d88da2dcb37dcf5533fa78aeeb5cc933eb8c7e5b9041213a4e135a7fff14
    rebuilder_hash=d96530ecd74bbe15ed1efc686c125db24578379fc0e0706ec70d4c53a856e0aa
    appload_hash=0c5592e48098288fd00b71f72b3eb6821aae0bf17cc3646dd8e95bf19c391810
    shim_hash=4eab5f8f54d5fbaaba86497128b3b5029dc07033ac9dd499a226638cc255e9d2
    shim32_hash=19a9d2c75741113f37f81f7affead40eeb12fa3cc41109b7f41ba154f60799cc
fi
XOVI_ARCHIVE="$STAGE/xovi-$payload.tar.gz"
DEVICE_ARCHIVE="$STAGE/trmnl-remarkable-device-$payload.tar.gz"

expect_hash "$xovi_archive_hash" "$XOVI_ARCHIVE"
source_hash=$(tr -d '[:space:]' <"$STAGE/trmnl-remarkable-device-$payload.sha256")
case "$source_hash" in *[!0-9a-f]*|'') echo "Invalid source checksum manifest" >&2; exit 30;; esac
[ "${#source_hash}" -eq 64 ] || { echo "Invalid source checksum length" >&2; exit 30; }
expect_hash "$source_hash" "$DEVICE_ARCHIVE"
expect_hash "$appload_hash" "$STAGE/appload/appload-$payload.so"
expect_hash "$shim_hash" "$STAGE/appload/qtfb-shim-$payload.so"
expect_hash "$shim32_hash" "$STAGE/appload/qtfb-shim-32bit-$payload.so"

mkdir -p "$STATE/install-backup" "$STATE/upstream-licenses"
{
    echo "timestamp=$(date '+%Y-%m-%dT%H:%M:%S%z')"
    echo "model=$model"
    echo "machine=$machine"
    echo "device=$device"
    echo "os_version=$os_version"
    echo "architecture=$arch"
    echo "xochitl=$(systemctl is-active xochitl)"
    echo "root_mount=$(mount | awk '$3=="/"{print $0}')"
    echo "home_free_kb=$(df -Pk /home/root | awk 'NR==2{print $4}')"
    # Only the Paper Pro has a front light.
    echo "brightness=$(cat /sys/class/backlight/rm_frontlight/brightness 2>/dev/null || echo none)"
} > "$STATE/install-backup/preinstall-state.txt"
chmod 600 "$STATE/install-backup/preinstall-state.txt"

if [ ! -e /home/root/xovi ]; then
    tar -tzf "$XOVI_ARCHIVE" | awk '/^\// || /(^|\/)\.\.($|\/)/ || $0 !~ /^xovi\// { bad=1 } END { exit bad }' || { echo "Unsafe XOVI archive" >&2; exit 35; }
    created_xovi=1
    tar -xzf "$XOVI_ARCHIVE" -C /home/root
fi
if ! { [ -f /home/root/xovi/xovi.so ] && [ -f /home/root/xovi/extensions.d/qt-resource-rebuilder.so ]; }; then
    echo "Compatible XOVI runtime is missing or incomplete" >&2
    exit 36
fi
expect_hash "$xovi_so_hash" /home/root/xovi/xovi.so
expect_hash "$rebuilder_hash" /home/root/xovi/extensions.d/qt-resource-rebuilder.so

mkdir -p /home/root/xovi/exthome/appload /home/root/shims
mkdir -p "$rollback_dir"
[ ! -f /home/root/xovi/extensions.d/appload.so ] || cp /home/root/xovi/extensions.d/appload.so "$rollback_dir/appload.so"
[ ! -f /home/root/shims/qtfb-shim.so ] || cp /home/root/shims/qtfb-shim.so "$rollback_dir/qtfb-shim.so"
[ ! -f /home/root/shims/qtfb-shim-32bit.so ] || cp /home/root/shims/qtfb-shim-32bit.so "$rollback_dir/qtfb-shim-32bit.so"
installed_appload=1
cp "$STAGE/appload/appload-$payload.so" /home/root/xovi/extensions.d/appload.so
cp "$STAGE/appload/qtfb-shim-$payload.so" /home/root/shims/qtfb-shim.so
cp "$STAGE/appload/qtfb-shim-32bit-$payload.so" /home/root/shims/qtfb-shim-32bit.so
chmod 644 /home/root/xovi/extensions.d/appload.so /home/root/shims/qtfb-shim.so /home/root/shims/qtfb-shim-32bit.so
cp "$STAGE/licenses/XOVI-LICENSE" "$STATE/upstream-licenses/XOVI-LICENSE"
cp "$STAGE/licenses/APPLOAD-LICENSE" "$STATE/upstream-licenses/APPLOAD-LICENSE"
cp "$STAGE/licenses/EXTENSIONS-LICENSE" "$STATE/upstream-licenses/EXTENSIONS-LICENSE"
printf '%s\n' 'xovi_appload_runtime=installed-by-trmnl-remarkable' > "$STATE/install-backup/runtime-owned"
chmod 600 "$STATE/install-backup/runtime-owned"

tar -tzf "$DEVICE_ARCHIVE" | awk '/^\// || /(^|\/)\.\.($|\/)/ { bad=1 } END { exit bad }' || { echo "Unsafe TRMNL archive" >&2; exit 37; }
if [ -e "$PROJECT" ]; then
    project_backup="$STATE/install-backup/source.$(date '+%Y%m%d-%H%M%S')"
    mv "$PROJECT" "$project_backup"
fi
mkdir -p "$PROJECT"
project_created=1
tar -xzf "$DEVICE_ARCHIVE" -C "$PROJECT"
chmod 755 "$PROJECT"/*.sh "$PROJECT"/dist/trmnl-remarkable-app/scripts/brightness_guard.sh "$PROJECT"/dist/trmnl-remarkable-app/backend/entry
chmod 755 "$PROJECT"/scripts/*.sh 2>/dev/null || true
sh "$PROJECT/install.sh"

completed=1
trap - EXIT HUP INT TERM

# The rollback copy is no longer needed. Timestamped names sort chronologically,
# so dropping the leading entries keeps the newest and stops repeated reinstalls
# from filling /home/root.
prune_oldest() {
    keep=$1
    shift
    while [ "$#" -gt "$keep" ]; do
        if [ -e "$1" ]; then rm -rf -- "$1"; fi
        shift
    done
}
prune_oldest 1 "$STATE/install-backup"/source.*

echo "RUNTIME_INSTALL_OK"
echo "model=$model machine=$machine device=$device os=$os_version arch=$arch payload=$payload"
echo "xovi=0.3.3 extensions=19.0.0 appload=0.5.3"
