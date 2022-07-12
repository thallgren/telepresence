package tcp

import (
	"context"
	"encoding/binary"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/datawire/dlib/dlog"
	"github.com/telepresenceio/telepresence/v2/pkg/ipproto"
	"github.com/telepresenceio/telepresence/v2/pkg/tunnel"
	"github.com/telepresenceio/telepresence/v2/pkg/vif/ip"
)

type state int32

const (
	// simplified server-side tcp states
	stateSynReceived = state(iota)
	stateSynSent
	stateEstablished
	stateFinWait1
	stateFinWait2
	stateTimedWait
	stateIdle
)

func (s state) String() (txt string) {
	switch s {
	case stateIdle:
		txt = "IDLE"
	case stateSynSent:
		txt = "SYN SENT"
	case stateSynReceived:
		txt = "SYN RECEIVED"
	case stateEstablished:
		txt = "ESTABLISHED"
	case stateFinWait1:
		txt = "FIN_WAIT_1"
	case stateFinWait2:
		txt = "FIN_WAIT_2"
	case stateTimedWait:
		txt = "TIMED WAIT"
	default:
		panic("unknown state")
	}
	return txt
}

const myWindowScale = 8
const maxReceiveWindow = 4096 << myWindowScale // 1MB
const defaultMTU = 1500

var maxSegmentSize = defaultMTU - (20 + HeaderLen) // Ethernet MTU of 1500 - 20 byte IP header and 20 byte TCP header
var ioChannelSize = maxReceiveWindow / maxSegmentSize

type queueElement struct {
	sequence uint32
	retries  int32
	cTime    time.Time
	packet   Packet
	next     *queueElement
}

type quitReason int

const (
	pleaseContinue = quitReason(iota)
	quitByContext
	quitByReset
	quitByUs
	quitByPeer
	quitByBoth
)

type PacketHandler interface {
	tunnel.Handler

	// HandlePacket handles a packet that was read from the TUN device
	HandlePacket(ctx context.Context, pkt Packet)
}

type StreamCreator func(ctx context.Context) (tunnel.Stream, error)

type handler struct {
	streamCreator StreamCreator

	// Handle will have either a connection specific stream or a muxTunnel (the old style)
	// depending on what the handler is talking to
	stream tunnel.Stream

	// id identifies this connection. It contains source and destination IPs and ports
	id tunnel.ConnID

	// remove is the function that removes this instance from the pool
	remove func()

	// TUN I/O
	toTun   ip.Writer
	fromTun chan Packet

	// the dispatcher signals its intent to close in dispatcherClosing. 0 == running, 1 == closing, 2 == closed
	dispatcherClosing *int32

	// Channel to use when receiving messages to the traffic-manager
	tunDone chan struct{}

	// Channel to use when sending packets to the traffic-manager
	toMgrCh chan Packet

	// Channel to use when sending messages to the traffic-manager
	toMgrMsgCh chan tunnel.Message

	// Waitgroup that the processPackets (reader of TUN packets) and readFromMgrLoop (reader of packets from
	// the traffic manager) will signal when they are tunDone.
	wg sync.WaitGroup

	// queue where unacked elements are placed until they are acked
	ackWaitQueue     *queueElement
	ackWaitQueueSize uint32

	// oooQueue is where out-of-order packets are placed until they can be processed
	oooQueue *queueElement

	// wfState is the current workflow state
	wfState state

	// seq is the sequence that we provide in the packets we send to TUN
	seq uint32

	// seqAcked is the last sequence acked by the peer
	seqAcked uint32

	// lastKnown is generally the same as last ACK except for when packets are lost when sending them
	// to the manager. Those packets are not ACKed so we need to keep track of what we loose to prevent
	// treating subsequent packets as out-of-order since they must be considered lost as well.
	lastKnown uint32

	// packetLostTimer starts on first packet loss and is reset when a packet succeeds. The connection is
	// closed if the timer fires.
	packetLostTimer *time.Timer

	// Packets lost counts the total number of packets that are lost, regardless of if they were
	// recovered again.
	packetsLost int64

	// finalSeq is the ack sent with FIN when a connection is closing.
	finalSeq uint32

	// myWindow and is the actual size of my window
	myWindow int64

	// peerSeqToAck is the peer sequence that will be acked on next send
	peerSeqToAck uint32

	// peerSeqAcked was the last ack sent to the peer
	peerSeqAcked uint32

	// peerWindow is the actual size of the peers window
	peerWindow int64

	// peerWindowScale is the number of bits to shift the windowSize of received packet to
	// determine the actual peerWindow
	peerWindowScale uint8

	// peerMaxSegmentSize is the maximum size of a segment sent to the peer (not counting IP-header)
	peerMaxSegmentSize uint16

	// sendLock and sendCondition are used when throttling writes to the TUN device
	sendLock      sync.Mutex
	sendCondition *sync.Cond

	// random generator for initial sequence number
	rnd *rand.Rand
}

