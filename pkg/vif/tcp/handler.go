package tcp

import (
	"context"
	"encoding/binary"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/datawire/dlib/derror"
	"github.com/datawire/dlib/dlog"
	"github.com/telepresenceio/telepresence/v2/pkg/tunnel"
	"github.com/telepresenceio/telepresence/v2/pkg/vif/ip"
)

type state int32

const (
	// simplified server-side tcp states
	stateListen = state(iota)
	stateSynReceived
	stateSynSent
	stateEstablished
	stateFinWait1
	stateFinWait2
	stateCloseWait
	stateClosing
	stateTimeWait
	stateClosed
)

func (s state) String() (txt string) {
	switch s {
	case stateListen:
		txt = "LISTEN"
	case stateSynSent:
		txt = "SYN-SENT"
	case stateSynReceived:
		txt = "SYN-RECEIVED"
	case stateEstablished:
		txt = "ESTABLISHED"
	case stateFinWait1:
		txt = "FIN-WAIT-1"
	case stateFinWait2:
		txt = "FIN-WAIT-2"
	case stateCloseWait:
		txt = "CLOSE-WAIT"
	case stateTimeWait:
		txt = "TIME-WAIT"
	case stateClosing:
		txt = "CLOSING"
	case stateClosed:
		txt = "CLOSED"
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
	cTime    int64
	packet   Packet
	next     *queueElement
}

type awaitWinSize struct {
	done   chan struct{}
	rqSize int64
}

type PacketHandler interface {
	tunnel.Handler

	// HandlePacket handles a packet that was read from the TUN device
	HandlePacket(ctx context.Context, pkt Packet)
}

type StreamCreator func(ctx context.Context) (tunnel.Stream, error)

type handler struct {
	sync.Mutex

	// cancel will cancel all goroutines started by this handler
	cancel context.CancelFunc

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

	// sequence is the sequence that we provide in the packets we send to TUN
	sequence uint32

	// sequenceAcked is the last sequence acked by the peer
	sequenceAcked uint32

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

	// myWindowSize and is the actual size of my window
	myWindowSize uint32

	// peerSequenceToAck is the peer sequence that will be acked on next send
	peerSequenceToAck uint32

	// peerSequenceAcked was the last ack sent to the peer
	peerSequenceAcked uint32

	// peerWindow is the actual size of the peers window
	peerWindow int64

	awaitWinSize *awaitWinSize

	// peerPermitsSACK
	peerPermitsSACK bool

	// peerWindowScale is the number of bits to shift the windowSize of received packet to
	// determine the actual peerWindow
	peerWindowScale uint8

	// peerMaxSegmentSize is the maximum size of a segment sent to the peer (not counting IP-header)
	peerMaxSegmentSize uint16

	// random generator for initial sequence number
	rnd *rand.Rand

	stopTimer *time.Timer

	sqStart  uint32
	ackStart uint32
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
		toMgrMsgCh:        make(chan tunnel.Message, 50),
		myWindowSize:      maxReceiveWindow,
		wfState:           stateListen,
		rnd:               rand.New(rndSource),
	}
	return h
}

func (h *handler) RandomSequence() int32 {
	return h.rnd.Int31()
}

func (h *handler) HandlePacket(ctx context.Context, pkt Packet) {
	select {
	case <-ctx.Done():
		dlog.Debugf(ctx, "!! TUN %s discarded because context is cancelled", pkt)
	case h.fromTun <- pkt:
	}
}

func (h *handler) Stop(ctx context.Context) {
	h.Lock()
	h.stopLocked(ctx)
	h.Unlock()
}

const timeWaitDuration = 30 * time.Second

func (h *handler) setStopTimer(ctx context.Context) {
	if h.stopTimer != nil {
		h.stopTimer.Reset(timeWaitDuration)
	} else {
		h.stopTimer = time.AfterFunc(timeWaitDuration, func() {
			h.Stop(ctx)
		})
	}
}

