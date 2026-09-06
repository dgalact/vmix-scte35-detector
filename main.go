package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	tsPacketSize = 188
	ptsMask      = uint64(1<<33 - 1)
)

type options struct {
	input      string
	ffmpeg     string
	localSRT   string
	localMode  string
	vmix       string
	main       string
	ad         string
	dryRun     bool
	logPath    string
	statusPath string
	passFile   string
}


type cue struct {
	PID        uint16
	EventID    uint32
	Out        bool
	Immediate  bool
	PTS        *uint64
	Duration   *time.Duration
	AutoReturn bool
	ReceivedAt time.Time
}

type sectionBuffer struct {
	data []byte
}

type detector struct {
	sections      map[uint16]*sectionBuffer
	seen          map[string]time.Time
	onCue         func(cue)
	ctx           context.Context
	pcr90         uint64
	havePCR       bool
	onFirstPacket func()
	firstPacket   sync.Once
}

type vmixState struct {
	Active string      `xml:"active"`
	Inputs []vmixInput `xml:"inputs>input"`
}

type vmixInput struct {
	Number     string `xml:"number,attr"`
	Key        string `xml:"key,attr"`
	Title      string `xml:"title,attr"`
	ShortTitle string `xml:"shortTitle,attr"`
}

type controller struct {
	mu       sync.Mutex
	baseURL  string
	main     string
	ad       string
	dryRun   bool
	client   *http.Client
	inBreak  bool
	returnTo string
}

func main() {
	var cfg options
	flag.StringVar(&cfg.input, "input", "", "input stream URL (udp://... or srt://...)")
	flag.StringVar(&cfg.ffmpeg, "ffmpeg", "ffmpeg.exe", "path to ffmpeg.exe")
	flag.StringVar(&cfg.localSRT, "local-srt", "127.0.0.1:10000", "local SRT endpoint for vMix")
	flag.StringVar(&cfg.localMode, "local-mode", "caller", "local SRT mode: caller (vMix is listener) or listener (vMix is caller)")
	flag.StringVar(&cfg.vmix, "vmix", "http://127.0.0.1:8088", "vMix Web API base URL")
	flag.StringVar(&cfg.main, "main", "SRT 9001", "main programme input name or number")
	flag.StringVar(&cfg.ad, "ad", "AD_BREAK", "advertising slate input name or number")
	flag.BoolVar(&cfg.dryRun, "dry-run", true, "log cues without switching vMix")
	flag.StringVar(&cfg.logPath, "log", "detector.log", "log file path")
	flag.StringVar(&cfg.statusPath, "status", "relay.status", "relay status file")
	flag.Parse()

	inputURL := strings.TrimSpace(cfg.input)
	if inputURL == "" {
		inputURL = strings.TrimSpace(os.Getenv("SCTE_INPUT_URL"))
	}
	if inputURL == "" {
		inputURL = strings.TrimSpace(os.Getenv("SCTE_SRT_URL"))
	}
	if inputURL == "" {
		inputURL = "udp://127.0.0.1:10002"
	}
	lowerURL := strings.ToLower(inputURL)
	if !strings.HasPrefix(lowerURL, "srt://") && !strings.HasPrefix(lowerURL, "udp://") {
		log.Fatalf("input URL must start with srt:// or udp://: %q", inputURL)
	}

	logFile, err := os.OpenFile(cfg.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		log.Fatalf("open log: %v", err)
	}
	defer logFile.Close()
	log.SetOutput(io.MultiWriter(os.Stdout, logFile))
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ctl := &controller{
		baseURL: strings.TrimRight(cfg.vmix, "/"),
		main:    cfg.main,
		ad:      cfg.ad,
		dryRun:  cfg.dryRun,
		client:  &http.Client{Timeout: 2 * time.Second},
	}
	if !cfg.dryRun {
		if err := ctl.preflight(); err != nil {
			log.Fatalf("vMix preflight failed: %v", err)
		}
	}

	mode := "ACTIVE"
	if cfg.dryRun {
		mode = "DRY-RUN"
	}
	log.Printf("starting mode=%s local-mode=%s local-srt=%s vmix=%s main=%q ad=%q", mode, cfg.localMode, cfg.localSRT, cfg.vmix, cfg.main, cfg.ad)

	for ctx.Err() == nil {
		writeStatus(cfg.statusPath, "CONNECTING")
		err = runRelay(ctx, cfg, inputURL, ctl)
		if ctx.Err() != nil {
			break
		}
		log.Printf("relay stopped: %v; reconnecting in 3 seconds", err)
		writeStatus(cfg.statusPath, "RECONNECTING")
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
		}
	}
	log.Print("stopped")
	writeStatus(cfg.statusPath, "STOPPED")
}

