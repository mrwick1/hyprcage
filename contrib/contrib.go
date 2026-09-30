// Package contrib embeds the files that hyprcage setup installs.
package contrib

import _ "embed"

// NotifydUnit is the systemd user unit of hyprcage notifyd.
//
//go:embed hyprcage-notifyd.service
var NotifydUnit []byte