func (h *handler) stopLocked(ctx context.Context) {
	dlog.Debugf(ctx, "   TUN %s STOP", h.id)
	switch h.state() {
	case stateEstablished, stateSynReceived:
		h.setState(ctx, stateFinWait1)
		h.sendFin(ctx, true)
	case stateClosed:
		if rm := h.remove; rm != nil {
			h.remove = nil
			rm()
			// Drain any incoming to unblock
			h.cancel()
			for {
				select {
				case <-h.fromTun:
				default:
					return
				}
			}
		}
	}
}

// Reset replies to the sender of the initialPacket with a RST packet.
func (h *handler) Reset(ctx context.Context, initialPacket Packet) {
	pkt := initialPacket.Reset()
	h.tunWriteUnlocked(ctx, pkt)
}

func (h *handler) Start(ctx context.Context) {
	ctx, h.cancel = context.WithCancel(ctx)
	go h.processResends(ctx)
	go func() {
		defer func() {
			dlog.Debugf(ctx, "   CON %s closed", h.id)
			h.setState(ctx, stateClosed)
			h.Stop(ctx)
		}()
		h.processPackets(ctx)

		// Closing the toMgrCh will cause the WriteLoop that reads from it to
		// call CloseSend() on the stream once the toMgrCh and the toMgrMsgCh
		// channels are drained.
		close(h.toMgrCh)
		h.wg.Wait()
	}()
}

// prepareToSend must be called with the lock in place
func (h *handler) prepareToSend(ctx context.Context, pkt Packet, seqAdd uint32) bool {
	ackNbr := h.peerSequenceToAck
	seq := h.sequence
	tcpHdr := pkt.Header()
	tcpHdr.SetACK(true)
	if seqAdd > 0 {
		h.sequence += seqAdd
		h.ackWaitQueue = &queueElement{
			sequence: h.sequence,
			cTime:    time.Now().UnixNano(),
			packet:   pkt,
			next:     h.ackWaitQueue,
		}
		h.ackWaitQueueSize++
		wz := h.peerWindow - int64(h.sequence-h.sequenceAcked)
		if h.ackWaitQueueSize%200 == 0 {
			dlog.Tracef(ctx, "   CON %s, Ack-queue size %d, seq %d peer window size %d",
				h.id, h.ackWaitQueueSize, h.ackWaitQueue.sequence, wz)
		}
	} else if ackNbr == h.peerSequenceAcked && tcpHdr.OnlyACK() {
		// Redundant, skip it
		return false
	}

	tcpHdr.SetSequence(seq)
	tcpHdr.SetAckNumber(ackNbr)
	tcpHdr.SetWindowSize(uint16(h.receiveWindow() >> myWindowScale))
	tcpHdr.SetChecksum(pkt.IPHeader())
	h.peerSequenceAcked = ackNbr
	return true
}

// prepareToResend must be called with the lock in place
func (h *handler) prepareToResend(ctx context.Context, origPkt Packet) Packet {
	origHdr := origPkt.Header()
	pkt := h.newResponse(ctx, origHdr.PayloadLen())
	tcpHdr := pkt.Header()
	tcpHdr.CopyFlagsFrom(origHdr)
	tcpHdr.SetSequence(origHdr.Sequence())
	tcpHdr.SetAckNumber(h.peerSequenceToAck)
	tcpHdr.SetWindowSize(uint16(h.receiveWindow() >> myWindowScale))
	tcpHdr.SetChecksum(pkt.IPHeader())
	copy(tcpHdr.Payload(), origHdr.Payload())
	return pkt
}

func (h *handler) sendACK(ctx context.Context) {
	h.sendToTun(ctx, h.newResponse(ctx, 0), 0)
}

