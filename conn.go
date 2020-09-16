package main

import (
	"context"
	"io"
	"net"
	"os"
	"time"
)

const (
	MaxRecvPktCount = 1000
	MaxSendPktCount = 1000
)

type KumakoConn struct {
	io.ReadWriteCloser
	subConns []*KumakoSubConn

	threads int

	recvSeq    int
	sendSeq    int
	readRemain []byte

	//dataArrivedCond *sync.Cond
	//dataSentCond    *sync.Cond

	dataArrived chan int
	dataSent chan int

	subConnArrived chan int

	sendIndex int

	running bool
	started bool
}

func NewKumakoConn() *KumakoConn {
	conn := &KumakoConn{
		threads: 0,
		running: true,
		started: false,
		recvSeq: 0,
		sendSeq: 1,

		subConns: make([]*KumakoSubConn, 0),

		readRemain:      make([]byte, 0),
		//dataArrivedCond: sync.NewCond(&sync.Mutex{}),
		//dataSentCond:    sync.NewCond(&sync.Mutex{}),

		dataArrived: make(chan int),
		dataSent: make(chan int),
		subConnArrived:  make(chan int),

		sendIndex: 0,
	}

	return conn
}

func (k *KumakoConn) AddSubConn(conn *net.TCPConn) {
	subConn := NewKumakoSubConn(conn, k)
	k.subConns = append(k.subConns, subConn)

	subConn.Start()

	k.started = true

	k.threads++
	select {
	case k.subConnArrived <- 1:
	default:
	}
}

func (k *KumakoConn) Close() error {
	if !k.running {
		return nil
	}

	k.running = false

	for _, subConn := range k.subConns {
		subConn.running = false
		subConn.realConn.Close()
	}

	return nil
}

func (k *KumakoConn) getSeq(seq int) *KumakoPacket {
	for _, subConn := range k.subConns {
		//we only need get the first packet because the packet seq and the desired seq is both ordered
		pktIntf := subConn.recvQueue.GetFirst()

		if pktIntf == nil {
			continue
		}

		pkt := pktIntf.(*KumakoPacket)

		if pkt.Seq == uint32(seq) {
			subConn.recvQueue.Dequeue()

			//notify recv routine to continue receiving
			select {
			case subConn.recvFree <- 1:
			default:
			}

			return pkt
		}
	}

	return nil
}

func (k *KumakoConn) calcRunningCount() int {
	runningCount := 0
	for _, subConn := range k.subConns {
		if subConn.running {
			runningCount++
		}
	}

	if k.started && runningCount == 0 {
		k.running = false
	}

	return runningCount
}

func (k *KumakoConn) Read(p []byte) (int, error) {
	//read the remain
	if len(k.readRemain) > 0 {
		l := len(k.readRemain)

		if len(p) < l {
			l = len(p)
		}

		copy(p, k.readRemain[:l])
		k.readRemain = k.readRemain[l:]

		return l, nil
	}

	for {
		//get running sub connection count
		runningCount := k.calcRunningCount()

		if runningCount > 0 {
			break
		}

		select {
		case <-k.subConnArrived:
		case <-time.After(100 * time.Millisecond):
		}

		if !k.running {
			break
		}
	}

	if !k.running {
		return 0, io.EOF
	}

	var pkt *KumakoPacket

	pkt = k.getSeq(k.recvSeq + 1)
	for pkt == nil {
		select {
		case <-k.dataArrived:
		case <-time.After(100 * time.Millisecond):
		}

		if !k.running {
			break
		}

		pkt = k.getSeq(k.recvSeq + 1)
	}

	var err error = nil
	if !k.running {
		err = io.EOF
	}

	if pkt != nil {
		l := len(pkt.Data)
		if len(p) < l {
			l = len(p)
		}

		copy(p, pkt.Data[:l])

		if len(p) < len(pkt.Data) {
			k.readRemain = append(k.readRemain, pkt.Data[len(p):]...)
		}

		k.recvSeq++

		LogD("RECV", "Read data %d bytes seq %d.", l, pkt.Seq)

		return l, err
	}

	return 0, err
}

func (k *KumakoConn) Write(p []byte) (int, error) {
	if !k.running {
		return 0, io.EOF
	}

	offset := 0

	for offset < len(p) && k.running {
		l := len(p) - offset

		if l > DATA_MSG_MAX_LEN {
			l = DATA_MSG_MAX_LEN
		}

		pkt := NewKumakoPacket(uint32(k.sendSeq), p[offset:offset+l])

		success := false

		for i:=0; i<k.threads; i++ {
			k.sendIndex = (k.sendIndex + 1) % k.threads
			if k.subConns[k.sendIndex].SendPacket(pkt) {
				success = true
				LogD("SEND", "Sent data %d bytes seq %d to #%d.", l, k.sendSeq, k.sendIndex)
				break
			}
		}

		if !success {
			//no buffer, need wait
			select {
			case <-k.dataSent:
			case <-time.After(100 * time.Millisecond):
			}
		} else {
			offset += l
			k.sendSeq++
		}
	}

	var err error = nil
	if !k.running {
		err = io.EOF
	}

	return offset, err
}