func runRelay(ctx context.Context, cfg options, inputURL string, ctl *controller) error {
	isUDP := strings.HasPrefix(strings.ToLower(inputURL), "udp://")
	if isUDP {
		u, err := url.Parse(inputURL)
		if err != nil {
			return fmt.Errorf("invalid UDP URL: %w", err)
		}
		host := u.Host
		if !strings.Contains(host, ":") {
			host = net.JoinHostPort(host, "10002")
		}
		addr, err := net.ResolveUDPAddr("udp", host)
		if err != nil {
			return fmt.Errorf("resolve UDP addr: %w", err)
		}
		conn, err := net.ListenUDP("udp", addr)
		if err != nil {
			return fmt.Errorf("listen UDP: %w", err)
		}
		defer conn.Close()
		_ = conn.SetReadBuffer(4 * 1024 * 1024)

		log.Printf("detector listening directly on UDP %s (direct socket)", addr.String())

		d := &detector{
			sections: make(map[uint16]*sectionBuffer), seen: make(map[string]time.Time),
			onCue: ctl.handleCue, ctx: ctx,
			onFirstPacket: func() { writeStatus(cfg.statusPath, "LIVE") },
		}

		go func() {
			<-ctx.Done()
			_ = conn.Close()
		}()

		return relayPackets(ctx, conn, nil, d)
	}

	ffmpegPath, err := locateBeside(cfg.ffmpeg)
	if err != nil {
		return err
	}
	var args []string
	var localMode string

	u, err := url.Parse(inputURL)
	if err != nil || u.Hostname() == "" || u.Port() == "" {
		return fmt.Errorf("invalid SRT URL")
	}
	if _, err = strconv.Atoi(u.Port()); err != nil {
		return fmt.Errorf("invalid SRT port")
	}
	localMode = strings.ToLower(strings.TrimSpace(cfg.localMode))
	if localMode == "" {
		localMode = "caller"
	}

	hostPort := strings.TrimSpace(cfg.localSRT)
	hostPort = strings.TrimPrefix(hostPort, "srt://")
	var localSRTURL string
	if strings.Contains(hostPort, "?") {
		localSRTURL = "srt://" + hostPort
		if strings.Contains(hostPort, "mode=listener") {
			localMode = "listener"
		} else if strings.Contains(hostPort, "mode=caller") {
			localMode = "caller"
		}
	} else {
		if _, err = net.ResolveUDPAddr("udp4", hostPort); err != nil {
			return fmt.Errorf("invalid local SRT endpoint: %w", err)
		}
		if localMode == "listener" {
			localSRTURL = "srt://" + hostPort + "?mode=listener&transtype=live&pkt_size=1316&latency=100000&tlpktdrop=1&snddropdelay=0"
		} else {
			localSRTURL = "srt://" + hostPort + "?mode=caller&transtype=live&pkt_size=1316&latency=50000"
		}
	}

	// The data demuxer/muxer copies SRT payload bytes without parsing or
	// rebuilding the MPEG-TS. A 1316-byte packet is exactly seven TS packets.
	args = []string{
		"-hide_banner", "-loglevel", "warning",
		"-f", "data", "-raw_packet_size", "1316", "-i", inputURL,
		"-map", "0:0", "-c", "copy", "-f", "data", "pipe:1",
		"-map", "0:0", "-c", "copy", "-f", "data",
		localSRTURL,
	}
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return fmt.Errorf("start transport reader: %w", err)
	}

	if !isUDP && !cfg.dryRun && ctl != nil && localMode == "listener" {
		go func() {
			time.Sleep(400 * time.Millisecond)
			if err := ctl.resetInput(); err != nil {
				log.Printf("vMix input reset note: %v", err)
			} else {
				log.Printf("vMix input %q reset triggered", cfg.main)
			}
		}()
	}
	go func() {
		s := bufio.NewScanner(stderr)
		for s.Scan() {
			line := redact(s.Text(), inputURL)
			log.Printf("relay: %s", line)
		}
	}()

	d := &detector{
		sections: make(map[uint16]*sectionBuffer), seen: make(map[string]time.Time),
		onCue: ctl.handleCue, ctx: ctx,
		onFirstPacket: func() { writeStatus(cfg.statusPath, "LIVE") },
	}
	readDone := make(chan error, 1)
	go func() { readDone <- relayPackets(ctx, stdout, nil, d) }()
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err = <-readDone:
		if err == nil {
			err = errors.New("transport reader stopped")
		}
		return err
	case err = <-waitDone:
		if err == nil {
			err = errors.New("SRT relay stopped")
		}
		return err
	}
}

