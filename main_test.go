package main

import (
	"encoding/hex"
	"io"
	"os"
	"testing"
	"time"
)

func TestParseSpliceInsert(t *testing.T) {
	tests := []struct {
		hex      string
		out      bool
		event    uint32
		duration time.Duration
	}{
		{
			hex:   "fc303b00000000000000fff01405000042697feffe9732d7637e01208748000000000016021443554549000069797fc0000120930c0000320000dc414b19",
			out:   true,
			event: 17001,
			duration: 210 * time.Second,
		},
		{
			hex:   "fc303100000000000000fff00f05000042697f4ffe98536de1000000000011020f43554549000069797f80000033000077755219",
			out:   false,
			event: 17001,
		},
	}
	for _, tt := range tests {
		data, err := hex.DecodeString(tt.hex)
		if err != nil {
			t.Fatal(err)
		}
		if crc32MPEG(data) != 0 {
			t.Fatal("test section CRC is invalid")
		}
		cue, ok := parseSpliceInsert(0x105, data)
		if !ok {
			t.Fatal("section was not parsed")
		}
		if cue.Out != tt.out || cue.EventID != tt.event {
			t.Fatalf("got out=%t event=%d", cue.Out, cue.EventID)
		}
		if tt.duration > 0 {
			if cue.Duration == nil || cue.Duration.Round(time.Second) != tt.duration {
				t.Fatalf("duration=%v", cue.Duration)
			}
		}
	}
}

func TestRecordedTransportStream(t *testing.T) {
	path := os.Getenv("SCTE_TEST_TS")
	if path == "" {
		t.Skip("SCTE_TEST_TS is not set")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var cues []cue
	d := &detector{
		sections: make(map[uint16]*sectionBuffer),
		seen:     make(map[string]time.Time),
		onCue:    func(c cue) { cues = append(cues, c) },
	}
	packet := make([]byte, tsPacketSize)
	for {
		_, err = io.ReadFull(f, packet)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		d.consume(packet)
	}
	if len(cues) != 9 {
		t.Fatalf("got %d cues, want 9", len(cues))
	}
	outs, ins := 0, 0
	for _, c := range cues {
		if c.Out {
			outs++
		} else {
			ins++
		}
	}
	if outs != 4 || ins != 5 {
		t.Fatalf("got OUT=%d IN=%d, want OUT=4 IN=5", outs, ins)
	}
}

func TestSignedPTSDelta(t *testing.T) {
	if got := signedPTSDelta(190000, 100000); got != 90000 {
		t.Fatalf("future delta=%d", got)
	}
	if got := signedPTSDelta(100000, 190000); got != -90000 {
		t.Fatalf("past delta=%d", got)
	}
	if got := signedPTSDelta(45000, ptsMask-44999); got != 90000 {
		t.Fatalf("wrap delta=%d", got)
	}
}
