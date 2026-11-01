package cpnp

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestPacketGoldenAndMalformed(t *testing.T) {
	want, _ := hex.DecodeString("43504e5001300000000200000000000400000000")
	if got := encode(0x30, 2, 0, make([]byte, 4)); !bytes.Equal(got, want) {
		t.Fatalf("device-ID request: %x", got)
	}
	want, _ = hex.DecodeString("00000000400000000000000001000400020000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000")
	if got := StartPage(Page{Size: 4}); !bytes.Equal(got, want) {
		t.Fatalf("start print: %x", got)
	}
	good := encode(0x20, 7, 3, []byte{1, 2, 3})
	good[4] = 0x81
	for i := 0; i < len(good); i++ {
		if _, err := decode(good[:i]); err == nil {
			t.Fatalf("accepted truncated packet at %d", i)
		}
	}
	if _, err := readPacket(bytes.NewReader(append([]byte("CPNP\x81\x20\x00\x00\x00\x07\x00\x03"), 0xff, 0xff, 0xff, 0xff))); err == nil {
		t.Fatal("accepted oversized payload")
	}
	p, err := readPacket(&oneByteReader{good})
	if err != nil || !bytes.Equal(p.Payload, []byte{1, 2, 3}) {
		t.Fatalf("fragmented TCP response: %+v %v", p, err)
	}
}

type oneByteReader struct{ data []byte }

func (r *oneByteReader) Read(b []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	b[0] = r.data[0]
	r.data = r.data[1:]
	return 1, nil
}

func TestTransferRangeValidation(t *testing.T) {
	p := Page{JPEG: []byte("abcdef"), Width: 1248, Height: 1872, CP: true}
	b, err := Transfer(p, 2, 3)
	if err != nil || string(b[104:]) != "cde" {
		t.Fatalf("%x %v", b, err)
	}
	if le.Uint32(b[4:]) != 107 || le.Uint32(b[20:]) != 6 || le.Uint32(b[96:]) != 2 || le.Uint32(b[100:]) != 3 || b[32] != 2 {
		t.Fatalf("bad transfer fields: %x", b)
	}
	for _, r := range [][2]uint32{{0, 0}, {6, 1}, {0xffffffff, 2}, {2, 0xffffffff}} {
		if _, err := Transfer(p, r[0], r[1]); err == nil {
			t.Fatalf("accepted range %v", r)
		}
	}
}

// A printer simulator that does actual UDP/TCP I/O, fragments responses, accepts
// only 137 bytes per write, and asks for separate/repeated JPEG ranges.
type simulator struct {
	t          *testing.T
	udp        *net.UDPConn
	tcp        net.Listener
	wg         sync.WaitGroup
	cp         bool
	jpeg       []byte
	received   []byte
	commands   []uint16
	zeroAck    bool
	errorCode  uint32
	badRange   bool
	reject     bool
	neverReady bool
}