func (h *handler) newResponse(ctx context.Context, payloadLen int) Packet {
	el := h.oooQueue
	if el == nil {
		return NewReplyPacket(HeaderLen, payloadLen, h.id)
	}

	// Add SACK option with edges

	// The data offset is stored in 4 bits and uses a multiplier of 4 which
	// gives us a maximum of 15 quad-bytes. In this range, we must fit size
	// of the TCP header (5), the size of the option header (1) and 2 edges
	// (1 each) per SACK. 15-5-1 == 9, so 2 * 4 edges.
	const maxEdges = 8

	edges := make([]uint32, 0, maxEdges)
	var mreTs int64
	mreIdx := -1 // Index of edge of most recently received out-of-order packet
	for i := 0; el != nil; i += 2 {
		leftEdge := el.sequence
		rightEdge := leftEdge
		for {
			if el.cTime > mreTs {
				mreIdx = i
				mreTs = el.cTime
			}
			rightEdge += uint32(el.packet.Header().PayloadLen())
			el = el.next
			if el == nil || el.sequence != rightEdge {
				break
			}
		}
		edges = append(edges, leftEdge, rightEdge)
	}
	ne := len(edges)
	if mreIdx > 0 {
		// Ensure that first SACK contains the most recently received packet
		le := edges[mreIdx]
		re := edges[mreIdx+1]
		edges[mreIdx] = edges[0]
		edges[mreIdx+1] = edges[1]
		edges[0] = le
		edges[1] = re
	}

	if ne > maxEdges {
		ne = maxEdges
	}

	hl := HeaderLen + 4 + ne*4 // Must be on 4 byte boundary
	pkt := NewReplyPacket(hl, payloadLen, h.id)
	tcpHdr := pkt.Header()
	// adjust data offset to account for options
	opts := tcpHdr.OptionBytes()
	opts[0] = selectiveAck
	opts[1] = byte(2 + ne*4)
	i := 2

	for e := 0; e < ne; {
		re := edges[e]
		binary.BigEndian.PutUint32(opts[i:], re)
		e++
		i += 4
		le := edges[e]
		binary.BigEndian.PutUint32(opts[i:], le)
		e++
		i += 4
		dlog.Tracef(ctx, "-> TUN %s SACK %d,%d", h.id, re-h.ackStart, le-h.ackStart)
	}
	return pkt
}

func (h *handler) sendFin(ctx context.Context, expectAck bool) {
	pkt := NewReplyPacket(HeaderLen, 0, h.id)
	tcpHdr := pkt.Header()
	tcpHdr.SetFIN(true)
	l := uint32(0)
	if expectAck {
		l = 1
		h.finalSeq = h.sequence
	}
	h.sendToTun(ctx, pkt, l)
}

func (h *handler) sendToTun(ctx context.Context, pkt Packet, seqAdd uint32) {
	if h.prepareToSend(ctx, pkt, seqAdd) {
		h.tunWriteUnlocked(ctx, pkt)
	}
}

func (h *handler) tunWriteUnlocked(ctx context.Context, pkt Packet) {
	h.Unlock()
	if err := h.toTun.Write(ctx, pkt); err != nil {
		dlog.Errorf(ctx, "!! TUN %s: %v", h.id, err)
	}
	h.Lock()
}

func (h *handler) tunWrite(ctx context.Context, pkt Packet) {
	if err := h.toTun.Write(ctx, pkt); err != nil {
		dlog.Errorf(ctx, "!! TUN %s: %v", h.id, err)
	}
}

func (h *handler) sendSynReply(ctx context.Context, syn Packet) {
	synHdr := syn.Header()
	if !synHdr.SYN() {
		return
	}
	h.peerSequenceToAck = synHdr.Sequence() + 1
	h.sendSyn(ctx)
}

func (h *handler) sendSyn(ctx context.Context) {
	hl := HeaderLen
	hl += 12 // for the Maximum Segment Size, Window Scale, and Selective Ack Permitted options

	pkt := h.newResponse(ctx, hl)
	tcpHdr := pkt.Header()
	tcpHdr.SetSYN(true)
	tcpHdr.SetWindowSize(maxReceiveWindow >> myWindowScale) // The SYN packet itself is not subject to scaling

	// adjust data offset to account for options
	tcpHdr.SetDataOffset(hl / 4)

	opts := tcpHdr.OptionBytes()
	opts[0] = maximumSegmentSize
	opts[1] = 4
	binary.BigEndian.PutUint16(opts[2:], uint16(maxSegmentSize))

	opts[4] = windowScale
	opts[5] = 3
	opts[6] = myWindowScale

	opts[7] = selectiveAckPermitted
	opts[8] = 2
	h.sendToTun(ctx, pkt, 1)
}

