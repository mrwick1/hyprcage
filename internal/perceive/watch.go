package perceive

import "image"

// noiseFloor is the changed-pixel ratio above which a frame counts as changed.
const noiseFloor = 0.0001

// WatchResult is the outcome of sampling a screen.
type WatchResult struct {
	Verdict string
	Ratios  []float64     // change of frame i+1 against frame i
	Frames  []*image.RGBA // every sampled frame
}

// Watch takes n frames with capture, calling wait between two captures, and
// returns a verdict: jitter when more than half of the diffs exceed the noise
// floor, frozen when nothing changed and expectChange is set, else stable.
func Watch(capture func() (*image.RGBA, error), n int, wait func(), expectChange bool) (WatchResult, error) {
	var res WatchResult
	for i := 0; i < n; i++ {
		if i > 0 {
			wait()
		}
		f, err := capture()
		if err != nil {
			return res, err
		}
		if i > 0 {
			res.Ratios = append(res.Ratios, Compare(res.Frames[i-1], f).Ratio)
		}
		res.Frames = append(res.Frames, f)
	}
	changed := 0
	for _, r := range res.Ratios {
		if r > noiseFloor {
			changed++
		}
	}
	switch {
	case changed*2 > len(res.Ratios):
		res.Verdict = "jitter"
	case changed == 0 && expectChange:
		res.Verdict = "frozen"
	default:
		res.Verdict = "stable"
	}
	return res, nil
}