func newSimulator(t *testing.T, cp bool) *simulator {
	t.Helper()
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &simulator{t: t, udp: udp, tcp: tcp, cp: cp, jpeg: bytes.Repeat([]byte{0xab, 0xcd, 0xef}, 7000)}
	return s
}
func (s *simulator) run()   { s.wg.Add(2); go s.serveUDP(); go s.serveTCP() }
func (s *simulator) close() { s.udp.Close(); s.tcp.Close(); s.wg.Wait() }
func (s *simulator) client() *Client {
	c := New("127.0.0.1", 300*time.Millisecond)
	c.Port = s.udp.LocalAddr().(*net.UDPAddr).Port
	return c
}
func response(req []byte, payload []byte) []byte {
	b := encode(req[5], be.Uint16(req[8:]), be.Uint16(req[10:]), payload)
	b[4] = 0x81
	return b
}
func (s *simulator) serveUDP() {
	defer s.wg.Done()
	for {
		b := make([]byte, 65535)
		n, addr, err := s.udp.ReadFromUDP(b)
		if err != nil {
			return
		}
		b = b[:n]
		if n < 16 {
			continue
		}
		var payload []byte
		switch b[5] {
		case 1, 3:
			payload = make([]byte, 16)
			copy(payload[6:], []byte{0, 1, 2, 3, 4, 5})
			copy(payload[12:], []byte{127, 0, 0, 1})
		case 0x10:
			if n != 408 || be.Uint32(b[12:]) != 392 {
				s.t.Errorf("invalid session size: %d", n)
			}
			payload = make([]byte, 6)
			be.PutUint16(payload[4:], uint16(s.tcp.Addr().(*net.TCPAddr).Port))
		case 0x11:
		case 0x30:
			payload = append([]byte{0, 25}, []byte("MFG:Canon;MDL:SELPHY CP1500;CMD:CPNP;")...)
		case 0x20:
			payload = make([]byte, 512)
			payload[0], payload[2], payload[3] = 1, 4, 4
			le.PutUint16(payload[132:], 16)
			le.PutUint16(payload[134:], 16)
			le.PutUint16(payload[136:], 4096)
			le.PutUint16(payload[138:], 4096)
			le.PutUint32(payload[144:], 1)
		default:
			s.t.Errorf("unknown UDP cmd: %x", b[5])
			continue
		}
		r := response(b, payload)
		if b[5] == 0x10 {
			be.PutUint16(r[10:], 42)
			if s.reject {
				be.PutUint16(r[6:], 0x8502)
			}
		}
		_, _ = s.udp.WriteToUDP(r, addr)
	}
}
func (s *simulator) serveTCP() {
	defer s.wg.Done()
	for {
		conn, err := s.tcp.Accept()
		if err != nil {
			return
		}
		s.serveConnection(conn)
	}
}
func (s *simulator) serveConnection(conn net.Conn) {
	var err error
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	var assembled []byte
	requestIndex := 0
	started := false
	zeroSent := false
	warmup := 0
	for {
		h := make([]byte, 16)
		if _, err = io.ReadFull(conn, h); err != nil {
			return
		}
		n := be.Uint32(h[12:])
		if n > 2<<20 {
			s.t.Error("oversized request")
			return
		}
		b := make([]byte, n)
		if _, err = io.ReadFull(conn, b); err != nil {
			return
		}
		var payload []byte
		switch h[5] {
		case 0x52:
			if len(b) != 4 || be.Uint32(b) != 33792 {
				s.t.Errorf("bad write negotiation: %x", b)
			}
		case 0x51:
			payload = []byte{0, 0, 4, 0}
		case 0x20:
			payload = make([]byte, 64)
			payload[2] = 2
			if s.cp {
				payload[2] = 1
			}
			req := uint32(0x10000)
			if !started && (warmup < 3 || s.neverReady) {
				req = 0
				if warmup == 0 {
					le.PutUint16(payload[8:], 15)
				}
				warmup++
			}
			if started {
				// Duplicate request 0 tests suppression. Retry of offset 9000 must be sent.
				offsets := []uint32{0, 0, 9000, 9000, 18000}
				lengths := []uint32{9000, 9000, 9000, 9000, 3000}
				if requestIndex < len(offsets) {
					req = 0x20000
					le.PutUint32(payload[24:], offsets[requestIndex])
					le.PutUint32(payload[28:], lengths[requestIndex])
					if requestIndex == 3 {
						le.PutUint32(payload[20:], 1)
					}
					if s.badRange {
						le.PutUint32(payload[24:], uint32(len(s.jpeg)+1))
					}
					requestIndex++
				} else {
					req = 0x30000
					if s.cp {
						req = 0x70000
					}
				}
				le.PutUint32(payload[12:], s.errorCode)
			}
			le.PutUint32(payload[16:], req)
		case 0x21:
			accepted := min(len(b), 137)
			if s.zeroAck && !zeroSent {
				accepted = 0
				zeroSent = true
			}
			assembled = append(assembled, b[:accepted]...)
			for len(assembled) >= 8 && len(assembled) >= int(le.Uint32(assembled[4:])) {
				size := int(le.Uint32(assembled[4:]))
				if size < 8 || size > 1<<20 {
					s.t.Error("invalid print command size")
					return
				}
				cmd := assembled[:size]
				code := le.Uint16(cmd[2:])
				s.commands = append(s.commands, code)
				switch code {
				case 0, 7:
					started = true
					if s.cp && (size != 192 || le.Uint32(cmd[32:]) != uint32(len(s.jpeg)) || cmd[16] != 1 || cmd[17] != 3 || cmd[18] != 2 || cmd[19] != 2) {
						s.t.Errorf("invalid CP spool header: %x", cmd)
					}
				case 1:
					off, l := le.Uint32(cmd[96:]), le.Uint32(cmd[100:])
					if int(off+l) > len(s.jpeg) || !bytes.Equal(cmd[104:], s.jpeg[off:off+l]) {
						s.t.Error("JPEG payload corrupted")
					}
					if len(s.received) == 0 {
						s.received = make([]byte, len(s.jpeg))
					}
					copy(s.received[off:], cmd[104:])
				case 2, 3, 8:
				default:
					s.t.Errorf("unknown print command %d", code)
				}
				assembled = assembled[size:]
			}
			payload = make([]byte, 4)
			be.PutUint32(payload, uint32(accepted))
		default:
			s.t.Errorf("unknown TCP command %x", h[5])
			return
		}
		r := response(h, payload)
		// TCP reads must handle split headers and split payloads.
		for len(r) > 0 {
			n := min(7, len(r))
			if _, err = conn.Write(r[:n]); err != nil {
				return
			}
			r = r[n:]
		}
	}
}

