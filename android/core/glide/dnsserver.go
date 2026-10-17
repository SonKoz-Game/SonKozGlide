package glide

import (
	"context"

	"golang.org/x/net/dns/dnsmessage"
)

func (t *Tunnel) handleDNS(ctx context.Context, query []byte) []byte {
	var parser dnsmessage.Parser
	header, err := parser.Start(query)
	if err != nil {
		return nil
	}
	question, err := parser.Question()
	if err != nil {
		return t.forwardDNS(ctx, query)
	}
	name := trimName(question.Name.String())
	if (question.Type == dnsmessage.TypeA || question.Type == dnsmessage.TypeAAAA) && t.rules.match(name) {
		pair := t.fakePool.assign(name)
		return t.fakeReply(header, question, pair)
	}
	return t.forwardDNS(ctx, query)
}

func (t *Tunnel) forwardDNS(ctx context.Context, query []byte) []byte {
	reply, err := t.resolver.exchange(ctx, query)
	if err != nil {
		return servFail(query)
	}
	return reply
}

func (t *Tunnel) fakeReply(reqHeader dnsmessage.Header, question dnsmessage.Question, pair fakePair) []byte {
	header := dnsmessage.Header{
		ID:                 reqHeader.ID,
		Response:           true,
		OpCode:             reqHeader.OpCode,
		RecursionDesired:   reqHeader.RecursionDesired,
		RecursionAvailable: true,
		RCode:              dnsmessage.RCodeSuccess,
	}
	builder := dnsmessage.NewBuilder(make([]byte, 0, 128), header)
	builder.EnableCompression()
	if builder.StartQuestions() != nil || builder.Question(question) != nil {
		return nil
	}
	if builder.StartAnswers() != nil {
		return nil
	}
	rh := dnsmessage.ResourceHeader{
		Name:  question.Name,
		Class: dnsmessage.ClassINET,
		TTL:   60,
	}
	switch question.Type {
	case dnsmessage.TypeA:
		var a dnsmessage.AResource
		copy(a.A[:], pair.v4.AsSlice())
		if builder.AResource(rh, a) != nil {
			return nil
		}
	case dnsmessage.TypeAAAA:
		var aaaa dnsmessage.AAAAResource
		copy(aaaa.AAAA[:], pair.v6.AsSlice())
		if builder.AAAAResource(rh, aaaa) != nil {
			return nil
		}
	}
	out, err := builder.Finish()
	if err != nil {
		return nil
	}
	return out
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

func trimName(name string) string {
	if len(name) > 0 && name[len(name)-1] == '.' {
		return name[:len(name)-1]
	}
	return name
}
