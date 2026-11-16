package screen

import (
	"github.com/hexadecimil/offstage/internal/config"
	"github.com/hexadecimil/offstage/internal/hypr"
)

// Ctx bundles what every operation needs: the configuration and, when one
// runs, the Hyprland instance with its configuration driver. A screen
// itself needs no Hyprland: cage brings its own output. Hyprland is used to
// place the mirror window, by doctor, and to tear down screens made by a
// offstage before 0.3, whose outputs were Hyprland's.
type Ctx struct {
	Cfg    config.Config
	Hypr   *hypr.Instance
	Driver hypr.ConfigDriver
}

// Connect discovers Hyprland when there is one and picks its configuration
// driver. Without Hyprland the context still works, mirror placement aside.
func Connect(cfg config.Config) (*Ctx, error) {
	c := &Ctx{Cfg: cfg}
	if inst, err := hypr.Discover(); err == nil {
		c.Hypr, c.Driver = inst, inst.Driver()
	}
	return c, nil
}
