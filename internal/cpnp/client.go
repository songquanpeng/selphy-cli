package cpnp

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf16"
)

type Client struct {
	Host     string
	Port     int
	Timeout  time.Duration
	sequence atomic.Uint32
}

func New(host string, timeout time.Duration) *Client {
	return &Client{Host: host, Port: Port, Timeout: timeout}
}
func (c *Client) next() uint16    { return uint16((c.sequence.Add(1)-1)%65535 + 1) }
func (c *Client) address() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) }
func (c *Client) deadline(ctx context.Context) time.Time {
	t := time.Now().Add(c.Timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(t) {
		return d
	}
	return t
}

func (c *Client) udp(ctx context.Context, cmd byte, session uint16, payload []byte) (packet, error) {
	id := c.next()
	conn, err := (&net.Dialer{}).DialContext(ctx, "udp4", c.address())
	if err != nil {
		return packet{}, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	data := encode(cmd, id, session, payload)
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return packet{}, err
		}
		_ = conn.SetDeadline(c.deadline(ctx))
		if _, err = conn.Write(data); err != nil {
			return packet{}, err
		}
		b := make([]byte, 65535)
		for {
			n, err := conn.Read(b)
			if err != nil {
				last = err
				break
			}
			p, err := decode(b[:n])
			if p.Command != cmd || p.ID != id {
				continue
			}
			if err != nil {
				return packet{}, err
			}
			return p, nil
		}
	}
	return packet{}, fmt.Errorf("%s UDP command 0x%02x: %w; connect this computer to the printer's Wi-Fi (QX) or the same network (CP)", c.address(), cmd, last)
}

func (c *Client) DeviceID(ctx context.Context) (map[string]string, error) {
	p, err := c.udp(ctx, 0x30, 0, make([]byte, 4))
	if err != nil {
		return nil, err
	}
	if len(p.Payload) < 2 {
		return nil, fmt.Errorf("device ID response too short")
	}
	result := map[string]string{}
	for _, part := range strings.Split(strings.TrimRight(string(p.Payload[2:]), "\x00"), ";") {
		if k, v, ok := strings.Cut(part, ":"); ok {
			result[k] = v
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("empty device ID")
	}
	return result, nil
}

func (c *Client) Status(ctx context.Context) (Status, error) {
	p, err := c.udp(ctx, 0x20, 0, nil)
	if err != nil {
		return Status{}, err
	}
	return ParseStatus(p.Payload)
}

type Session struct {
	client   *Client
	conn     net.Conn
	id       uint16
	maxWrite int
}

// Open negotiates UDP ownership before opening the printer-assigned TCP port.
func (c *Client) Open(ctx context.Context) (*Session, error) {
	b := make([]byte, 392)
	putUTF16 := func(offset int, s string) {
		for i, v := range utf16.Encode([]rune(s)) {
			if i >= 32 {
				break
			}
			be.PutUint16(b[offset+i*2:], v)
		}
	}
	putUTF16(8, "selphy-cli")
	putUTF16(72, "SPL v2.0")
	putUTF16(136, "Square")
	p, err := c.udp(ctx, 0x10, 0, b)
	if err != nil {
		return nil, err
	}
	if p.Session == 0 || len(p.Payload) < 6 || be.Uint16(p.Payload[4:]) == 0 {
		return nil, fmt.Errorf("invalid session negotiation response")
	}
	s := &Session{client: c, id: p.Session, maxWrite: 4096}
	conn, err := (&net.Dialer{Timeout: c.Timeout}).DialContext(ctx, "tcp4", net.JoinHostPort(c.Host, strconv.Itoa(int(be.Uint16(p.Payload[4:])))))
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	s.conn = conn
	desired := make([]byte, 4)
	be.PutUint32(desired, 33792)
	if _, err = s.exchange(ctx, 0x52, desired); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("negotiate write size: %w", err)
	}
	p, err = s.exchange(ctx, 0x51, nil)
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	if len(p.Payload) < 4 {
		_ = s.Close()
		return nil, fmt.Errorf("missing maximum write size")
	}
	n := int(be.Uint32(p.Payload))
	if n < 1 || n > maxPayload {
		_ = s.Close()
		return nil, fmt.Errorf("invalid maximum write size: %d", n)
	}
	s.maxWrite = min(n, 33792)
	return s, nil
}

func (s *Session) exchange(ctx context.Context, cmd byte, b []byte) (packet, error) {
	if err := ctx.Err(); err != nil {
		return packet{}, err
	}
	_ = s.conn.SetDeadline(s.client.deadline(ctx))
	stop := context.AfterFunc(ctx, func() { _ = s.conn.SetDeadline(time.Now()) })
	defer stop()
	id := s.client.next()
	data := encode(cmd, id, s.id, b)
	if _, err := io.Copy(s.conn, strings.NewReader(string(data))); err != nil {
		return packet{}, err
	}
	p, err := readPacket(s.conn)
	if err != nil {
		return packet{}, err
	}
	if p.Command != cmd || p.ID != id || p.Session != s.id {
		return packet{}, fmt.Errorf("CPNP response command, packet ID or session mismatch")
	}
	return p, nil
}

func (s *Session) Write(ctx context.Context, b []byte) error {
	for len(b) > 0 {
		n := min(len(b), s.maxWrite)
		p, err := s.exchange(ctx, 0x21, b[:n])
		if err != nil {
			return err
		}
		if len(p.Payload) < 4 {
			return fmt.Errorf("missing write acknowledgement")
		}
		accepted := int(be.Uint32(p.Payload))
		if accepted < 0 || accepted > n {
			return fmt.Errorf("invalid write acknowledgement: %d > %d", accepted, n)
		}
		b = b[accepted:]
		if accepted == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	return nil
}
func (s *Session) Status(ctx context.Context) (ConnectedStatus, error) {
	p, err := s.exchange(ctx, 0x20, nil)
	if err != nil {
		return ConnectedStatus{}, err
	}
	return ParseConnected(p.Payload)
}

// Ready polls through the transient NO_SESSION/idle warm-up observed on CP1500.
// It does not send StartPrint or StartSpool.
func (s *Session) Ready(ctx context.Context) (ConnectedStatus, error) {
	for {
		st, err := s.Status(ctx)
		if err != nil {
			return st, err
		}
		if st.ErrorCode != 0 {
			return st, fmt.Errorf("printer: %s", st.Error)
		}
		if st.Request == 0x10000 {
			return st, nil
		}
		select {
		case <-ctx.Done():
			return st, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
func (s *Session) Close() error {
	if s.conn != nil {
		_ = s.conn.Close()
	}
	if s.id == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.client.Timeout)
	defer cancel()
	_, err := s.client.udp(ctx, 0x11, s.id, nil)
	s.id = 0
	return err
}
