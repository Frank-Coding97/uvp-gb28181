package captchahelper

import (
	"encoding/base64"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/mojocn/base64Captcha"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 生成一张验证码图并解码。
func drawOne(t *testing.T, length int) image.Image {
	t.Helper()
	driver := NewReadableDriver(length)
	item, err := driver.DrawCaptcha(base64Captcha.RandText(length, driver.source))
	require.NoError(t, err)

	encoded := item.EncodeB64string()
	const prefix = "data:image/png;base64,"
	require.True(t, strings.HasPrefix(encoded, prefix), "验证码图必须是 png 的 data URI")

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, prefix))
	require.NoError(t, err)

	img, err := png.Decode(strings.NewReader(string(raw)))
	require.NoError(t, err)
	return img
}

// 验证码字太浅/太小 = 人眼认不出，这里用「深色像素数量」兜住这个回归。
func TestDrawCaptchaHasEnoughDarkInk(t *testing.T) {
	const minDarkPixels = 400
	for i := 0; i < 20; i++ {
		img := drawOne(t, 4)
		dark := 0
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				r, g, bl, _ := img.At(x, y).RGBA()
				// ITU-R BT.601 亮度，取 16 位色深后与 110/255 等价
				if (299*r+587*g+114*bl)/1000 < 110*257 {
					dark++
				}
			}
		}
		assert.GreaterOrEqual(t, dark, minDarkPixels, "第 %d 张验证码深色像素过少，说明字太浅或太小", i+1)
	}
}

func TestDrawCaptchaSize(t *testing.T) {
	img := drawOne(t, 4)
	wantW := CaptchaDisplayWidth * captchaRenderScale
	wantH := CaptchaDisplayHeight * captchaRenderScale
	assert.Equal(t, wantW, img.Bounds().Dx())
	assert.Equal(t, wantH, img.Bounds().Dy())
}

func TestNewReadableDriverLengthFallback(t *testing.T) {
	for _, in := range []int{0, -3, 99} {
		assert.Equal(t, 4, NewReadableDriver(in).length, "非法长度 %d 应回落为 4", in)
	}
	assert.Equal(t, 5, NewReadableDriver(5).length)
}

// 长度与 Store 校验链路：答案正确才通过，且校验后立即失效。
func TestReadableDriverVerifyFlow(t *testing.T) {
	store := base64Captcha.NewMemoryStore(64, captchaTTL)
	driver := NewReadableDriver(4)
	c := base64Captcha.NewCaptcha(driver, store)

	id, _, answer, err := c.Generate()
	require.NoError(t, err)
	require.Len(t, []rune(answer), 4)
	for _, r := range answer {
		assert.Contains(t, driver.source, string(r))
	}

	assert.False(t, c.Verify(id, answer+"x", false), "错误答案不应通过")
	assert.True(t, store.Verify(id, answer, true), "正确答案应通过")
	assert.False(t, store.Verify(id, answer, true), "已消费的验证码应失效")

	id2, _, answer2, err := c.Generate()
	require.NoError(t, err)
	assert.True(t, store.Verify(id2, answer2, true), "正确答案应通过")
	assert.False(t, store.Verify(id2, answer2, true), "已消费的验证码应失效")
}
