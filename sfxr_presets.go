package main

import (
	"math"
	"math/rand"
)

// The seven generators sfxr shipped with, plus randomise and mutate. These are
// ports of DrPetter's originals: each one is a recipe that scatters a handful
// of parameters inside ranges chosen to land on a recognisable game sound, so
// the result is a usable starting point rather than noise.

// sfxRand wraps the random helpers the recipes are written in terms of: frnd(n)
// is a float in [0,n), and chance(n) is sfxr's rnd(n)==0, a one-in-(n+1) shot.
type sfxRand struct{ r *rand.Rand }

func newSfxRand() sfxRand { return sfxRand{rand.New(rand.NewSource(rand.Int63()))} }

func (g sfxRand) frnd(n float32) float32 { return g.r.Float32() * n }

// coin is sfxr's rnd(1): a fair yes or no.
func (g sfxRand) coin() bool { return g.r.Intn(2) == 1 }

// oneIn reports a 1-in-(n+1) chance, matching sfxr's rnd(n)==0.
func (g sfxRand) oneIn(n int) bool { return g.r.Intn(n+1) == 0 }

// sfxPreset is one entry in the generator list.
type sfxPreset struct {
	name string
	gen  func(g sfxRand) sfxParams
}

var sfxPresets = []sfxPreset{
	{"Pickup / Coin", sfxPickupCoin},
	{"Laser / Shoot", sfxLaserShoot},
	{"Explosion", sfxExplosion},
	{"Powerup", sfxPowerup},
	{"Hit / Hurt", sfxHitHurt},
	{"Jump", sfxJump},
	{"Blip / Select", sfxBlipSelect},
	{"Randomize", sfxRandomize},
}

func sfxPickupCoin(g sfxRand) sfxParams {
	p := sfxDefaults()
	p.Freq = 0.4 + g.frnd(0.5)
	p.Attack = 0
	p.Sustain = g.frnd(0.1)
	p.Decay = 0.1 + g.frnd(0.4)
	p.Punch = 0.3 + g.frnd(0.3)
	if g.coin() {
		p.ArpSpeed = 0.5 + g.frnd(0.2)
		p.ArpMod = 0.2 + g.frnd(0.4)
	}
	return p
}

func sfxLaserShoot(g sfxRand) sfxParams {
	p := sfxDefaults()
	p.Wave = sfxWave(g.r.Intn(3))
	if p.Wave == sfxSine && g.coin() {
		p.Wave = sfxWave(g.r.Intn(2))
	}
	p.Freq = 0.5 + g.frnd(0.5)
	p.FreqLimit = p.Freq - 0.2 - g.frnd(0.6)
	if p.FreqLimit < 0.2 {
		p.FreqLimit = 0.2
	}
	p.FreqRamp = -0.15 - g.frnd(0.2)
	if g.oneIn(2) {
		p.Freq = 0.3 + g.frnd(0.6)
		p.FreqLimit = g.frnd(0.1)
		p.FreqRamp = -0.35 - g.frnd(0.3)
	}
	if g.coin() {
		p.Duty = g.frnd(0.5)
		p.DutyRamp = g.frnd(0.2)
	} else {
		p.Duty = 0.4 + g.frnd(0.5)
		p.DutyRamp = -g.frnd(0.7)
	}
	p.Attack = 0
	p.Sustain = 0.1 + g.frnd(0.2)
	p.Decay = g.frnd(0.4)
	if g.coin() {
		p.Punch = g.frnd(0.3)
	}
	if g.oneIn(2) {
		p.PhaserOffset = g.frnd(0.2)
		p.PhaserRamp = -g.frnd(0.2)
	}
	if g.coin() {
		p.HpfFreq = g.frnd(0.3)
	}
	return p
}

func sfxExplosion(g sfxRand) sfxParams {
	p := sfxDefaults()
	p.Wave = sfxNoise
	if g.coin() {
		p.Freq = 0.1 + g.frnd(0.4)
		p.FreqRamp = -0.1 + g.frnd(0.4)
	} else {
		p.Freq = 0.2 + g.frnd(0.7)
		p.FreqRamp = -0.2 - g.frnd(0.2)
	}
	p.Freq *= p.Freq
	if g.oneIn(4) {
		p.FreqRamp = 0
	}
	if g.oneIn(2) {
		p.RepeatSpeed = 0.3 + g.frnd(0.5)
	}
	p.Attack = 0
	p.Sustain = 0.1 + g.frnd(0.3)
	p.Decay = g.frnd(0.5)
	if g.coin() {
		p.PhaserOffset = -0.3 + g.frnd(0.9)
		p.PhaserRamp = -g.frnd(0.3)
	}
	p.Punch = 0.2 + g.frnd(0.6)
	if g.coin() {
		p.Vibrato = g.frnd(0.7)
		p.VibratoSpeed = g.frnd(0.6)
	}
	if g.oneIn(2) {
		p.ArpSpeed = 0.6 + g.frnd(0.3)
		p.ArpMod = 0.8 - g.frnd(1.6)
	}
	return p
}

