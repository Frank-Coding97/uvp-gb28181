package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

const publicVideoShareBodyLimit int64 = 4 << 10

// Both anonymous admission routes share process-wide bounds. Media transfer and
// viewer status must not use this middleware.
var publicVideoShareAdmission = newPublicVideoShareAdmission(20, 40, 8)

func PublicVideoShareAdmission() gin.HandlerFunc {
	return publicVideoShareAdmission
}

func newPublicVideoShareAdmission(rps rate.Limit, burst, maxInFlight int) gin.HandlerFunc {
	limiter := rate.NewLimiter(rps, burst)
	inFlight := make(chan struct{}, maxInFlight)
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if c.Request.ContentLength > publicVideoShareBodyLimit {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"message": "请求内容过大"})
			return
		}
		select {
		case inFlight <- struct{}{}:
			defer func() { <-inFlight }()
		default:
			c.Header("Retry-After", "1")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"message": "请求过于频繁，请稍后重试"})
			return
		}
		if !limiter.Allow() {
			c.Header("Retry-After", "1")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"message": "请求过于频繁，请稍后重试"})
			return
		}
		// The JSON handler maps *http.MaxBytesError to 413, including chunked
		// requests. Wrapping alone does not read or retain any bearer credential.
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, publicVideoShareBodyLimit)
		}
		c.Next()
	}
}