func NewHandler(
	streamCreator StreamCreator,
	dispatcherClosing *int32,
	toTun ip.Writer,
	id tunnel.ConnID,
	remove func(),
	rndSource rand.Source,
) PacketHandler {
	h := &handler{
		streamCreator:     streamCreator,
		id:                id,
		remove:            remove,
		toTun:             toTun,
		dispatcherClosing: dispatcherClosing,
		fromTun:           make(chan Packet, ioChannelSize),
		toMgrCh:           make(chan Packet, ioChannelSize),
		toMgrMsgCh:        make(chan tunnel.Message),
		myWindow:          maxReceiveWindow,
		wfState:           stateIdle,
		rnd:               rand.New(rndSource),
		tunDone:           make(chan struct{}),
	}
	h.sendCondition = sync.NewCond(&h.sendLock)
	return h
}

func (h *handler) RandomSequence() int32 {
	return h.rnd.Int31()
}

func (h *handler) HandlePacket(ctx context.Context, pkt Packet) {
	select {
	case <-ctx.Done():
		dlog.Debugf(ctx, "!! TUN %s discarded because context is cancelled", pkt)
	case <-h.tunDone:
		dlog.Debugf(ctx, "!! TUN %s discarded because TCP handler's input processing was cancelled", pkt)
	case h.fromTun <- pkt:
	}
}

func (h *handler) Stop(ctx context.Context) {
	if h.state() == stateEstablished || h.state() == stateSynReceived {
		h.setState(ctx, stateFinWait1)
		h.sendFin(ctx, true)
	}
	// Wake up if waiting for larger window size (ends processPayload)
	h.sendCondition.Broadcast()
}

// Reset replies to the sender of the initialPacket with a RST packet.
func (h *handler) Reset(ctx context.Context, initialPacket ip.Packet) error {
	return h.toTun.Write(ctx, initialPacket.(Packet).Reset())
}

func (h *handler) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	go h.processResends(ctx)
	go func() {
		defer cancel()
		defer func() {
			h.remove()
			// Drain any incoming to unblock
			for {
				select {
				case <-h.fromTun:
				default:
					return
				}
			}
		}()
		defer func() {
			if h.stream != nil {
				_ = h.stream.CloseSend(ctx)
			}
		}()
		h.processPackets(ctx)
		h.wg.Wait()
	}()
}