func locateBeside(name string) (string, error) {
	if filepath.IsAbs(name) || strings.ContainsAny(name, `/\\`) {
		if _, err := os.Stat(name); err != nil {
			return "", fmt.Errorf("relay executable not found at %q", name)
		}
		return name, nil
	}
	if exe, err := os.Executable(); err == nil {
		beside := filepath.Join(filepath.Dir(exe), name)
		if _, err = os.Stat(beside); err == nil {
			return beside, nil
		}
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s not found; keep ffmpeg.exe beside the detector", name)
	}
	return p, nil
}

func relayPackets(ctx context.Context, src io.Reader, dst net.Conn, d *detector) error {
	r := bufio.NewReaderSize(src, 1024*1024)
	packet := make([]byte, tsPacketSize)
	datagram := make([]byte, 0, 7*tsPacketSize)
	for {
		if _, err := io.ReadFull(r, packet); err != nil {
			return err
		}
		if packet[0] != 0x47 {
			if err := resync(r, packet); err != nil {
				return err
			}
		}
		d.consume(packet)
		datagram = append(datagram, packet...)
		if len(datagram) == cap(datagram) {
			if dst != nil {
				if _, err := dst.Write(datagram); err != nil {
					return err
				}
			}
			datagram = datagram[:0]
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

func resync(r *bufio.Reader, packet []byte) error {
	for i := 1; i < len(packet); i++ {
		if packet[i] == 0x47 {
			copy(packet, packet[i:])
			_, err := io.ReadFull(r, packet[len(packet)-i:])
			return err
		}
	}
	for {
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		if b == 0x47 {
			packet[0] = b
			_, err = io.ReadFull(r, packet[1:])
			return err
		}
	}
}

func (d *detector) consume(pkt []byte) {
	if len(pkt) != tsPacketSize || pkt[0] != 0x47 || pkt[1]&0x80 != 0 {
		return
	}
	d.firstPacket.Do(func() {
		if d.onFirstPacket != nil {
			d.onFirstPacket()
		}
	})
	pid := uint16(pkt[1]&0x1f)<<8 | uint16(pkt[2])
	pusi := pkt[1]&0x40 != 0
	afc := (pkt[3] >> 4) & 0x03
	if afc == 2 || afc == 3 {
		d.readPCR(pkt)
	}
	if afc == 0 || afc == 2 {
		return
	}
	off := 4
	if afc == 3 {
		off += 1 + int(pkt[4])
	}
	if off >= len(pkt) {
		return
	}
	payload := pkt[off:]
	b := d.sections[pid]
	if b == nil {
		b = &sectionBuffer{}
		d.sections[pid] = b
	}
	// FFmpeg may preserve SCTE-35 either as private sections or wrap each
	// section in a private_stream_1 PES packet. Support both representations.
	if pusi && len(payload) >= 9 && payload[0] == 0x00 && payload[1] == 0x00 && payload[2] == 0x01 {
		headerLength := 9 + int(payload[8])
		b.data = nil
		if headerLength > len(payload) {
			return
		}
		b.data = append(b.data, payload[headerLength:]...)
		d.processSections(pid, b)
		return
	}
	if pusi {
		if len(payload) == 0 {
			return
		}
		pointer := int(payload[0])
		if pointer > len(payload)-1 {
			b.data = nil
			return
		}
		if pointer > 0 && len(b.data) > 0 {
			b.data = append(b.data, payload[1:1+pointer]...)
			d.processSections(pid, b)
		}
		b.data = nil
		payload = payload[1+pointer:]
	}
	if len(payload) > 0 {
		b.data = append(b.data, payload...)
		d.processSections(pid, b)
	}
}

func (d *detector) processSections(pid uint16, b *sectionBuffer) {
	for len(b.data) >= 3 {
		if b.data[0] == 0xff {
			b.data = nil
			return
		}
		total := 3 + int(binary.BigEndian.Uint16(b.data[1:3])&0x0fff)
		if total < 7 || total > 4096 {
			b.data = nil
			return
		}
		if len(b.data) < total {
			return
		}
		section := append([]byte(nil), b.data[:total]...)
		b.data = b.data[total:]
		if section[0] != 0xfc || crc32MPEG(section) != 0 {
			continue
		}
		c, ok := parseSpliceInsert(pid, section)
		if !ok {
			continue
		}
		key := fmt.Sprintf("%d:%t", c.EventID, c.Out)
		now := time.Now()
		if previous, exists := d.seen[key]; exists && now.Sub(previous) < 30*time.Minute {
			continue
		}
		d.seen[key] = now
		c.ReceivedAt = now
		d.emit(c)
	}
}

func (d *detector) readPCR(pkt []byte) {
	if len(pkt) < 12 || pkt[4] < 7 || pkt[5]&0x10 == 0 {
		return
	}
	d.pcr90 = (uint64(pkt[6]) << 25) |
		(uint64(pkt[7]) << 17) |
		(uint64(pkt[8]) << 9) |
		(uint64(pkt[9]) << 1) |
		(uint64(pkt[10]) >> 7)
	d.havePCR = true
}

func (d *detector) emit(c cue) {
	if c.PTS == nil || !d.havePCR {
		d.onCue(c)
		return
	}
	deltaTicks := signedPTSDelta(*c.PTS, d.pcr90)
	// Schedule normal live pre-roll. Captured/remuxed test files may carry an
	// old SCTE clock which is unrelated to the rewritten PCR; in that case the
	// cue is intentionally acted on at packet arrival.
	if deltaTicks <= 0 || deltaTicks > 120*90000 {
		if deltaTicks > 120*90000 || deltaTicks < -2*90000 {
			log.Printf("SCTE clock differs from PCR by %.3fs; using cue arrival", float64(deltaTicks)/90000)
		}
		d.onCue(c)
		return
	}
	delay := time.Duration(float64(deltaTicks) / 90000 * float64(time.Second))
	log.Printf("SCTE event=%d scheduled in %s from PTS/PCR", c.EventID, delay.Round(time.Millisecond))
	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-d.ctx.Done():
		case <-timer.C:
			d.onCue(c)
		}
	}()
}

func signedPTSDelta(target, current uint64) int64 {
	d := (target - current) & ptsMask
	if d >= 1<<32 {
		return int64(d) - 1<<33
	}
	return int64(d)
}

func parseSpliceInsert(pid uint16, s []byte) (cue, bool) {
	var c cue
	c.PID = pid
	if len(s) < 20 || s[0] != 0xfc || s[3] != 0x00 {
		return c, false
	}
	ptsAdjustment := (uint64(s[4]&0x01) << 32) | uint64(binary.BigEndian.Uint32(s[5:9]))
	commandLength := int(uint16(s[11]&0x0f)<<8 | uint16(s[12]))
	if s[13] != 0x05 || commandLength < 6 || 14+commandLength > len(s)-4 {
		return c, false
	}
	p := 14
	c.EventID = binary.BigEndian.Uint32(s[p : p+4])
	p += 4
	if s[p]&0x80 != 0 {
		return c, false
	}
	p++
	flags := s[p]
	p++
	c.Out = flags&0x80 != 0
	programSplice := flags&0x40 != 0
	durationFlag := flags&0x20 != 0
	c.Immediate = flags&0x10 != 0

	if programSplice && !c.Immediate {
		value, n, ok := parseSpliceTime(s[p:])
		if !ok {
			return c, false
		}
		p += n
		value = (value + ptsAdjustment) & ptsMask
		c.PTS = &value
	} else if !programSplice {
		if p >= len(s) {
			return c, false
		}
		count := int(s[p])
		p++
		for i := 0; i < count; i++ {
			if p >= len(s) {
				return c, false
			}
			p++
			if !c.Immediate {
				_, n, ok := parseSpliceTime(s[p:])
				if !ok {
					return c, false
				}
				p += n
			}
		}
	}
	if durationFlag {
		if p+5 > len(s) {
			return c, false
		}
		c.AutoReturn = s[p]&0x80 != 0
		ticks := (uint64(s[p]&0x01) << 32) | uint64(binary.BigEndian.Uint32(s[p+1:p+5]))
		d := time.Duration(float64(ticks) / 90000 * float64(time.Second))
		c.Duration = &d
	}
	return c, true
}

func parseSpliceTime(b []byte) (uint64, int, bool) {
	if len(b) < 1 {
		return 0, 0, false
	}
	if b[0]&0x80 == 0 {
		return 0, 1, true
	}
	if len(b) < 5 {
		return 0, 0, false
	}
	return (uint64(b[0]&0x01) << 32) | uint64(binary.BigEndian.Uint32(b[1:5])), 5, true
}

func crc32MPEG(data []byte) uint32 {
	crc := uint32(0xffffffff)
	for _, b := range data {
		crc ^= uint32(b) << 24
		for i := 0; i < 8; i++ {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func (c *controller) handleCue(cue cue) {
	c.mu.Lock()
	defer c.mu.Unlock()
	direction := "IN"
	if cue.Out {
		direction = "OUT"
	}
	extra := ""
	if cue.PTS != nil {
		extra += fmt.Sprintf(" pts=%d", *cue.PTS)
	}
	if cue.Duration != nil {
		extra += fmt.Sprintf(" duration=%s auto_return=%t", cue.Duration.Round(time.Millisecond), cue.AutoReturn)
	}
	log.Printf("SCTE-35 %s pid=0x%04X event=%d immediate=%t%s", direction, cue.PID, cue.EventID, cue.Immediate, extra)
	if c.dryRun {
		log.Printf("DRY-RUN would CUT to %q", map[bool]string{true: c.ad, false: c.main}[cue.Out])
		return
	}
	state, err := c.state()
	if err != nil {
		log.Printf("vMix state error: %v", err)
		return
	}
	active := state.inputName(state.Active)
	if cue.Out {
		if matches(active, c.ad) || matches(state.Active, c.ad) {
			log.Printf("vMix already on %q; OUT ignored", c.ad)
			return
		}
		if c.main != "" && !matches(active, c.main) && !matches(state.Active, c.main) {
			log.Printf("SAFETY: active input is %q, expected main %q; OUT ignored", active, c.main)
			return
		}
		c.returnTo = state.Active
		if err := c.cut(c.ad); err != nil {
			log.Printf("CUT to ad failed: %v", err)
			return
		}
		c.inBreak = true
		log.Printf("CUT -> %q", c.ad)

		if cue.Duration != nil && *cue.Duration > 0 {
			dur := *cue.Duration
			target := c.returnTo
			if target == "" {
				target = c.main
			}
			go func(breakDur time.Duration, retTarget string) {
				time.Sleep(breakDur)
				c.mu.Lock()
				defer c.mu.Unlock()
				if !c.inBreak {
					return
				}
				curState, err := c.state()
				if err != nil {
					log.Printf("vMix state error on fallback return: %v", err)
					return
				}
				curActive := curState.inputName(curState.Active)
				if !matches(curActive, c.ad) && !matches(curState.Active, c.ad) {
					log.Printf("SAFETY: operator changed active input to %q; fallback return cancelled", curActive)
					c.inBreak = false
					c.returnTo = ""
					return
				}
				log.Printf("FALLBACK TIMER: break duration %s expired without SCTE-35 IN; CUT -> %q", breakDur.Round(time.Millisecond), retTarget)
				if err := c.cut(retTarget); err != nil {
					log.Printf("Fallback CUT back failed: %v", err)
					return
				}
				c.inBreak = false
				c.returnTo = ""
			}(dur, target)
		}
		return
	}
	if !c.inBreak {
		log.Printf("SAFETY: no detector-owned break; IN ignored")
		return
	}
	if !matches(active, c.ad) && !matches(state.Active, c.ad) {
		log.Printf("SAFETY: operator changed active input to %q; automatic return cancelled", active)
		c.inBreak = false
		c.returnTo = ""
		return
	}
	target := c.returnTo
	if target == "" {
		target = c.main
	}
	if err := c.cut(target); err != nil {
		log.Printf("CUT back failed: %v", err)
		return
	}
	log.Printf("CUT -> %q", target)
	c.inBreak = false
	c.returnTo = ""
}

func (c *controller) state() (vmixState, error) {
	var state vmixState
	resp, err := c.client.Get(c.baseURL + "/api")
	if err != nil {
		return state, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return state, fmt.Errorf("HTTP %s", resp.Status)
	}
	if err := xml.NewDecoder(resp.Body).Decode(&state); err != nil {
		return state, err
	}
	return state, nil
}

func (c *controller) preflight() error {
	state, err := c.state()
	if err != nil {
		return fmt.Errorf("Web API %s is unavailable: %w", c.baseURL, err)
	}
	if !state.hasInput(c.main) {
		return fmt.Errorf("main input %q was not found", c.main)
	}
	if !state.hasInput(c.ad) {
		return fmt.Errorf("advertising input %q was not found", c.ad)
	}
	log.Printf("vMix preflight OK: active=%q main=%q ad=%q", state.inputName(state.Active), c.main, c.ad)
	return nil
}

func (c *controller) cut(input string) error {
	q := url.Values{}
	q.Set("Function", "Cut")
	q.Set("Input", input)
	resp, err := c.client.Get(c.baseURL + "/api/?" + q.Encode())
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	return nil
}

func (c *controller) resetInput() error {
	q := url.Values{}
	q.Set("Function", "ResetInput")
	q.Set("Input", c.main)
	resp, err := c.client.Get(c.baseURL + "/api/?" + q.Encode())
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	return nil
}

func writeStatus(path, status string) {
	if err := os.WriteFile(path, []byte(status+"\n"), 0o600); err != nil {
		log.Printf("status file error: %v", err)
	}
}

func (s vmixState) inputName(active string) string {
	for _, in := range s.Inputs {
		if in.Number == active || in.Key == active {
			if in.ShortTitle != "" {
				return in.ShortTitle
			}
			return in.Title
		}
	}
	return active
}

func (s vmixState) hasInput(name string) bool {
	for _, in := range s.Inputs {
		if matches(in.Number, name) || matches(in.Key, name) || matches(in.Title, name) || matches(in.ShortTitle, name) {
			return true
		}
	}
	return false
}

func matches(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func redact(line, secret string) string {
	if secret == "" {
		return line
	}
	return strings.ReplaceAll(line, secret, "srt://[redacted]")
}
