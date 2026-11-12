package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	_ "image/jpeg"
	"image/png"
)

const maxPixels = 40_000_000 // 四千万像素上限

// checkPixels 只用 DecodeConfig 读头部，不分配像素缓冲，挡住像素炸弹。
func checkPixels(head []byte) (int, int, string, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(head))
	if err != nil {
		return 0, 0, "", fmt.Errorf("解码配置失败: %w", err)
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return 0, 0, "", fmt.Errorf("像素炸弹: %dx%d 超过上限", cfg.Width, cfg.Height)
	}
	return cfg.Width, cfg.Height, format, nil
}

func tiffOrientation(t []byte) (int, error) {
	if len(t) < 8 {
		return 1, errors.New("TIFF 头过短")
	}
	var order binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1, errors.New("TIFF 字节序非法")
	}
	if order.Uint16(t[2:4]) != 42 {
		return 1, errors.New("TIFF 魔数错误")
	}
	ifd := int(order.Uint32(t[4:8]))
	if ifd+2 > len(t) {
		return 1, errors.New("IFD 越界")
	}
	n := int(order.Uint16(t[ifd : ifd+2]))
	for i := 0; i < n; i++ {
		e := ifd + 2 + i*12
		if e+12 > len(t) {
			break
		}
		if order.Uint16(t[e:e+2]) == 0x0112 && order.Uint16(t[e+2:e+4]) == 3 {
			v := int(order.Uint16(t[e+8 : e+10]))
			if v >= 1 && v <= 8 {
				return v, nil
			}
		}
	}
	return 1, nil
}

// jpegOrientation 扫描 APP1 段，读取 Exif 里的 Orientation 标签。
func jpegOrientation(data []byte) (int, error) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1, errors.New("不是 JPEG")
	}
	for pos := 2; pos+4 <= len(data); {
		if data[pos] != 0xFF {
			pos++
			continue
		}
		marker := data[pos+1]
		if marker == 0xD9 || marker == 0xDA {
			break
		}
		if marker == 0xD8 || (marker >= 0xD0 && marker <= 0xD7) {
			pos += 2
			continue
		}
		segLen := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
		if segLen < 2 || pos+2+segLen > len(data) {
			return 1, errors.New("段长度非法")
		}
		payload := data[pos+4 : pos+2+segLen]
		if marker == 0xE1 && len(payload) >= 6 && string(payload[:6]) == "Exif\x00\x00" {
			return tiffOrientation(payload[6:])
		}
		pos += 2 + segLen
	}
	return 1, nil
}

// buildExifJPEG 构造一段最小 JPEG：SOI 加 APP1 加 EOI，仅用于验证解析器。
func buildExifJPEG(o int) []byte {
	tiff := []byte{
		'I', 'I', 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00,
		0x01, 0x00,
		0x12, 0x01, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00, byte(o), 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
	payload := append([]byte("Exif\x00\x00"), tiff...)
	segLen := len(payload) + 2
	seg := append([]byte{0xFF, 0xE1, byte(segLen >> 8), byte(segLen & 0xFF)}, payload...)
	out := append([]byte{0xFF, 0xD8}, seg...)
	return append(out, 0xFF, 0xD9)
}

// pngBomb 构造 CRC 合法的 IHDR，声明 30000 乘 30000 像素，不携带像素数据。
func pngBomb() []byte {
	ihdr := []byte{0, 0, 0x75, 0x30, 0, 0, 0x75, 0x30, 8, 2, 0, 0, 0}
	body := append([]byte("IHDR"), ihdr...)
	crc := crc32.ChecksumIEEE(body)
	chunk := []byte{0, 0, 0, 13}
	chunk = append(chunk, body...)
	chunk = append(chunk, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc))
	sig := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	return append(sig, chunk...)
}

func main() {
	var src bytes.Buffer
	if err := png.Encode(&src, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		fmt.Println("造图失败:", err)
		return
	}
	w, h, format, err := checkPixels(src.Bytes())
	fmt.Println("合法小图:", w, h, format, err)
	_, _, _, err = checkPixels(pngBomb())
	fmt.Println("像素炸弹:", err)
	o, err := jpegOrientation(buildExifJPEG(6))
	fmt.Println("EXIF 方向:", o, err)
}
