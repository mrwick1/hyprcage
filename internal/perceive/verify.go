package perceive

import (
	"fmt"
	"image"
)

// Verdict says whether an action did what its target implies.
type Verdict string

const (
	NoOp      Verdict = "NO_OP"     // the screen did not change beyond the noise floor
	Confirmed Verdict = "CONFIRMED" // the changed area holds every target point
	Partial   Verdict = "PARTIAL"   // the screen changed, and part of the target did not
	Diverged  Verdict = "DIVERGED"  // the screen changed far from the target
)

const (
	// NoiseFloor is the change ratio below which an action counts as a no-op.
	NoiseFloor = 0.0001
	// nearMargin is how far (pixels) a change may sit from a target point and
	// still count as part of that target.
	nearMargin = 48
)

// Sense is the machine-readable summary that follows a verdict. Opened is set
// by the caller, which owns the window list. Modal is always false: a Wayland
// toplevel list carries no dialog role, so hyprcage cannot tell a dialog.
type Sense struct {
	Changed bool `json:"changed"`
	Opened  bool `json:"opened"`
	Modal   bool `json:"modal"`
	NoOp    bool `json:"no_op"`
}

// Verification is the verdict on one action. It is the value that other
// features (the audit log) attach to an action record.
type Verification struct {
	Verdict Verdict   `json:"verdict"`
	Diff    FrameDiff `json:"-"`
	Sense   Sense     `json:"sense"`
}

// NewVerification judges diff against the screen points the action aimed at.
// With no point (a key press or typed text) any change above the noise floor
// confirms.
func NewVerification(d FrameDiff, points ...image.Point) Verification {
	v := Verification{Diff: d}
	switch {
	case d.Ratio < NoiseFloor:
		v.Verdict = NoOp
	case len(points) == 0:
		v.Verdict = Confirmed
	default:
		in, near := 0, 0
		for _, p := range points {
			switch {
			case p.In(d.BBox):
				in++
			case p.In(d.BBox.Inset(-nearMargin)):
				near++
			}
		}
		switch {
		case in == len(points):
			v.Verdict = Confirmed
		case in+near > 0:
			v.Verdict = Partial
		default:
			v.Verdict = Diverged
		}
	}
	v.Sense = Sense{Changed: v.Verdict != NoOp, NoOp: v.Verdict == NoOp}
	return v
}

// String renders the result line.
func (v Verification) String() string {
	if v.Verdict == NoOp {
		return fmt.Sprintf("verify: %s (changed %.1f%%)", v.Verdict, v.Diff.Ratio*100)
	}
	b := v.Diff.BBox
	return fmt.Sprintf("verify: %s (changed %.1f%%, bbox %d,%d %dx%d)", v.Verdict, v.Diff.Ratio*100, b.Min.X, b.Min.Y, b.Dx(), b.Dy())
}