func (h *handler) sendToTun(ctx context.Context, pkt Packet, seqAdd uint32, forceAck bool) {
	h.sendLock.Lock()
	defer h.sendLock.Unlock()
	ackNbr := h.peerSequenceToAck()
	seq := h.sequence()
	tcpHdr := pkt.Header()
	if seqAdd > 0 {
		sq := h.addSequence(seqAdd)
		h.ackWaitQueue = &queueElement{
			sequence: sq,
			cTime:    time.Now(),
			packet:   pkt,
			next:     h.ackWaitQueue,
		}
		wz := int(h.peerWindow) - int(sq-h.seqAcked)
		h.ackWaitQueueSize++
		if h.ackWaitQueueSize%200 == 0 {
			dlog.Tracef(ctx, "   CON %s, Ack-queue size %d, seq %d peer window size %d",
				h.id, h.ackWaitQueueSize, h.ackWaitQueue.sequence, wz)
		}
	} else if !forceAck && ackNbr == h.peerSequenceAcked() && tcpHdr.NoFlags() {
		// Redundant, skip it
		return
	}

	tcpHdr.SetACK(true)
	tcpHdr.SetSequence(seq)
	tcpHdr.SetAckNumber(ackNbr)
	tcpHdr.SetChecksum(pkt.IPHeader())
	h.setPeerSequenceAcked(ackNbr)
	if err := h.toTun.Write(ctx, pkt); err != nil {
		dlog.Errorf(ctx, "!! TUN %s: %v", h.id, err)
	}
}

func (h *handler) newResponse(ipPayloadLen int, withAck bool) Packet {
	pkt := NewPacket(ipPayloadLen, h.id.Destination(), h.id.Source(), withAck)
	ipHdr := pkt.IPHeader()
	ipHdr.SetL4Protocol(ipproto.TCP)
	ipHdr.SetChecksum()

	tcpHdr := Header(ipHdr.Payload())
	tcpHdr.SetDataOffset(5)
	tcpHdr.SetSourcePort(h.id.DestinationPort())
	tcpHdr.SetDestinationPort(h.id.SourcePort())
	h.myWindowToHeader(tcpHdr)
	return pkt
}

func (h *handler) sendAck(ctx context.Context) {
	h.sendToTun(ctx, h.newResponse(HeaderLen, false), 0, false)
}

func (h *handler) forceSendAck(ctx context.Context) {
	h.sendToTun(ctx, h.newResponse(HeaderLen, false), 0, true)
}

func (h *handler) sendFin(ctx context.Context, expectAck bool) {
	pkt := h.newResponse(HeaderLen, true)
	tcpHdr := pkt.Header()
	tcpHdr.SetFIN(true)
	l := uint32(0)
	if expectAck {
		l = 1
		h.finalSeq = h.sequence()
	}
	h.sendToTun(ctx, pkt, l, true)
}

func (h *handler) sendSynReply(ctx context.Context, syn Packet) {
	synHdr := syn.Header()
	if !synHdr.SYN() {
		return
	}
	h.setPeerSequenceToAck(synHdr.Sequence() + 1)
	h.sendSyn(ctx)
}

func (h *handler) sendSyn(ctx context.Context) {
	hl := HeaderLen
	hl += 8 // for the Maximum Segment Size option and for the Window Scale option

	pkt := h.newResponse(hl, true)
	tcpHdr := pkt.Header()
	tcpHdr.SetSYN(true)
	tcpHdr.SetWindowSize(maxReceiveWindow >> myWindowScale) // The SYN packet itself is not subject to scaling

	// adjust data offset to account for options
	tcpHdr.SetDataOffset(hl / 4)

	opts := tcpHdr.OptionBytes()
	opts[0] = byte(maximumSegmentSize)
	opts[1] = 4
	binary.BigEndian.PutUint16(opts[2:], uint16(maxSegmentSize))

	opts[4] = byte(windowScale)
	opts[5] = 3
	opts[6] = myWindowScale
	opts[7] = byte(noOp)
	h.sendToTun(ctx, pkt, 1, true)
}

