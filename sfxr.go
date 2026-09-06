package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
)

// A port of DrPetter's sfxr synthesiser (2007, MIT), the engine behind sfxr,
// bfxr, sfxr-qt and raylib's own rFXGen. The parameter set and the arithmetic
// below are kept faithful to the original so that a sound designed in any of
// those tools sounds the same here; only the serialisation is ours, since sfxr
// wrote a private binary format and we want something a human can diff.

const (
	sfxSampleRate = 44100
	sfxBitDepth   = 16
	sfxChannels   = 1

	// The original mixes at 8x the output rate and averages, which is what
	// keeps the square and sawtooth edges from aliasing into a whine.
	sfxSuperSample = 8

	// sfxr's fixed output trim. Its sliders are calibrated against this, so a
	// sound at volume 0.5 is as loud here as it was there.
	sfxMasterVol = 0.05

	// A runaway envelope (long attack, long sustain, long decay) can ask for
	// minutes of audio. sfxr had the same hazard and no guard; this caps a
	// render at something a game would plausibly use.
	sfxMaxSeconds = 30
)

// sfxWave is the oscillator shape. These are the four sfxr had; the numbering
// is part of the file format, so new shapes must be appended, never inserted.
type sfxWave int32

const (
	sfxSquare sfxWave = iota
	sfxSawtooth
	sfxSine
	sfxNoise
)

var sfxWaveNames = [...]string{"Square", "Sawtooth", "Sine", "Noise"}

func (w sfxWave) String() string {
	if int(w) < len(sfxWaveNames) {
		return sfxWaveNames[w]
	}
	return "Square"
}

// sfxParams is one sound. The field names are sfxr's, spelled out; the JSON
// names are what a person would type, and the whole struct is the file format.
//
// Ranges are not uniform: some parameters are one-sided (a duration cannot be
// negative) and some are signed sweeps. sfxParamSpecs below is the authority,
// and the UI and the randomiser both read their bounds from it.
type sfxParams struct {
	Wave sfxWave `json:"wave"`

	// Envelope, in seconds-ish units the original squares before use.
	Attack  float32 `json:"attack"`
	Sustain float32 `json:"sustain"`
	Punch   float32 `json:"punch"`
	Decay   float32 `json:"decay"`

	// Pitch, and where it slides to.
	Freq      float32 `json:"freq"`
	FreqLimit float32 `json:"freqLimit"`
	FreqRamp  float32 `json:"freqRamp"`
	FreqDramp float32 `json:"freqDramp"`

	Vibrato      float32 `json:"vibrato"`
	VibratoSpeed float32 `json:"vibratoSpeed"`

	// Arpeggio: a single pitch jump partway through.
	ArpMod   float32 `json:"arpMod"`
	ArpSpeed float32 `json:"arpSpeed"`

	// Square duty cycle; ignored by the other three shapes.
	Duty     float32 `json:"duty"`
	DutyRamp float32 `json:"dutyRamp"`

	RepeatSpeed float32 `json:"repeatSpeed"`

	PhaserOffset float32 `json:"phaserOffset"`
	PhaserRamp   float32 `json:"phaserRamp"`

	LpfFreq      float32 `json:"lpfFreq"`
	LpfRamp      float32 `json:"lpfRamp"`
	LpfResonance float32 `json:"lpfResonance"`
	HpfFreq      float32 `json:"hpfFreq"`
	HpfRamp      float32 `json:"hpfRamp"`

	Volume float32 `json:"volume"`
}

// sfxDefaults is sfxr's blank sound: a plain tone that is audible when played,
// so a fresh editor is not silent.
func sfxDefaults() sfxParams {
	return sfxParams{
		Wave:    sfxSquare,
		Freq:    0.3,
		Sustain: 0.3,
		Decay:   0.4,
		LpfFreq: 1,
		Volume:  0.5,
	}
}

// ── parameter table ───────────────────────────────────────────────────────────

