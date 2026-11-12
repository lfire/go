package kv

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	magic       byte = 0x4B // 'K'
	version     byte = 1
	headerLen        = 12
	maxFrame         = 16 << 20 // 单帧上限 16 MiB，防伪造长度打爆内存
	maxKeyLen        = 1024
	maxValueLen      = 16 << 20
)

const (
	statusRequest byte = 0
	statusOK      byte = 1
	statusError   byte = 2
	statusPing    byte = 3
	statusPong    byte = 4
)

var ErrBadFrame = errors.New("kv: bad frame")

// Frame 是网络上传输的一个协议帧。请求与响应共用结构，靠 Status 区分；
// Op 为 0 的帧是控制帧（心跳），不映射到 Command。
type Frame struct {
	Op      Op
	Status  byte
	ReqID   uint32
	Key     string
	Value   []byte
	Found   bool
	ErrCode byte
}

func writeFrame(w io.Writer, f Frame) error {
	if len(f.Key) > maxKeyLen {
		return fmt.Errorf("%w: key too long", ErrBadFrame)
	}
	if len(f.Value) > maxValueLen {
		return fmt.Errorf("%w: value too long", ErrBadFrame)
	}
	kl := make([]byte, 2)
	binary.BigEndian.PutUint16(kl, uint16(len(f.Key)))
	found := byte(0)
	if f.Found {
		found = 1
	}
	body := make([]byte, 0, 2+len(f.Key)+2+len(f.Value))
	body = append(body, kl...)
	body = append(body, f.Key...)
	body = append(body, found, f.ErrCode)
	body = append(body, f.Value...)

	head := make([]byte, headerLen)
	head[0], head[1], head[2], head[3] = magic, version, byte(f.Op), f.Status
	binary.BigEndian.PutUint32(head[4:8], uint32(len(body)))
	binary.BigEndian.PutUint32(head[8:12], f.ReqID)
	if _, err := w.Write(head); err != nil {
		return err
	}
	_, err := w.Write(body)
	return err
}

// readFrame 先读满 12 字节头，校验魔数与版本，再按 bodyLen 读满 body。
// 所有长度校验都在分配 body 之前完成，毒包不会触发大内存分配。
func readFrame(r io.Reader) (Frame, error) {
	head := make([]byte, headerLen)
	if _, err := io.ReadFull(r, head); err != nil {
		return Frame{}, err
	}
	if head[0] != magic {
		return Frame{}, fmt.Errorf("%w: bad magic", ErrBadFrame)
	}
	if head[1] != version {
		return Frame{}, fmt.Errorf("%w: unsupported version", ErrBadFrame)
	}
	n := binary.BigEndian.Uint32(head[4:8])
	if n > maxFrame {
		return Frame{}, fmt.Errorf("%w: frame size %d", ErrBadFrame, n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return Frame{}, err
	}
	if len(body) < 4 {
		return Frame{}, fmt.Errorf("%w: body too short", ErrBadFrame)
	}
	kl := int(binary.BigEndian.Uint16(body[:2]))
	if kl > maxKeyLen || 2+kl+2 > len(body) {
		return Frame{}, fmt.Errorf("%w: bad key length", ErrBadFrame)
	}
	return Frame{
		Op:      Op(head[2]),
		Status:  head[3],
		ReqID:   binary.BigEndian.Uint32(head[8:12]),
		Key:     string(body[2 : 2+kl]),
		Found:   body[2+kl] == 1,
		ErrCode: body[2+kl+1],
		Value:   append([]byte(nil), body[2+kl+2:]...),
	}, nil
}