func (h *handler) processPayload(ctx context.Context, data []byte) {
	start := 0
	n := len(data)
	for n > start {
		var pkt Packet
		h.Lock()
		start, pkt = h.preparePackageFromPayload(ctx, data, start)
		h.Unlock()
		if pkt == nil {
			break
		}
		h.tunWrite(ctx, pkt)
		// We only write packages with a payload here, and all of those packages
		// will be in the waitForAck queue and will be released when the ack arrives,
		// so no call to pkg.Release() must be made.
	}
}

func (h *handler) preparePackageFromPayload(ctx context.Context, data []byte, start int) (int, Packet) {
	mxSeg := int(h.peerMaxSegmentSize)
	window := h.peerWindow - int64(h.sequence-h.sequenceAcked)
	minWin := int64(mxSeg * 4)
	if window < minWin {
		// The intended receiver is currently not accepting data. We must
		// wait for the window to increase.
		dlog.Tracef(ctx, "   CON %s TCP window is too small (%d < %d)", h.id, window, minWin)
		if !h.awaitWindowSize(ctx, minWin) {
			return 0, nil
		}
		dlog.Tracef(ctx, "   CON %s TCP window is big enough", h.id)
	}

	// Give up if context is done
	if ctx.Err() != nil {
		return 0, nil
	}

	n := len(data)
	mxSend := n - start
	if mxSend > mxSeg {
		mxSend = mxSeg
	}

	pkt := h.newResponse(ctx, mxSend)
	tcpHdr := pkt.Header()

	end := start + mxSend
	copy(tcpHdr.Payload(), data[start:end])
	tcpHdr.SetPSH(end == n)
	// Decrease the window size with the bytes that we're about to send
	h.peerWindow -= int64(mxSend)
	if !h.prepareToSend(ctx, pkt, uint32(mxSend)) {
		pkt = nil
	}
	return end, pkt
}

func (h *handler) listen(ctx context.Context, syn Packet) {
	tcpHdr := syn.Header()
	if tcpHdr.RST() {
		dlog.Debugf(ctx, "   CON %s, got RST while listen", h.id)
		h.stopLocked(ctx)
		return
	}
	if !tcpHdr.SYN() {
		dlog.Debugf(ctx, "   CON %s while listen", syn)
		h.Reset(ctx, syn)
		h.stopLocked(ctx)
		return
	}

	synOpts, err := options(tcpHdr)
	if err != nil {
		dlog.Debug(ctx, err)
		h.Reset(ctx, syn)
		h.stopLocked(ctx)
		return
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
			h.peerPermitsSACK = true
		case timestamps:
			dlog.Tracef(ctx, "   CON %s timestamps enabled", h.id)
		default:
			dlog.Tracef(ctx, "   CON %s option %d with len %d", h.id, synOpt.kind(), synOpt.len())
		}
	}

	h.sequence = uint32(h.RandomSequence())
	h.sqStart = h.sequence + 1
	h.ackStart = tcpHdr.Sequence() + 1

	h.setState(ctx, stateSynReceived)
	// Reply to the SYN, then establish a connection. We send a reset if that fails.
	h.sendSynReply(ctx, syn)
	if h.stream, err = h.streamCreator(ctx); err == nil {
		go h.readFromMgrLoop(ctx)
	}
	if err != nil {
		dlog.Error(ctx, err)
		h.Reset(ctx, syn)
	}
}

func (h *handler) inReceiveWindow(sq uint32) bool {
	return sq >= h.peerSequenceAcked && sq < h.peerSequenceAcked+h.myWindowSize
}

