#!/bin/zsh
set -eu
cd "$(dirname "$0")"

DEST="$HOME/Library/Application Support/DJOneHub"
APP_SOURCE="$(pwd)/djonehub/DJOneHub.app"
APP_DEST="$DEST/DJOneHub.app"
AGENTS="$HOME/Library/LaunchAgents"
UID_NUM=$(id -u)

echo "DJOneHub 一键安装"
echo "安装目录：$DEST"
echo

# 1. 验证安装源并停止已有服务
if [[ ! -x "$APP_SOURCE/Contents/MacOS/djonehub" || ! -f "$APP_SOURCE/Contents/Info.plist" ]]; then
  echo "安装失败：DMG 内容不完整，请重新下载并完整打开 DMG。" >&2
  exit 1
fi
launchctl bootout "gui/$UID_NUM/com.jamie.djonehub" >/dev/null 2>&1 || true

# 2. 复制统一 App bundle
rm -rf "$APP_DEST"
mkdir -p "$DEST"
ditto --norsrc --noextattr --noqtn --noacl "$APP_SOURCE" "$APP_DEST"
chmod 755 "$APP_DEST/Contents/MacOS/djonehub" "$APP_DEST/Contents/lib/libusb-1.0.0.dylib"

# 3. 注册单一 LaunchAgent
mkdir -p "$AGENTS" "$HOME/Library/Logs/DJOneHub"
cat > "$AGENTS/com.jamie.djonehub.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.jamie.djonehub</string>
  <key>ProgramArguments</key>
  <array>
    <string>$APP_DEST/Contents/MacOS/djonehub</string>
    <string>-listen</string>
    <string>127.0.0.1:7575</string>
    <string>-web-dir</string>
    <string>$APP_DEST/Contents/Resources/web/dist</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ThrottleInterval</key>
  <integer>5</integer>
  <key>ProcessType</key>
  <string>Interactive</string>
  <key>StandardOutPath</key>
  <string>$HOME/Library/Logs/DJOneHub/launchd.log</string>
  <key>StandardErrorPath</key>
  <string>$HOME/Library/Logs/DJOneHub/launchd.log</string>
  <key>WorkingDirectory</key>
  <string>$APP_DEST/Contents</string>
</dict>
</plist>
EOF

launchctl bootstrap "gui/$UID_NUM" "$AGENTS/com.jamie.djonehub.plist"

# 4. 等待服务就绪并打开管理页面
printf '正在启动 DJOneHub'
ok=0
for i in {1..50}; do
  if curl -fsS --max-time 1 http://127.0.0.1:7575/ >/dev/null 2>&1; then ok=1; break; fi
  sleep 0.2
done
if [ "$ok" = 1 ]; then
  echo "，管理页面已打开。"
  open http://127.0.0.1:7575
else
  echo "，未能就绪，请查看 $HOME/Library/Logs/DJOneHub/launchd.log"
fi
echo "安装完成：开机自动启动；请在管理页面调整设备和通知设置。"
echo
sleep 1