// sfxParamSpec describes one slider: where it lives in the struct, what it is
// called, and what it may hold. Keeping this as data means the UI, the
// randomiser and the mutator all agree about bounds without repeating them.
type sfxParamSpec struct {
	group string // section heading; empty continues the previous section
	label string
	get   func(*sfxParams) *float32
	min   float32
	max   float32
}

var sfxParamSpecs = []sfxParamSpec{
	{"ENVELOPE", "Attack", func(p *sfxParams) *float32 { return &p.Attack }, 0, 1},
	{"", "Sustain", func(p *sfxParams) *float32 { return &p.Sustain }, 0, 1},
	{"", "Punch", func(p *sfxParams) *float32 { return &p.Punch }, 0, 1},
	{"", "Decay", func(p *sfxParams) *float32 { return &p.Decay }, 0, 1},

	{"FREQUENCY", "Start", func(p *sfxParams) *float32 { return &p.Freq }, 0, 1},
	{"", "Min", func(p *sfxParams) *float32 { return &p.FreqLimit }, 0, 1},
	{"", "Slide", func(p *sfxParams) *float32 { return &p.FreqRamp }, -1, 1},
	{"", "Delta slide", func(p *sfxParams) *float32 { return &p.FreqDramp }, -1, 1},

	{"TONE", "Duty", func(p *sfxParams) *float32 { return &p.Duty }, 0, 1},
	{"", "Duty sweep", func(p *sfxParams) *float32 { return &p.DutyRamp }, -1, 1},
	{"", "Vibrato", func(p *sfxParams) *float32 { return &p.Vibrato }, 0, 1},
	{"", "Vib speed", func(p *sfxParams) *float32 { return &p.VibratoSpeed }, 0, 1},

	{"CHANGE", "Arp mod", func(p *sfxParams) *float32 { return &p.ArpMod }, -1, 1},
	{"", "Arp speed", func(p *sfxParams) *float32 { return &p.ArpSpeed }, 0, 1},
	{"", "Repeat", func(p *sfxParams) *float32 { return &p.RepeatSpeed }, 0, 1},

	{"PHASER", "Offset", func(p *sfxParams) *float32 { return &p.PhaserOffset }, -1, 1},
	{"", "Sweep", func(p *sfxParams) *float32 { return &p.PhaserRamp }, -1, 1},

	{"FILTER", "LP cutoff", func(p *sfxParams) *float32 { return &p.LpfFreq }, 0, 1},
	{"", "LP sweep", func(p *sfxParams) *float32 { return &p.LpfRamp }, -1, 1},
	{"", "LP resonance", func(p *sfxParams) *float32 { return &p.LpfResonance }, 0, 1},
	{"", "HP cutoff", func(p *sfxParams) *float32 { return &p.HpfFreq }, 0, 1},
	{"", "HP sweep", func(p *sfxParams) *float32 { return &p.HpfRamp }, -1, 1},
}

// clampToSpecs pulls every parameter back inside its range, so a hand-edited or
// foreign JSON file cannot drive the synthesiser somewhere it cannot recover
// from (a negative period, say, which would loop forever).
func (p *sfxParams) clampToSpecs() {
	for _, spec := range sfxParamSpecs {
		v := spec.get(p)
		*v = clampF(*v, spec.min, spec.max)
	}
	p.Volume = clampF(p.Volume, 0, 1)
	if p.Wave < sfxSquare || p.Wave > sfxNoise {
		p.Wave = sfxSquare
	}
}

func clampF(v, lo, hi float32) float32 {
	return float32(math.Max(float64(lo), math.Min(float64(hi), float64(v))))
}

// ── synthesis ─────────────────────────────────────────────────────────────────

// sfxSynth is the running state of one render. sfxr kept all of this in globals
// and reset it between sounds; here it is a value, so rendering is reentrant
// and a preview cannot disturb whatever else is playing.
type sfxSynth struct {
	p sfxParams

	phase  int
	period int

	fperiod    float64
	fmaxperiod float64
	fslide     float64
	fdslide    float64

	squareDuty  float32
	squareSlide float32

	envStage  int
	envTime   int
	envLength [3]int
	envVol    float32

	fltp, fltdp, fltw, fltwD, fltdmp float32
	fltphp, flthp, flthpD            float32

	vibPhase, vibSpeed, vibAmp float32

	repTime, repLimit int
	arpTime, arpLimit int
	arpMod            float64

	fphase, fdphase float32
	iphase, ipp     int
	phaserBuf       [1024]float32

	noiseBuf [32]float32

	rng     *rand.Rand
	playing bool
}

