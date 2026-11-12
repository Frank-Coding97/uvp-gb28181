package captchahelper

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"math/rand"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	base64Captcha "github.com/mojocn/base64Captcha"
	"golang.org/x/image/font"
)

// 验证码图的展示尺寸（CSS 像素），必须与 web/src/views/login/components/login-form.vue
// 里 .verifyCodeImg 的 width/height 保持一致：
// 后端按 2 倍分辨率出图、前端等比缩放，高清屏下才不会发虚；尺寸一致则不会被裁剪。
const (
	CaptchaDisplayWidth  = 142
	CaptchaDisplayHeight = 46
	captchaRenderScale   = 2
)

// 背景与字色都是固定调色板，不取随机 RGB：
// 库自带的 RandDeepColor 会算出接近白色（对比度极低）的颜色，是"看不清"的根因之一。
var (
	captchaBgColor = color.RGBA{R: 0xF6, G: 0xF9, B: 0xFF, A: 0xFF}

	captchaTextPalette = []color.RGBA{
		{R: 0x1F, G: 0x4E, B: 0x79, A: 0xFF}, // 深蓝
		{R: 0x7A, G: 0x2E, B: 0x2E, A: 0xFF}, // 深红
		{R: 0x1F, G: 0x6B, B: 0x4A, A: 0xFF}, // 深绿
		{R: 0x5B, G: 0x3A, B: 0x8E, A: 0xFF}, // 深紫
		{R: 0x8A, G: 0x4B, B: 0x1A, A: 0xFF}, // 深棕
		{R: 0x14, G: 0x67, B: 0x6B, A: 0xFF}, // 深青
		{R: 0x2C, G: 0x3E, B: 0x50, A: 0xFF}, // 深灰蓝
	}
)

// 干扰元素统一用浅色，且全部绘制在文字之前（文字压在干扰之上），
// 既能防简单 OCR，又不会像"干扰线划在字上"那样把人眼挡住。
var (
	captchaLineColor = color.RGBA{R: 0xD4, G: 0xE2, B: 0xF3, A: 0xFF}
	captchaDotColor  = color.RGBA{R: 0xC2, G: 0xD6, B: 0xEC, A: 0xFF}
)

// CaptchaFonts 可用的验证码字体：只用字形清晰、笔画够粗的字体。
// chromohv / Comismsh / DeborahFancyDress 这类手写细体在登录框这么小的高度下几乎不可辨认，已弃用；
// actionj / 3Dumb / ApothecaryFont 是空心或描边字形，笔画被掏空后同样不好认，也不再混用。
var CaptchaFonts = []string{
	"wqy-microhei.ttc",
}

// ReadableDriver 自绘验证码：可控的字号/站位/配色，保证"人眼一眼能认出来"。
// 只实现 base64Captcha.Driver，存储与校验仍然复用库的 Store / Verify。
type ReadableDriver struct {
	width  int
	height int
	length int
	source string
	fonts  []*truetype.Font
}

// NewReadableDriver 创建自绘验证码驱动，length 为验证码字符数（非法值回落为 4）。
func NewReadableDriver(length int) *ReadableDriver {
	if length <= 0 || length > 8 {
		length = 4
	}
	return &ReadableDriver{
		width:  CaptchaDisplayWidth * captchaRenderScale,
		height: CaptchaDisplayHeight * captchaRenderScale,
		length: length,
		// 去掉 i/l/o/0/1 这类易混字符
		source: "abcdefghjkmnpqrstuvwxyz23456789",
		fonts:  loadCaptchaFonts(CaptchaFonts),
	}
}

func loadCaptchaFonts(names []string) []*truetype.Font {
	loaded := make([]*truetype.Font, 0, len(names))
	for _, name := range names {
		if f := base64Captcha.DefaultEmbeddedFonts.LoadFontByName("fonts/" + name); f != nil {
			loaded = append(loaded, f)
		}
	}
	return loaded
}

func (d *ReadableDriver) GenerateIdQuestionAnswer() (id, content, answer string) {
	content = base64Captcha.RandText(d.length, d.source)
	return base64Captcha.RandomId(), content, content
}

