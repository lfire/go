// 文件：record.go
package kv

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

// WAL 记录格式：length(4, uint32 BE) | crc32c(4) | payload。
// payload = op(1) keyLen(2) valLen(4) key value。
// 长度挡住半条记录，CRC32C 挡住位翻转与扇区撕裂：两者缺一不可。
const (
	recordHead = 8
	maxRecord  = 16 << 20
)

var crcTable = crc32.MakeTable(crc32.Castagnoli)

func validOp(op Op) bool {
	return op == OpGet || op == OpSet || op == OpDel
}

func encodeCommand(cmd Command) []byte {
	buf := make([]byte, 0, 7+len(cmd.Key)+len(cmd.Value))
	buf = append(buf, byte(cmd.Op))
	var kl [2]byte
	binary.BigEndian.PutUint16(kl[:], uint16(len(cmd.Key)))
	buf = append(buf, kl[:]...)
	var vl [4]byte
	binary.BigEndian.PutUint32(vl[:], uint32(len(cmd.Value)))
	buf = append(buf, vl[:]...)
	buf = append(buf, cmd.Key...)
	buf = append(buf, cmd.Value...)
	return buf
}

func decodeCommand(b []byte) (Command, error) {
	if len(b) < 7 {
		return Command{}, ErrCorrupt
	}
	kl := uint64(binary.BigEndian.Uint16(b[1:3]))
	vl := uint64(binary.BigEndian.Uint32(b[3:7]))
	if kl+vl+7 != uint64(len(b)) {
		return Command{}, ErrCorrupt
	}
	if !validOp(Op(b[0])) {
		return Command{}, ErrBadOp
	}
	off := 7 + int(kl)
	return Command{
		Op:    Op(b[0]),
		Key:   string(b[7:off]),
		Value: append([]byte(nil), b[off:]...),
	}, nil
}

// encodeRecord 把命令封成带长度与 CRC32C 的完整记录。
func encodeRecord(cmd Command) ([]byte, error) {
	payload := encodeCommand(cmd)
	if len(payload)+recordHead > maxRecord {
		return nil, fmt.Errorf("%w: payload %d", ErrPartialWrite, len(payload))
	}
	rec := make([]byte, recordHead+len(payload))
	binary.BigEndian.PutUint32(rec[0:4], uint32(len(payload)))
	binary.BigEndian.PutUint32(rec[4:8], crc32.Checksum(payload, crcTable))
	copy(rec[recordHead:], payload)
	return rec, nil
}

// decodeRecord 校验一条完整记录；CRC 不符返回 ErrCorrupt，绝不静默跳过。
func decodeRecord(rec []byte) (Command, error) {
	if len(rec) < recordHead {
		return Command{}, ErrCorrupt
	}
	n := uint64(binary.BigEndian.Uint32(rec[0:4]))
	if n+recordHead != uint64(len(rec)) || n > maxRecord {
		return Command{}, ErrCorrupt
	}
	payload := rec[recordHead:]
	if crc32.Checksum(payload, crcTable) != binary.BigEndian.Uint32(rec[4:8]) {
		return Command{}, ErrCorrupt
	}
	return decodeCommand(payload)
}