func (h *handler) processPayload(ctx context.Context, data []byte) {
	start := 0
	n := len(data)
	for n > start {
		h.sendLock.Lock()
		window := int(h.peerWindow) - int(h.sequence()-h.seqAcked)
		for window <= 0 {
			// The intended receiver is currently not accepting data. We must
			// wait for the window to increase.
			dlog.Debugf(ctx, "   CON %s TCP window is zero", h.id)
			h.sendCondition.Wait()
			if h.state() != stateEstablished {
				h.sendLock.Unlock()
				return
			}
			window = int(h.peerWindow) - int(h.sequence()-h.seqAcked)
		}
		h.sendLock.Unlock()

		// Give up if done is closed
		select {
		case <-ctx.Done():
			return
		case <-h.tunDone:
			return
		default:
		}

		mxSend := n - start
		if mxSend > int(h.peerMaxSegmentSize) {
			mxSend = int(h.peerMaxSegmentSize)
		}
		if mxSend > window {
			mxSend = window
		}

		pkt := h.newResponse(HeaderLen+mxSend, true)
		ipHdr := pkt.IPHeader()
		tcpHdr := pkt.Header()
		ipHdr.SetPayloadLen(HeaderLen + mxSend)
		ipHdr.SetChecksum()

		end := start + mxSend
		copy(tcpHdr.Payload(), data[start:end])
		tcpHdr.SetPSH(end == n)
		h.sendToTun(ctx, pkt, uint32(mxSend), false)

		// Decrease the window size with the bytes that we just sent unless it's already updated
		// from a received packet
		atomic.CompareAndSwapInt64(&h.peerWindow, int64(window), int64(window-mxSend))
		start = end
	}
}

func (h *handler) idle(ctx context.Context, syn Packet) quitReason {
	tcpHdr := syn.Header()
	if tcpHdr.RST() {
		dlog.Errorf(ctx, "   CON %s, got RST while idle", h.id)
		return quitByReset
	}
	if !tcpHdr.SYN() {
		if err := h.toTun.Write(ctx, syn.Reset()); err != nil {
			dlog.Errorf(ctx, "!! CON %s, send of RST failed: %v", h.id, err)
		}
		return quitByUs
	}

	synOpts, err := options(tcpHdr)
	if err != nil {
		dlog.Error(ctx, err)
		if err := h.toTun.Write(ctx, syn.Reset()); err != nil {
			dlog.Errorf(ctx, "!! CON %s, send of RST failed: %v", h.id, err)
		}
		return quitByUs
	}
	for _, synOpt := range synOpts {
		switch synOpt.kind() {
		case maximumSegmentSize:
			h.peerMaxSegmentSize = binary.BigEndian.Uint16(synOpt.data())
			dlog.Tracef(ctx, "   CON %s maximum segment size %d", h.id, h.peerMaxSegmentSize)
		case windowScale:
			h.peerWindowScale = synOpt.data()[0]
			dlog.Tracef(ctx, "   CON %s window scale %d", h.id, h.peerWindowScale)
		case selectiveAckPermitted:
			dlog.Tracef(ctx, "   CON %s selective acknowledgments permitted", h.id)
		default:
			dlog.Tracef(ctx, "   CON %s option %d with len %d", h.id, synOpt.kind(), synOpt.len())
		}
	}

	h.setSequence(uint32(h.RandomSequence()))
	h.setState(ctx, stateSynReceived)
	// Reply to the SYN, then establish a connection. We send a reset if that fails.
	h.sendSynReply(ctx, syn)
	if h.stream, err = h.streamCreator(ctx); err == nil {
		go h.readFromMgrLoop(ctx)
	}
	if err != nil {
		dlog.Error(ctx, err)
		if err := h.toTun.Write(ctx, syn.Reset()); err != nil {
			dlog.Errorf(ctx, "!! CON %s, send of RST failed: %v", h.id, err)
		}
		return quitByUs
	}
	return pleaseContinue
}

func (h *handler) synReceived(ctx context.Context, pkt Packet) quitReason {
	tcpHdr := pkt.Header()
	if tcpHdr.RST() {
		return quitByReset
	}
	if !tcpHdr.ACK() {
		return pleaseContinue
	}

	h.onAckReceived(ctx, tcpHdr.AckNumber())
	h.setState(ctx, stateEstablished)
	go h.writeToMgrLoop(ctx)

	pl := len(tcpHdr.Payload())
	if pl != 0 {
		h.lastKnown = tcpHdr.Sequence() + uint32(pl)
		if h.sendToMgr(ctx, pkt) {
			h.setPeerSequenceToAck(h.lastKnown)
			h.sendAck(ctx)
		} else {
			h.packetsLost++
		}
	}
	return pleaseContinue
}

