package main

import (
	"encoding/binary"
	"fmt"
)

const (
	DATA_MSG_MAX_LEN = 1420
	KUMAKO_MAGIC     = 0x4B554D41
)

type KumakoHandshakePacket struct {
	Magic uint32
	Version uint8
	SessionID uint32
}

func NewKumakoHandshakePacket(sessionID uint32) *KumakoHandshakePacket {
	pkt := &KumakoHandshakePacket{
		Magic: KUMAKO_MAGIC,
		Version: 1,
		SessionID: sessionID,
	}

	return pkt
}

func (pkt *KumakoHandshakePacket) Encode(buf []byte) (int, error) {
	if len(buf) < pkt.HeaderSize() {
		return 0, fmt.Errorf("buffer size too small: %d, need %d", len(buf), pkt.HeaderSize())
	}

	binary.LittleEndian.PutUint32(buf[0:4], pkt.Magic)
	buf[4] = byte(pkt.Version)
	binary.LittleEndian.PutUint32(buf[5:9], pkt.SessionID)

	return pkt.HeaderSize(), nil
}

func (pkt *KumakoHandshakePacket) Decode(buf []byte) (int, error) {
	if len(buf) < pkt.HeaderSize() {
		return 0, nil
	}

	magic := binary.LittleEndian.Uint32(buf[0:4])
	if magic != KUMAKO_MAGIC {
		return 4, fmt.Errorf("not a kumako packet: magic %X != %X", magic, KUMAKO_MAGIC)
	}

	version := buf[4]
	if version != 1 {
		return 5, fmt.Errorf("unknown version: %d", version)
	}

	sessionID := binary.LittleEndian.Uint32(buf[5:9])

	pkt.Magic = magic
	pkt.Version = version
	pkt.SessionID = sessionID

	return pkt.HeaderSize(), nil
}

func (pkt *KumakoHandshakePacket) HeaderSize() int {
	return 9
}

type KumakoPacket struct {
	Magic   uint32
	Version uint8
	Seq     uint32
	DataLen uint16
	Data    []byte
}

func NewKumakoPacket(seq uint32, data []byte) *KumakoPacket {
	pkt := &KumakoPacket{
		Magic:   KUMAKO_MAGIC,
		Version: 1,
		Seq:     seq,
		DataLen: uint16(len(data)),
		Data: make([]byte, len(data)),
	}

	copy(pkt.Data, data)

	return pkt
}

func (pkt *KumakoPacket) Encode(buf []byte) (int, error) {
	l := pkt.HeaderSize() + len(pkt.Data)
	if len(buf) < l {
		return 0, fmt.Errorf("buffer size too small: %d, need %d", len(buf), l)
	}

	if int(pkt.DataLen) != len(pkt.Data) {
		pkt.DataLen = uint16(len(pkt.Data))
	}

	binary.LittleEndian.PutUint32(buf[0:4], pkt.Magic)
	buf[4] = byte(pkt.Version)
	binary.LittleEndian.PutUint32(buf[5:9], pkt.Seq)
	binary.LittleEndian.PutUint16(buf[9:11], pkt.DataLen)
	copy(buf[pkt.HeaderSize():pkt.HeaderSize()+int(pkt.DataLen)], pkt.Data[:pkt.DataLen])

	//LogD("PROTO", "(encode) first is %d", pkt.Data[0])

	return l, nil
}

func (pkt *KumakoPacket) Decode(buf []byte) (int, error) {
	if len(buf) < pkt.HeaderSize() {
		return 0, nil
	}

	magic := binary.LittleEndian.Uint32(buf[0:4])
	if magic != KUMAKO_MAGIC {
		return 4, fmt.Errorf("not a kumako packet: magic %X != %X", magic, KUMAKO_MAGIC)
	}

	version := buf[4]
	if version != 1 {
		return 5, fmt.Errorf("unknown version: %d", version)
	}

	seq := binary.LittleEndian.Uint32(buf[5:9])
	dataLen := binary.LittleEndian.Uint16(buf[9:11])

	pkt.Magic = magic
	pkt.Version = uint8(version)
	pkt.Seq = seq
	pkt.DataLen = dataLen

	return pkt.HeaderSize(), nil
}

func (pkt *KumakoPacket) HeaderSize() int {
	return 11
}

func (pkt *KumakoPacket) Size() int {
	return pkt.HeaderSize() + len(pkt.Data)
}
