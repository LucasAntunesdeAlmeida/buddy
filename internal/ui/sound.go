package ui

import (
	"encoding/binary"
	"math"
)

// chirpWAV synthesizes a short rising chirp as a 16-bit mono WAV file.
func chirpWAV() []byte {
	const (
		rate = 22050
		dur  = 0.18
	)
	n := int(dur * rate)
	pcm := make([]byte, 0, n*2)
	phase := 0.0
	for i := range n {
		p := float64(i) / float64(n)
		phase += 2 * math.Pi * (1100 + 900*p) / rate
		env := math.Sin(math.Pi * p) // fade in and out, no clicks
		pcm = binary.LittleEndian.AppendUint16(pcm, uint16(int16(0.25*env*math.Sin(phase)*math.MaxInt16)))
	}

	wav := make([]byte, 0, 44+len(pcm))
	wav = append(wav, "RIFF"...)
	wav = binary.LittleEndian.AppendUint32(wav, uint32(36+len(pcm)))
	wav = append(wav, "WAVEfmt "...)
	wav = binary.LittleEndian.AppendUint32(wav, 16)     // fmt chunk size
	wav = binary.LittleEndian.AppendUint16(wav, 1)      // PCM
	wav = binary.LittleEndian.AppendUint16(wav, 1)      // mono
	wav = binary.LittleEndian.AppendUint32(wav, rate)   // sample rate
	wav = binary.LittleEndian.AppendUint32(wav, rate*2) // bytes per second
	wav = binary.LittleEndian.AppendUint16(wav, 2)      // bytes per frame
	wav = binary.LittleEndian.AppendUint16(wav, 16)     // bits per sample
	wav = append(wav, "data"...)
	wav = binary.LittleEndian.AppendUint32(wav, uint32(len(pcm)))
	return append(wav, pcm...)
}