func (h *handler) synReceived(ctx context.Context, pkt Packet) {
	tcpHdr := pkt.Header()
	if tcpHdr.RST() {
		sq := pkt.Header().Sequence()
		if h.inReceiveWindow(sq) {
			if sq == h.peerSequenceAcked {
				dlog.Debugf(ctx, "   CON %s, got RST after SYN", h.id)
				h.setState(ctx, stateClosed)
				h.stopLocked(ctx)
			} else {
				h.sendACK(ctx)
			}
		}
		return
	}
	if !tcpHdr.ACK() {
		dlog.Debugf(ctx, "   CON %s, did not get ACK after SYN", h.id)
		return
	}

	h.setState(ctx, stateEstablished)
	go h.writeToMgrLoop(ctx)
	h.handleReceived(ctx, pkt)
}

func (h *handler) handleReceived(ctx context.Context, pkt Packet) {
	tcpHdr := pkt.Header()
	lastAck := h.peerSequenceAcked
	sq := tcpHdr.Sequence()
	if tcpHdr.RST() {
		if h.inReceiveWindow(sq) {
			if sq == lastAck {
				dlog.Debugf(ctx, "   CON %s", pkt)
				h.setState(ctx, stateClosed)
				h.stopLocked(ctx)
			} else {
				h.sendACK(ctx)
			}
		}
		return
	}

	if !tcpHdr.ACK() {
		// Just ignore packets that have no ack
		if !tcpHdr.FIN() {
			dlog.Debugf(ctx, "   CON %s, ACK not set", pkt)
			return
		}
	}
	payloadLen := len(tcpHdr.Payload())
	switch {
	case sq == lastAck:
		switch {
		case payloadLen > 0:
			h.sendToMgr(ctx, pkt)
			h.processOutOfOrderPackets(ctx, sq+uint32(payloadLen))
			h.sendACK(ctx)
		case tcpHdr.FIN():
			h.peerSequenceToAck = lastAck + 1
			switch h.state() {
			case stateEstablished:
				h.setState(ctx, stateCloseWait)
				h.sendFin(ctx, false)
				return
			case stateFinWait1:
				if !tcpHdr.ACK() {
					h.setState(ctx, stateClosing)
					h.sendACK(ctx)
				} else {
					h.setStopTimer(ctx)
					h.setState(ctx, stateTimeWait)
				}
			case stateFinWait2:
				h.setStopTimer(ctx)
				h.setState(ctx, stateTimeWait)
				h.sendACK(ctx)
			}
		default:
			// ACK
			switch h.state() {
			case stateCloseWait: // ACK of FIN
				h.setState(ctx, stateClosed)
				h.stopLocked(ctx)
			case stateClosing, stateTimeWait:
				h.setStopTimer(ctx)
				h.setState(ctx, stateTimeWait)
			case stateFinWait1:
				h.setState(ctx, stateFinWait2)
			}
		}
	case sq > lastAck:
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
			break
		}
		if payloadLen > 0 {
			// Oops. Packet loss! Let sender know by sending an ACK so that we ack the receipt
			// and also tell the sender about our expected number
			dlog.Tracef(ctx, "   CON %s, sq %d, an %d, wz %d, len %d, flags %s, ack-diff %d",
				h.id, sq-h.ackStart, tcpHdr.AckNumber()-h.sqStart, tcpHdr.WindowSize(), payloadLen, tcpHdr.Flags(), sq-lastAck)

			if h.peerPermitsSACK {
				h.addOutOfOrderPacket(pkt)
			}
			h.sendACK(ctx)
		}
	case sq == lastAck-1 && payloadLen == 0:
		// keep alive, force is needed because the ackNbr is unchanged
		h.peerSequenceAcked--
		h.sendACK(ctx)
		go h.sendStreamControl(ctx, tunnel.KeepAlive)
	default:
		// resend of already acknowledged packet. Just ignore
		if payloadLen > 0 {
			dlog.Tracef(ctx, "   CON %s, sq %d, an %d, wz %d, len %d, flags %s, resends already acked",
				h.id, sq-h.ackStart, tcpHdr.AckNumber()-h.sqStart, tcpHdr.WindowSize(), payloadLen, tcpHdr.Flags())
		}
	}
}

const initialResendDelayMs = int64(200)
const maxResends = 7