func TestPrintFullNetworkWorkflow(t *testing.T) {
	for _, cp := range []bool{true, false} {
		t.Run(fmt.Sprint("CP=", cp), func(t *testing.T) {
			s := newSimulator(t, cp)
			s.zeroAck = true
			s.run()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			c := s.client()
			status, err := c.Status(ctx)
			if err != nil || status.Tray != "cp-postcard" || !status.JPEG {
				t.Fatalf("status: %+v %v", status, err)
			}
			id, err := c.DeviceID(ctx)
			if err != nil || id["MDL"] != "SELPHY CP1500" {
				t.Fatalf("identity: %+v %v", id, err)
			}
			result, err := c.Print(ctx, Page{s.jpeg, 1248, 1872, 4, cp, true}, nil)
			s.close()
			if err != nil {
				t.Fatal(err)
			}
			want := "completed"
			if cp {
				want = "submitted"
			}
			if result.State != want {
				t.Fatalf("state: %s", result.State)
			}
			if !bytes.Equal(s.received, s.jpeg) {
				t.Fatal("JPEG was not transferred intact")
			}
			transfers := 0
			for _, cmd := range s.commands {
				if cp && cmd == 8 {
					t.Fatal("single-page CP job must close session at final spool request, not send ExecuteSpoolPrint")
				}
				if cmd == 1 {
					transfers++
				}
			}
			if transfers != 4 {
				t.Fatalf("expected 4 transfers including retry but excluding duplicate; got %d (%v)", transfers, s.commands)
			}
		})
	}
}
func TestPrintRejectsBadRangesAndPrinterErrors(t *testing.T) {
	for _, kind := range []string{"range", "printer-error", "busy"} {
		t.Run(kind, func(t *testing.T) {
			s := newSimulator(t, true)
			s.badRange = kind == "range"
			if kind == "printer-error" {
				s.errorCode = 65793
			}
			s.reject = kind == "busy"
			s.run()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, err := s.client().Print(ctx, Page{s.jpeg, 1248, 1872, 4, true, true}, nil)
			s.close()
			if err == nil {
				t.Fatal("expected failure")
			}
			if kind != "busy" {
				found := false
				for _, c := range s.commands {
					if c == 2 {
						found = true
					}
				}
				if !found {
					t.Fatal("failed job did not send cancel")
				}
			}
		})
	}
}
func TestDiscovery(t *testing.T) {
	s := newSimulator(t, true)
	s.run()
	defer s.close()
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	ds, err := Discover(ctx, []string{"127.0.0.1"}, s.client().Port)
	if err != nil || len(ds) != 1 || ds[0].MAC != "00:01:02:03:04:05" {
		t.Fatalf("%+v %v", ds, err)
	}
}
func TestStatusBoundsAndErrors(t *testing.T) {
	for i := 0; i < 512; i++ {
		if _, err := ParseStatus(make([]byte, i)); err == nil {
			t.Fatalf("short status accepted: %d", i)
		}
	}
	b := make([]byte, 512)
	le.PutUint32(b[68:], 16711940)
	st, err := ParseStatus(b)
	if err != nil || st.Error != "paper jam" {
		t.Fatalf("%+v %v", st, err)
	}
	if _, err := ParseConnected(make([]byte, 63)); err == nil {
		t.Fatal("short connected status accepted")
	}
}

func TestTwoConsecutiveCPJobs(t *testing.T) {
	s := newSimulator(t, true)
	s.run()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var failures []error
	for i := 0; i < 2; i++ {
		_, err := s.client().Print(ctx, Page{s.jpeg, 1248, 1872, 4, true, true}, nil)
		if err != nil {
			failures = append(failures, err)
			break
		}
	}
	s.close()
	if len(failures) > 0 {
		t.Fatal(failures)
	}
	starts := 0
	for _, c := range s.commands {
		if c == 7 {
			starts++
		}
		if c == 8 {
			t.Fatal("terminal spool request must be completed by session close")
		}
	}
	if starts != 2 {
		t.Fatalf("got %d jobs", starts)
	}
}

func TestUnreadySessionSendsNoJob(t *testing.T) {
	s := newSimulator(t, true)
	s.neverReady = true
	s.run()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := s.client().Print(ctx, Page{s.jpeg, 1248, 1872, 4, true, true}, nil)
	s.close()
	if err == nil {
		t.Fatal("unready session must fail")
	}
	if len(s.commands) != 0 {
		t.Fatalf("unready session sent print commands: %v", s.commands)
	}
}
