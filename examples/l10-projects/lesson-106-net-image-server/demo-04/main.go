package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
)

// applyOrientation 按 EXIF 方向 1 到 8 做镜像与旋转，返回校正后的图。
func applyOrientation(src image.Image, o int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	switch o {
	case 5, 6, 7, 8:
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := src.At(b.Min.X+x, b.Min.Y+y)
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			default:
				dx, dy = x, y
			}
			dst.Set(dx, dy, c)
		}
	}
	return dst
}

// resizeNN 用最近邻等比缩到指定宽度，高度按原比例取整。
func resizeNN(src image.Image, width int) image.Image {
	b := src.Bounds()
	if width <= 0 || b.Dx() == 0 {
		return src
	}
	height := b.Dy() * width / b.Dx()
	if height <= 0 {
		height = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		sy := b.Min.Y + y*b.Dy()/height
		for x := 0; x < width; x++ {
			sx := b.Min.X + x*b.Dx()/width
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

// flatten 把带 alpha 的图合成到白底，PNG 转 JPEG 前必须调用。
func flatten(src image.Image) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(dst, b, src, b.Min, draw.Over)
	return dst
}

func main() {
	src := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			src.Set(x, y, color.RGBA{R: uint8(x * 60), G: 120, B: uint8(y * 100), A: 255})
		}
	}
	rotated := applyOrientation(src, 6) // 模拟手机竖拍，方向 6
	fmt.Println("方向校正:", src.Bounds(), "到", rotated.Bounds())
	thumb := resizeNN(rotated, 3)
	fmt.Println("缩略图:", thumb.Bounds())

	// 解码后重编码既是产物，也是一道安全清洗，把输入归一化成安全格式。
	var jbuf bytes.Buffer
	if err := jpeg.Encode(&jbuf, flatten(thumb), &jpeg.Options{Quality: 80}); err != nil {
		fmt.Println("JPEG 编码失败:", err)
		return
	}
	decoded, err := jpeg.Decode(bytes.NewReader(jbuf.Bytes()))
	if err != nil {
		fmt.Println("JPEG 回读失败:", err)
		return
	}
	fmt.Println("JPEG 回读:", decoded.Bounds(), "字节=", jbuf.Len())

	var pbuf bytes.Buffer
	if err := png.Encode(&pbuf, thumb); err != nil {
		fmt.Println("PNG 编码失败:", err)
		return
	}
	pdec, err := png.Decode(bytes.NewReader(pbuf.Bytes()))
	if err != nil {
		fmt.Println("PNG 回读失败:", err)
		return
	}
	fmt.Println("PNG 回读:", pdec.Bounds(), "字节=", pbuf.Len())
}
