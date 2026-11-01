package cpnp

import (
	"context"
	"fmt"
	"time"
)

type Page struct {
	JPEG          []byte
	Width, Height int
	Size          uint16
	CP            bool
	Optimize      bool
}

func StartPage(p Page) []byte {
	if !p.CP {
		b := command(0, 64)
		le.PutUint16(b[12:], 1)
		le.PutUint16(b[14:], p.Size)
		b[16] = 2
		return b
	}
	b := command(7, 192)
	le.PutUint16(b[12:], 1)
	le.PutUint16(b[14:], p.Size)
	b[16], b[17], b[18], b[19] = 1, 2, 2, 2
	if p.Optimize {
		b[17] = 3
	}
	le.PutUint32(b[32:], uint32(len(p.JPEG)))
	return b
}
func Transfer(p Page, offset, length uint32) ([]byte, error) {
	if length == 0 || uint64(offset)+uint64(length) > uint64(len(p.JPEG)) || length > 1<<20 {
		return nil, fmt.Errorf("printer requested invalid JPEG range %d+%d (file %d)", offset, length, len(p.JPEG))
	}
	b := command(1, 104+int(length))
	le.PutUint32(b[12:], 1)
	le.PutUint32(b[20:], uint32(len(p.JPEG)))
	le.PutUint32(b[24:], uint32(p.Width))
	le.PutUint32(b[28:], uint32(p.Height))
	if p.CP {
		b[32] = 2
	}
	le.PutUint32(b[96:], offset)
	le.PutUint32(b[100:], length)
	copy(b[104:], p.JPEG[int(offset):int(offset)+int(length)])
	return b, nil
}

type PrintResult struct {
	State string `json:"state"`
	Bytes int    `json:"jpeg_bytes"`
}

// Print sends one page. A successful CP result means spool acceptance, matching
// the Android app's IMAGE_SENT state. QX waits for END_PRINT.
func (c *Client) Print(ctx context.Context, p Page, progress func(ConnectedStatus)) (result PrintResult, err error) {
	s, err := c.Open(ctx)
	if err != nil {
		return result, err
	}
	defer func() {
		closeErr := s.Close()
		if err == nil && closeErr != nil {
			err = fmt.Errorf("job accepted but session cleanup failed: %w", closeErr)
		}
	}()
	initial, err := s.Ready(ctx)
	if err != nil {
		return result, err
	}
	expected := byte(2)
	if p.CP {
		expected = 1
	}
	if initial.DeviceType != expected {
		return result, fmt.Errorf("printer reports device type %d; requested page expects %d", initial.DeviceType, expected)
	}
	if initial.ErrorCode != 0 {
		return result, fmt.Errorf("printer: %s", initial.Error)
	}
	started := false
	defer func() {
		if !started {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), c.Timeout)
		defer cancel()
		if err != nil {
			_ = s.Write(cleanup, command(2, 64))
		}
		if !p.CP || err != nil {
			_ = s.Write(cleanup, command(3, 64))
		}
	}()
	// Mark started before writing: a lost acknowledgement can still mean accepted.
	started = true
	if err = s.Write(ctx, StartPage(p)); err != nil {
		return result, err
	}
	var previous ConnectedStatus
	sent := false
	for {
		st, e := s.Status(ctx)
		if e != nil {
			return result, e
		}
		if progress != nil && st != previous {
			progress(st)
		}
		if st.ErrorCode != 0 {
			return result, fmt.Errorf("printer: %s (0x%x)", st.Error, st.ErrorCode)
		}
		if st.StateCode == 240 || st.StateCode == 15 {
			return result, fmt.Errorf("printer ended session before job acceptance")
		}
		req := st.Request
		if p.CP && sent && (req == 0x70000 || req == 0x10000) {
			// CP1500 begins printing after ExecuteSpoolPrint, then may drop the
			// connection. Do not claim physical completion from spool acceptance.
			if req == 0x70000 {
				if err = s.Write(ctx, command(8, 64)); err != nil {
					return result, err
				}
			}
			return PrintResult{State: "submitted", Bytes: len(p.JPEG)}, nil
		}
		if req == 0x30000 {
			if !sent {
				return result, fmt.Errorf("printer ended job without requesting image data")
			}
			return PrintResult{State: "completed", Bytes: len(p.JPEG)}, nil
		}
		isData := req == 0x20000
		if p.CP {
			isData = req&0xffffff00 == 0x20000
		}
		if isData && (st.Request != previous.Request || st.Retry != previous.Retry || st.Offset != previous.Offset || st.Length != previous.Length) {
			if p.CP && req&255 != 0 {
				return result, fmt.Errorf("unexpected page index %d for single-page job", req&255)
			}
			b, e := Transfer(p, st.Offset, st.Length)
			if e != nil {
				return result, e
			}
			if err = s.Write(ctx, b); err != nil {
				return result, err
			}
			sent = true
		} else if req|255 == 0x2ffff {
			return result, fmt.Errorf("printer requested custom overcoat data for a glossy job")
		} else if req == 0x40000 {
			return result, fmt.Errorf("printer canceled the job")
		}
		previous = st
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
