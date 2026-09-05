# vMix SCTE-35 Detector & Ingest Relay

Automated SCTE-35 commercial insertion and stream relay designed for broadcast production with vMix.

## Overview

In professional broadcasting and live streaming, downstream automation needs to detect SCTE-35 splice flags (cue-out / cue-in) and switch to local ad breaks or graphic slates without compromising main stream playback stability or multi-channel audio tracks.

This project provides a complete, decoupled solution:
- **Stream Ingest Relay**: A low-latency raw passthrough relay that receives an incoming SRT/UDP feed and distributes it locally to vMix and the SCTE-35 detector.
- **SCTE-35 Detector**: A pure Go application that analyzes MPEG-TS packets in real time, parses SCTE-35 splice commands (`splice_insert`, `splice_null`, cue-out, cue-in, duration auto-return), and triggers vMix via its local HTTP API.
- **Operator GUI**: A clean Windows GUI for monitoring playback status, selecting active inputs, and visual cue feedback.

---

## Signal Architecture

```text
Incoming Stream (SRT Caller / UDP)
                 │
                 ▼
     [ START-RELAY.cmd ] (FFmpeg Raw Ingest Relay)
         │
         ├── srt://127.0.0.1:10001 (SRT Listener, Raw TS)
         │       └──▶ vMix (Stream / SRT Caller Input, "Audio Track: All")
         │
         └── udp://127.0.0.1:10002 (UDP, Raw TS)
                 └──▶ [ vmix-scte35-detector.exe ] (Go Realtime Parser)
                             │
                             └──▶ HTTP API (127.0.0.1:8088)
                                     └──▶ CutDirect to Ad slate / Main input
```

### Why Decoupled Raw Passthrough?

1. **Zero Remuxing / Zero Clock Drift**: Unlike standard demuxing/remuxing pipelines (`-f mpegts`), the relay uses `-f data -raw_packet_size 1316`. It copies 188-byte MPEG-TS packets directly without regenerating PCR/PTS timestamps, preventing decoder clock resets.
2. **Audio Track Preservation**: Multilingual audio tracks (e.g., multiple stereo pairs) pass through unmodified without downmixing or re-encoding.
3. **Sub-millisecond Local Latency**: Local UDP transmission to the detector introduces negligible latency (< 1 ms), ensuring cue-out cuts occur exactly on time.
4. **Resilience**: If the detector restarts or experiences a momentary hiccup, vMix's live stream playback is unaffected.

---

## Prerequisites

- **Windows 10 / 11** (Intel/AMD `x64` or ARM64 / Apple Silicon Parallels VM).
- **vMix** (with Web Controller enabled on default port `8088`).
- **FFmpeg** for Windows (used by `START-RELAY.cmd`). A download helper `Install-FFmpeg.ps1` is included.

---

## Quick Start

### 1. vMix Preparation
1. Add an input: **Stream / SRT** -> Type: **SRT (Caller)**, Port: `10001`, Address: `127.0.0.1`.
2. In the input settings, ensure audio tracks are mapped as needed (e.g. `Audio Track: All`).
3. Add or designate your advertising/slate input (e.g., input titled `Blank` or `AD_BREAK`).
4. Ensure vMix Web Controller is enabled under **Settings -> Web Controller** (port `8088`).

### 2. Configure Stream URL
Create or edit `stream-url.txt` in the root folder with your source stream address:
```text
srt://your-stream-server.com:9001?mode=caller&transtype=live&latency=2000000&pkt_size=1316
```
*(You can also paste a raw `IP:PORT` or domain; the launcher will automatically wrap it with recommended SRT parameters).*

### 3. Launch Ingest Relay
Run `START-RELAY.cmd`. A console window will open and begin receiving the stream and listening on `127.0.0.1:10001` for vMix.

### 4. Launch SCTE-35 Detector GUI
Run `START-GUI.cmd`.
- Verify the **Main Input** (e.g., `1`) and **Ad Input** (e.g., `Blank`).
- Click **Start Automation**.
- The interface will display live vMix status and indicate `COMMERCIAL IN PROGRESS` whenever an ad break is triggered.

---

## Safety & Operational Logic

- **State Verification**: Splice OUT (cue-out) only triggers a switch if the configured Main Input is currently active in Program.
- **Operator Override**: If an operator manually changes the Program input in vMix during a commercial break, the automatic return cut is safely canceled.
- **Automatic Fallback Return**: If a cue-in signal is lost or corrupted upstream, the detector automatically returns to the main input when the advertised break duration expires.
- **Duplicate Suppression**: Duplicate splice events with the same event ID are debounced to prevent flapping.

---

## Building from Source

The detector is written in pure Go using only the Go standard library (zero external dependencies).

```bash
# Run unit tests
go test -v ./...

# Build for 64-bit Windows (x64)
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/windows-amd64/vmix-scte35-detector.exe .

# Build for ARM64 Windows (e.g. Parallels on Apple Silicon)
GOOS=windows GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist/windows-arm64/vmix-scte35-detector.exe .
```

---

## License

This project is licensed under the [MIT License](LICENSE).