func (h *handler) processPackets(ctx context.Context) {
	h.wg.Add(1)
	defer h.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			dlog.Errorf(ctx, "%+v", derror.PanicToError(r))
		}
		h.setState(ctx, stateClosed)
		h.Lock()
		h.ackWaitQueue = nil
		h.oooQueue = nil
		h.Unlock()
	}()
	for {
		select {
		case <-ctx.Done():
			dlog.Debugf(ctx, "   CON %s context done", h.id)
			return
		case pkt, ok := <-h.fromTun:
			if !ok || h.state() == stateClosed {
				dlog.Debugf(ctx, "   CON %s %t, %s", h.id, ok, h.state())
				return
			}
			h.Lock()
			h.process(ctx, pkt)
			h.Unlock()
		}
	}
}

func (h *handler) process(ctx context.Context, pkt Packet) {
	h.checkAckAndPeerWindowSize(ctx, pkt.Header())
	switch h.state() {
	case stateListen:
		h.listen(ctx, pkt)
	case stateSynReceived:
		h.synReceived(ctx, pkt)
	default:
		h.handleReceived(ctx, pkt)
	}
}

func (h *handler) processOutOfOrderPackets(ctx context.Context, seq uint32) {
	for el := h.oooQueue; el != nil; el = el.next {
		if el.sequence != seq {
			break
		}
		th := el.packet.Header()
		payloadLen := len(th.Payload())
		dlog.Tracef(ctx, "   CON %s, Processing out-of-order packet sq %d, an %d, wz %d, len %d, flags %s",
			h.id, th.Sequence()-h.ackStart, th.AckNumber()-h.sqStart, th.WindowSize(), payloadLen, th.Flags())
		seq = el.sequence + uint32(payloadLen)
		h.oooQueue = el.next
		h.sendToMgr(ctx, el.packet)
	}
	h.lastKnown = seq
	h.peerSequenceToAck = seq
}

type resend struct {
	el   *queueElement
	next *resend
}

// processResends resends packages that hasn't been acked using a timeout. This also acts as a fallback
// when no SACKs arrive for those packages.
func (h *handler) processResends(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			dlog.Errorf(ctx, "%+v", derror.PanicToError(r))
		}
	}()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if h.state() == stateClosed {
				return
			}

			var resends *resend

			h.Lock()
			// Reverse the queue because it's in descending ack-order
			for el := h.ackWaitQueue; el != nil; el = el.next {
				resends = &resend{el: el, next: resends}
			}
			h.resend(ctx, now.UnixNano(), resends)
			h.Unlock()
		}
	}
}

func (h *handler) resend(ctx context.Context, now int64, resends *resend) {
	for ; resends != nil; resends = resends.next {
		el := resends.el
		pkt := el.packet
		th := pkt.Header()
		sq := th.Sequence()

		// The tunWriteUnlocked unlocks, so the h.sequenceAcked may be updated for each
		// iteration. We must check that no ack has arrived.
		if sq <= h.sequenceAcked {
			// Packet has been acked already
			continue
		}

		msecs := initialResendDelayMs << el.retries // 200, 400, 800, 1600, ...
		if h.peerPermitsSACK {
			// peer will send SACK unless there's a temporary outage, so timeout can be fairly large
			msecs *= 10
		}
		deadLine := el.cTime + msecs*int64(time.Millisecond)
		if now < deadLine {
			continue
		}

		if el.retries < maxResends {
			el.retries++
			pkt = h.prepareToResend(ctx, pkt)
			dlog.Tracef(ctx, "   CON %s, sq %d, resent after %d ms", h.id, sq-h.sqStart, msecs)
			h.tunWriteUnlocked(ctx, pkt)
			continue
		}

		dlog.Errorf(ctx, "   CON %s, sq %d, packet resent %d times, giving up", h.id, sq-h.sqStart, maxResends)

		// Unlink (queue is reversed here, so this is simple)
		if resends.next == nil {
			// "beginning" of the queue
			h.ackWaitQueue = el.next
		} else {
			resends.next.el.next = el.next
		}
		h.ackWaitQueueSize--
	}
}

