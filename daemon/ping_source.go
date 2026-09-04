package main

import (
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

func (e *Engine) fetchPing(runID string, ds DataSource, params map[string]interface{}) (map[string]interface{}, error) {
	address := expandByTemplate(ds.Address, params)
	if address == "" {
		return nil, fmt.Errorf("ping source '%s' requires an address", ds.ID)
	}

	timeout := 2 * time.Second
	if ds.Timeout != "" {
		if d, err := time.ParseDuration(ds.Timeout); err == nil {
			timeout = d
		}
	}

	count := ds.Count
	if count <= 0 {
		count = 1
	}

	dst, err := net.ResolveIPAddr("ip4", address)
	if err != nil {
		return map[string]interface{}{
			"reachable":   false,
			"rtt_ms":      0.0,
			"rtt_min_ms":  0.0,
			"rtt_max_ms":  0.0,
			"packet_loss": 1.0,
		}, nil
	}

	c, err := icmp.ListenPacket("ip4:icmp", "")
	if err != nil {
		return nil, fmt.Errorf("ping: failed to open raw socket (requires CAP_NET_RAW): %v", err)
	}
	defer c.Close()

	id := os.Getpid() & 0xffff
	sent := 0
	received := 0
	var totalRTT time.Duration
	var minRTT, maxRTT time.Duration

	for seq := 1; seq <= count; seq++ {
		msg := icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Code: 0,
			Body: &icmp.Echo{
				ID:   id,
				Seq:  seq,
				Data: []byte("automagic"),
			},
		}
		wb, err := msg.Marshal(nil)
		if err != nil {
			continue
		}

		sent++
		start := time.Now()
		if _, err := c.WriteTo(wb, dst); err != nil {
			continue
		}

		c.SetReadDeadline(time.Now().Add(timeout))
		rb := make([]byte, 1500)
		for {
			n, _, err := c.ReadFrom(rb)
			if err != nil {
				break
			}
			rm, err := icmp.ParseMessage(1, rb[:n])
			if err != nil {
				continue
			}
			echo, ok := rm.Body.(*icmp.Echo)
			if ok && rm.Type == ipv4.ICMPTypeEchoReply && echo.ID == id && echo.Seq == seq {
				rtt := time.Since(start)
				received++
				totalRTT += rtt
				if received == 1 || rtt < minRTT {
					minRTT = rtt
				}
				if rtt > maxRTT {
					maxRTT = rtt
				}
				break
			}
		}
	}

	reachable := received > 0
	var avgMs, minMs, maxMs float64
	if received > 0 {
		avgMs = float64(totalRTT) / float64(received) / float64(time.Millisecond)
		minMs = float64(minRTT) / float64(time.Millisecond)
		maxMs = float64(maxRTT) / float64(time.Millisecond)
	}

	return map[string]interface{}{
		"reachable":   reachable,
		"rtt_ms":      avgMs,
		"rtt_min_ms":  minMs,
		"rtt_max_ms":  maxMs,
		"packet_loss": float64(sent-received) / float64(sent),
	}, nil
}
