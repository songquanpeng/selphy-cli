package cpnp

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"time"
)

type Device struct {
	Host     string `json:"host"`
	MAC      string `json:"mac"`
	Protocol string `json:"protocol"`
}

// Discover sends only NIC-info queries, never opens a print session.
func Discover(ctx context.Context, hosts []string, port int) ([]Device, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if len(hosts) == 0 {
		hosts = discoveryTargets()
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetReadDeadline(time.Now()) })
	defer stop()
	send := func() {
		for _, host := range hosts {
			addr, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(host, strconv.Itoa(port)))
			if err == nil {
				for _, cmd := range []byte{1, 3} {
					_, _ = conn.WriteToUDP(encode(cmd, 1, 0, nil), addr)
				}
			}
		}
	}
	send()
	results := map[string]Device{}
	retry := time.Now().Add(500 * time.Millisecond)
	for ctx.Err() == nil {
		_ = conn.SetReadDeadline(retry)
		b := make([]byte, 65535)
		n, addr, err := conn.ReadFromUDP(b)
		if err != nil {
			if e, ok := err.(net.Error); ok && e.Timeout() {
				send()
				retry = time.Now().Add(time.Second)
				continue
			}
			return nil, err
		}
		p, err := decode(b[:n])
		if err != nil || (p.Command != 1 && p.Command != 3) || p.ID != 1 || len(p.Payload) < 16 {
			continue
		}
		host := addr.IP.String()
		results[host] = Device{Host: host, MAC: net.HardwareAddr(p.Payload[6:12]).String(), Protocol: "cpnp"}
	}
	devices := make([]Device, 0, len(results))
	for _, d := range results {
		devices = append(devices, d)
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Host < devices[j].Host })
	if ctx.Err() != nil && ctx.Err() != context.DeadlineExceeded {
		return nil, ctx.Err()
	}
	return devices, nil
}
func discoveryTargets() []string {
	result := []string{"192.168.0.1", "255.255.255.255"}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok || n.IP.IsLoopback() || n.IP.To4() == nil {
			continue
		}
		ip := n.IP.To4()
		if len(n.Mask) != 4 {
			continue
		}
		b := make(net.IP, 4)
		for i := range b {
			b[i] = ip[i] | ^n.Mask[i]
		}
		result = append(result, b.String())
	}
	return result
}

func (c *Client) NIC(ctx context.Context) (Device, error) {
	var last error
	for _, cmd := range []byte{1, 3} {
		p, err := c.udp(ctx, cmd, 0, nil)
		if err != nil {
			last = err
			continue
		}
		if len(p.Payload) < 16 {
			return Device{}, fmt.Errorf("short NIC response")
		}
		return Device{c.Host, net.HardwareAddr(p.Payload[6:12]).String(), "cpnp"}, nil
	}
	return Device{}, last
}