func (d *ReadableDriver) DrawCaptcha(content string) (base64Captcha.Item, error) {
	img := image.NewNRGBA(image.Rect(0, 0, d.width, d.height))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: captchaBgColor}, image.Point{}, draw.Src)

	d.drawInterference(img)
	if err := d.drawChars(img, content); err != nil {
		return nil, err
	}
	return &pngItem{img: img}, nil
}

// drawInterference 画浅色波浪线 + 浅色散点，全部在文字之下。
func (d *ReadableDriver) drawInterference(img *image.NRGBA) {
	w, h := d.width, d.height
	for i := 0; i < 2; i++ {
		amp := float64(h) * (0.10 + 0.10*rand.Float64())
		mid := float64(h) * (0.30 + 0.40*rand.Float64())
		freq := 2 * math.Pi / float64(w) * (1 + rand.Float64())
		phase := rand.Float64() * 2 * math.Pi
		thickness := 1 * captchaRenderScale
		for x := 0; x < w; x++ {
			y := int(math.Round(mid + amp*math.Sin(freq*float64(x)+phase)))
			for t := 0; t < thickness; t++ {
				setPixel(img, x, y+t, captchaLineColor)
			}
		}
	}
	for i := 0; i < 40; i++ {
		cx, cy := rand.Intn(w), rand.Intn(h)
		radius := 1 * captchaRenderScale
		for dx := -radius; dx <= radius; dx++ {
			for dy := -radius; dy <= radius; dy++ {
				if dx*dx+dy*dy <= radius*radius {
					setPixel(img, cx+dx, cy+dy, captchaDotColor)
				}
			}
		}
	}
}

// drawChars 逐字符居中绘制：字号随行高变化但有上限（不挤出单元格），
// 纵向做轻微抖动，颜色取深色系固定调色板。
func (d *ReadableDriver) drawChars(img *image.NRGBA, content string) error {
	runes := []rune(content)
	if len(runes) == 0 {
		return fmt.Errorf("验证码内容为空")
	}
	cellWidth := float64(d.width) / float64(len(runes))

	for i, r := range runes {
		if len(d.fonts) == 0 {
			return fmt.Errorf("验证码字体未加载")
		}
		f := d.fonts[rand.Intn(len(d.fonts))]
		fontSize := float64(d.height) * (0.54 + 0.10*rand.Float64())
		if maxByCell := cellWidth * 0.68; fontSize > maxByCell {
			fontSize = maxByCell
		}

		face := truetype.NewFace(f, &truetype.Options{Size: fontSize, DPI: 72, Hinting: font.HintingFull})
		advance, ok := face.GlyphAdvance(r)
		metrics := face.Metrics()
		_ = face.Close()
		if !ok {
			continue
		}

		advancePx := float64(advance) / 64
		ascent := float64(metrics.Ascent) / 64
		descent := float64(metrics.Descent) / 64

		centerX := cellWidth*float64(i) + cellWidth/2
		centerY := float64(d.height)/2 + (rand.Float64()-0.5)*float64(d.height)*0.08
		x := centerX - advancePx/2
		baseline := centerY + (ascent-descent)/2

		c := freetype.NewContext()
		c.SetDPI(72)
		c.SetFont(f)
		c.SetFontSize(fontSize)
		c.SetClip(img.Bounds())
		c.SetDst(img)
		c.SetSrc(image.NewUniform(captchaTextPalette[rand.Intn(len(captchaTextPalette))]))
		c.SetHinting(font.HintingFull)
		pt := freetype.Pt(int(math.Round(x)), int(math.Round(baseline)))
		if _, err := c.DrawString(string(r), pt); err != nil {
			return err
		}
	}
	return nil
}

func setPixel(img *image.NRGBA, x, y int, c color.RGBA) {
	if !(image.Point{X: x, Y: y}).In(img.Bounds()) {
		return
	}
	img.SetNRGBA(x, y, color.NRGBA{R: c.R, G: c.G, B: c.B, A: c.A})
}

type pngItem struct {
	img *image.NRGBA
}

func (i *pngItem) encode() ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, i.img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (i *pngItem) WriteTo(w io.Writer) (int64, error) {
	raw, err := i.encode()
	if err != nil {
		return 0, err
	}
	n, err := w.Write(raw)
	return int64(n), err
}

func (i *pngItem) EncodeB64string() string {
	raw, err := i.encode()
	if err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
}