func newSfxSynth(p sfxParams) *sfxSynth {
	p.clampToSpecs()
	s := &sfxSynth{p: p, rng: rand.New(rand.NewSource(1))}
	s.reset(false)
	s.playing = true
	return s
}

// reset recomputes the per-note state. restart=true is the retrigger path: it
// leaves the filters, envelope and phaser alone, which is what makes the
// "repeat" parameter a re-attack rather than a new sound.
func (s *sfxSynth) reset(restart bool) {
	p := &s.p

	s.fperiod = 100.0 / (float64(p.Freq)*float64(p.Freq) + 0.001)
	s.period = int(s.fperiod)
	s.fmaxperiod = 100.0 / (float64(p.FreqLimit)*float64(p.FreqLimit) + 0.001)
	s.fslide = 1.0 - math.Pow(float64(p.FreqRamp), 3.0)*0.01
	s.fdslide = -math.Pow(float64(p.FreqDramp), 3.0) * 0.000001

	s.squareDuty = 0.5 - p.Duty*0.5
	s.squareSlide = -p.DutyRamp * 0.00005

	if p.ArpMod >= 0 {
		s.arpMod = 1.0 - math.Pow(float64(p.ArpMod), 2.0)*0.9
	} else {
		s.arpMod = 1.0 + math.Pow(float64(p.ArpMod), 2.0)*10.0
	}
	s.arpTime = 0
	s.arpLimit = int(math.Pow(float64(1-p.ArpSpeed), 2.0)*20000 + 32)
	if p.ArpSpeed == 1 {
		s.arpLimit = 0
	}

	if restart {
		return
	}
	s.phase = 0

	s.fltp, s.fltdp = 0, 0
	s.fltw = float32(math.Pow(float64(p.LpfFreq), 3.0)) * 0.1
	s.fltwD = 1 + p.LpfRamp*0.0001
	s.fltdmp = 5.0 / (1 + float32(math.Pow(float64(p.LpfResonance), 2.0))*20) * (0.01 + s.fltw)
	if s.fltdmp > 0.8 {
		s.fltdmp = 0.8
	}
	s.fltphp = 0
	s.flthp = float32(math.Pow(float64(p.HpfFreq), 2.0)) * 0.1
	s.flthpD = 1 + p.HpfRamp*0.0003

	s.vibPhase = 0
	s.vibSpeed = float32(math.Pow(float64(p.VibratoSpeed), 2.0)) * 0.01
	s.vibAmp = p.Vibrato * 0.5

	s.envVol, s.envStage, s.envTime = 0, 0, 0
	s.envLength[0] = int(p.Attack * p.Attack * 100000)
	s.envLength[1] = int(p.Sustain * p.Sustain * 100000)
	s.envLength[2] = int(p.Decay * p.Decay * 100000)

	s.fphase = float32(math.Pow(float64(p.PhaserOffset), 2.0)) * 1020
	if p.PhaserOffset < 0 {
		s.fphase = -s.fphase
	}
	s.fdphase = float32(math.Pow(float64(p.PhaserRamp), 2.0))
	if p.PhaserRamp < 0 {
		s.fdphase = -s.fdphase
	}
	s.iphase = abs(int(s.fphase))
	s.ipp = 0
	s.phaserBuf = [1024]float32{}

	for i := range s.noiseBuf {
		s.noiseBuf[i] = s.rng.Float32()*2 - 1
	}

	s.repTime = 0
	s.repLimit = int(math.Pow(float64(1-p.RepeatSpeed), 2.0)*20000 + 32)
	if p.RepeatSpeed == 0 {
		s.repLimit = 0
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// render produces the whole sound as mono float32 samples in [-1,1].
func (p sfxParams) render() []float32 {
	s := newSfxSynth(p)
	out := make([]float32, 0, sfxSampleRate)
	limit := sfxSampleRate * sfxMaxSeconds

	for s.playing && len(out) < limit {
		out = append(out, s.step())
	}
	return out
}

// step advances one output sample. It is the body of sfxr's SynthSample loop.
func (s *sfxSynth) step() float32 {
	p := &s.p

	s.repTime++
	if s.repLimit != 0 && s.repTime >= s.repLimit {
		s.repTime = 0
		s.reset(true)
	}

	s.arpTime++
	if s.arpLimit != 0 && s.arpTime >= s.arpLimit {
		s.arpLimit = 0
		s.fperiod *= s.arpMod
	}

	s.fslide += s.fdslide
	s.fperiod *= s.fslide
	if s.fperiod > s.fmaxperiod {
		s.fperiod = s.fmaxperiod
		// A minimum frequency of zero means "no limit"; anything above it ends
		// the sound when the slide reaches the floor.
		if p.FreqLimit > 0 {
			s.playing = false
		}
	}

	rfperiod := s.fperiod
	if s.vibAmp > 0 {
		s.vibPhase += s.vibSpeed
		rfperiod = s.fperiod * (1.0 + math.Sin(float64(s.vibPhase))*float64(s.vibAmp))
	}
	s.period = int(rfperiod)
	if s.period < 8 {
		s.period = 8
	}

	s.squareDuty = clampF(s.squareDuty+s.squareSlide, 0, 0.5)

	s.envTime++
	if s.envTime > s.envLength[s.envStage] {
		s.envTime = 0
		s.envStage++
		if s.envStage == 3 {
			s.playing = false
			return 0
		}
	}
	switch s.envStage {
	case 0:
		s.envVol = safeDiv(float32(s.envTime), float32(s.envLength[0]))
	case 1:
		s.envVol = 1 + (1-safeDiv(float32(s.envTime), float32(s.envLength[1])))*2*p.Punch
	case 2:
		s.envVol = 1 - safeDiv(float32(s.envTime), float32(s.envLength[2]))
	}

	s.fphase += s.fdphase
	s.iphase = min(abs(int(s.fphase)), 1023)

	if s.flthpD != 0 {
		s.flthp = clampF(s.flthp*s.flthpD, 0.00001, 0.1)
	}

	var ssample float32
	for i := 0; i < sfxSuperSample; i++ {
		s.phase++
		if s.phase >= s.period {
			s.phase %= s.period
			if p.Wave == sfxNoise {
				for j := range s.noiseBuf {
					s.noiseBuf[j] = s.rng.Float32()*2 - 1
				}
			}
		}

		var sample float32
		fp := float32(s.phase) / float32(s.period)
		switch p.Wave {
		case sfxSquare:
			if fp < s.squareDuty {
				sample = 0.5
			} else {
				sample = -0.5
			}
		case sfxSawtooth:
			sample = 1 - fp*2
		case sfxSine:
			sample = float32(math.Sin(float64(fp) * 2 * math.Pi))
		case sfxNoise:
			sample = s.noiseBuf[min(s.phase*32/s.period, 31)]
		}

		// Low-pass. A cutoff of exactly 1 is the documented bypass.
		pp := s.fltp
		s.fltw = clampF(s.fltw*s.fltwD, 0, 0.1)
		if p.LpfFreq != 1 {
			s.fltdp += (sample - s.fltp) * s.fltw
			s.fltdp -= s.fltdp * s.fltdmp
		} else {
			s.fltp, s.fltdp = sample, 0
		}
		s.fltp += s.fltdp

		// High-pass, fed from the low-pass's own delta.
		s.fltphp += s.fltp - pp
		s.fltphp -= s.fltphp * s.flthp
		sample = s.fltphp

		// Phaser: a 1024-sample ring mixed back in at a moving offset.
		s.phaserBuf[s.ipp&1023] = sample
		sample += s.phaserBuf[(s.ipp-s.iphase+1024)&1023]
		s.ipp = (s.ipp + 1) & 1023

		ssample += sample * s.envVol
	}

	ssample = ssample / sfxSuperSample * sfxMasterVol
	ssample *= 2 * p.Volume
	return clampF(ssample, -1, 1)
}

// safeDiv guards the envelope stages, whose lengths are zero whenever the
// corresponding slider is. sfxr divided by zero here and relied on the IEEE
// infinity landing outside the clamp; being explicit is cheaper to read.
func safeDiv(a, b float32) float32 {
	if b == 0 {
		return 1
	}
	return a / b
}

// ── wav ───────────────────────────────────────────────────────────────────────

// sfxEncodeWAV renders the samples as a 16-bit mono RIFF/WAVE file. This is
// both what gets written to disk and what gets handed to raylib for preview,
// so there is one code path and the preview is exactly the exported file.
func sfxEncodeWAV(samples []float32) []byte {
	const bytesPerSample = sfxBitDepth / 8
	dataLen := len(samples) * bytesPerSample

	var buf bytes.Buffer
	buf.Grow(44 + dataLen)
	w := func(v any) { _ = binary.Write(&buf, binary.LittleEndian, v) }

	buf.WriteString("RIFF")
	w(uint32(36 + dataLen))
	buf.WriteString("WAVEfmt ")
	w(uint32(16))                                           // fmt chunk size
	w(uint16(1))                                            // PCM, uncompressed
	w(uint16(sfxChannels))                                  //
	w(uint32(sfxSampleRate))                                //
	w(uint32(sfxSampleRate * sfxChannels * bytesPerSample)) // byte rate
	w(uint16(sfxChannels * bytesPerSample))                 // block align
	w(uint16(sfxBitDepth))
	buf.WriteString("data")
	w(uint32(dataLen))

	for _, s := range samples {
		// Scale by 32767 rather than 32768 so that +1.0 does not wrap to the
		// most negative sample.
		w(int16(clampF(s, -1, 1) * 32767))
	}
	return buf.Bytes()
}

// ── files ─────────────────────────────────────────────────────────────────────

// sfxDir is where a project keeps its sounds; both the JSON parameter sets and
// the rendered WAVs live here, side by side under the same base name.
const sfxDir = "assets/sfx"

// sfxFile is the on-disk shape: the parameters plus a marker saying what wrote
// them, so a file that turns up in a project is self-describing.
type sfxFile struct {
	Tool    string    `json:"tool"`
	Version int       `json:"version"`
	Params  sfxParams `json:"params"`
}

const sfxFormatVersion = 1

func sfxSaveJSON(path string, p sfxParams) error {
	data, err := json.MarshalIndent(sfxFile{Tool: "fz sfx", Version: sfxFormatVersion, Params: p}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// sfxLoadJSON reads a parameter set. It accepts both the wrapped form this
// tool writes and a bare parameter object, so a file trimmed by hand or
// produced by a script still loads.
func sfxLoadJSON(path string) (sfxParams, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return sfxParams{}, err
	}
	var f sfxFile
	if err := json.Unmarshal(data, &f); err == nil && f.Params != (sfxParams{}) {
		f.Params.clampToSpecs()
		return f.Params, nil
	}
	var p sfxParams
	if err := json.Unmarshal(data, &p); err != nil {
		return sfxParams{}, err
	}
	p.clampToSpecs()
	return p, nil
}

func sfxSaveWAV(path string, p sfxParams) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, sfxEncodeWAV(p.render()), 0o644)
}

// sfxListJSON returns the parameter sets in assets/sfx, sorted, as bare names
// without the extension. A missing directory is not an error: a project simply
// has no sounds yet.
func sfxListJSON() []string {
	entries, err := os.ReadDir(sfxDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
			continue
		}
		out = append(out, strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
	}
	return out
}

// sfxPath builds the path for one of a sound's two files.
func sfxPath(name, ext string) string {
	return filepath.Join(sfxDir, name+ext)
}
