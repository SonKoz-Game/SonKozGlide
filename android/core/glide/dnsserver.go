package glide

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
)

const dnsServeIdle = 10 * time.Second

func (t *Tunnel) handleDNS(ctx context.Context, query []byte) []byte {
	if len(query) < 12 {
		return nil
	}
	reply, err := t.resolver.exchange(ctx, query)
	if err != nil {
		return servFail(query)
	}
	return reply
}

func (t *Tunnel) serveDNSUDP(conn *gonet.UDPConn) {
	defer conn.Close()
	buf := make([]byte, 1500)
	for {
		conn.SetReadDeadline(time.Now().Add(dnsServeIdle))
		n, err := conn.Read(buf)
		if n > 0 {
			if reply := t.handleDNS(context.Background(), buf[:n]); reply != nil {
				conn.SetWriteDeadline(time.Now().Add(dnsServeIdle))
				conn.Write(reply)
			}
		}
		if err != nil {
			return
		}
	}
}

func (t *Tunnel) serveDNSTCP(conn net.Conn) {
	defer conn.Close()
	header := make([]byte, 2)
	for {
		conn.SetReadDeadline(time.Now().Add(dnsServeIdle))
		if _, err := io.ReadFull(conn, header); err != nil {
			return
		}
		size := int(binary.BigEndian.Uint16(header))
		if size == 0 || size > dnsMessageMax {
			return
		}
		query := make([]byte, size)
		if _, err := io.ReadFull(conn, query); err != nil {
			return
		}
		reply := t.handleDNS(context.Background(), query)
		if reply == nil {
			return
		}
		out := make([]byte, 2+len(reply))
		binary.BigEndian.PutUint16(out, uint16(len(reply)))
		copy(out[2:], reply)
		conn.SetWriteDeadline(time.Now().Add(dnsServeIdle))
		if _, err := conn.Write(out); err != nil {
			return
		}
	}
}

func servFail(query []byte) []byte {
	var parser dnsmessage.Parser
	header, err := parser.Start(query)
	if err != nil {
		return nil
	}
	questions, err := parser.AllQuestions()
	if err != nil {
		questions = nil
	}
	out := dnsmessage.Header{
		ID:                 header.ID,
		Response:           true,
		OpCode:             header.OpCode,
		RecursionDesired:   header.RecursionDesired,
		RecursionAvailable: true,
		RCode:              dnsmessage.RCodeServerFailure,
	}
	builder := dnsmessage.NewBuilder(make([]byte, 0, 128), out)
	if builder.StartQuestions() != nil {
		return nil
	}
	for _, q := range questions {
		if builder.Question(q) != nil {
			return nil
		}
	}
	reply, err := builder.Finish()
	if err != nil {
		return nil
	}
	return reply
}
