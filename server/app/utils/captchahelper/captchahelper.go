package captchahelper

import (
	"sync"
	"time"
	"uvplatform.com/uvp-gb28181/app/global/app"

	"github.com/mojocn/base64Captcha"
)

type CaptchaHelper struct {
	driver *base64Captcha.Captcha
	store  base64Captcha.Store
}

var (
	captchaHelperInstance *CaptchaHelper
	captchaHelperOnce     sync.Once
)

// 验证码有效期：超时即失效，避免一次出码被无限次重试。
const captchaTTL = 5 * time.Minute

// GetCaptchaHelper 获取CaptchaHelper单例
func GetCaptchaHelper() *CaptchaHelper {
	captchaHelperOnce.Do(func() {
		captchaHelperInstance = NewCaptchaHelper()
	})
	return captchaHelperInstance
}

// NewCaptchaHelper 创建CaptchaHelper实例
func NewCaptchaHelper() *CaptchaHelper {
	// 验证码有限期内有效，用完即销毁（Verify 的 clear=true）
	store := base64Captcha.NewMemoryStore(1024, captchaTTL)
	// 图形由 ReadableDriver 自绘（字号/配色可控），不再用库里 RandomColor 那套随机器，
	// 避免出现浅色字压浅色底导致"看不出来"。
	driver := NewReadableDriver(app.ConfigYml.GetInt("captcha.length"))
	return &CaptchaHelper{
		driver: base64Captcha.NewCaptcha(driver, store),
		store:  store,
	}
}

// GetVerifyImgString 获取验证码图片字符串
func (ch *CaptchaHelper) GetVerifyImgString() (idKeyC string, base64stringC string, err error) {
	idKeyC, base64stringC, _, err = ch.driver.Generate()
	return
}

// VerifyVerifyImgString 验证验证码图片字符串
func (ch *CaptchaHelper) VerifyVerifyImgString(idKeyC string, verifyValueC string) bool {
	return ch.store.Verify(idKeyC, verifyValueC, true)
}