type KumakoSubConn struct {
	realConn *net.TCPConn
	//sendPkt  []*KumakoPacket
	//sendCond *sync.Cond
	//recvPkt  []*KumakoPacket
	//
	//sendPktLock  sync.Mutex
	//recvPktLock  sync.Mutex
	//recvFreeCond *sync.Cond

	sendQueue *Queue
	recvQueue *Queue

	recvFree chan int

	ctx context.Context

	running bool

	parent *KumakoConn
}

func NewKumakoSubConn(realConn *net.TCPConn, parent *KumakoConn) *KumakoSubConn {
	subConn := &KumakoSubConn{
		realConn: realConn,
		parent:   parent,

		sendQueue: NewQueue(),
		recvQueue: NewQueue(),
		recvFree: make(chan int),

		running: false,
	}

	return subConn
}

func (k *KumakoSubConn) Start() {
	k.running = true

	go k.SendRoutine()
	go k.RecvRoutine()
}

func (k *KumakoSubConn) SendRoutine() {
	buf := make([]byte, 1500)
	defer k.realConn.Close()

	for k.running || k.sendQueue.GetLen() > 0 {
		//get first packet
		pktIntf, err := k.sendQueue.DequeueTimeout(100 * time.Millisecond)

		if err != nil {
			continue
		}

		pkt := pktIntf.(*KumakoPacket)

		//encode packet
		l, err := pkt.Encode(buf)
		if err != nil {
			LogF("SEND", "pkt.Encode error: %s!", err.Error())
			os.Exit(1)
		}

		_, err = WriteFull(buf[:l], k.realConn)

		if err != nil {
			if k.running {
				LogE("SEND", "k.realConn.Write error: %s!", err.Error())
			}
			k.parent.Close()
			return
		}

		//send complete, notify parent writer
		LogD("SEND", "###sent packet %d bytes seq %d.###", pkt.DataLen, pkt.Seq)

		select {
		case k.parent.dataSent <- 1:
		default:
		}
	}
}

func (k *KumakoSubConn) RecvRoutine() {
	defer k.realConn.Close()

	for k.running {
		//recv packet
		complete := false
		eof := false
		pkt := &KumakoPacket{}

		buf := make([]byte, pkt.HeaderSize())

		l, err := ReadFull(buf, k.realConn)

		if l == pkt.HeaderSize() {
			_, err2 := pkt.Decode(buf)
			if err2 == nil {
				data := make([]byte, pkt.DataLen)

				ll, err3 := ReadFull(data, k.realConn)

				if ll == int(pkt.DataLen) {
					pkt.Data = data
					complete = true
				}

				if err3 != nil {
					if err3 == io.EOF {
						eof = true
					} else {
						if k.running {
							LogE("RECV", "recv packet data failed: %s!", err3.Error())
						}
						k.parent.Close()
					}
				}
			} else {
				LogE("RECV", "pkt.Decode error: %s!", err2.Error())
				k.parent.Close()
			}
		}

		if err != nil {
			if err == io.EOF {
				eof = true
			} else {
				if k.running {
					LogE("RECV", "recv packet header failed: %s!", err.Error())
				}
				k.parent.Close()
			}
		}

		if complete {
			//if queue full, wait parent to read
			for k.recvQueue.GetLen() > MaxRecvPktCount {
				select {
				case <-k.recvFree:
				case <-time.After(100 * time.Millisecond):
				}

				if !k.running {
					return
				}
			}

			//add packet to queue
			k.recvQueue.Enqueue(pkt)

			LogD("RECV", "###recv packet %d bytes seq %d.###", pkt.DataLen, pkt.Seq)

			//notify parent
			select {
			case k.parent.dataArrived <- 1:
			default:
			}
		}

		if eof {
			if k.running {
				LogI("RECV", "connection closed.")
			}

			k.running = false
			k.parent.calcRunningCount()
		}
	}
}

func (k *KumakoSubConn) SendPacket(pkt *KumakoPacket) bool {
	if !k.running {
		return false
	}

	if k.sendQueue.GetLen() > MaxSendPktCount {
		return false
	}

	k.sendQueue.Enqueue(pkt)

	return true
}