func (h *handler) onReceivedSACK(ctx context.Context, sacks []byte) {
	rightEdge := h.sequenceAcked
	if rightEdge >= binary.BigEndian.Uint32(sacks) {
		// DSACK. Already acked now so we won't resend.
		return
	}
	var resends *resend
	// Resend the gaps between the SACKs, and reset the timeout in
	// the process
	now := time.Now().UnixNano()
	for i := 0; i < len(sacks); i += 8 {
		leftEdge := binary.BigEndian.Uint32(sacks[i:])
		for el := h.ackWaitQueue; el != nil; el = el.next {
			if el.sequence >= rightEdge && el.sequence < leftEdge {
				dlog.Tracef(ctx, "   TUN %s, SACK %d-%d", h.id, rightEdge-h.sqStart, leftEdge-h.sqStart)
				el.cTime = now // Let SACK reset resend timeout
				resends = &resend{el: el, next: resends}
			}
		}
		rightEdge = binary.BigEndian.Uint32(sacks[i+4:])
	}
	h.resend(ctx, now, resends)
}

func (h *handler) onReceivedACK(seq uint32) {
	// ack-queue is guaranteed to be sorted descending on sequence, so we cut from the packet with
	// a sequence less than or equal to the received sequence.
	h.sequenceAcked = seq
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
}

func (h *handler) addOutOfOrderPacket(pkt Packet) {
	hdr := pkt.Header()
	sq := hdr.Sequence()

	var prev *queueElement
	for el := h.oooQueue; el != nil; el = el.next {
		if el.sequence == sq {
			return
		}
		if el.sequence > sq {
			break
		}
		prev = el
	}
	pl := &queueElement{
		sequence: sq,
		cTime:    time.Now().UnixNano(),
		packet:   pkt,
	}

	if prev == nil {
		pl.next = h.oooQueue
		h.oooQueue = pl
	} else {
		pl.next = prev.next
		prev.next = pl
	}
}

func (h *handler) state() state {
	return state(atomic.LoadInt32((*int32)(&h.wfState)))
}

func (h *handler) setState(ctx context.Context, s state) {
	oldState := h.state()
	if atomic.CompareAndSwapInt32((*int32)(&h.wfState), int32(oldState), int32(s)) && s != oldState {
		dlog.Debugf(ctx, "   CON %s, state %s -> %s", h.id, oldState, s)
	}
}

// awaitWindowSize must be called with lock in place
func (h *handler) awaitWindowSize(ctx context.Context, sz int64) (proceed bool) {
	ap := &awaitWinSize{
		done:   make(chan struct{}),
		rqSize: sz,
	}
	h.awaitWinSize = ap
	h.Unlock()
	select {
	case <-ctx.Done():
		proceed = false
	case <-ap.done:
		proceed = h.state() != stateClosed
	}
	h.Lock()
	return proceed
}

func (h *handler) checkAckAndPeerWindowSize(ctx context.Context, tcpHeader Header) {
	if tcpHeader.ACK() {
		ackNbr := tcpHeader.AckNumber()
		if ackNbr == 0 {
			return
		}
		h.onReceivedACK(ackNbr)
		opts := tcpHeader.OptionBytes()
		for len(opts) > 0 {
			opt := opts[0]
			if opt == selectiveAck {
				h.onReceivedSACK(ctx, opts[2:])
			}
			if opt == endOfOptions {
				break
			}
			ol := 1
			if opt != noOp {
				ol = int(opts[1])
			}
			opts = opts[ol:]
		}

		sz := int64(tcpHeader.WindowSize()) << h.peerWindowScale
		h.peerWindow = sz

		// Is the processPayload currently waiting for a larger window size in order to continue?
		if ap := h.awaitWinSize; ap != nil {
			wsz := sz - int64(h.sequence-ackNbr)

			// Can we fulfill the request now? If so, remove the awaitWinSize and  close its channel.
			if wsz >= ap.rqSize {
				h.awaitWinSize = nil
				close(ap.done)
			}
		}
	}
}

func (h *handler) receiveWindow() uint32 {
	return atomic.LoadUint32(&h.myWindowSize)
}

func (h *handler) setReceiveWindow(v uint32) {
	atomic.StoreUint32(&h.myWindowSize, v)
}