func (h *handler) handleReceived(ctx context.Context, pkt Packet) quitReason {
	tcpHdr := pkt.Header()
	if tcpHdr.RST() {
		return quitByReset
	}

	if !tcpHdr.ACK() {
		// Just ignore packets that have no ack
		return pleaseContinue
	}

	ackNbr := tcpHdr.AckNumber()
	h.onAckReceived(ctx, ackNbr)

	sq := tcpHdr.Sequence()
	lastAck := h.peerSequenceAcked()
	payloadLen := len(tcpHdr.Payload())
	state := h.state()
	switch {
	case sq == lastAck:
		if state == stateFinWait1 && ackNbr == h.finalSeq && !tcpHdr.FIN() {
			h.setState(ctx, stateTimedWait)
			return quitByUs
		}
	case sq > lastAck:
		if payloadLen == 0 {
			break
		}
		if sq <= h.lastKnown {
			// Previous packet lost by us. Don't ack this one, just treat it
			// as the next lost packet.
			if payloadLen > 0 {
				lk := sq + uint32(payloadLen)
				if lk > h.lastKnown {
					h.lastKnown = lk
					h.packetsLost++
				}
			}
			return pleaseContinue
		}
		// Oops. Packet loss! Let sender know by sending an ACK so that we ack the receipt
		// and also tell the sender about our expected number
		dlog.Debugf(ctx, "   CON %s, ack-diff %d", pkt, sq-lastAck)
		h.sendAck(ctx)
		h.addOutOfOrderPacket(ctx, pkt)
		return pleaseContinue
	case sq == lastAck-1 && payloadLen == 0:
		// keep alive, force is needed because the ackNbr is unchanged
		h.forceSendAck(ctx)
		go h.sendStreamControl(ctx, tunnel.KeepAlive)
		return pleaseContinue
	default:
		// resend of already acknowledged packet. Just ignore
		if payloadLen > 0 {
			dlog.Debugf(ctx, "   CON %s, resends already acked len=%d, sq=%d", h.id, payloadLen, sq)
		}
		return pleaseContinue
	}

	switch {
	case payloadLen > 0:
		h.lastKnown = sq + uint32(payloadLen)
		if !h.sendToMgr(ctx, pkt) {
			h.packetsLost++
			return pleaseContinue
		}
		h.setPeerSequenceToAck(h.lastKnown)
	case tcpHdr.FIN():
		h.setPeerSequenceToAck(lastAck + 1)
	default:
		// don't ack an ack
		return pleaseContinue
	}
	h.sendAck(ctx)

	switch state {
	case stateEstablished:
		if tcpHdr.FIN() {
			h.sendFin(ctx, false)
			h.setState(ctx, stateTimedWait)
			return quitByPeer
		}
	case stateFinWait1:
		if tcpHdr.FIN() {
			h.setState(ctx, stateTimedWait)
			return quitByBoth
		}
		h.setState(ctx, stateFinWait2)
	case stateFinWait2:
		if tcpHdr.FIN() {
			return quitByUs
		}
	}
	return pleaseContinue
}

func (h *handler) processPackets(ctx context.Context) {
	h.wg.Add(1)
	defer h.wg.Done()
	defer func() {
		close(h.tunDone)
		h.setState(ctx, stateIdle)
		h.sendLock.Lock()
		h.ackWaitQueue = nil
		h.oooQueue = nil
		h.sendLock.Unlock()
		if h.stream != nil {
			go func() {
				if err := h.stream.CloseSend(ctx); err != nil {
					dlog.Errorf(ctx, "!! CON %s CloseSend() failed %v", h.id, err)
				}
			}()
		}
	}()

	process := func(ctx context.Context, pkt Packet) bool {
		h.peerWindowFromHeader(ctx, pkt.Header())
		var end quitReason
		switch h.state() {
		case stateIdle:
			end = h.idle(ctx, pkt)
		case stateSynReceived:
			end = h.synReceived(ctx, pkt)
		default:
			end = h.handleReceived(ctx, pkt)
		}
		switch end {
		case quitByReset, quitByContext:
			return false
		case quitByUs, quitByPeer, quitByBoth:
			h.processFinalPackets(ctx)
			return false
		default:
			return true
		}
	}
	h.processPacketsWithProcessor(ctx, process)
}

