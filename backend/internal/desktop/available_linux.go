//go:build linux && !android

package desktop

import "github.com/godbus/dbus/v5"

func available() bool {
	connection, err := dbus.ConnectSessionBus()
	if err != nil {
		return false
	}
	defer connection.Close()
	var watcher bool
	err = connection.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.kde.StatusNotifierWatcher").Store(&watcher)
	return err == nil && watcher
}
