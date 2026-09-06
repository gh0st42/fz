package main

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestRenderProducesAudibleSound(t *testing.T) {
	for _, pre := range sfxPresets {
		g := newSfxRand()
		for try := 0; try < 20; try++ {
			p := pre.gen(g)
			out := p.render()
			if len(out) == 0 {
				t.Fatalf("%s: rendered nothing", pre.name)
			}
			if secs := float64(len(out)) / sfxSampleRate; secs > sfxMaxSeconds+0.01 {
				t.Errorf("%s: %.1fs exceeds the cap", pre.name, secs)
			}
			var peak float64
			for i, v := range out {
				if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
					t.Fatalf("%s: sample %d is %v", pre.name, i, v)
				}
				if v < -1 || v > 1 {
					t.Fatalf("%s: sample %d out of range: %v", pre.name, i, v)
				}
				peak = math.Max(peak, math.Abs(float64(v)))
			}
			if peak == 0 {
				t.Errorf("%s: silent (try %d)", pre.name, try)
			}
		}
	}
}

func TestDefaultsAreAudible(t *testing.T) {
	out := sfxDefaults().render()
	if len(out) < sfxSampleRate/50 {
		t.Fatalf("default sound is only %d samples", len(out))
	}
	var peak float32
	for _, v := range out {
		if v < 0 {
			v = -v
		}
		if v > peak {
			peak = v
		}
	}
	if peak < 0.001 {
		t.Errorf("default sound peaks at %v", peak)
	}
}

// A sine at a known parameter should come out at the frequency sfxr's formula
// predicts: period = 100/(freq^2+0.001) supersamples, at 8x oversampling.
func TestSinePitchMatchesFormula(t *testing.T) {
	p := sfxDefaults()
	p.Wave = sfxSine
	p.Freq = 0.5
	p.Sustain = 0.5
	p.Decay = 0.1
	p.LpfFreq = 1 // bypass the filter so the pitch is unmodified
	out := p.render()

	// Count zero crossings over the steady middle of the sound.
	lo, hi := len(out)/4, len(out)*3/4
	crossings := 0
	for i := lo + 1; i < hi; i++ {
		if (out[i-1] < 0) != (out[i] < 0) {
			crossings++
		}
	}
	secs := float64(hi-lo) / sfxSampleRate
	gotHz := float64(crossings) / 2 / secs

	period := 100.0 / (0.5*0.5 + 0.001) // in supersamples
	wantHz := sfxSampleRate * sfxSuperSample / period

	if math.Abs(gotHz-wantHz)/wantHz > 0.05 {
		t.Errorf("sine came out at %.0f Hz, formula says %.0f Hz", gotHz, wantHz)
	}
}

func TestWavHeaderAndPayload(t *testing.T) {
	samples := []float32{0, 0.5, -0.5, 1, -1}
	data := sfxEncodeWAV(samples)

	if len(data) != 44+len(samples)*2 {
		t.Fatalf("wav is %d bytes, want %d", len(data), 44+len(samples)*2)
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		t.Fatalf("not a RIFF/WAVE file: %q", data[:12])
	}
	get32 := func(off int) uint32 { return binary.LittleEndian.Uint32(data[off:]) }
	get16 := func(off int) uint16 { return binary.LittleEndian.Uint16(data[off:]) }

	if got := get32(4); got != uint32(36+len(samples)*2) {
		t.Errorf("RIFF size = %d", got)
	}
	if got := get16(20); got != 1 {
		t.Errorf("format = %d, want 1 (PCM)", got)
	}
	if got := get16(22); got != sfxChannels {
		t.Errorf("channels = %d", got)
	}
	if got := get32(24); got != sfxSampleRate {
		t.Errorf("sample rate = %d", got)
	}
	if got := get32(28); got != sfxSampleRate*sfxChannels*2 {
		t.Errorf("byte rate = %d", got)
	}
	if got := get16(32); got != sfxChannels*2 {
		t.Errorf("block align = %d", got)
	}
	if got := get16(34); got != sfxBitDepth {
		t.Errorf("bit depth = %d", got)
	}
	if string(data[36:40]) != "data" || get32(40) != uint32(len(samples)*2) {
		t.Errorf("bad data chunk")
	}

	pcm := make([]int16, len(samples))
	if err := binary.Read(bytes.NewReader(data[44:]), binary.LittleEndian, pcm); err != nil {
		t.Fatal(err)
	}
	want := []int16{0, 16383, -16383, 32767, -32767}
	for i := range want {
		if pcm[i] != want[i] {
			t.Errorf("sample %d = %d, want %d", i, pcm[i], want[i])
		}
	}
}

func TestJSONRoundTrip(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	os.Chdir(dir)

	p := sfxLaserShoot(newSfxRand())
	path := sfxPath("zap", ".json")
	if err := sfxSaveJSON(path, p); err != nil {
		t.Fatal(err)
	}
	got, err := sfxLoadJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != p {
		t.Errorf("round trip changed the sound:\n got %+v\nwant %+v", got, p)
	}

	// A bare parameter object, as someone might hand-write it.
	bare := `{"wave":3,"freq":0.5,"sustain":0.2,"decay":0.3,"volume":0.5,"lpfFreq":1}`
	if err := os.WriteFile(sfxPath("bare", ".json"), []byte(bare), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := sfxLoadJSON(sfxPath("bare", ".json"))
	if err != nil {
		t.Fatalf("bare object did not load: %v", err)
	}
	if b.Wave != sfxNoise || b.Freq != 0.5 {
		t.Errorf("bare object parsed as %+v", b)
	}

	// Out-of-range values are pulled back rather than trusted.
	wild := `{"wave":99,"freq":50,"decay":-8,"volume":9}`
	if err := os.WriteFile(sfxPath("wild", ".json"), []byte(wild), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := sfxLoadJSON(sfxPath("wild", ".json"))
	if err != nil {
		t.Fatal(err)
	}
	if w.Wave != sfxSquare || w.Freq != 1 || w.Decay != 0 || w.Volume != 1 {
		t.Errorf("wild values not clamped: %+v", w)
	}
	if out := w.render(); len(out) > sfxSampleRate*sfxMaxSeconds {
		t.Errorf("clamped sound still ran away: %d samples", len(out))
	}
}

func TestListingAndSaveWav(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	os.Chdir(dir)

	if got := sfxListJSON(); got != nil {
		t.Errorf("empty project listed %v", got)
	}
	for _, n := range []string{"coin", "blip"} {
		if err := sfxSaveJSON(sfxPath(n, ".json"), sfxDefaults()); err != nil {
			t.Fatal(err)
		}
	}
	// Non-JSON files in the directory are ignored.
	if err := os.WriteFile(sfxPath("coin", ".wav"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := sfxListJSON()
	if len(got) != 2 {
		t.Fatalf("listed %v", got)
	}

	if err := sfxSaveWAV(sfxPath("coin", ".wav"), sfxDefaults()); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, sfxPath("coin", ".wav")))
	if err != nil || st.Size() < 44 {
		t.Fatalf("wav not written: %v", err)
	}
}

func TestMutateStaysInRange(t *testing.T) {
	g := newSfxRand()
	p := sfxRandomize(g)
	for i := 0; i < 500; i++ {
		p = sfxMutate(g, p)
	}
	for _, spec := range sfxParamSpecs {
		if v := *spec.get(&p); v < spec.min || v > spec.max {
			t.Errorf("%s drifted to %v, range %v..%v", spec.label, v, spec.min, spec.max)
		}
	}
}
