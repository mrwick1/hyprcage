// Package contrib embeds the files that offstage setup installs.
package contrib

import _ "embed"

// NotifydUnit is the systemd user unit of offstage notifyd.
//
//go:embed offstage-notifyd.service
var NotifydUnit []byte