func sfxPowerup(g sfxRand) sfxParams {
	p := sfxDefaults()
	if g.coin() {
		p.Wave = sfxSawtooth
	} else {
		p.Duty = g.frnd(0.6)
	}
	if g.coin() {
		p.Freq = 0.2 + g.frnd(0.3)
		p.FreqRamp = 0.1 + g.frnd(0.4)
		p.RepeatSpeed = 0.4 + g.frnd(0.4)
	} else {
		p.Freq = 0.2 + g.frnd(0.3)
		p.FreqRamp = 0.05 + g.frnd(0.2)
		if g.coin() {
			p.Vibrato = g.frnd(0.7)
			p.VibratoSpeed = g.frnd(0.6)
		}
	}
	p.Attack = 0
	p.Sustain = g.frnd(0.4)
	p.Decay = 0.1 + g.frnd(0.4)
	return p
}

func sfxHitHurt(g sfxRand) sfxParams {
	p := sfxDefaults()
	p.Wave = sfxWave(g.r.Intn(3))
	if p.Wave == sfxSine {
		p.Wave = sfxNoise
	}
	if p.Wave == sfxSquare {
		p.Duty = g.frnd(0.6)
	}
	p.Freq = 0.2 + g.frnd(0.6)
	p.FreqRamp = -0.3 - g.frnd(0.4)
	p.Attack = 0
	p.Sustain = g.frnd(0.1)
	p.Decay = 0.1 + g.frnd(0.2)
	if g.coin() {
		p.HpfFreq = g.frnd(0.3)
	}
	return p
}

func sfxJump(g sfxRand) sfxParams {
	p := sfxDefaults()
	p.Wave = sfxSquare
	p.Duty = g.frnd(0.6)
	p.Freq = 0.3 + g.frnd(0.3)
	p.FreqRamp = 0.1 + g.frnd(0.2)
	p.Attack = 0
	p.Sustain = 0.1 + g.frnd(0.3)
	p.Decay = 0.1 + g.frnd(0.2)
	if g.coin() {
		p.HpfFreq = g.frnd(0.3)
	}
	if g.coin() {
		p.LpfFreq = 1 - g.frnd(0.6)
	}
	return p
}

func sfxBlipSelect(g sfxRand) sfxParams {
	p := sfxDefaults()
	p.Wave = sfxWave(g.r.Intn(2))
	if p.Wave == sfxSquare {
		p.Duty = g.frnd(0.6)
	}
	p.Freq = 0.2 + g.frnd(0.4)
	p.Attack = 0
	p.Sustain = 0.1 + g.frnd(0.1)
	p.Decay = g.frnd(0.2)
	p.HpfFreq = 0.1
	return p
}

// sfxRandomize is sfxr's "Randomize" button: every parameter scattered, with a
// few corrections that stop the common ways of producing silence.
func sfxRandomize(g sfxRand) sfxParams {
	p := sfxDefaults()
	pow := func(v float32, e float64) float32 {
		return float32(math.Pow(float64(v), e))
	}
	sym := func() float32 { return g.frnd(2) - 1 } // [-1,1)

	p.Wave = sfxWave(g.r.Intn(len(sfxWaveNames)))

	p.Freq = pow(sym(), 2)
	if g.coin() {
		p.Freq = pow(sym(), 3) + 0.5
	}
	p.FreqLimit = 0
	p.FreqRamp = pow(sym(), 5)
	if p.Freq > 0.7 && p.FreqRamp > 0.2 {
		p.FreqRamp = -p.FreqRamp
	}
	if p.Freq < 0.2 && p.FreqRamp < -0.05 {
		p.FreqRamp = -p.FreqRamp
	}
	p.FreqDramp = pow(sym(), 3)

	p.Duty = sym()
	p.DutyRamp = pow(sym(), 3)

	p.Vibrato = pow(sym(), 3)
	p.VibratoSpeed = sym()

	p.Attack = pow(sym(), 3)
	p.Sustain = pow(sym(), 2)
	p.Decay = sym()
	p.Punch = pow(g.frnd(0.8), 2)
	// Too short to hear: lengthen it rather than hand back a click.
	if p.Attack+p.Sustain+p.Decay < 0.2 {
		p.Sustain += 0.2 + g.frnd(0.3)
		p.Decay += 0.2 + g.frnd(0.3)
	}

	p.LpfResonance = sym()
	p.LpfFreq = 1 - pow(g.frnd(1), 3)
	p.LpfRamp = pow(sym(), 3)
	if p.LpfFreq < 0.1 && p.LpfRamp < -0.05 {
		p.LpfRamp = -p.LpfRamp
	}
	p.HpfFreq = pow(g.frnd(1), 5)
	p.HpfRamp = pow(sym(), 5)

	p.PhaserOffset = pow(sym(), 3)
	p.PhaserRamp = pow(sym(), 3)

	p.RepeatSpeed = sym()
	p.ArpSpeed = sym()
	p.ArpMod = sym()

	p.clampToSpecs()
	return p
}

// sfxMutate nudges roughly half the parameters by a small amount, which is how
// sfxr let you walk from a sound you almost liked to one you did.
func sfxMutate(g sfxRand, p sfxParams) sfxParams {
	out := p
	for _, spec := range sfxParamSpecs {
		if g.coin() {
			v := spec.get(&out)
			*v += g.frnd(0.1) - 0.05
		}
	}
	out.clampToSpecs()
	return out
}