func (h *handler) processFinalPackets(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	defer h.setState(ctx, stateIdle)

	h.processPacketsWithProcessor(ctx, func(ctx context.Context, pkt Packet) bool {
		h.peerWindowFromHeader(ctx, pkt.Header())
		end := h.handleReceived(ctx, pkt)
		return end == pleaseContinue
	})
}

const initialResendDelay = 2
const maxResends = 7

type resend struct {
	packet Packet
	secs   int
	next   *resend
}

func (h *handler) processPacketsWithProcessor(ctx context.Context, process func(ctx context.Context, pkt Packet) bool) {
	for {
		select {
		case pkt := <-h.fromTun:
			if !process(ctx, pkt) {
				return
			}
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				continueProcessing, next := h.processNextOutOfOrderPacket(ctx, process)
				if !continueProcessing {
					return
				}
				if !next {
					break
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

func (h *handler) copyPacket(orig Packet) Packet {
	origHdr := orig.Header()
	ipLen := HeaderLen + orig.PayloadLen()
	pkt := h.newResponse(ipLen, true)
	ipHdr := pkt.IPHeader()
	tcpHdr := pkt.Header()
	ipHdr.SetPayloadLen(ipLen)
	ipHdr.SetChecksum()
	copy(tcpHdr.Payload(), origHdr.Payload())
	tcpHdr.SetPSH(origHdr.PSH())
	return pkt
}

func (h *handler) processResends(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.tunDone:
			return
		case <-ticker.C:
		}
		now := time.Now()
		var resends *resend
		h.sendLock.Lock()
		var prev *queueElement
		for el := h.ackWaitQueue; el != nil; {
			secs := initialResendDelay << el.retries // 2, 4, 8, 16, ...
			deadLine := el.cTime.Add(time.Duration(secs) * time.Second)
			if deadLine.Before(now) {
				el.retries++
				if el.retries > maxResends {
					dlog.Errorf(ctx, "   CON %s, packet resent %d times, giving up", h.id, maxResends)
					// Drop from queue and point to next
					el = el.next
					if prev == nil {
						h.ackWaitQueue = el
					} else {
						prev.next = el
					}
					continue
				}

				// reverse (i.e. put in right order since ackWaitQueue is in fact reversed)
				resends = &resend{packet: el.packet, secs: secs, next: resends}
			}
			prev = el
			el = el.next
		}
		h.sendLock.Unlock()
		for resends != nil {
			pkt := h.copyPacket(resends.packet)
			dlog.Debugf(ctx, "   CON %s resent after %d seconds", pkt, resends.secs)
			h.sendToTun(ctx, pkt, uint32(len(pkt.Header().Payload())), false)
			resends = resends.next
		}
	}
}

func (h *handler) onAckReceived(ctx context.Context, seq uint32) {
	h.sendLock.Lock()
	// ack-queue is guaranteed to be sorted descending on sequence, so we cut from the packet with
	// a sequence less than or equal to the received sequence.
	sq := h.sequence()
	oldWindow := int(h.peerWindow) - int(sq-h.seqAcked)
	h.seqAcked = seq
	newWindow := int(h.peerWindow) - int(sq-h.seqAcked)

	el := h.ackWaitQueue
	var prev *queueElement
	for el != nil && el.sequence > seq {
		prev = el
		el = el.next
	}

	if el != nil {
		if prev == nil {
			h.ackWaitQueue = nil
		} else {
			prev.next = nil
		}
		for {
			h.ackWaitQueueSize--
			if el = el.next; el == nil {
				break
			}
		}
	}
	h.sendLock.Unlock()
	if oldWindow <= 0 && newWindow > 0 {
		dlog.Debugf(ctx, "   CON %s, TCP window %d after ack", h.id, newWindow)
		h.sendCondition.Signal()
	}
}

func (h *handler) processNextOutOfOrderPacket(ctx context.Context, process func(ctx context.Context, pkt Packet) bool) (bool, bool) {
	seq := h.peerSequenceToAck()
	var prev *queueElement
	for el := h.oooQueue; el != nil; el = el.next {
		if el.sequence == seq {
			if prev != nil {
				prev.next = el.next
			} else {
				h.oooQueue = el.next
			}
			dlog.Debugf(ctx, "   CON %s, Processing out-of-order packet %s", h.id, el.packet)
			return process(ctx, el.packet), true
		}
		prev = el
	}
	return true, false
}

func (h *handler) addOutOfOrderPacket(ctx context.Context, pkt Packet) {
	hdr := pkt.Header()
	sq := hdr.Sequence()

	var prev *queueElement
	for el := h.oooQueue; el != nil; el = el.next {
		if el.sequence == sq {
			return
		}
		prev = el
	}
	dlog.Debugf(ctx, "   CON %s, out-of-order", pkt)
	el := &queueElement{
		sequence: sq,
		cTime:    time.Now(),
		packet:   pkt,
	}
	if prev == nil {
		h.oooQueue = el
	} else {
		prev.next = el
	}
}

func (h *handler) state() state {
	return state(atomic.LoadInt32((*int32)(&h.wfState)))
}

func (h *handler) setState(ctx context.Context, s state) {
	oldState := h.state()
	if oldState != s {
		dlog.Debugf(ctx, "   CON %s, state %s -> %s", h.id, h.state(), s)
		atomic.StoreInt32((*int32)(&h.wfState), int32(s))
		if oldState == stateEstablished {
			// Unblock any sender when moving from stateEstablished
			h.sendCondition.Signal()
		}
	}
}

// sequence is the sequence number of the packets that this client
// sends to the TUN device.
func (h *handler) sequence() uint32 {
	return atomic.LoadUint32(&h.seq)
}

func (h *handler) addSequence(v uint32) uint32 {
	return atomic.AddUint32(&h.seq, v)
}

func (h *handler) setSequence(v uint32) {
	atomic.StoreUint32(&h.seq, v)
}

// peerSequenceToAck is the received sequence that this will ack on next send
func (h *handler) peerSequenceToAck() uint32 {
	return atomic.LoadUint32(&h.peerSeqToAck)
}

func (h *handler) setPeerSequenceToAck(v uint32) {
	atomic.StoreUint32(&h.peerSeqToAck, v)
}

// peerSequenceAcked was the last ack sent to the peer
func (h *handler) peerSequenceAcked() uint32 {
	return atomic.LoadUint32(&h.peerSeqAcked)
}

func (h *handler) setPeerSequenceAcked(v uint32) {
	atomic.StoreUint32(&h.peerSeqAcked, v)
}

func (h *handler) peerWindowFromHeader(ctx context.Context, tcpHeader Header) {
	h.sendLock.Lock()
	sq := h.sequence()
	oldWindow := int(h.peerWindow) - int(sq-h.seqAcked)
	h.peerWindow = int64(tcpHeader.WindowSize()) << h.peerWindowScale
	newWindow := int(h.peerWindow) - int(sq-h.seqAcked)
	h.sendLock.Unlock()
	if oldWindow <= 0 && newWindow > 0 {
		dlog.Debugf(ctx, "   CON %s, TCP window %d after window update", h.id, newWindow)
		h.sendCondition.Signal()
	}
}

func (h *handler) myWindowToHeader(tcpHeader Header) {
	tcpHeader.SetWindowSize(uint16(h.receiveWindow() >> myWindowScale))
}

func (h *handler) receiveWindow() int {
	return int(atomic.LoadInt64(&h.myWindow))
}

func (h *handler) setReceiveWindow(v int) {
	atomic.StoreInt64(&h.myWindow, int64(v))
}
