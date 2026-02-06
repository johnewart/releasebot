package sound

import (
	"bytes"
	"encoding/binary"
	"math"
	"sync"

	"github.com/faiface/beep"
	"github.com/faiface/beep/speaker"
	"github.com/faiface/beep/wav"
)

const sampleRate = 44100

var initOnce sync.Once
var initErr error

func ensureSpeaker() error {
	initOnce.Do(func() {
		initErr = speaker.Init(beep.SampleRate(sampleRate), sampleRate/22) // ~2kb buffer
	})
	return initErr
}

// writeWAVHeader writes a 44-byte WAV header for 16-bit mono PCM.
func writeWAVHeader(b *bytes.Buffer, numSamples int) {
	dataBytes := numSamples * 2 // 16-bit
	fileSize := 36 + dataBytes
	b.Grow(44)
	b.WriteString("RIFF")
	binary.Write(b, binary.LittleEndian, uint32(fileSize))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	binary.Write(b, binary.LittleEndian, uint32(16))
	binary.Write(b, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(b, binary.LittleEndian, uint16(1)) // mono
	binary.Write(b, binary.LittleEndian, uint32(sampleRate))
	binary.Write(b, binary.LittleEndian, uint32(sampleRate*2))
	binary.Write(b, binary.LittleEndian, uint16(2))
	binary.Write(b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	binary.Write(b, binary.LittleEndian, uint32(dataBytes))
}

// tone generates numSamples of a sine wave at freq Hz, amplitude 0..1, with optional fade-in/fade-out samples.
func tone(numSamples int, freq float64, amplitude float64, fadeSamples int) []int16 {
	out := make([]int16, numSamples)
	scale := amplitude * 32767.0
	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(sampleRate)
		s := math.Sin(2 * math.Pi * freq * t)
		// soft envelope
		env := 1.0
		if fadeSamples > 0 {
			if i < fadeSamples {
				env = float64(i) / float64(fadeSamples)
			} else if i >= numSamples-fadeSamples {
				env = float64(numSamples-1-i) / float64(fadeSamples)
			}
		}
		v := int16(s * scale * env)
		out[i] = v
	}
	return out
}

func appendSamples(b *bytes.Buffer, samples []int16) {
	for _, s := range samples {
		binary.Write(b, binary.LittleEndian, s)
	}
}

// successWAV returns a short, pleasant two-tone success sound (ascending).
func successWAV() []byte {
	buf := &bytes.Buffer{}
	fade := sampleRate / 25             // ~40ms fade
	dur := sampleRate / 5               // 0.2s per tone
	s1 := tone(dur, 523.25, 0.28, fade) // C5
	s2 := tone(dur, 659.25, 0.26, fade) // E5
	all := append(s1, s2...)
	writeWAVHeader(buf, len(all))
	appendSamples(buf, all)
	return buf.Bytes()
}

// failureWAV returns a soft, clear two-tone alert (not jarring).
func failureWAV() []byte {
	buf := &bytes.Buffer{}
	fade := sampleRate / 20          // ~50ms fade
	dur := sampleRate / 4            // 0.25s per tone
	s1 := tone(dur, 392, 0.14, fade) // G4
	s2 := tone(dur, 330, 0.12, fade) // E4
	all := append(s1, s2...)
	writeWAVHeader(buf, len(all))
	appendSamples(buf, all)
	return buf.Bytes()
}

// PlaySuccess plays a short pleasant success sound. Safe to call from any goroutine; runs playback in background.
func PlaySuccess() {
	go play(successWAV())
}

// PlayFailure plays a soft, clear failure alert. Safe to call from any goroutine; runs playback in background.
func PlayFailure() {
	go play(failureWAV())
}

func play(wavBytes []byte) {
	if ensureSpeaker() != nil {
		return
	}
	r := bytes.NewReader(wavBytes)
	streamer, _, err := wav.Decode(r)
	if err != nil {
		return
	}
	// Speaker drains the streamer in the background; do not close until done.
	speaker.Play(beep.Seq(streamer, beep.Callback(func() { streamer.Close() })))
}
