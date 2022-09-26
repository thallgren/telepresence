package state

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/miekg/dns"

	"github.com/datawire/dlib/dlog"
	rpc "github.com/telepresenceio/telepresence/rpc/v2/manager"
	"github.com/telepresenceio/telepresence/v2/pkg/dnsproxy"
)

// We can use our own Rcodes in the range that is reserved for private use

// RcodeNoAgents means that no agents replied to the DNS request
const RcodeNoAgents = 3841

// AgentsLookupDNS will send the given request to all agents currently intercepted by the client identified with
// the clientSessionID, it will then wait for results to arrive, collect those results, and return the result.
func (s *State) AgentsLookupDNS(ctx context.Context, clientSessionID string, request *rpc.DNSRequest) ([]dns.RR, int, error) {
	rs, err := s.agentsLookup(ctx, clientSessionID, request)
	if err != nil {
		return nil, 0, err
	}
	if len(rs) == 0 {
		return nil, RcodeNoAgents, nil
	}
	var bestRRs []dns.RR
	bestRcode := math.MaxInt
	for _, r := range rs {
		rrs, rCode, err := dnsproxy.FromRPC(r.(*rpc.DNSResponse))
		if err != nil {
			return nil, rCode, err
		}
		if rCode < bestRcode {
			bestRcode = rCode
			if len(rrs) > len(bestRRs) {
				bestRRs = rrs
			}
		}
	}
	return bestRRs, bestRcode, nil
}

// PostLookupDNSResponse receives lookup responses from an agent and places them in the channel
// that corresponds to the lookup request
func (s *State) PostLookupDNSResponse(response *rpc.DNSAgentResponse) {
	request := response.GetRequest()
	rid := requestId(request)
	var rch chan<- *rpc.DNSResponse
	s.mu.RLock()
	if as, ok := s.sessions[response.GetSession().SessionId].(*agentSessionState); ok {
		rch = as.dnsResponses[rid]
	}
	s.mu.RUnlock()
	if rch != nil {
		rch <- response.GetResponse()
	} else {
		dlog.Warnf(s.ctx, "Posting lookup response but there was no channel")
	}
}

func (s *State) WatchLookupDNS(agentSessionID string) <-chan *rpc.DNSRequest {
	s.mu.RLock()
	ss, ok := s.sessions[agentSessionID]
	s.mu.RUnlock()
	if !ok {
		return nil
	}
	return ss.(*agentSessionState).dnsRequests
}

func (s *State) agentsLookup(ctx context.Context, clientSessionID string, request *rpc.DNSRequest) ([]fmt.Stringer, error) {
	aIDs := s.getAgentsInterceptedByClient(clientSessionID)
	aCount := len(aIDs)
	if aCount == 0 {
		return nil, nil
	}

	rsBuf := make(chan fmt.Stringer, aCount)

	timout, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	wg := sync.WaitGroup{}
	wg.Add(aCount)
	for _, aID := range aIDs {
		go func(aID string) {
			rid := requestId(request)
			defer func() {
				s.endLookup(aID, rid)
				wg.Done()
			}()

			rsCh := s.startLookup(aID, rid, request)
			if rsCh == nil {
				return
			}
			select {
			case <-timout.Done():
			case rs, ok := <-rsCh:
				if ok {
					rsBuf <- rs
				}
			}
		}(aID)
	}
	wg.Wait() // wait for timeout or that all agents have responded
	bz := len(rsBuf)
	rs := make([]fmt.Stringer, bz)
	for i := 0; i < bz; i++ {
		rs[i] = <-rsBuf
	}
	return rs, nil
}

func (s *State) startLookup(agentSessionID, rid string, request *rpc.DNSRequest) <-chan *rpc.DNSResponse {
	var (
		rch chan *rpc.DNSResponse
		as  *agentSessionState
		ok  bool
	)
	s.mu.Lock()
	if as, ok = s.sessions[agentSessionID].(*agentSessionState); ok {
		if rch, ok = as.dnsResponses[rid]; !ok {
			rch = make(chan *rpc.DNSResponse)
			as.dnsResponses[rid] = rch
		}
	}
	s.mu.Unlock()
	if as != nil {
		// the as.lookups channel may be closed at this point, so guard for panic
		func() {
			defer func() {
				if r := recover(); r != nil {
					close(rch)
				}
			}()
			as.dnsRequests <- request
		}()
	}
	return rch
}

func (s *State) endLookup(agentSessionID, rid string) {
	s.mu.Lock()
	if as, ok := s.sessions[agentSessionID].(*agentSessionState); ok {
		if rch, ok := as.dnsResponses[rid]; ok {
			delete(as.dnsResponses, rid)
			close(rch)
		}
	}
	s.mu.Unlock()
}

func requestId(request *rpc.DNSRequest) string {
	return fmt.Sprintf("%s:%s:%d", request.Session.SessionId, request.Name, request.Type)
}
