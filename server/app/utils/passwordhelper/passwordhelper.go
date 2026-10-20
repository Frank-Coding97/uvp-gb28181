package passwordhelper

import (
	"errors"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// HashPassword 密码加密
func HashPassword(password string) (string, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashedPassword), nil
}

// ComparePassword 比较密码
func ComparePassword(hashedPassword, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
}

// dummyHash 是 CompareDummy 用的固定哈希：随机串 + 默认 cost，只生成一次。
var dummyHash = sync.OnceValue(func() []byte {
	hash, err := bcrypt.GenerateFromPassword([]byte("uvp-login-timing-equalizer"), bcrypt.DefaultCost)
	if err != nil {
		return nil
	}
	return hash
})

// CompareDummy 在没有真实口令哈希可比时，执行一次**等价耗时**的 bcrypt 比较。
//
// 为什么要有它：账号不存在时若直接返回，bcrypt 的 ~60ms 与"立刻返回"的 <1ms 之间
// 形成可测量的时间差 —— 攻击者不需要看任何提示语，只测响应时间就能枚举用户名。
// 这里用固定随机串的哈希走一遍同样的流程，结果永远不相等，也不参与任何判断。
//
// ⛔ 不要把它当成真实校验，也不要拿它的返回值做逻辑分支。
func CompareDummy(password string) {
	if hash := dummyHash(); hash != nil {
		_ = bcrypt.CompareHashAndPassword(hash, []byte(password))
	}
}

// ValidatePassword 验证密码强度
func ValidatePassword(password string) error {
	if len(password) < 6 {
		return errors.New("password length must be at least 6 characters")
	}
	return nil
}
