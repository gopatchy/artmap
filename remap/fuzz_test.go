package remap

import (
	"testing"

	"github.com/gopatchy/artmap/config"
)

func FuzzRemap(f *testing.F) {
	f.Add(uint16(0), uint16(1), 0, 0, 512, make([]byte, 512))
	f.Add(uint16(0), uint16(1), 100, 200, 100, make([]byte, 512))
	f.Add(uint16(0), uint16(1), 511, 511, 1, make([]byte, 512))
	f.Add(uint16(100), uint16(200), 0, 0, 256, make([]byte, 512))

	f.Fuzz(func(t *testing.T, srcUni, dstUni uint16, fromChan, toChan, count int, inputData []byte) {
		if fromChan < 0 || fromChan >= 512 {
			return
		}
		if toChan < 0 || toChan >= 512 {
			return
		}
		if count < 1 || count > 512 {
			return
		}
		if fromChan+count > 512 || toChan+count > 512 {
			return
		}
		if len(inputData) < 512 {
			return
		}

		srcU, _ := config.NewUniverse(config.ProtocolArtNet, srcUni)
		dstU, _ := config.NewUniverse(config.ProtocolArtNet, dstUni)

		mappings := []config.NormalizedMapping{{
			From:     srcU,
			FromChan: fromChan,
			To:       dstU,
			ToChan:   toChan,
			Count:    count,
		}}

		engine := NewEngine(mappings)

		var srcData [512]byte
		copy(srcData[:], inputData[:512])

		engine.Remap(srcU, srcData)
		outputs := engine.GetDirtyOutputs()

		if len(outputs) != 1 {
			t.Fatalf("expected 1 output, got %d", len(outputs))
		}

		for i := 0; i < count; i++ {
			srcIdx := fromChan + i
			dstIdx := toChan + i
			if outputs[0].Data[dstIdx] != srcData[srcIdx] {
				t.Fatalf("channel mismatch at offset %d: src[%d]=%d != dst[%d]=%d",
					i, srcIdx, srcData[srcIdx], dstIdx, outputs[0].Data[dstIdx])
			}
		}
	})
}

func FuzzRemapMultipleMappings(f *testing.F) {
	f.Add(make([]byte, 512))

	f.Fuzz(func(t *testing.T, inputData []byte) {
		if len(inputData) < 512 {
			return
		}

		srcU, _ := config.NewUniverse(config.ProtocolArtNet, 0)
		dstU1, _ := config.NewUniverse(config.ProtocolArtNet, 1)
		dstU2, _ := config.NewUniverse(config.ProtocolSACN, 1)

		mappings := []config.NormalizedMapping{
			{From: srcU, FromChan: 0, To: dstU1, ToChan: 0, Count: 256},
			{From: srcU, FromChan: 256, To: dstU2, ToChan: 0, Count: 256},
		}

		engine := NewEngine(mappings)

		var srcData [512]byte
		copy(srcData[:], inputData[:512])

		engine.Remap(srcU, srcData)
		outputs := engine.GetDirtyOutputs()

		if len(outputs) != 2 {
			t.Fatalf("expected 2 outputs, got %d", len(outputs))
		}

		for _, out := range outputs {
			if out.Universe == dstU1 {
				for i := 0; i < 256; i++ {
					if out.Data[i] != srcData[i] {
						t.Fatalf("dstU1 mismatch at %d", i)
					}
				}
			} else if out.Universe == dstU2 {
				for i := 0; i < 256; i++ {
					if out.Data[i] != srcData[256+i] {
						t.Fatalf("dstU2 mismatch at %d", i)
					}
				}
			}
		}
	})
}

func FuzzRemapUnmatchedUniverse(f *testing.F) {
	f.Add(make([]byte, 512))

	f.Fuzz(func(t *testing.T, inputData []byte) {
		if len(inputData) < 512 {
			return
		}

		srcU, _ := config.NewUniverse(config.ProtocolArtNet, 0)
		otherU, _ := config.NewUniverse(config.ProtocolArtNet, 99)
		dstU, _ := config.NewUniverse(config.ProtocolArtNet, 1)

		mappings := []config.NormalizedMapping{{
			From: srcU, FromChan: 0, To: dstU, ToChan: 0, Count: 512,
		}}

		engine := NewEngine(mappings)

		var srcData [512]byte
		copy(srcData[:], inputData[:512])

		engine.Remap(otherU, srcData)
		outputs := engine.GetDirtyOutputs()
		if len(outputs) != 0 {
			t.Fatalf("expected 0 outputs for unmatched universe, got %d outputs", len(outputs))
		}
	})
}
