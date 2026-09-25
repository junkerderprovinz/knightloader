package testenv

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"

	"golang.org/x/net/dns/dnsmessage"
)

// NoDNS makes the test binary answer its own name lookups: localhost is the
// loopback address and every other name does not exist. Call it from TestMain,
// before any test resolves a name.
//
// Tests stage links on reserved names such as host.example, and the app looks
// them up as it would a real host. The answer is always "no such host", but a
// workstation's resolver can take ten seconds to give it, and Close waits for
// every lookup still running. Answering in process also keeps a test that
// wires up a real service, such as a debrid account, from reaching the
// internet.
func NoDNS() {
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: dialLocalDNS}
}

// dialLocalDNS hands the resolver one end of an in-memory pipe and answers on
// the other, whichever server the resolver meant to ask.
func dialLocalDNS(context.Context, string, string) (net.Conn, error) {
	client, server := net.Pipe()
	go serveDNS(server)
	return client, nil
}

// serveDNS answers queries in the TCP framing, a two byte length before each
// message, which the resolver uses on any connection that is not a
// net.PacketConn.
func serveDNS(c net.Conn) {
	defer c.Close()
	for {
		var size [2]byte
		if _, err := io.ReadFull(c, size[:]); err != nil {
			return
		}
		query := make([]byte, binary.BigEndian.Uint16(size[:]))
		if _, err := io.ReadFull(c, query); err != nil {
			return
		}
		reply, err := answerDNS(query)
		if err != nil {
			return
		}
		framed := binary.BigEndian.AppendUint16(nil, uint16(len(reply)))
		if _, err := c.Write(append(framed, reply...)); err != nil {
			return
		}
	}
}

func answerDNS(query []byte) ([]byte, error) {
	var p dnsmessage.Parser
	h, err := p.Start(query)
	if err != nil {
		return nil, err
	}
	q, err := p.Question()
	if err != nil {
		return nil, err
	}
	local := strings.EqualFold(q.Name.String(), "localhost.")
	rcode := dnsmessage.RCodeNameError
	if local {
		rcode = dnsmessage.RCodeSuccess
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{
		ID: h.ID, Response: true, Authoritative: true, RecursionDesired: h.RecursionDesired,
		RecursionAvailable: true, RCode: rcode,
	})
	b.EnableCompression()
	if err := b.StartQuestions(); err != nil {
		return nil, err
	}
	if err := b.Question(q); err != nil {
		return nil, err
	}
	if err := b.StartAnswers(); err != nil {
		return nil, err
	}
	rh := dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: 60}
	switch {
	case local && q.Type == dnsmessage.TypeA:
		err = b.AResource(rh, dnsmessage.AResource{A: [4]byte{127, 0, 0, 1}})
	case local && q.Type == dnsmessage.TypeAAAA:
		err = b.AAAAResource(rh, dnsmessage.AAAAResource{AAAA: [16]byte(net.IPv6loopback)})
	}
	if err != nil {
		return nil, err
	}
	return b.Finish()
}
